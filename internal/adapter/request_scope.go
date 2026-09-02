package adapter

import (
	"context"
	"net/http"

	"depsilo/internal/accesslog"
)

// requestScopeContextKey is deliberately private: only this package may attach
// or interpret adapter dependency snapshots on a request.
type requestScopeContextKey struct{}

// RequestScope is an immutable snapshot of the adapter hooks owned by one
// server instance. A field may intentionally be nil; once attached to a
// request, that nil is authoritative and must not fall back to process globals.
type RequestScope struct {
	access       accessHookSnapshot
	checker      QuarantineChecker
	packageRules PackageRuleChecker
	observer     RequestObserver
}

// NewRequestScope captures one server owner's adapter dependencies.
func NewRequestScope(
	recorder accesslog.Recorder,
	audit AuditLogger,
	checker QuarantineChecker,
	packageRules PackageRuleChecker,
	observers ...RequestObserver,
) *RequestScope {
	scope := &RequestScope{
		access: accessHookSnapshot{
			recorder: recorder,
			audit:    audit,
		},
		checker:      checker,
		packageRules: packageRules,
	}
	if len(observers) > 0 {
		scope.observer = observers[0]
	}
	return scope
}

// Wrap attaches a fixed copy of scope to every request entering next. Copying
// here prevents later package-internal mutation of the constructor result from
// changing requests handled by this wrapper.
func (scope *RequestScope) Wrap(next http.Handler) http.Handler {
	frozen := &RequestScope{}
	if scope != nil {
		*frozen = *scope
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ctx := context.WithValue(request.Context(), requestScopeContextKey{}, frozen)
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

func requestScopeFromContext(ctx context.Context) (*RequestScope, bool) {
	if ctx == nil {
		return nil, false
	}
	scope, ok := ctx.Value(requestScopeContextKey{}).(*RequestScope)
	return scope, ok && scope != nil
}
