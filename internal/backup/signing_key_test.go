package backup_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"depsilo/internal/backup"
	"depsilo/internal/config"
	"github.com/golang-jwt/jwt/v5"
)

func TestCreateRestorePreservesManagedSigningKeyWithoutSourceFile(t *testing.T) {
	t.Setenv("DEPSILO_AUTH_JWT_SECRET", "")
	root := t.TempDir()
	sourceConfig := filepath.Join(root, "source.toml")
	sourceDatabase := filepath.Join(root, "source.db")
	writeTestState(t, sourceConfig, sourceDatabase)
	document := []byte("# operator comment\n[database]\ndriver = \"sqlite\"\n\n[custom]\nowner = \"keep exactly\" # custom comment\n")
	if err := os.WriteFile(sourceConfig, document, 0o600); err != nil {
		t.Fatal(err)
	}
	secret, secretPath := writeManagedSigningKey(t, sourceConfig)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "portable-instance"}).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(root, "backup.tar.gz")
	if _, err := backup.Create(context.Background(), backup.Paths{Config: sourceConfig, Database: sourceDatabase}, archivePath); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, sourceConfig, document)
	assertFileContent(t, secretPath, []byte(secret+"\n"))
	files := readArchive(t, archivePath)
	if len(files) != 3 {
		t.Fatalf("archive has %d entries, want manifest plus portable config and database", len(files))
	}
	for _, preserved := range []string{"# operator comment\n", "[custom]\nowner = \"keep exactly\" # custom comment\n"} {
		if !strings.Contains(string(files["config.toml"]), preserved) {
			t.Fatal("portable config changed unrelated operator-owned bytes")
		}
	}
	if err := os.Remove(secretPath); err != nil {
		t.Fatal(err)
	}
	targetConfig := filepath.Join(root, "restored", "config.toml")
	targetDatabase := filepath.Join(root, "restored", "data", "depsilo.db")
	if _, err := backup.Restore(context.Background(), archivePath, backup.Paths{Config: targetConfig, Database: targetDatabase}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", targetConfig)
	restored, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if restored.Auth.JWTSecret != secret {
		t.Fatal("restored configuration changed the instance signing key")
	}
	if _, err := jwt.Parse(token, func(token *jwt.Token) (any, error) {
		return []byte(restored.Auth.JWTSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"})); err != nil {
		t.Fatal("restored instance cannot verify a token from before backup")
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("archive permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestCreatePreservesExplicitLegacySigningConfigBytes(t *testing.T) {
	t.Setenv("DEPSILO_AUTH_JWT_SECRET", "")
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	databasePath := filepath.Join(root, "depsilo.db")
	writeTestState(t, configPath, databasePath)
	document := mustReadFile(t, configPath)
	archivePath := filepath.Join(root, "backup.tar.gz")
	if _, err := backup.Create(context.Background(), backup.Paths{Config: configPath, Database: databasePath}, archivePath); err != nil {
		t.Fatal(err)
	}
	if string(readArchive(t, archivePath)["config.toml"]) != string(document) {
		t.Fatal("archiving legacy inline configuration changed its bytes")
	}
}

func TestCreateIncludesEffectiveEnvironmentSigningKey(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	databasePath := filepath.Join(root, "depsilo.db")
	writeTestState(t, configPath, databasePath)
	document := mustReadFile(t, configPath)
	secret, err := config.NewSecureToken()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_AUTH_JWT_SECRET", secret)
	archivePath := filepath.Join(root, "backup.tar.gz")
	if _, err := backup.Create(context.Background(), backup.Paths{Config: configPath, Database: databasePath}, archivePath); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, configPath, document)
	t.Setenv("DEPSILO_AUTH_JWT_SECRET", "")
	targetConfig := filepath.Join(root, "restored", "config.toml")
	if _, err := backup.Restore(context.Background(), archivePath, backup.Paths{Config: targetConfig, Database: filepath.Join(root, "restored", "depsilo.db")}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", targetConfig)
	restored, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if restored.Auth.JWTSecret != secret {
		t.Fatal("backup did not preserve the effective environment signing key")
	}
}

func TestCreateRejectsMissingOrCorruptManagedSigningKey(t *testing.T) {
	t.Setenv("DEPSILO_AUTH_JWT_SECRET", "")
	for _, kind := range []string{"missing", "corrupt"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			configPath := filepath.Join(root, "config.toml")
			databasePath := filepath.Join(root, "depsilo.db")
			writeTestState(t, configPath, databasePath)
			if err := os.WriteFile(configPath, []byte("[database]\ndriver = \"sqlite\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			const corrupt = "corrupt-private-key"
			if kind == "corrupt" {
				_, secretPath := writeManagedSigningKey(t, configPath)
				if err := os.WriteFile(secretPath, []byte(corrupt), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			archivePath := filepath.Join(root, "backup.tar.gz")
			_, err := backup.Create(context.Background(), backup.Paths{Config: configPath, Database: databasePath}, archivePath)
			if err == nil {
				t.Fatal("Create() published a backup without a valid instance signing key")
			}
			if strings.Contains(err.Error(), corrupt) {
				t.Fatal("Create() error disclosed signing key bytes")
			}
			if _, err := os.Stat(archivePath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed backup published an output: %v", err)
			}
			workspaces, err := filepath.Glob(filepath.Join(root, ".depsilo-backup-*"))
			if err != nil || len(workspaces) != 0 {
				t.Fatal("failed backup retained temporary secret-bearing files")
			}
		})
	}
}

func TestCreateRefusesToOverwriteManagedSigningKey(t *testing.T) {
	t.Setenv("DEPSILO_AUTH_JWT_SECRET", "")
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	databasePath := filepath.Join(root, "depsilo.db")
	writeTestState(t, configPath, databasePath)
	secret, secretPath := writeManagedSigningKey(t, configPath)
	if _, err := backup.Create(context.Background(), backup.Paths{Config: configPath, Database: databasePath}, secretPath); err == nil {
		t.Fatal("Create() overwrote the managed signing key")
	}
	assertFileContent(t, secretPath, []byte(secret+"\n"))
}

func writeManagedSigningKey(t *testing.T, configPath string) (string, string) {
	t.Helper()
	secretPath, err := config.ManagedJWTSecretPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(secretPath), 0o700); err != nil {
		t.Fatal(err)
	}
	secret, err := config.NewSecureToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretPath, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return secret, secretPath
}
