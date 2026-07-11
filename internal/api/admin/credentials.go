package admin

import (
	"errors"
	"net/url"

	"github.com/gin-gonic/gin"

	"depsilo/internal/middleware"
)

func principalCanViewCredentials(c *gin.Context) bool {
	principal, ok := middleware.PrincipalFromContext(c)
	return ok && principal.CanWrite
}

func maskWebhookURL(raw string) string {
	parsed, err := parseCredentialURL(raw)
	if err != nil {
		return "***"
	}
	return parsed.Scheme + "://" + parsed.Host + "/***"
}

func maskURLUserInfo(raw string) string {
	if raw == "" {
		return raw
	}
	parsed, err := parseCredentialURL(raw)
	if err != nil {
		return "***"
	}
	if parsed.User == nil {
		return raw
	}
	parsed.User = url.UserPassword("***", "***")
	return parsed.String()
}

func parseCredentialURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("missing URL scheme or host")
	}
	return parsed, nil
}
