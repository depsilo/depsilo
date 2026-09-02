package server

import (
	"path/filepath"
	"testing"
	"time"

	"depsilo/internal/db"
)

func TestBackfillPackageNamesDoesNotRestoreUntrustedIdentityGuesses(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "startup.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	if err := database.AutoMigrate(&db.CacheEntry{}); err != nil {
		t.Fatal(err)
	}

	entries := []db.CacheEntry{
		{Key: "apt/ubuntu/pool/main/c/curl/curl_8.1.0-1_amd64.deb", AdapterType: "apt", CreatedAt: time.Now()},
		{Key: "nuget/v3-flatcontainer/newtonsoft.json/13.0.3/newtonsoft.json.13.0.3.nupkg", AdapterType: "nuget", CreatedAt: time.Now()},
		{Key: "rubygems/gems/nokogiri-1.16.5-x86_64-linux.gem", AdapterType: "rubygems", CreatedAt: time.Now()},
		{Key: "npm/legacy/metadata.json", AdapterType: "npm", CreatedAt: time.Now()},
		{Key: "npm-exact-v1/Express/metadata.json", AdapterType: "npm", CreatedAt: time.Now()},
		{Key: "composer/p2/symfony/console.json", AdapterType: "composer", CreatedAt: time.Now()},
		{Key: "maven/org/example/app/1.0/app-1.0.jar", AdapterType: "maven", CreatedAt: time.Now()},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatal(err)
	}

	backfillPackageNames(database)

	wants := map[string]string{
		entries[0].Key: "",
		entries[1].Key: "",
		entries[2].Key: "",
		entries[3].Key: "",
		entries[4].Key: "Express",
		entries[5].Key: "symfony/console",
		entries[6].Key: "org.example:app",
	}
	for key, want := range wants {
		var entry db.CacheEntry
		if err := database.Where("key = ?", key).Take(&entry).Error; err != nil {
			t.Fatal(err)
		}
		if entry.PackageName != want {
			t.Errorf("backfilled %s package name = %q, want %q", key, entry.PackageName, want)
		}
	}
}
