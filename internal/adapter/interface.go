package adapter

import "github.com/gin-gonic/gin"

// Adapter defines the interface for a package source adapter.
type Adapter interface {
	// Register mounts the adapter's routes onto the given router group.
	Register(rg *gin.RouterGroup)
	// Type returns the adapter type identifier (e.g. "pypi", "apt").
	Type() string
}
