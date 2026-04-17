package sbom

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/version"
)

// Generator creates SBOM documents from project package data.
type Generator struct {
	db *gorm.DB
}

// NewGenerator creates a new SBOM generator.
func NewGenerator(database *gorm.DB) *Generator {
	return &Generator{db: database}
}

// GenerateSPDX produces an SPDX 2.3 JSON document.
func (g *Generator) GenerateSPDX(project *db.Project, packages []db.ProjectPackage) ([]byte, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	type spdxExtRef struct {
		Category string `json:"referenceCategory"`
		Type     string `json:"referenceType"`
		Locator  string `json:"referenceLocator"`
	}
	type spdxPackage struct {
		SPDXID       string       `json:"SPDXID"`
		Name         string       `json:"name"`
		Version      string       `json:"versionInfo"`
		DownloadLoc  string       `json:"downloadLocation"`
		Supplier     string       `json:"supplier"`
		ExternalRefs []spdxExtRef `json:"externalRefs"`
		Purpose      string       `json:"primaryPackagePurpose"`
	}
	type spdxRelationship struct {
		ElementID string `json:"spdxElementId"`
		Type      string `json:"relationshipType"`
		Related   string `json:"relatedSpdxElement"`
	}
	type spdxDoc struct {
		Version       string                 `json:"spdxVersion"`
		DataLicense   string                 `json:"dataLicense"`
		SPDXID        string                 `json:"SPDXID"`
		Name          string                 `json:"name"`
		Namespace     string                 `json:"documentNamespace"`
		CreationInfo  map[string]interface{} `json:"creationInfo"`
		Packages      []spdxPackage          `json:"packages"`
		Relationships []spdxRelationship     `json:"relationships"`
	}

	doc := spdxDoc{
		Version:     "SPDX-2.3",
		DataLicense: "CC0-1.0",
		SPDXID:      "SPDXRef-DOCUMENT",
		Name:        project.Slug + "-sbom",
		Namespace:   fmt.Sprintf("https://depsilo.com/spdx/%s/%s", project.Slug, time.Now().Format("2006-01-02")),
		CreationInfo: map[string]interface{}{
			"created":            now,
			"creators":           []string{"Tool: Depsilo " + version.Version},
			"licenseListVersion": "3.22",
		},
		Packages:      make([]spdxPackage, 0, len(packages)),
		Relationships: make([]spdxRelationship, 0, len(packages)),
	}

	for _, pkg := range packages {
		spdxID := fmt.Sprintf("SPDXRef-Package-%s-%s-%s", pkg.Ecosystem, sanitizeSPDXID(pkg.PackageName), pkg.Version)
		purl := FormatPURL(pkg.Ecosystem, pkg.PackageName, pkg.Version)

		doc.Packages = append(doc.Packages, spdxPackage{
			SPDXID:      spdxID,
			Name:        pkg.PackageName,
			Version:     pkg.Version,
			DownloadLoc: "NOASSERTION",
			Supplier:    "NOASSERTION",
			ExternalRefs: []spdxExtRef{
				{Category: "PACKAGE-MANAGER", Type: "purl", Locator: purl},
			},
			Purpose: "LIBRARY",
		})
		doc.Relationships = append(doc.Relationships, spdxRelationship{
			ElementID: "SPDXRef-DOCUMENT",
			Type:      "DESCRIBES",
			Related:   spdxID,
		})
	}

	return json.MarshalIndent(doc, "", "  ")
}

// GenerateCycloneDX produces a CycloneDX 1.5 JSON document.
func (g *Generator) GenerateCycloneDX(project *db.Project, packages []db.ProjectPackage) ([]byte, error) {
	type cdxComponent struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		Version string `json:"version"`
		PURL    string `json:"purl"`
		BomRef  string `json:"bom-ref"`
	}
	type cdxTool struct {
		Vendor  string `json:"vendor"`
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	type cdxMetadata struct {
		Timestamp string                 `json:"timestamp"`
		Tools     []cdxTool              `json:"tools"`
		Component map[string]interface{} `json:"component"`
	}
	type cdxDoc struct {
		BomFormat    string         `json:"bomFormat"`
		SpecVersion  string         `json:"specVersion"`
		SerialNumber string         `json:"serialNumber"`
		Version      int            `json:"version"`
		Metadata     cdxMetadata    `json:"metadata"`
		Components   []cdxComponent `json:"components"`
	}

	doc := cdxDoc{
		BomFormat:    "CycloneDX",
		SpecVersion:  "1.5",
		SerialNumber: "urn:uuid:" + uuid.New().String(),
		Version:      1,
		Metadata: cdxMetadata{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Tools: []cdxTool{
				{Vendor: "Depsilo", Name: "Depsilo SBOM Generator", Version: version.Version},
			},
			Component: map[string]interface{}{
				"type":    "application",
				"name":    project.Name,
				"version": "snapshot-" + time.Now().Format("2006-01-02"),
			},
		},
		Components: make([]cdxComponent, 0, len(packages)),
	}

	for _, pkg := range packages {
		purl := FormatPURL(pkg.Ecosystem, pkg.PackageName, pkg.Version)
		doc.Components = append(doc.Components, cdxComponent{
			Type:    "library",
			Name:    pkg.PackageName,
			Version: pkg.Version,
			PURL:    purl,
			BomRef:  fmt.Sprintf("%s-%s-%s", pkg.Ecosystem, pkg.PackageName, pkg.Version),
		})
	}

	return json.MarshalIndent(doc, "", "  ")
}

// sanitizeSPDXID removes characters not allowed in SPDX identifiers.
func sanitizeSPDXID(s string) string {
	result := make([]byte, 0, len(s))
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '.' {
			result = append(result, c)
		} else {
			result = append(result, '-')
		}
	}
	return string(result)
}
