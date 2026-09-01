package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

const (
	s3ReadinessMarkerKey = "__depsilo_internal__/readiness-v1"
	s3MultipartPartSize  = 8 * 1024 * 1024
	s3MaxUploadParts     = 10_000
)

// S3Storage implements the Storage interface using an S3-compatible backend.
type S3Storage struct {
	client *s3.Client
	bucket string
}

// NewS3Storage creates a new S3Storage instance. It configures the client for
// MinIO compatibility via a custom endpoint and verifies object access with a
// stable readiness marker.
func NewS3Storage(endpoint, bucket, region, accessKey, secretKey string) (*S3Storage, error) {
	if bucket == "" {
		return nil, fmt.Errorf("s3: bucket name must not be empty")
	}

	client := s3.New(s3.Options{
		Region:       region,
		BaseEndpoint: aws.String(endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		UsePathStyle: true, // required for MinIO compatibility
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	storage := &S3Storage{
		client: client,
		bucket: bucket,
	}
	// A stable zero-byte marker lets readiness exercise GetObject without
	// requiring ListBucket. It is managed storage metadata rather than a
	// per-probe temporary object, so readiness remains read-only and cheap.
	if err := storage.writeReadinessMarker(ctx); err != nil {
		if !hasS3ErrorCode(err, "NoSuchBucket") {
			return nil, fmt.Errorf("s3: initialize readiness marker: %w", err)
		}
		if err := storage.createBucket(ctx, region); err != nil {
			return nil, fmt.Errorf("s3: create bucket %q: %w", bucket, err)
		}
		if err := storage.writeReadinessMarker(ctx); err != nil {
			return nil, fmt.Errorf("s3: initialize readiness marker after creating bucket: %w", err)
		}
	}
	return storage, nil
}

func (s *S3Storage) createBucket(ctx context.Context, region string) error {
	input := &s3.CreateBucketInput{Bucket: aws.String(s.bucket)}
	if region != "" && region != "us-east-1" {
		input.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		}
	}
	_, err := s.client.CreateBucket(ctx, input)
	if err == nil || hasS3ErrorCode(err, "BucketAlreadyOwnedByYou") || hasS3ErrorCode(err, "BucketAlreadyExists") {
		return nil
	}
	return err
}

func hasS3ErrorCode(err error, code string) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && strings.EqualFold(strings.TrimSpace(apiErr.ErrorCode()), code)
}

func (s *S3Storage) writeReadinessMarker(ctx context.Context) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(s3ReadinessMarkerKey),
		Body:          bytes.NewReader(nil),
		ContentLength: aws.Int64(0),
		ContentType:   aws.String("application/octet-stream"),
	})
	return err
}

// CheckReady exercises the cache-hit GetObject path against the marker
// created during storage initialization. In particular, it never calls
// ListObjectsV2, so an otherwise sufficient object policy does not need the
// broader ListBucket permission merely to satisfy /ready.
func (s *S3Storage) CheckReady(ctx context.Context) (err error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s3ReadinessMarkerKey),
	})
	if err != nil {
		return fmt.Errorf("s3: read readiness marker: %w", err)
	}
	defer func() { err = errors.Join(err, output.Body.Close()) }()
	if _, err := io.Copy(io.Discard, output.Body); err != nil {
		return fmt.Errorf("s3: read readiness marker body: %w", err)
	}
	return nil
}

func (s *S3Storage) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var notFound *types.NotFound
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &notFound) || errors.As(err, &noSuchKey) {
			return false, nil
		}
		return false, fmt.Errorf("s3: head object %q: %w", key, err)
	}
	return true, nil
}

func (s *S3Storage) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return nil, 0, fmt.Errorf("s3: object %q not found", key)
		}
		return nil, 0, fmt.Errorf("s3: get object %q: %w", key, err)
	}

	size := int64(0)
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	return out.Body, size, nil
}

func (s *S3Storage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	var err error
	if size >= 0 {
		if _, seekable := r.(io.Seeker); seekable {
			err = s.putObject(ctx, key, r, size, contentType)
		} else {
			err = s.putStream(ctx, key, r, &size, contentType)
		}
	} else {
		err = s.putStream(ctx, key, r, nil, contentType)
	}
	if err != nil {
		return fmt.Errorf("s3: put object %q: %w", key, err)
	}
	return nil
}

func (s *S3Storage) putObject(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	input := &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          r,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
	}

	_, err := s.client.PutObject(ctx, input)
	return err
}

// putStream preserves a bounded single pass for readers that cannot be
// rewound. Small streams are buffered once and signed as a normal PutObject;
// larger streams use signed multipart requests with one part in memory. A
// non-nil expectedSize additionally makes truncated or overlong producers fail
// instead of publishing bytes that disagree with cache metadata.
func (s *S3Storage) putStream(ctx context.Context, key string, r io.Reader, expectedSize *int64, contentType string) (err error) {
	if expectedSize != nil && *expectedSize <= s3MultipartPartSize {
		buffer := make([]byte, int(*expectedSize))
		n, readErr := io.ReadFull(r, buffer)
		if readErr != nil {
			return fmt.Errorf("read known-size upload stream: got %d of %d bytes: %w", n, *expectedSize, readErr)
		}
		if err := requireUploadEOF(r); err != nil {
			return err
		}
		return s.putObject(ctx, key, bytes.NewReader(buffer), *expectedSize, contentType)
	}

	buffer := make([]byte, s3MultipartPartSize)
	n, readErr := io.ReadFull(r, buffer)
	if expectedSize == nil {
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			return s.putObject(ctx, key, bytes.NewReader(buffer[:n]), int64(n), contentType)
		}
		if readErr != nil {
			return fmt.Errorf("read upload stream: %w", readErr)
		}
	} else {
		if readErr != nil {
			return fmt.Errorf("read known-size upload stream: got %d of %d bytes: %w", n, *expectedSize, readErr)
		}
		partCount := ((*expectedSize - 1) / s3MultipartPartSize) + 1
		if partCount > s3MaxUploadParts {
			return fmt.Errorf("multipart upload needs %d parts, maximum is %d", partCount, s3MaxUploadParts)
		}
	}

	created, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("create multipart upload: %w", err)
	}
	if created.UploadId == nil || *created.UploadId == "" {
		return errors.New("create multipart upload returned an empty upload ID")
	}
	uploadID := created.UploadId
	completed := false
	defer func() {
		if completed {
			return
		}
		abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_, abortErr := s.client.AbortMultipartUpload(abortCtx, &s3.AbortMultipartUploadInput{
			Bucket:   aws.String(s.bucket),
			Key:      aws.String(key),
			UploadId: uploadID,
		})
		if abortErr != nil {
			err = errors.Join(err, fmt.Errorf("abort multipart upload: %w", abortErr))
		}
	}()

	partCapacity := 8
	remaining := int64(-1)
	if expectedSize != nil {
		remaining = *expectedSize
		partCapacity = int((remaining-1)/s3MultipartPartSize + 1)
	}
	parts := make([]types.CompletedPart, 0, partCapacity)
	for partNumber := int32(1); ; partNumber++ {
		if partNumber > s3MaxUploadParts {
			return fmt.Errorf("multipart upload exceeds %d parts", s3MaxUploadParts)
		}
		part, uploadErr := s.client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket:        aws.String(s.bucket),
			Key:           aws.String(key),
			UploadId:      uploadID,
			PartNumber:    aws.Int32(partNumber),
			Body:          bytes.NewReader(buffer[:n]),
			ContentLength: aws.Int64(int64(n)),
		})
		if uploadErr != nil {
			return fmt.Errorf("upload part %d: %w", partNumber, uploadErr)
		}
		parts = append(parts, types.CompletedPart{ETag: part.ETag, PartNumber: aws.Int32(partNumber)})

		if expectedSize != nil {
			remaining -= int64(n)
			if remaining == 0 {
				if err := requireUploadEOF(r); err != nil {
					return err
				}
				break
			}
			nextSize := min(remaining, int64(len(buffer)))
			n, readErr = io.ReadFull(r, buffer[:int(nextSize)])
			if readErr != nil {
				return fmt.Errorf("read known-size upload stream with %d bytes remaining: %w", remaining, readErr)
			}
			continue
		}

		n, readErr = io.ReadFull(r, buffer)
		if readErr == nil {
			continue
		}
		if readErr == io.EOF {
			break
		}
		if readErr == io.ErrUnexpectedEOF {
			// The final short part is uploaded on the next loop iteration.
			continue
		}
		return fmt.Errorf("read upload stream: %w", readErr)
	}

	_, err = s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(s.bucket),
		Key:      aws.String(key),
		UploadId: uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: parts,
		},
	})
	if err != nil {
		return fmt.Errorf("complete multipart upload: %w", err)
	}
	completed = true
	return nil
}

func requireUploadEOF(r io.Reader) error {
	var extra [1]byte
	n, err := io.ReadFull(r, extra[:])
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read upload stream after declared size: %w", err)
	}
	if n != 0 {
		return errors.New("upload stream contains more bytes than its declared size")
	}
	return nil
}

func (s *S3Storage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("s3: delete object %q: %w", key, err)
	}
	return nil
}

func (s *S3Storage) Stat(ctx context.Context, key string) (*ObjectMeta, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3: head object %q: %w", key, err)
	}

	meta := &ObjectMeta{
		Key: key,
	}
	if out.ContentLength != nil {
		meta.Size = *out.ContentLength
	}
	if out.ContentType != nil {
		meta.ContentType = *out.ContentType
	}
	if out.LastModified != nil {
		meta.LastModified = *out.LastModified
	}
	return meta, nil
}

func (s *S3Storage) List(ctx context.Context, prefix string) ([]ObjectMeta, error) {
	var result []ObjectMeta

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3: list objects with prefix %q: %w", prefix, err)
		}
		for _, obj := range page.Contents {
			meta := ObjectMeta{}
			if obj.Key != nil {
				meta.Key = *obj.Key
			}
			if obj.Size != nil {
				meta.Size = *obj.Size
			}
			if obj.LastModified != nil {
				meta.LastModified = *obj.LastModified
			}
			result = append(result, meta)
		}
	}

	return result, nil
}

func (s *S3Storage) TotalSize(ctx context.Context) (int64, error) {
	var total int64

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return 0, fmt.Errorf("s3: list objects for total size: %w", err)
		}
		for _, obj := range page.Contents {
			if obj.Size != nil {
				total += *obj.Size
			}
		}
	}

	return total, nil
}

var _ ReadinessProber = (*S3Storage)(nil)
