package logging

import (
	"testing"

	"go.uber.org/zap"
)

func TestNewProductionSharesAtomicLevelWithCore(t *testing.T) {
	logger, level, err := NewProduction()
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Sync()

	if logger.Core().Enabled(zap.DebugLevel) {
		t.Fatal("debug unexpectedly enabled")
	}
	level.SetLevel(zap.DebugLevel)
	if !logger.Core().Enabled(zap.DebugLevel) {
		t.Fatal("atomic level did not update logger core")
	}
}
