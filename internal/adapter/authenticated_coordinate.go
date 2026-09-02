package adapter

import (
	"context"

	"github.com/gin-gonic/gin"
)

const authenticatedArtifactCoordinateKey = "depsilo.adapter.authenticated-artifact-coordinate"

type authenticatedArtifactCoordinateContextKey struct{}

// AuthenticatedArtifactCoordinate is an adapter-proven package/version pair
// whose authority is stronger than filename or request-path inference.
type AuthenticatedArtifactCoordinate struct {
	Ecosystem   string
	PackageName string
	Version     string
}

// BindAuthenticatedArtifactCoordinate lets downstream/post-response
// middleware consume identity established inside an adapter. The context key
// remains private so an HTTP header or route parameter cannot forge it.
func BindAuthenticatedArtifactCoordinate(
	c *gin.Context,
	ecosystem, packageName, version string,
) {
	if c == nil || ecosystem == "" || packageName == "" || version == "" {
		return
	}
	coordinate := AuthenticatedArtifactCoordinate{
		Ecosystem: ecosystem, PackageName: packageName, Version: version,
	}
	c.Set(authenticatedArtifactCoordinateKey, coordinate)
	c.Request = c.Request.WithContext(context.WithValue(
		c.Request.Context(),
		authenticatedArtifactCoordinateContextKey{},
		coordinate,
	))
}

// GetAuthenticatedArtifactCoordinate returns identity previously established
// by a trusted adapter in this request.
func GetAuthenticatedArtifactCoordinate(c *gin.Context) (AuthenticatedArtifactCoordinate, bool) {
	if c == nil {
		return AuthenticatedArtifactCoordinate{}, false
	}
	value, exists := c.Get(authenticatedArtifactCoordinateKey)
	if exists {
		coordinate, ok := value.(AuthenticatedArtifactCoordinate)
		if ok && validAuthenticatedArtifactCoordinate(coordinate) {
			return coordinate, true
		}
	}
	return AuthenticatedArtifactCoordinateFromContext(c.Request.Context())
}

// AuthenticatedArtifactCoordinateFromContext exposes the same trusted
// coordinate to lower-level logging and observation code that receives only a
// standard request context.
func AuthenticatedArtifactCoordinateFromContext(ctx context.Context) (AuthenticatedArtifactCoordinate, bool) {
	if ctx == nil {
		return AuthenticatedArtifactCoordinate{}, false
	}
	coordinate, ok := ctx.Value(authenticatedArtifactCoordinateContextKey{}).(AuthenticatedArtifactCoordinate)
	return coordinate, ok && validAuthenticatedArtifactCoordinate(coordinate)
}

func validAuthenticatedArtifactCoordinate(coordinate AuthenticatedArtifactCoordinate) bool {
	return coordinate.Ecosystem != "" && coordinate.PackageName != "" && coordinate.Version != ""
}
