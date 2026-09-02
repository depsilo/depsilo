package db

import (
	"strings"
	"testing"
)

func TestAutoMigrateAddsHuggingFaceRefPinsAndRetiresLegacyCaseAliases(t *testing.T) {
	database, err := Open("sqlite", ":memory:")
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
	if !database.Migrator().HasTable(&HuggingFaceRefPin{}) {
		t.Fatal("Hugging Face ref pin table was not created")
	}
	var retired CacheEntry
	if err := database.First(&retired, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retired.Key == legacy.Key || !strings.HasPrefix(retired.Key, schemaV3RetiredHuggingFaceKeyPrefix) ||
		retired.AdapterType != schemaV3RetiredHuggingFaceAdapterType || retired.PackageName != "" ||
		retired.StoragePath != legacy.StoragePath || retired.Size != legacy.Size {
		t.Fatalf("legacy cache row was not safely retired: %+v", retired)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
}
