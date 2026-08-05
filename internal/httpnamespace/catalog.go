// Package httpnamespace owns the stable top-level HTTP namespaces exposed by
// Depsilo. It keeps frontend fallback protection and configurable proxy-path
// validation aligned as the server grows new machine endpoints.
package httpnamespace

import "depsilo/internal/ecosystem"

var registeredMachineRoots = [...]string{
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
}

var fallbackOnlyMachineRoots = [...]string{
	// Invalid aliases used by older or incorrectly generated client
	// configuration. They remain machine namespaces so failures return a
	// protocol 404 instead of the Portal document.
	"/cargo",
	"/docker",
	"/pip",
	"/gem",

	// Conventional probe names remain machine namespaces even when this build
	// does not register them.
	"/healthz",
	"/readyz",
	"/livez",
	"/metric",
	"/metricsz",

	// Legacy invalid Debian-security route. Keeping it out of the SPA prevents
	// APT from parsing index.html as repository metadata.
	"/apt-security",
}

var uiAndStaticRoots = [...]string{
	"/admin",
	"/monitor",
	"/favicon.svg",
	"/icons.svg",
	"/index.html",
}

// MachineRoots returns the fixed and ecosystem protocol namespaces that must
// never fall through to the browser application. The caller owns the result.
func MachineRoots() []string {
	roots := registeredRoots()
	roots = append(roots, fallbackOnlyMachineRoots[:]...)
	return roots
}

func registeredRoots() []string {
	definitions := ecosystem.All()
	roots := make([]string, 0, len(registeredMachineRoots)+len(definitions))
	roots = append(roots, registeredMachineRoots[:]...)
	for _, definition := range definitions {
		roots = append(roots, definition.Route)
	}
	return roots
}

// ExtraIndexReservedRoots returns every top-level namespace that a configured
// extra index must not claim. The caller owns the result.
func ExtraIndexReservedRoots() []string {
	registered := registeredRoots()
	roots := make([]string, 0, len(registered)+len(uiAndStaticRoots))
	roots = append(roots, registered...)
	roots = append(roots, uiAndStaticRoots[:]...)
	return roots
}
