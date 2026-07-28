package huggingface

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"depsilo/internal/cache"
)

// wrapMultipartPartialResponseBody validates multipart/byteranges while the
// original wire representation is streamed unchanged to the caller. Header
// validation has already run in expectedPartialBodySize; repeating the small
// parse here keeps the body validator self-contained and fail-closed.
func wrapMultipartPartialResponseBody(
	response *http.Response,
	headers http.Header,
	requestRangeValues []string,
) error {
	if response == nil ||
		response.StatusCode != http.StatusPartialContent ||
		strings.TrimSpace(headers.Get("Content-Range")) != "" {
		return nil
	}
	if response.Request != nil && response.Request.Method == http.MethodHead {
		return nil
	}
	if response.Body == nil || response.Body == http.NoBody {
		return multipartBodyMismatch(errors.New("multipart response has no body"))
	}

	requested, ok := parseRequestedByteRanges(requestRangeValues)
	if !ok || len(requested) < 2 {
		return multipartBodyMismatch(errors.New("multipart response has no valid multi-range request"))
	}
	contentTypeValues := headers.Values("Content-Type")
	if len(contentTypeValues) != 1 {
		return multipartBodyMismatch(fmt.Errorf(
			"multipart response has %d Content-Type values",
			len(contentTypeValues),
		))
	}
	boundary, validBoundary := multipartByteRangesBoundary(contentTypeValues[0])
	if !validBoundary {
		return multipartBodyMismatch(fmt.Errorf(
			"multipart response has invalid Content-Type %q",
			contentTypeValues[0],
		))
	}
	linkedSize, hasLinkedSize := parseResponseSize(headers.Get("X-Linked-Size"))
	response.Body = newMultipartValidatingBody(
		response.Body,
		boundary,
		requested,
		linkedSize,
		hasLinkedSize,
	)
	return nil
}

func multipartBodyMismatch(cause error) error {
	if cause == nil {
		return cache.ErrBodySizeMismatch
	}
	return fmt.Errorf(
		"%w: invalid multipart/byteranges body: %v",
		cache.ErrBodySizeMismatch,
		cause,
	)
}

type multipartValidatingBody struct {
	source     io.ReadCloser
	writer     *io.PipeWriter
	validation <-chan error

	finishOnce    sync.Once
	validationErr error
	closed        atomic.Bool
	closeOnce     sync.Once
	closeErr      error
}

func newMultipartValidatingBody(
	source io.ReadCloser,
	boundary string,
	requested []requestedByteRange,
	linkedSize int64,
	hasLinkedSize bool,
) io.ReadCloser {
	reader, writer := io.Pipe()
	validation := make(chan error, 1)
	go func() {
		err := validateMultipartPartialBody(
			reader,
			boundary,
			requested,
			linkedSize,
			hasLinkedSize,
		)
		// Never stop the raw stream at the first semantic error. Apart from
		// preserving the original response bytes, draining lets the caller
		// distinguish a corrupt complete document (critical) from a transport
		// failure that merely made MIME parsing fail (transient).
		_, drainErr := io.Copy(io.Discard, reader)
		if err == nil {
			err = drainErr
		}
		_ = reader.Close()
		validation <- err
	}()
	return &multipartValidatingBody{
		source:     source,
		writer:     writer,
		validation: validation,
	}
}

func (b *multipartValidatingBody) Read(buffer []byte) (int, error) {
	if b.closed.Load() {
		return 0, context.Canceled
	}
	n, sourceErr := b.source.Read(buffer)
	if n > 0 {
		written, validationWriteErr := b.writer.Write(buffer[:n])
		if validationWriteErr != nil {
			validationErr := b.finish(validationWriteErr)
			if b.closed.Load() {
				return written, context.Canceled
			}
			if sourceErr != nil && sourceErr != io.EOF {
				return written, sourceErr
			}
			if errors.Is(validationErr, context.Canceled) ||
				errors.Is(validationWriteErr, context.Canceled) {
				return written, context.Canceled
			}
			if validationErr != nil {
				return written, multipartBodyMismatch(validationErr)
			}
			return written, validationWriteErr
		}
	}
	if b.closed.Load() {
		return n, context.Canceled
	}
	if sourceErr == nil {
		return n, nil
	}

	validationErr := b.finish(sourceErr)
	if sourceErr != io.EOF {
		// A transport or cancellation error can make an otherwise valid MIME
		// document look truncated. Preserve its classification instead of
		// blaming multipart syntax on the upstream representation.
		return n, sourceErr
	}
	if errors.Is(validationErr, context.Canceled) {
		return n, context.Canceled
	}
	if validationErr != nil {
		return n, multipartBodyMismatch(validationErr)
	}
	return n, io.EOF
}

func (b *multipartValidatingBody) finish(cause error) error {
	b.finishOnce.Do(func() {
		_ = b.writer.CloseWithError(cause)
		b.validationErr = <-b.validation
	})
	return b.validationErr
}

func (b *multipartValidatingBody) Close() error {
	b.closeOnce.Do(func() {
		// Closing early is a downstream decision, not evidence of corrupt
		// upstream bytes. It still must unblock both sides of the validation
		// pipe so the parser goroutine cannot leak.
		b.closed.Store(true)
		_ = b.finish(context.Canceled)
		b.closeErr = b.source.Close()
	})
	return b.closeErr
}

func validateMultipartPartialBody(
	body io.Reader,
	boundary string,
	requested []requestedByteRange,
	linkedSize int64,
	hasLinkedSize bool,
) error {
	reader := multipart.NewReader(body, boundary)
	partCount := 0
	var responseTotal int64
	var hasResponseTotal bool

	for {
		part, err := reader.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read MIME part: %w", err)
		}
		partCount++
		if partCount > maxRequestedByteRanges {
			return fmt.Errorf(
				"multipart response has more than %d parts",
				maxRequestedByteRanges,
			)
		}
		if values := part.Header.Values("Content-Transfer-Encoding"); len(values) > 0 {
			if len(values) != 1 || !identityMultipartTransferEncoding(values[0]) {
				return fmt.Errorf(
					"part %d has unsupported Content-Transfer-Encoding",
					partCount,
				)
			}
		}

		contentRangeValues := part.Header.Values("Content-Range")
		if len(contentRangeValues) != 1 {
			return fmt.Errorf(
				"part %d has %d Content-Range values",
				partCount,
				len(contentRangeValues),
			)
		}
		content, ok := parseContentRange(contentRangeValues[0])
		if !ok {
			return fmt.Errorf(
				"part %d has invalid Content-Range %q",
				partCount,
				contentRangeValues[0],
			)
		}
		if content.hasTotal {
			switch {
			case hasLinkedSize && content.total != linkedSize:
				return fmt.Errorf(
					"part %d total is %d but Hub X-Linked-Size is %d",
					partCount,
					content.total,
					linkedSize,
				)
			case hasResponseTotal && content.total != responseTotal:
				return fmt.Errorf(
					"part %d total is %d but another part declared %d",
					partCount,
					content.total,
					responseTotal,
				)
			}
			responseTotal = content.total
			hasResponseTotal = true
		}
		if !requestedRangesMatchContentRange(
			requested,
			content,
			linkedSize,
			hasLinkedSize,
		) {
			return fmt.Errorf(
				"part %d Content-Range %q is outside the request",
				partCount,
				contentRangeValues[0],
			)
		}
		if contentLengthValues := part.Header.Values("Content-Length"); len(contentLengthValues) > 0 {
			if len(contentLengthValues) != 1 {
				return fmt.Errorf(
					"part %d has %d Content-Length values",
					partCount,
					len(contentLengthValues),
				)
			}
			contentLength, ok := parseResponseSize(strings.TrimSpace(contentLengthValues[0]))
			if !ok || contentLength != content.size() {
				return fmt.Errorf(
					"part %d Content-Length %q disagrees with its %d-byte range",
					partCount,
					contentLengthValues[0],
					content.size(),
				)
			}
		}

		partSize, err := io.Copy(io.Discard, part)
		if err != nil {
			return fmt.Errorf("read part %d body: %w", partCount, err)
		}
		if partSize != content.size() {
			return fmt.Errorf(
				"part %d Content-Range declares %d bytes but contains %d",
				partCount,
				content.size(),
				partSize,
			)
		}
	}
	if partCount == 0 {
		return errors.New("multipart response contains no parts")
	}
	return nil
}

func identityMultipartTransferEncoding(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "7bit", "8bit", "binary":
		return true
	default:
		return false
	}
}
