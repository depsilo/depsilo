package cache

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

type closeObservedFetchBody struct {
	closed chan struct{}
	once   sync.Once
}

func newCloseObservedFetchBody() *closeObservedFetchBody {
	return &closeObservedFetchBody{closed: make(chan struct{})}
}

func (*closeObservedFetchBody) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (b *closeObservedFetchBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func TestFetchPassthroughClosesBodyReturnedWithError(t *testing.T) {
	manager := NewManager(newMemStorage(), openStreamTestDB(t), NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })
	body := newCloseObservedFetchBody()
	fetchErr := errors.New("upstream returned body and error")

	result, err := manager.fetchPassthrough(t.Context(),
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return body, "", -1, "upstream", fetchErr
		})
	if result != nil || !errors.Is(err, fetchErr) {
		t.Fatalf("passthrough = result:%+v err:%v", result, err)
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("passthrough leaked the body returned alongside an error")
	}
}

func TestBackgroundRefreshClosesBodyReturnedWithError(t *testing.T) {
	manager := NewManager(newMemStorage(), openStreamTestDB(t), NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })
	body := newCloseObservedFetchBody()
	fetchErr := errors.New("upstream returned body and error")

	manager.backgroundRefresh(
		t.Context(),
		"pypi/files/body-error.whl",
		"pypi",
		time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return body, "", -1, "upstream", fetchErr
		},
	)
	select {
	case <-body.closed:
	default:
		t.Fatal("background refresh leaked the body returned alongside an error")
	}
}
