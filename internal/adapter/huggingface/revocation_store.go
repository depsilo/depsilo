package huggingface

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"depsilo/internal/db"
)

const maxPersistedRepositoryRevocations = maxTrackedRepositoryRevocations

var (
	errRepositoryRevocationStoreUnavailable = errors.New(
		"Hugging Face repository revocation store is unavailable",
	)
	errRepositoryRevocationStoreCapacity = errors.New(
		"Hugging Face repository revocation store capacity exceeded",
	)
	errRepositoryRevocationMarkerInvalid = errors.New(
		"invalid Hugging Face repository revocation marker",
	)
	errRepositoryRevocationMarkerCorrupt = errors.New(
		"corrupt Hugging Face repository revocation marker",
	)
	errRepositoryRevocationTokenUnavailable = errors.New(
		"Hugging Face repository revocation token generation failed",
	)
)

// repositoryRevocationMarker is the persistence-neutral representation used
// to rebuild the in-memory fail-closed gate during startup.
type repositoryRevocationMarker struct {
	Repository  string
	EscapedRepo string
	Token       string
	CleanupSafe bool
}

// repositoryRevocationStorage deliberately exposes no unconstrained delete.
// A marker can only be removed after the same token has been marked safe,
// preventing an old cleanup or anonymous probe from clearing a newer
// revocation generation.
type repositoryRevocationStorage interface {
	Load(context.Context) ([]repositoryRevocationMarker, error)
	Begin(context.Context, string, string) (string, error)
	MarkCleanupSafe(context.Context, string, string) (bool, error)
	DeleteCleanupSafe(context.Context, string, string) (bool, error)
}

type repositoryRevocationStore struct {
	database *gorm.DB
	newToken func() (string, error)

	// Serialize capacity checks within one process. The post-upsert count is
	// still authoritative: an external writer or another process can exceed
	// the soft cap, and Load will then fail closed instead of omitting rows.
	beginMu sync.Mutex
}

func newRepositoryRevocationStore(database *gorm.DB) *repositoryRevocationStore {
	return &repositoryRevocationStore{
		database: database,
		newToken: newRepositoryRevocationToken,
	}
}

func (s *repositoryRevocationStore) Load(
	ctx context.Context,
) ([]repositoryRevocationMarker, error) {
	if s == nil || s.database == nil {
		return nil, errRepositoryRevocationStoreUnavailable
	}

	var records []db.HuggingFaceRepositoryRevocation
	result := s.database.WithContext(nonNilContext(ctx)).
		Order("repository ASC").
		Limit(maxPersistedRepositoryRevocations + 1).
		Find(&records)
	if result.Error != nil {
		return nil, repositoryRevocationStoreError("load markers", result.Error)
	}
	if len(records) > maxPersistedRepositoryRevocations {
		return nil, fmt.Errorf(
			"%w: found at least %d markers (maximum %d)",
			errRepositoryRevocationStoreCapacity,
			len(records),
			maxPersistedRepositoryRevocations,
		)
	}

	markers := make([]repositoryRevocationMarker, 0, len(records))
	for _, record := range records {
		if err := validatePersistedRepositoryRevocation(record); err != nil {
			return nil, fmt.Errorf(
				"%w for repository %q: %v",
				errRepositoryRevocationMarkerCorrupt,
				record.Repository,
				err,
			)
		}
		markers = append(markers, repositoryRevocationMarker{
			Repository:  record.Repository,
			EscapedRepo: record.EscapedRepo,
			Token:       record.Token,
			CleanupSafe: record.CleanupSafe,
		})
	}
	return markers, nil
}

// Begin persists a new revocation generation and returns its CAS token. An
// existing repository row is updated in place, with CleanupSafe reset to
// false. The upsert is expressed through GORM's portable ON CONFLICT clause so
// it works with both SQLite and PostgreSQL.
func (s *repositoryRevocationStore) Begin(
	ctx context.Context,
	repository string,
	escapedRepo string,
) (string, error) {
	if s == nil || s.database == nil {
		return "", errRepositoryRevocationStoreUnavailable
	}
	if err := validateRepositoryRevocationIdentity(repository, escapedRepo); err != nil {
		return "", err
	}
	if s.newToken == nil {
		return "", errRepositoryRevocationTokenUnavailable
	}
	token, err := s.newToken()
	if err != nil {
		return "", fmt.Errorf("%w: %v", errRepositoryRevocationTokenUnavailable, err)
	}
	if !validRepositoryRevocationToken(token) {
		return "", fmt.Errorf(
			"%w: token generator returned an invalid token",
			errRepositoryRevocationTokenUnavailable,
		)
	}

	now := time.Now().UTC()
	record := db.HuggingFaceRepositoryRevocation{
		Repository:  repository,
		EscapedRepo: escapedRepo,
		Token:       token,
		CleanupSafe: false,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.beginMu.Lock()
	defer s.beginMu.Unlock()

	err = s.database.WithContext(nonNilContext(ctx)).Transaction(func(tx *gorm.DB) error {
		upsert := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "repository"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"escaped_repo",
				"token",
				"cleanup_safe",
				"updated_at",
			}),
		}).Create(&record)
		if upsert.Error != nil {
			return upsert.Error
		}

		var count int64
		if countResult := tx.Model(&db.HuggingFaceRepositoryRevocation{}).
			Count(&count); countResult.Error != nil {
			return countResult.Error
		}
		if count > maxPersistedRepositoryRevocations {
			return fmt.Errorf(
				"%w: found %d markers (maximum %d)",
				errRepositoryRevocationStoreCapacity,
				count,
				maxPersistedRepositoryRevocations,
			)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errRepositoryRevocationStoreCapacity) {
			return "", err
		}
		return "", repositoryRevocationStoreError("begin revocation", err)
	}
	return token, nil
}

// MarkCleanupSafe performs a token-CAS update. A false result with a nil error
// means the marker was deleted or a newer Begin superseded this generation.
func (s *repositoryRevocationStore) MarkCleanupSafe(
	ctx context.Context,
	repository string,
	token string,
) (bool, error) {
	if s == nil || s.database == nil {
		return false, errRepositoryRevocationStoreUnavailable
	}
	if err := validateRepositoryAndToken(repository, token); err != nil {
		return false, err
	}

	result := s.database.WithContext(nonNilContext(ctx)).
		Model(&db.HuggingFaceRepositoryRevocation{}).
		Where("repository = ? AND token = ?", repository, token).
		Updates(map[string]interface{}{
			"cleanup_safe": true,
			"updated_at":   time.Now().UTC(),
		})
	if result.Error != nil {
		return false, repositoryRevocationStoreError("mark cleanup safe", result.Error)
	}
	return result.RowsAffected == 1, nil
}

// DeleteCleanupSafe removes only the cleanup-safe marker carrying the current
// token. A true result means the row was removed by this call or was already
// absent, so the caller may attempt its in-memory generation-CAS restore. A
// false result means a marker still exists (either this generation is not safe
// or a newer token replaced it).
func (s *repositoryRevocationStore) DeleteCleanupSafe(
	ctx context.Context,
	repository string,
	token string,
) (bool, error) {
	if s == nil || s.database == nil {
		return false, errRepositoryRevocationStoreUnavailable
	}
	if err := validateRepositoryAndToken(repository, token); err != nil {
		return false, err
	}

	result := s.database.WithContext(nonNilContext(ctx)).
		Where(
			"repository = ? AND token = ? AND cleanup_safe = ?",
			repository,
			token,
			true,
		).
		Delete(&db.HuggingFaceRepositoryRevocation{})
	if result.Error != nil {
		return false, repositoryRevocationStoreError("delete cleanup-safe marker", result.Error)
	}
	if result.RowsAffected == 1 {
		return true, nil
	}

	var current db.HuggingFaceRepositoryRevocation
	read := s.database.WithContext(nonNilContext(ctx)).
		Select("repository").
		Where("repository = ?", repository).
		Take(&current)
	switch {
	case errors.Is(read.Error, gorm.ErrRecordNotFound):
		// Another observer may have completed the same cleanup and deletion.
		// The caller's generation CAS still decides whether its in-memory
		// revocation generation can be restored.
		return true, nil
	case read.Error != nil:
		return false, repositoryRevocationStoreError(
			"verify cleanup-safe marker deletion",
			read.Error,
		)
	default:
		return false, nil
	}
}

func newRepositoryRevocationToken() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func validateRepositoryRevocationIdentity(repository, escapedRepo string) error {
	switch {
	case repository == "":
		return fmt.Errorf("%w: repository is empty", errRepositoryRevocationMarkerInvalid)
	case len(repository) > 256:
		return fmt.Errorf(
			"%w: repository is longer than 256 bytes",
			errRepositoryRevocationMarkerInvalid,
		)
	case escapedRepo == "":
		return fmt.Errorf("%w: escaped repository is empty", errRepositoryRevocationMarkerInvalid)
	case repository != strings.ToLower(repository) || escapedRepo != strings.ToLower(escapedRepo):
		return fmt.Errorf("%w: repository identity must be lowercase canonical form", errRepositoryRevocationMarkerInvalid)
	case len(escapedRepo) > 512:
		return fmt.Errorf(
			"%w: escaped repository is longer than 512 bytes",
			errRepositoryRevocationMarkerInvalid,
		)
	default:
		return nil
	}
}

func validateRepositoryAndToken(repository, token string) error {
	switch {
	case repository == "":
		return fmt.Errorf("%w: repository is empty", errRepositoryRevocationMarkerInvalid)
	case len(repository) > 256:
		return fmt.Errorf(
			"%w: repository is longer than 256 bytes",
			errRepositoryRevocationMarkerInvalid,
		)
	case repository != strings.ToLower(repository):
		return fmt.Errorf("%w: repository identity must be lowercase canonical form", errRepositoryRevocationMarkerInvalid)
	case !validRepositoryRevocationToken(token):
		return fmt.Errorf("%w: token is not 32 lowercase hexadecimal characters",
			errRepositoryRevocationMarkerInvalid,
		)
	default:
		return nil
	}
}

func validatePersistedRepositoryRevocation(
	record db.HuggingFaceRepositoryRevocation,
) error {
	if err := validateRepositoryRevocationIdentity(
		record.Repository,
		record.EscapedRepo,
	); err != nil {
		return err
	}
	if !validRepositoryRevocationToken(record.Token) {
		return errors.New("token is not 32 lowercase hexadecimal characters")
	}
	return nil
}

func validRepositoryRevocationToken(token string) bool {
	if len(token) != 32 {
		return false
	}
	for _, char := range token {
		if !('0' <= char && char <= '9') && !('a' <= char && char <= 'f') {
			return false
		}
	}
	return true
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func repositoryRevocationStoreError(operation string, err error) error {
	return fmt.Errorf(
		"%w: %s: %w",
		errRepositoryRevocationStoreUnavailable,
		operation,
		err,
	)
}
