package adapter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBindAuthenticatedArtifactCoordinatePublishesGinAndRequestContexts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/npm/fixture/-/opaque.tgz", nil)

	BindAuthenticatedArtifactCoordinate(c, "npm", "fixture", "1.0.0")

	want := AuthenticatedArtifactCoordinate{
		Ecosystem: "npm", PackageName: "fixture", Version: "1.0.0",
	}
	if got, ok := GetAuthenticatedArtifactCoordinate(c); !ok || got != want {
		t.Fatalf("Gin coordinate = (%#v, %v), want (%#v, true)", got, ok, want)
	}
	if got, ok := AuthenticatedArtifactCoordinateFromContext(c.Request.Context()); !ok || got != want {
		t.Fatalf("request coordinate = (%#v, %v), want (%#v, true)", got, ok, want)
	}
}

func TestGetAuthenticatedArtifactCoordinateFallsBackToRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	source, _ := gin.CreateTestContext(httptest.NewRecorder())
	source.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	BindAuthenticatedArtifactCoordinate(source, "npm", "fixture", "1.0.0")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = source.Request
	got, ok := GetAuthenticatedArtifactCoordinate(c)
	if !ok || got.PackageName != "fixture" || got.Version != "1.0.0" {
		t.Fatalf("fallback coordinate = (%#v, %v)", got, ok)
	}
}

func TestBindAuthenticatedArtifactCoordinateRejectsIncompleteIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	BindAuthenticatedArtifactCoordinate(c, "npm", "fixture", "")
	if got, ok := GetAuthenticatedArtifactCoordinate(c); ok {
		t.Fatalf("incomplete coordinate was published: %#v", got)
	}
}
