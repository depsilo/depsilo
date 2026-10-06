package admin

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/config"
	"depsilo/internal/db"
)

func newProjectsCRAHandler(t *testing.T, compliance config.ComplianceConfig) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "projects-cra.db")),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.Project{}, &db.ProjectPackage{}, &db.TamperRecord{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	handler := NewProjectsHandler(database)
	handler.SetCompliance(compliance)
	r := gin.New()
	r.GET("/projects/:id/sbom", handler.ExportSBOM)
	return r, database
}

func seedCRAProject(t *testing.T, database *gorm.DB) db.Project {
	t.Helper()
	project := createContractProject(t, database)
	if err := database.Create(&db.ProjectPackage{
		ProjectID: project.ID, Ecosystem: "pypi", PackageName: "requests", Version: "2.31.0",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&db.TamperRecord{
		Key: "pypi/files/requests-2.31.0.whl", Ecosystem: "pypi", Package: "requests",
		Version: "2.31.0", SHA256: "deadbeef",
	}).Error; err != nil {
		t.Fatal(err)
	}
	return project
}

func TestExportProjectSBOMTechnicalFilePreset(t *testing.T) {
	r, database := newProjectsCRAHandler(t, config.ComplianceConfig{
		Organization: "Acme GmbH",
		Contact:      "security@acme.example",
		Components: []config.ComplianceComponentConfig{
			{ID: "pypi:requests", Supplier: "Python Software Foundation", License: "Apache-2.0"},
		},
	})
	project := seedCRAProject(t, database)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/projects/"+jsonNumber(project.ID)+"/sbom?format=cyclonedx&preset=technical-file", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if disposition := rec.Header().Get("Content-Disposition"); disposition !=
		`attachment; filename="ai-platform-cra-technical-file-`+nowDate()+`.json"` {
		t.Fatalf("content-disposition = %q", disposition)
	}

	var envelope struct {
		TechnicalFile struct {
			Format  string `json:"format"`
			Product struct {
				Slug string `json:"slug"`
			} `json:"product"`
			Manufacturer struct {
				Organization string `json:"organization"`
				Contact      string `json:"contact"`
			} `json:"manufacturer"`
			Coverage struct {
				Total      int `json:"components_total"`
				WithSHA256 int `json:"components_with_sha256"`
			} `json:"coverage"`
			Relationship string   `json:"relationship_depth"`
			Limitations  []string `json:"limitations"`
		} `json:"technical_file"`
		SBOM struct {
			Components []struct {
				PURL   string `json:"purl"`
				BomRef string `json:"bom-ref"`
				Hashes []struct {
					Alg     string `json:"alg"`
					Content string `json:"content"`
				} `json:"hashes"`
			} `json:"components"`
			Dependencies []struct {
				Ref       string   `json:"ref"`
				DependsOn []string `json:"dependsOn"`
			} `json:"dependencies"`
		} `json:"sbom"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.TechnicalFile.Format != "depsilo/cra-technical-file/v1" ||
		envelope.TechnicalFile.Product.Slug != "ai-platform" ||
		envelope.TechnicalFile.Manufacturer.Organization != "Acme GmbH" ||
		envelope.TechnicalFile.Manufacturer.Contact != "security@acme.example" {
		t.Fatalf("technical file = %+v", envelope.TechnicalFile)
	}
	if envelope.TechnicalFile.Coverage.Total != 1 || envelope.TechnicalFile.Coverage.WithSHA256 != 1 {
		t.Fatalf("coverage = %+v", envelope.TechnicalFile.Coverage)
	}
	if envelope.TechnicalFile.Relationship != "top-level" || len(envelope.TechnicalFile.Limitations) == 0 {
		t.Fatalf("technical file metadata = %+v", envelope.TechnicalFile)
	}
	if len(envelope.SBOM.Components) != 1 ||
		len(envelope.SBOM.Components[0].Hashes) != 1 ||
		envelope.SBOM.Components[0].Hashes[0].Content != "deadbeef" {
		t.Fatalf("sbom components = %+v", envelope.SBOM.Components)
	}
	if len(envelope.SBOM.Dependencies) != 1 || len(envelope.SBOM.Dependencies[0].DependsOn) != 1 {
		t.Fatalf("sbom dependencies = %+v", envelope.SBOM.Dependencies)
	}
}

func TestExportProjectSBOMSignsWhenKeyConfigured(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "sbom-key.pem")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	r, database := newProjectsCRAHandler(t, config.ComplianceConfig{SigningKeyFile: keyPath})
	project := seedCRAProject(t, database)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/projects/"+jsonNumber(project.ID)+"/sbom?sign=true", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Depsilo-SBOM-Signature-Algorithm") != "ed25519" {
		t.Fatalf("signature headers = %+v", rec.Header())
	}
	storedKey, err := base64.StdEncoding.DecodeString(rec.Header().Get("X-Depsilo-SBOM-Signature-PublicKey"))
	if err != nil || string(storedKey) != string(publicKey) {
		t.Fatalf("signature public key mismatch: %v", err)
	}
	rawSignature, err := base64.StdEncoding.DecodeString(rec.Header().Get("X-Depsilo-SBOM-Signature"))
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(publicKey, rec.Body.Bytes(), rawSignature) {
		t.Fatal("signature did not verify over the response body")
	}
	var document map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &document); err != nil {
		t.Fatalf("signed response body is not the raw SBOM: %v", err)
	}
	if document["spdxVersion"] != "SPDX-2.3" {
		t.Fatalf("signed response shape = %+v", document)
	}
}

func TestExportProjectSBOMRejectsUnknownPresetAndUnsignedRequests(t *testing.T) {
	r, database := newProjectsCRAHandler(t, config.ComplianceConfig{})
	project := seedCRAProject(t, database)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/projects/"+jsonNumber(project.ID)+"/sbom?preset=unknown", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown preset status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/projects/"+jsonNumber(project.ID)+"/sbom?preset=technical-file&sign=true", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsigned request status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func nowDate() string {
	return time.Now().UTC().Format("2006-01-02")
}
