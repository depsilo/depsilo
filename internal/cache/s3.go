package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Storage implements the Storage interface using an S3-compatible backend.
type S3Storage struct {
	client *s3.Client
	bucket string
}

// NewS3Storage creates a new S3Storage instance. It configures the client for
// MinIO compatibility via a custom endpoint and creates the bucket if it does
// not already exist.
func NewS3Storage(endpoint, bucket, region, accessKey, secretKey string) (*S3Storage, error) {
	if bucket == "" {
		return nil, fmt.Errorf("s3: bucket name must not be empty")
	}

	client := s3.New(s3.Options{
		Region: region,
		BaseEndpoint: aws.String(endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		UsePathStyle: true, // required for MinIO compatibility
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		var alreadyOwned *types.BucketAlreadyOwnedByYou
		var alreadyExists *types.BucketAlreadyExists
		if !errors.As(err, &alreadyOwned) && !errors.As(err, &alreadyExists) {
			return nil, fmt.Errorf("s3: create bucket %q: %w", bucket, err)
		}
	}

	return &S3Storage{
		client: client,
		bucket: bucket,
	}, nil
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
	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        r,
		ContentType: aws.String(contentType),
	}
	if size >= 0 {
		input.ContentLength = aws.Int64(size)
	}

	_, err := s.client.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("s3: put object %q: %w", key, err)
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
