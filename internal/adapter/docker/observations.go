package docker

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"depsilo/internal/db"
)

// observationStore records the first time this instance saw an image digest.
// The row is created with the first request and never updated: the first
// observation is the trusted truth, even under concurrent pulls.
type observationStore struct {
	db  *gorm.DB
	now func() time.Time
}

func newObservationStore(database *gorm.DB) *observationStore {
	return &observationStore{db: database, now: func() time.Time { return time.Now().UTC() }}
}

// Observe returns the first-seen time for (registry, digest), creating the row
// on first sight.
func (s *observationStore) Observe(ctx context.Context, registry, digest string) (time.Time, error) {
	if s == nil || s.db == nil {
		return time.Time{}, fmt.Errorf("docker observation store is not configured")
	}
	row := db.DockerImageObservation{Registry: registry, Digest: digest, FirstSeenAt: s.now()}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return time.Time{}, fmt.Errorf("record docker observation: %w", err)
	}
	var stored db.DockerImageObservation
	if err := s.db.WithContext(ctx).
		Where("registry = ? AND digest = ?", registry, digest).
		First(&stored).Error; err != nil {
		return time.Time{}, fmt.Errorf("read docker observation: %w", err)
	}
	return stored.FirstSeenAt, nil
}

// dockerObservationSourceID names the exact registry digest whose first
// observation supplied the age.
func dockerObservationSourceID(registry, digest string) string {
	hash := sha256.New()
	for _, value := range []string{
		"depsilo/docker-observation-source/v1",
		registry,
		digest,
	} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
}
