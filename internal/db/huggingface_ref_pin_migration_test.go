package db

import "testing"

func TestAutoMigrateAddsHuggingFaceRefPinsWithoutChangingCacheRows(t *testing.T) {
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
	var preserved CacheEntry
	if err := database.Where("key = ?", legacy.Key).First(&preserved).Error; err != nil {
		t.Fatal(err)
	}
	if preserved.StoragePath != legacy.StoragePath || preserved.Size != legacy.Size {
		t.Fatalf("legacy cache row changed during ref-pin migration: %+v", preserved)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
}
