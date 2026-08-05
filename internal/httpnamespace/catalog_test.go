package httpnamespace

import (
	"strings"
	"testing"

	"depsilo/internal/ecosystem"
)

func TestMachineRootsContainInfrastructureAliasesAndEcosystems(t *testing.T) {
	roots := MachineRoots()
	for _, route := range []string{
		"/api",
		"/mcp",
		"/health",
		"/live",
		"/ready",
		"/metrics",
		"/ccache",
		"/sccache",
		"/p",
		"/assets",
		"/cargo",
		"/docker",
		"/pip",
		"/gem",
		"/healthz",
		"/readyz",
		"/livez",
		"/metric",
		"/metricsz",
		"/apt-security",
	} {
		assertContainsRoute(t, roots, route)
	}
	for _, definition := range ecosystem.All() {
		assertContainsRoute(t, roots, definition.Route)
	}
}

func TestExtraIndexReservedRootsContainOccupiedNamespaces(t *testing.T) {
	reserved := ExtraIndexReservedRoots()
	for _, route := range []string{
		"/api",
		"/mcp",
		"/health",
		"/live",
		"/ready",
		"/metrics",
		"/ccache",
		"/sccache",
		"/p",
		"/assets",
		"/admin",
		"/monitor",
		"/favicon.svg",
		"/icons.svg",
		"/index.html",
	} {
		assertContainsRoute(t, reserved, route)
	}
	for _, definition := range ecosystem.All() {
		assertContainsRoute(t, reserved, definition.Route)
	}
}

func TestFallbackOnlyMachineRootsRemainAvailableToExtraIndexes(t *testing.T) {
	reserved := ExtraIndexReservedRoots()
	for _, route := range fallbackOnlyMachineRoots {
		if containsRoute(reserved, route) {
			t.Errorf("fallback-only route %q unexpectedly reserves an extra-index path", route)
		}
	}
}

func TestCatalogRootsAreCanonicalAndUnique(t *testing.T) {
	for name, routes := range map[string][]string{
		"machine":     MachineRoots(),
		"extra index": ExtraIndexReservedRoots(),
	} {
		t.Run(name, func(t *testing.T) {
			seen := make(map[string]struct{}, len(routes))
			for _, route := range routes {
				if !strings.HasPrefix(route, "/") ||
					route == "/" ||
					strings.HasSuffix(route, "/") ||
					strings.Count(route, "/") != 1 {
					t.Errorf("route %q is not a canonical top-level root", route)
				}
				key := strings.ToLower(route)
				if _, duplicate := seen[key]; duplicate {
					t.Errorf("route %q is duplicated case-insensitively", route)
				}
				seen[key] = struct{}{}
			}
		})
	}
}

func TestCatalogResultsAreCallerOwned(t *testing.T) {
	machine := MachineRoots()
	reserved := ExtraIndexReservedRoots()
	machine[0] = "/changed"
	reserved[0] = "/changed-again"

	if MachineRoots()[0] == "/changed" {
		t.Fatal("MachineRoots exposed mutable package storage")
	}
	if ExtraIndexReservedRoots()[0] == "/changed-again" {
		t.Fatal("ExtraIndexReservedRoots exposed mutable package storage")
	}
}

func assertContainsRoute(t *testing.T, routes []string, want string) {
	t.Helper()
	if containsRoute(routes, want) {
		return
	}
	t.Errorf("routes = %v, want %q", routes, want)
}

func containsRoute(routes []string, want string) bool {
	for _, route := range routes {
		if route == want {
			return true
		}
	}
	return false
}
