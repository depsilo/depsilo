package logging

import "go.uber.org/zap"

// NewProduction builds a production logger and returns the atomic level
// embedded in its core so runtime configuration can update that same logger.
func NewProduction() (*zap.Logger, zap.AtomicLevel, error) {
	level := zap.NewAtomicLevelAt(zap.InfoLevel)
	cfg := zap.NewProductionConfig()
	cfg.Level = level
	logger, err := cfg.Build()
	return logger, level, err
}
