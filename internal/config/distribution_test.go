package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDistributionConfigDefaultsAndEnvironment(t *testing.T) {
	home, working := t.TempDir(), t.TempDir()
	t.Chdir(working)
	t.Setenv("HOME", home)
	t.Setenv("DEPSILO_CONFIG", "")
	t.Setenv("DEPSILO_DISTRIBUTION_ENABLED", "")
	t.Setenv("DEPSILO_DISTRIBUTION_STORAGE_PATH", "")
	setTestJWTSecret(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Distribution.Enabled || cfg.Distribution.MaxSizeBytes != 20<<30 || cfg.Distribution.MaxObjectBytes != 1<<30 {
		t.Fatalf("unexpected distribution defaults: %+v", cfg.Distribution)
	}
	if want := filepath.Join(home, ".depsilo", "data", "distribution"); cfg.Distribution.Storage.Path != want {
		t.Fatalf("distribution root = %q, want %q", cfg.Distribution.Storage.Path, want)
	}
	for key, value := range map[string]string{
		"ENABLED": "true", "MAX_SIZE_BYTES": "8192", "MAX_OBJECT_BYTES": "4096",
		"STORAGE_TYPE": "s3", "STORAGE_BUCKET": "retained", "STORAGE_ENDPOINT": "https://objects.example.test",
		"STORAGE_REGION": "custom-region", "STORAGE_ACCESS_KEY": "test-access", "STORAGE_SECRET_KEY": "test-secret",
	} {
		t.Setenv("DEPSILO_DISTRIBUTION_"+key, value)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	want := DistributionConfig{Enabled: true, MaxSizeBytes: 8192, MaxObjectBytes: 4096, Storage: StorageConfig{
		Type: "s3", Path: filepath.Join(home, ".depsilo", "data", "distribution"), Bucket: "retained",
		Endpoint: "https://objects.example.test", Region: "custom-region", AccessKey: "test-access", SecretKey: "test-secret",
	}}
	if !reflect.DeepEqual(cfg.Distribution, want) {
		t.Fatal("distribution environment overrides were not composed")
	}
	if _, err := os.Stat(filepath.Join(home, ".depsilo", "data", "distribution")); !os.IsNotExist(err) {
		t.Fatalf("configuration loading created storage: %v", err)
	}
}

func TestDistributionConfigDocumentPreservesRelativePathAndValidatesLimits(t *testing.T) {
	data := []byte("[distribution]\nenabled = true\nmax_size_bytes = 4096\nmax_object_bytes = 1024\n[distribution.storage]\ntype = 'local'\npath = './data/distribution'\n")
	cfg, err := decodeConfigDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Distribution.Storage.Path != "./data/distribution" {
		t.Fatalf("relative path changed: %q", cfg.Distribution.Storage.Path)
	}
	if _, err := decodeConfigDocument([]byte(strings.ReplaceAll(string(data), "max_object_bytes = 1024", "max_object_bytes = 8192"))); err == nil {
		t.Fatal("config document accepted object size above retained-content capacity")
	}
}

func TestDistributionConfigDocumentLoadsTransferKeys(t *testing.T) {
	data := []byte("[distribution]\nenabled = true\nmax_size_bytes = 4096\nmax_object_bytes = 1024\n" +
		"transfer_signing_key_file = '/etc/depsilo/transfer.pem'\ntrusted_transfer_keys = ['ed25519:one', 'ed25519:two']\n" +
		"[distribution.storage]\ntype = 'local'\npath = './data/distribution'\n")
	cfg, err := decodeConfigDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Distribution.TransferSigningKeyFile != "/etc/depsilo/transfer.pem" ||
		!reflect.DeepEqual(cfg.Distribution.TrustedTransferKeys, []string{"ed25519:one", "ed25519:two"}) {
		t.Fatalf("transfer keys not loaded: %+v", cfg.Distribution)
	}
}

func TestDistributionConfigRejectsInvalidAndOverlappingLocalRoots(t *testing.T) {
	base := t.TempDir()
	packages := StorageConfig{Type: "local", Path: filepath.Join(base, "packages")}
	compiler := StorageConfig{Type: "local", Path: filepath.Join(base, "compiler")}
	valid := DistributionConfig{Enabled: true, MaxSizeBytes: 4096, MaxObjectBytes: 1024, Storage: StorageConfig{Type: "local", Path: filepath.Join(base, "retained")}}
	if err := ValidateDistributionConfig(valid, packages, compiler); err != nil {
		t.Fatal(err)
	}
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"empty": "", "filesystem root": string(filepath.Separator), "working directory": working,
		"home directory": home, "temporary directory": os.TempDir(),
		"same package root": packages.Path, "inside package root": filepath.Join(packages.Path, "objects"),
		"same compiler root": compiler.Path, "inside compiler root": filepath.Join(compiler.Path, "objects"),
		"contains both caches": base,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := valid
			cfg.Storage.Path = path
			if err := ValidateDistributionConfig(cfg, packages, compiler); err == nil {
				t.Fatalf("accepted unsafe root %q", path)
			}
		})
	}
	for _, sizes := range [][2]int64{{0, 1}, {-1, 1}, {1024, 0}, {1024, -1}, {1024, 1025}} {
		cfg := valid
		cfg.MaxSizeBytes, cfg.MaxObjectBytes = sizes[0], sizes[1]
		if err := ValidateDistributionConfig(cfg, packages, compiler); err == nil {
			t.Fatalf("accepted size limits %v", sizes)
		}
	}
	valid.MaxSizeBytes, valid.MaxObjectBytes = math.MaxInt64, math.MaxInt64
	if err := ValidateDistributionConfig(valid, packages, compiler); err == nil {
		t.Fatal("accepted object limit that overflows the one-byte integrity probe")
	}
	valid.MaxObjectBytes--
	if err := ValidateDistributionConfig(valid, packages, compiler); err != nil {
		t.Fatalf("rejected representable boundary limits: %v", err)
	}
	valid.Storage.Type = "invalid"
	if err := ValidateDistributionConfig(valid, packages, compiler); err == nil {
		t.Fatal("accepted unsupported storage")
	}
	valid.Enabled = false
	if err := ValidateDistributionConfig(valid, packages, compiler); err != nil {
		t.Fatalf("disabled configuration should not construct or validate inactive storage: %v", err)
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 0 {
		t.Fatalf("validation wrote local files: entries %v, error %v", entries, err)
	}
}

func TestDistributionConfigResolvesExistingSymlinkAncestors(t *testing.T) {
	base := t.TempDir()
	packages := StorageConfig{Type: "local", Path: filepath.Join(base, "packages")}
	if err := os.Mkdir(packages.Path, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "alias")
	if err := os.Symlink(packages.Path, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, filepath.Join(link, "missing", "retained")} {
		cfg := DistributionConfig{Enabled: true, MaxSizeBytes: 1024, MaxObjectBytes: 1024, Storage: StorageConfig{Type: "local", Path: path}}
		if err := ValidateDistributionConfig(cfg, packages, StorageConfig{}); err == nil {
			t.Fatalf("symlink root bypassed cache isolation: %q", path)
		}
	}
	broken := filepath.Join(base, "broken")
	if err := os.Symlink(filepath.Join(base, "absent"), broken); err != nil {
		t.Fatal(err)
	}
	regularFile := filepath.Join(base, "file")
	if err := os.WriteFile(regularFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{broken, filepath.Join(broken, "retained"), regularFile, filepath.Join(regularFile, "retained")} {
		cfg := DistributionConfig{Enabled: true, MaxSizeBytes: 1024, MaxObjectBytes: 1024, Storage: StorageConfig{Type: "local", Path: path}}
		if err := ValidateDistributionConfig(cfg, packages, StorageConfig{}); err == nil {
			t.Fatalf("accepted unresolvable storage root: %q", path)
		}
	}
}

func TestDistributionConfigRequiresDistinctS3BucketNames(t *testing.T) {
	for _, endpoints := range [][2]string{
		{"", ""}, {"https://objects.example.test", "https://OBJECTS.example.test:443/"},
		{"http://objects.example.test:80", "https://objects.example.test/"},
		{"http://objects.example.test:9000", "http://objects.example.test:9000/"},
		{"", "https://s3.us-east-1.amazonaws.com"},
		{"https://s3.amazonaws.com", "https://s3.us-west-2.amazonaws.com"},
		{"https://objects.example.test", "https://objects-alias.example.test"},
	} {
		t.Run(fmt.Sprintf("%s-%s", endpoints[0], endpoints[1]), func(t *testing.T) {
			cfg := DistributionConfig{Enabled: true, MaxSizeBytes: 4096, MaxObjectBytes: 1024, Storage: StorageConfig{Type: "s3", Endpoint: endpoints[0], Bucket: "shared"}}
			cache := StorageConfig{Type: "s3", Endpoint: endpoints[1], Bucket: "shared"}
			if err := ValidateDistributionConfig(cfg, cache, StorageConfig{}); err == nil {
				t.Fatal("accepted identical package bucket name even though endpoints may alias")
			}
			if err := ValidateDistributionConfig(cfg, StorageConfig{}, cache); err == nil {
				t.Fatal("accepted shared compiler bucket")
			}
			cfg.Storage.Bucket = "dedicated"
			if err := ValidateDistributionConfig(cfg, cache, StorageConfig{}); err != nil {
				t.Fatalf("rejected independent bucket: %v", err)
			}
		})
	}
	for _, storage := range []StorageConfig{
		{Type: "s3"}, {Type: "s3", Bucket: " trailing "}, {Type: "s3", Bucket: "dedicated", Endpoint: "ftp://objects.test"},
		{Type: "s3", Bucket: "dedicated", Endpoint: "https://secret@objects.test"},
		{Type: "s3", Bucket: "dedicated", Endpoint: "https://objects.test?token=secret"},
	} {
		cfg := DistributionConfig{Enabled: true, MaxSizeBytes: 4096, MaxObjectBytes: 1024, Storage: storage}
		if err := ValidateDistributionConfig(cfg, StorageConfig{}, StorageConfig{}); err == nil {
			t.Fatal("accepted invalid S3 configuration")
		}
	}
}
