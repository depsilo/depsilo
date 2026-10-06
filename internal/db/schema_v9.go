package db

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateDockerObservations adds the first-seen table backing Docker's
// observation-age gate. The gate is opt-in; existing installs start with no
// observations, so the first pull after enabling it starts the clock.
func migrateDockerObservations(database *gorm.DB) error {
	if err := database.AutoMigrate(&DockerImageObservation{}); err != nil {
		return fmt.Errorf("create docker observation table: %w", err)
	}
	return nil
}
