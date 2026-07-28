package db

import (
	"path/filepath"
	"testing"
)

func TestAutoMigrateAddsHuggingFaceRepositoryRevocations(t *testing.T) {
	database, err := Open("sqlite", filepath.Join(t.TempDir(), "depsilo.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&CacheEntry{}); err != nil {
		t.Fatal(err)
	}
	legacy := CacheEntry{
		Key:         "huggingface/acme/model/resolve/main/model.bin",
		AdapterType: "huggingface",
		StoragePath: "huggingface/acme/model/resolve/main/model.bin",
		Size:        7,
	}
	if err := database.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}

	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	if !database.Migrator().HasTable(&HuggingFaceRepositoryRevocation{}) {
		t.Fatal("Hugging Face repository revocation table was not created")
	}
	var preserved CacheEntry
	if err := database.Where("key = ?", legacy.Key).First(&preserved).Error; err != nil {
		t.Fatal(err)
	}
	if preserved.StoragePath != legacy.StoragePath || preserved.Size != legacy.Size {
		t.Fatalf("legacy cache row changed during migration: %+v", preserved)
	}

	marker := HuggingFaceRepositoryRevocation{
		Repository:  "acme/model",
		EscapedRepo: "acme/model",
		Token:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	if err := database.Create(&marker).Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	var persisted HuggingFaceRepositoryRevocation
	if err := database.Where("repository = ?", marker.Repository).
		First(&persisted).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.Token != marker.Token || persisted.EscapedRepo != marker.EscapedRepo {
		t.Fatalf("marker changed during repeat migration: %+v", persisted)
	}
}
