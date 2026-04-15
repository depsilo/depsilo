package unit

import (
	"testing"

	"depsilo/internal/adapter/docker"
	"depsilo/internal/config"
)

func newTestResolver() *docker.Resolver {
	cfg := config.DockerConfig{
		DefaultRegistry: "dockerhub",
		Registries: []config.RegistryConfig{
			{Name: "dockerhub", URL: "https://registry-1.docker.io"},
			{Name: "ghcr", URL: "https://ghcr.io"},
		},
	}
	return docker.NewResolver(cfg)
}

func TestResolver_DefaultRegistry(t *testing.T) {
	r := newTestResolver()
	reg, imageName, endpoint := r.Resolve("library/nginx/manifests/latest")
	if reg == nil || reg.Name != "dockerhub" {
		t.Fatalf("expected dockerhub, got %v", reg)
	}
	if imageName != "library/nginx" {
		t.Errorf("imageName = %q, want library/nginx", imageName)
	}
	if endpoint != "manifests/latest" {
		t.Errorf("endpoint = %q, want manifests/latest", endpoint)
	}
}

func TestResolver_DomainRouting(t *testing.T) {
	r := newTestResolver()
	reg, imageName, endpoint := r.Resolve("ghcr.io/owner/repo/manifests/v1.0")
	if reg == nil || reg.Name != "ghcr" {
		t.Fatalf("expected ghcr, got %v", reg)
	}
	if imageName != "owner/repo" {
		t.Errorf("imageName = %q, want owner/repo", imageName)
	}
	if endpoint != "manifests/v1.0" {
		t.Errorf("endpoint = %q, want manifests/v1.0", endpoint)
	}
}

func TestResolver_BlobEndpoint(t *testing.T) {
	r := newTestResolver()
	reg, imageName, endpoint := r.Resolve("myimage/blobs/sha256:abc")
	if reg == nil || reg.Name != "dockerhub" {
		t.Fatalf("expected dockerhub default, got %v", reg)
	}
	if imageName != "myimage" {
		t.Errorf("imageName = %q, want myimage", imageName)
	}
	if endpoint != "blobs/sha256:abc" {
		t.Errorf("endpoint = %q, want blobs/sha256:abc", endpoint)
	}
}

func TestResolver_MultiLevelImageName(t *testing.T) {
	r := newTestResolver()
	reg, imageName, endpoint := r.Resolve("team/service/api/tags/list")
	if reg == nil || reg.Name != "dockerhub" {
		t.Fatalf("expected dockerhub default, got %v", reg)
	}
	if imageName != "team/service/api" {
		t.Errorf("imageName = %q, want team/service/api", imageName)
	}
	if endpoint != "tags/list" {
		t.Errorf("endpoint = %q, want tags/list", endpoint)
	}
}

func TestResolver_DockerHubAlias(t *testing.T) {
	r := newTestResolver()
	reg, imageName, endpoint := r.Resolve("docker.io/library/alpine/manifests/3.18")
	if reg == nil || reg.Name != "dockerhub" {
		t.Fatalf("expected dockerhub via alias, got %v", reg)
	}
	if imageName != "library/alpine" {
		t.Errorf("imageName = %q, want library/alpine", imageName)
	}
	if endpoint != "manifests/3.18" {
		t.Errorf("endpoint = %q, want manifests/3.18", endpoint)
	}
}

func TestResolver_UnregisteredDomain(t *testing.T) {
	r := newTestResolver()
	reg, imageName, endpoint := r.Resolve("unknown.registry.io/img/manifests/v1")
	if reg == nil || reg.Name != "dockerhub" {
		t.Fatalf("expected dockerhub fallback, got %v", reg)
	}
	if imageName != "unknown.registry.io/img" {
		t.Errorf("imageName = %q, want unknown.registry.io/img", imageName)
	}
	if endpoint != "manifests/v1" {
		t.Errorf("endpoint = %q, want manifests/v1", endpoint)
	}
}

func TestResolver_TooShortPath(t *testing.T) {
	r := newTestResolver()
	reg, imageName, endpoint := r.Resolve("short")
	if reg != nil {
		t.Errorf("expected nil registry for short path, got %v", reg)
	}
	if imageName != "" || endpoint != "" {
		t.Errorf("expected empty imageName and endpoint, got %q %q", imageName, endpoint)
	}
}

func TestResolver_NoEndpointKeyword(t *testing.T) {
	r := newTestResolver()
	reg, _, _ := r.Resolve("a/b")
	if reg != nil {
		t.Errorf("expected nil registry for path without endpoint keyword, got %v", reg)
	}
}
