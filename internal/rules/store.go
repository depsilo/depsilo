package rules

import (
	"gorm.io/gorm"

	"depsilo/internal/db"
)

// Store provides CRUD operations for package rules.
type Store struct {
	db *gorm.DB
}

// NewStore creates a new rules Store.
func NewStore(database *gorm.DB) *Store {
	return &Store{db: database}
}

// List returns all package rules ordered by creation time descending.
func (s *Store) List() ([]db.PackageRule, error) {
	var rules []db.PackageRule
	err := s.db.Order("datetime(created_at) DESC").Find(&rules).Error
	return rules, err
}

// Create inserts a new package rule.
func (s *Store) Create(rule *db.PackageRule) error {
	return s.db.Create(rule).Error
}

// Update applies partial updates to a package rule by ID.
func (s *Store) Update(id uint, updates map[string]interface{}) (*db.PackageRule, error) {
	var rule db.PackageRule
	if err := s.db.First(&rule, id).Error; err != nil {
		return nil, err
	}
	if err := s.db.Model(&rule).Updates(updates).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

// Delete removes a package rule by ID.
func (s *Store) Delete(id uint) error {
	return s.db.Delete(&db.PackageRule{}, id).Error
}
