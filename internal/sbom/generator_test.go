package sbom

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"depsilo/internal/db"
)

func sbomTestProject() *db.Project {
	return &db.Project{ID: 1, Name: "Acme App", Slug: "acme-app", Description: "test product"}
}

func sbomTestComponents() []Component {
	return []Component{
		{
			Ecosystem: "pypi", Name: "requests", Version: "2.31.0",
			Hashes:   []string{"aaaa", "bbbb"},
			Supplier: "Python Software Foundation",
			License:  "Apache-2.0",
		},
		{Ecosystem: "npm", Name: "left-pad", Version: "1.3.0"},
	}
}

func TestGenerateCycloneDXCRAEnrichment(t *testing.T) {
	generator := NewGenerator(nil)
	options := Options{CRA: true, Profile: NewComplianceProfile("Acme GmbH", "security@acme.example", nil)}
	raw, err := generator.GenerateCycloneDX(sbomTestProject(), sbomTestComponents(), options)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Metadata struct {
			Component  map[string]interface{} `json:"component"`
			Properties []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"properties"`
		} `json:"metadata"`
		Components []struct {
			PURL   string `json:"purl"`
			BomRef string `json:"bom-ref"`
			Hashes []struct {
				Alg     string `json:"alg"`
				Content string `json:"content"`
			} `json:"hashes"`
			Licenses []struct {
				License struct {
					Name string `json:"name"`
				} `json:"license"`
			} `json:"licenses"`
			Supplier *struct {
				Name string `json:"name"`
			} `json:"supplier"`
		} `json:"components"`
		Dependencies []struct {
			Ref       string   `json:"ref"`
			DependsOn []string `json:"dependsOn"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Components) != 2 {
		t.Fatalf("components = %d, want 2", len(doc.Components))
	}
	requests := doc.Components[0]
	if requests.PURL != "pkg:pypi/requests@2.31.0" || requests.BomRef != requests.PURL {
		t.Fatalf("requests identity = %q / %q", requests.PURL, requests.BomRef)
	}
	if len(requests.Hashes) != 2 || requests.Hashes[0].Alg != "SHA-256" {
		t.Fatalf("requests hashes = %+v", requests.Hashes)
	}
	if requests.Supplier == nil || requests.Supplier.Name != "Python Software Foundation" {
		t.Fatalf("requests supplier = %+v", requests.Supplier)
	}
	if len(requests.Licenses) != 1 || requests.Licenses[0].License.Name != "Apache-2.0" {
		t.Fatalf("requests licenses = %+v", requests.Licenses)
	}
	if len(doc.Dependencies) != 1 || len(doc.Dependencies[0].DependsOn) != 2 {
		t.Fatalf("dependencies = %+v", doc.Dependencies)
	}
	if doc.Dependencies[0].Ref != "urn:depsilo:product:acme-app" {
		t.Fatalf("root ref = %q", doc.Dependencies[0].Ref)
	}
	if supplier, _ := doc.Metadata.Component["supplier"].(map[string]interface{}); supplier["name"] != "Acme GmbH" {
		t.Fatalf("metadata component supplier = %+v", doc.Metadata.Component["supplier"])
	}
	properties := map[string]string{}
	for _, property := range doc.Metadata.Properties {
		properties[property.Name] = property.Value
	}
	if properties["depsilo:cra:components-total"] != "2" ||
		properties["depsilo:cra:components-with-sha256"] != "1" ||
		properties["depsilo:cra:components-with-supplier"] != "1" ||
		properties["depsilo:cra:components-with-license"] != "1" ||
		properties["depsilo:cra:contact"] != "security@acme.example" {
		t.Fatalf("cra properties = %+v", properties)
	}
}

func TestGenerateSPDXCRAEnrichment(t *testing.T) {
	generator := NewGenerator(nil)
	options := Options{CRA: true, Profile: NewComplianceProfile("Acme GmbH", "", nil)}
	raw, err := generator.GenerateSPDX(sbomTestProject(), sbomTestComponents(), options)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Packages []struct {
			SPDXID           string `json:"SPDXID"`
			Supplier         string `json:"supplier"`
			LicenseConcluded string `json:"licenseConcluded"`
			Checksums        []struct {
				Algorithm string `json:"algorithm"`
				Value     string `json:"checksumValue"`
			} `json:"checksums"`
			ExternalRefs []struct {
				Locator string `json:"referenceLocator"`
			} `json:"externalRefs"`
		} `json:"packages"`
		Relationships []struct {
			ElementID string `json:"spdxElementId"`
			Type      string `json:"relationshipType"`
			Related   string `json:"relatedSpdxElement"`
		} `json:"relationships"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Packages) != 3 {
		t.Fatalf("packages = %d, want product + 2 components", len(doc.Packages))
	}
	if doc.Packages[0].SPDXID != "SPDXRef-Package-product" ||
		doc.Packages[0].Supplier != "Organization: Acme GmbH" {
		t.Fatalf("product package = %+v", doc.Packages[0])
	}
	if doc.Packages[1].Supplier != "Organization: Python Software Foundation" ||
		doc.Packages[1].LicenseConcluded != "Apache-2.0" ||
		len(doc.Packages[1].Checksums) != 2 {
		t.Fatalf("requests package = %+v", doc.Packages[1])
	}
	if doc.Packages[2].Supplier != "NOASSERTION" || doc.Packages[2].LicenseConcluded != "NOASSERTION" {
		t.Fatalf("unknown component = %+v", doc.Packages[2])
	}
	dependsOn := 0
	for _, relationship := range doc.Relationships {
		if relationship.Type == "DEPENDS_ON" && relationship.ElementID == "SPDXRef-Package-product" {
			dependsOn++
		}
	}
	if dependsOn != 2 {
		t.Fatalf("DEPENDS_ON relationships = %d, want 2", dependsOn)
	}
}

func TestGenerateWithoutCRACarriesNoChecksumsOrDependencies(t *testing.T) {
	generator := NewGenerator(nil)
	raw, err := generator.GenerateCycloneDX(sbomTestProject(), sbomTestComponents(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]interface{}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if _, present := document["dependencies"]; present {
		t.Fatal("plain CycloneDX export unexpectedly carries dependencies")
	}
	components, _ := document["components"].([]interface{})
	if len(components) != 2 {
		t.Fatalf("components = %d", len(components))
	}
	first, _ := components[0].(map[string]interface{})
	if _, present := first["hashes"]; present {
		t.Fatal("plain export unexpectedly carries hashes")
	}
}

func TestGeneratorComponentsJoinsHashesAndAnnotations(t *testing.T) {
	database, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	project := sbomTestProject()
	if err := database.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	packages := []db.ProjectPackage{
		{ProjectID: project.ID, Ecosystem: "pypi", PackageName: "requests", Version: "2.31.0"},
		{ProjectID: project.ID, Ecosystem: "npm", PackageName: "left-pad", Version: "1.3.0"},
	}
	if err := database.Create(&packages).Error; err != nil {
		t.Fatal(err)
	}
	hashes := []db.TamperRecord{
		{Key: "pypi/one", Ecosystem: "pypi", Package: "requests", Version: "2.31.0", SHA256: "bbbb"},
		{Key: "pypi/two", Ecosystem: "pypi", Package: "requests", Version: "2.31.0", SHA256: "aaaa"},
		{Key: "pypi/other", Ecosystem: "pypi", Package: "other", Version: "9.9.9", SHA256: "cccc"},
	}
	if err := database.Create(&hashes).Error; err != nil {
		t.Fatal(err)
	}

	profile := NewComplianceProfile("", "", map[string]ComponentFacts{
		"pypi:requests":        {Supplier: "package-wide", License: "MIT"},
		"pypi:requests@2.31.0": {Supplier: "version-specific"},
		"npm:left-pad":         {License: "WTFPL"},
	})
	components, err := NewGenerator(database).Components(context.Background(), project.ID, "", profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 2 {
		t.Fatalf("components = %+v", components)
	}
	byName := make(map[string]Component, len(components))
	for _, component := range components {
		byName[component.Name] = component
	}
	requests := byName["requests"]
	if !reflect.DeepEqual(requests.Hashes, []string{"aaaa", "bbbb"}) {
		t.Fatalf("requests hashes = %+v, want sorted deduped", requests.Hashes)
	}
	if requests.Supplier != "version-specific" || requests.License != "MIT" {
		t.Fatalf("requests facts = %+v", requests)
	}
	if leftPad := byName["left-pad"]; leftPad.License != "WTFPL" || leftPad.Supplier != "" {
		t.Fatalf("left-pad facts = %+v", leftPad)
	}

	filtered, err := NewGenerator(database).Components(context.Background(), project.ID, "npm", profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Name != "left-pad" {
		t.Fatalf("filtered components = %+v", filtered)
	}
}

func TestBuildTechnicalFileReportsCoverageAndLimitations(t *testing.T) {
	components := sbomTestComponents()
	options := Options{CRA: true, Profile: NewComplianceProfile("", "security@example", nil)}
	manifest := BuildTechnicalFile(sbomTestProject(), components, options, "cyclonedx", "", time.Now())

	if manifest.Format != TechnicalFileFormat || manifest.SBOM.SpecVersion != "1.5" {
		t.Fatalf("manifest = %+v", manifest)
	}
	if manifest.Coverage.Total != 2 || manifest.Coverage.WithSHA256 != 1 ||
		manifest.Coverage.WithSupplier != 1 || manifest.Coverage.WithLicense != 1 {
		t.Fatalf("coverage = %+v", manifest.Coverage)
	}
	joined := strings.Join(manifest.Limitations, "\n")
	for _, want := range []string{"top-level", "first-seen hashes", "NOASSERTION", "compliance.organization", "have no recorded SHA-256"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("limitations missing %q: %v", want, manifest.Limitations)
		}
	}
}

func TestSignDocumentRoundTrip(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sbom-key.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSigningKey(path)
	if err != nil {
		t.Fatal(err)
	}
	document := []byte(`{"bomFormat":"CycloneDX"}`)
	signature := SignDocument(loaded, document)
	if signature.Algorithm != "ed25519" || signature.PayloadSHA256 == "" || signature.Signature == "" {
		t.Fatalf("signature = %+v", signature)
	}
	rawSignature, err := base64.StdEncoding.DecodeString(signature.Signature)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(publicKey, document, rawSignature) {
		t.Fatal("signature did not verify")
	}
	if _, err := LoadSigningKey(filepath.Join(t.TempDir(), "missing.pem")); err == nil {
		t.Fatal("missing key file was accepted")
	}
}
