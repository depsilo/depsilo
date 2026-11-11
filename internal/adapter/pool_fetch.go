package adapter

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"go.uber.org/zap"

	"depsilo/internal/upstream"
)

// alternateSelector chooses a pool member that has not yet failed the current
// request.
type alternateSelector interface {
	SelectExcludingAll(context.Context, ...*upstream.Upstream) (*upstream.Upstream, error)
}

// FetchFromPool fetches path-addressed content from first and, while a member
// cannot provide it, from each remaining member of the pool once, in
// selection order. A member cannot provide the content when the exchange
// fails before a response or the member answers 5xx or 429; that is what
// failover after an unhealthy probe already does, one request earlier. With
// retryMissing a 404 also moves on: set it only for immutable content that
// every member mirrors and that clients verify against hashes from metadata,
// because a mirror can lack a file another member's index advertised. It
// returns the member that served the content, or the last member's error.
func FetchFromPool(
	ctx context.Context,
	selector upstream.Selector,
	first *upstream.Upstream,
	retryMissing bool,
	fetch func(*upstream.Upstream) (*upstream.FetchResult, error),
) (*upstream.FetchResult, *upstream.Upstream, error) {
	result, err := fetch(first)
	alternates, ok := selector.(alternateSelector)
	if !ok {
		return result, first, err
	}
	tried := []*upstream.Upstream{first}
	for err != nil && ctx.Err() == nil && memberCannotProvide(err, retryMissing) {
		next, selectErr := alternates.SelectExcludingAll(ctx, tried...)
		if selectErr != nil || next == nil || slices.Contains(tried, next) {
			break
		}
		zap.L().Info("upstream could not provide the content; asking another pool member",
			zap.String("failed_on", tried[len(tried)-1].Name),
			zap.String("upstream", next.Name),
			zap.Error(err),
		)
		tried = append(tried, next)
		result, err = fetch(next)
	}
	return result, tried[len(tried)-1], err
}

func memberCannotProvide(err error, retryMissing bool) bool {
	var response *upstream.ResponseError
	if !errors.As(err, &response) {
		// No response: connection, TLS or response-header failure.
		return true
	}
	switch {
	case response.Code == http.StatusNotFound:
		return retryMissing
	case response.Code == http.StatusTooManyRequests, response.Code >= http.StatusInternalServerError:
		return true
	default:
		return false
	}
}
