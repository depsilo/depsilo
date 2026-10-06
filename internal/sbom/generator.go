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

// Options controls how much regulatory detail a generated document carries.
type Options struct {
	// CRA enables the NTIA/CRA minimum elements: per-component SHA-256
	// checksums, supplier/license slots, and explicit dependency
	// relationships from the product to each component.
	CRA bool
	// Profile supplies operator-declared supplier/license facts and the
	// manufacturer identity used by CRA exports.
	Profile ComplianceProfile
}

// Generator creates SBOM documents from project package data.
type Generator struct {
	db *gorm.DB
}

// NewGenerator creates a new SBOM generator.
func NewGenerator(database *gorm.DB) *Generator {
	return &Generator{db: database}
}

// ProductVersion is the point-in-time identifier Depsilo uses for a project
// that has no operator-declared release version.
func ProductVersion(now time.Time) string {
	return "snapshot-" + now.UTC().Format("2006-01-02")
}

type spdxExtRef struct {
	Category string `json:"referenceCategory"`
	Type     string `json:"referenceType"`
	Locator  string `json:"referenceLocator"`
}

type spdxChecksum struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"checksumValue"`
}

type spdxPackage struct {
	SPDXID           string         `json:"SPDXID"`
	Name             string         `json:"name"`
	Version          string         `json:"versionInfo"`
	DownloadLoc      string         `json:"downloadLocation"`
	Supplier         string         `json:"supplier"`
	LicenseConcluded string         `json:"licenseConcluded"`
	LicenseDeclared  string         `json:"licenseDeclared"`
	Checksums        []spdxChecksum `json:"checksums,omitempty"`
	ExternalRefs     []spdxExtRef   `json:"externalRefs"`
	Purpose          string         `json:"primaryPackagePurpose"`
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

// GenerateSPDX produces an SPDX 2.3 JSON document.
func (g *Generator) GenerateSPDX(project *db.Project, components []Component, options Options) ([]byte, error) {
	now := time.Now().UTC()
	doc := spdxDoc{
		Version:     "SPDX-2.3",
		DataLicense: "CC0-1.0",
		SPDXID:      "SPDXRef-DOCUMENT",
		Name:        project.Slug + "-sbom",
		Namespace:   fmt.Sprintf("https://depsilo.com/spdx/%s/%s", project.Slug, now.Format("2006-01-02")),
		CreationInfo: map[string]interface{}{
			"created":            now.Format(time.RFC3339),
			"creators":           []string{"Tool: Depsilo " + version.Version},
			"licenseListVersion": "3.22",
		},
		Packages:      make([]spdxPackage, 0, len(components)+1),
		Relationships: make([]spdxRelationship, 0, len(components)+1),
	}

	rootID := "SPDXRef-Package-product"
	if options.CRA {
		doc.Packages = append(doc.Packages, spdxPackage{
			SPDXID:           rootID,
			Name:             project.Name,
			Version:          ProductVersion(now),
			DownloadLoc:      "NOASSERTION",
			Supplier:         spdxSupplier(options.Profile.Organization),
			LicenseConcluded: "NOASSERTION",
			LicenseDeclared:  "NOASSERTION",
			Purpose:          "APPLICATION",
		})
		doc.Relationships = append(doc.Relationships, spdxRelationship{
			ElementID: "SPDXRef-DOCUMENT",
			Type:      "DESCRIBES",
			Related:   rootID,
		})
	}

	for _, component := range components {
		spdxID := fmt.Sprintf("SPDXRef-Package-%s-%s-%s", component.Ecosystem, sanitizeSPDXID(component.Name), component.Version)
		pkg := spdxPackage{
			SPDXID:      spdxID,
			Name:        component.Name,
			Version:     component.Version,
			DownloadLoc: "NOASSERTION",
			Supplier:    "NOASSERTION",
			ExternalRefs: []spdxExtRef{
				{Category: "PACKAGE-MANAGER", Type: "purl", Locator: FormatPURL(component.Ecosystem, component.Name, component.Version)},
			},
			Purpose: "LIBRARY",
		}
		if options.CRA {
			pkg.LicenseConcluded = spdxLicense(component.License)
			pkg.LicenseDeclared = spdxLicense(component.License)
			pkg.Supplier = spdxSupplier(component.Supplier)
			for _, hash := range component.Hashes {
				pkg.Checksums = append(pkg.Checksums, spdxChecksum{Algorithm: "SHA256", Value: hash})
			}
		}
		doc.Packages = append(doc.Packages, pkg)
		if options.CRA {
			doc.Relationships = append(doc.Relationships, spdxRelationship{
				ElementID: rootID,
				Type:      "DEPENDS_ON",
				Related:   spdxID,
			})
			continue
		}
		doc.Relationships = append(doc.Relationships, spdxRelationship{
			ElementID: "SPDXRef-DOCUMENT",
			Type:      "DESCRIBES",
			Related:   spdxID,
		})
	}

	return json.MarshalIndent(doc, "", "  ")
}

type cdxHash struct {
	Algorithm string `json:"alg"`
	Content   string `json:"content"`
}

type cdxLicenseEntry struct {
	Name string `json:"name"`
}

type cdxLicense struct {
	License cdxLicenseEntry `json:"license"`
}

type cdxSupplier struct {
	Name string `json:"name"`
}

type cdxComponent struct {
	Type     string       `json:"type"`
	Name     string       `json:"name"`
	Version  string       `json:"version"`
	PURL     string       `json:"purl"`
	BomRef   string       `json:"bom-ref"`
	Hashes   []cdxHash    `json:"hashes,omitempty"`
	Licenses []cdxLicense `json:"licenses,omitempty"`
	Supplier *cdxSupplier `json:"supplier,omitempty"`
}

type cdxTool struct {
	Vendor  string `json:"vendor"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type cdxProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type cdxDependency struct {
	Ref       string   `json:"ref"`
	DependsOn []string `json:"dependsOn"`
}

type cdxMetadata struct {
	Timestamp  string                 `json:"timestamp"`
	Tools      []cdxTool              `json:"tools"`
	Component  map[string]interface{} `json:"component"`
	Properties []cdxProperty          `json:"properties,omitempty"`
}

type cdxDoc struct {
	BomFormat    string          `json:"bomFormat"`
	SpecVersion  string          `json:"specVersion"`
	SerialNumber string          `json:"serialNumber"`
	Version      int             `json:"version"`
	Metadata     cdxMetadata     `json:"metadata"`
	Components   []cdxComponent  `json:"components"`
	Dependencies []cdxDependency `json:"dependencies,omitempty"`
}

// GenerateCycloneDX produces a CycloneDX 1.5 JSON document.
func (g *Generator) GenerateCycloneDX(project *db.Project, components []Component, options Options) ([]byte, error) {
	now := time.Now().UTC()
	rootComponent := map[string]interface{}{
		"type":    "application",
		"name":    project.Name,
		"version": ProductVersion(now),
	}
	doc := cdxDoc{
		BomFormat:    "CycloneDX",
		SpecVersion:  "1.5",
		SerialNumber: "urn:uuid:" + uuid.New().String(),
		Version:      1,
		Metadata: cdxMetadata{
			Timestamp: now.Format(time.RFC3339),
			Tools: []cdxTool{
				{Vendor: "Depsilo", Name: "Depsilo SBOM Generator", Version: version.Version},
			},
			Component: rootComponent,
		},
		Components: make([]cdxComponent, 0, len(components)),
	}

	rootRef := ""
	if options.CRA {
		rootRef = "urn:depsilo:product:" + project.Slug
		rootComponent["bom-ref"] = rootRef
		if options.Profile.Organization != "" {
			rootComponent["supplier"] = map[string]string{"name": options.Profile.Organization}
		}
		doc.Metadata.Properties = craMetadataProperties(project, components, options)
		doc.Dependencies = []cdxDependency{{Ref: rootRef, DependsOn: make([]string, 0, len(components))}}
	}

	for _, component := range components {
		entry := cdxComponent{
			Type:    "library",
			Name:    component.Name,
			Version: component.Version,
			PURL:    FormatPURL(component.Ecosystem, component.Name, component.Version),
		}
		if options.CRA {
			entry.BomRef = entry.PURL
			for _, hash := range component.Hashes {
				entry.Hashes = append(entry.Hashes, cdxHash{Algorithm: "SHA-256", Content: hash})
			}
			if component.Supplier != "" {
				entry.Supplier = &cdxSupplier{Name: component.Supplier}
			}
			if component.License != "" {
				license := cdxLicense{}
				license.License.Name = component.License
				entry.Licenses = []cdxLicense{license}
			}
			doc.Dependencies[0].DependsOn = append(doc.Dependencies[0].DependsOn, entry.BomRef)
		} else {
			entry.BomRef = fmt.Sprintf("%s-%s-%s", component.Ecosystem, component.Name, component.Version)
		}
		doc.Components = append(doc.Components, entry)
	}

	return json.MarshalIndent(doc, "", "  ")
}

func craMetadataProperties(project *db.Project, components []Component, options Options) []cdxProperty {
	coverage := SummarizeCoverage(components)
	properties := make([]cdxProperty, 0, 8)
	if options.Profile.Organization != "" {
		properties = append(properties, cdxProperty{Name: "depsilo:cra:organization", Value: options.Profile.Organization})
	}
	if options.Profile.Contact != "" {
		properties = append(properties, cdxProperty{Name: "depsilo:cra:contact", Value: options.Profile.Contact})
	}
	properties = append(properties,
		cdxProperty{Name: "depsilo:cra:product-slug", Value: project.Slug},
		cdxProperty{Name: "depsilo:cra:relationship-depth", Value: "top-level"},
		cdxProperty{Name: "depsilo:cra:components-total", Value: fmt.Sprintf("%d", coverage.Total)},
		cdxProperty{Name: "depsilo:cra:components-with-sha256", Value: fmt.Sprintf("%d", coverage.WithSHA256)},
		cdxProperty{Name: "depsilo:cra:components-with-supplier", Value: fmt.Sprintf("%d", coverage.WithSupplier)},
		cdxProperty{Name: "depsilo:cra:components-with-license", Value: fmt.Sprintf("%d", coverage.WithLicense)},
	)
	return properties
}

func spdxSupplier(value string) string {
	if value == "" {
		return "NOASSERTION"
	}
	return "Organization: " + value
}

func spdxLicense(value string) string {
	if value == "" {
		return "NOASSERTION"
	}
	return value
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
