package db

import "time"

// DockerImageObservation records the first time this instance saw an image
// digest. It backs the observation-age gate for registries that expose no
// publish-time authority: the age is measured from when Depsilo first pulled
// the content, which the publisher cannot forge.
type DockerImageObservation struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	Registry    string    `gorm:"size:128;uniqueIndex:idx_docker_observation" json:"registry"`
	Digest      string    `gorm:"size:128;uniqueIndex:idx_docker_observation" json:"digest"`
	FirstSeenAt time.Time `json:"first_seen_at"`
}

func (DockerImageObservation) TableName() string { return "docker_image_observations" }
