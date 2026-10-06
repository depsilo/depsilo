package sbom

import (
	"fmt"
	"time"

	"depsilo/internal/db"
)

// TechnicalFileFormat identifies the manifest schema shipped by Depsilo.
const TechnicalFileFormat = "depsilo/cra-technical-file/v1"

// Coverage counts how many components carry each CRA minimum element.
type Coverage struct {
	Total        int `json:"components_total"`
	WithSHA256   int `json:"components_with_sha256"`
	WithSupplier int `json:"components_with_supplier"`
	WithLicense  int `json:"components_with_license"`
}

// SummarizeCoverage counts component completeness so a technical file makes
// its own gaps visible instead of implying full data.
func SummarizeCoverage(components []Component) Coverage {
	coverage := Coverage{Total: len(components)}
	for _, component := range components {
		if len(component.Hashes) > 0 {
			coverage.WithSHA256++
		}
		if component.Supplier != "" {
			coverage.WithSupplier++
		}
		if component.License != "" {
			coverage.WithLicense++
		}
	}
	return coverage
}

// Manufacturer is the operator-declared entity responsible for the product.
type Manufacturer struct {
	Organization string `json:"organization,omitempty"`
	Contact      string `json:"contact,omitempty"`
}

// Product identifies the project the technical file describes.
type Product struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
}

// DocumentInfo describes the embedded SBOM.
type DocumentInfo struct {
	Format         string `json:"format"`
	SpecVersion    string `json:"spec_version"`
	EcosystemScope string `json:"ecosystem_scope,omitempty"`
	ComponentCount int    `json:"component_count"`
}

// TechnicalFile is the CRA technical-file preset manifest. It is returned
// next to the SBOM document it describes.
type TechnicalFile struct {
	Format       string       `json:"format"`
	GeneratedAt  string       `json:"generated_at"`
	Product      Product      `json:"product"`
	Manufacturer Manufacturer `json:"manufacturer"`
	SBOM         DocumentInfo `json:"sbom"`
	Coverage     Coverage     `json:"coverage"`
	Relationship string       `json:"relationship_depth"`
	Limitations  []string     `json:"limitations"`
}

// BuildTechnicalFile assembles the manifest for one export.
func BuildTechnicalFile(
	project *db.Project,
	components []Component,
	options Options,
	sbomFormat, ecosystemScope string,
	generatedAt time.Time,
) TechnicalFile {
	specVersion := "SPDX-2.3"
	if sbomFormat == "cyclonedx" {
		specVersion = "1.5"
	}
	coverage := SummarizeCoverage(components)
	manifest := TechnicalFile{
		Format:      TechnicalFileFormat,
		GeneratedAt: generatedAt.UTC().Format(time.RFC3339),
		Product: Product{
			Name:        project.Name,
			Slug:        project.Slug,
			Description: project.Description,
			Version:     ProductVersion(generatedAt),
		},
		Manufacturer: Manufacturer{
			Organization: options.Profile.Organization,
			Contact:      options.Profile.Contact,
		},
		SBOM: DocumentInfo{
			Format:         sbomFormat,
			SpecVersion:    specVersion,
			EcosystemScope: ecosystemScope,
			ComponentCount: len(components),
		},
		Coverage:     coverage,
		Relationship: "top-level",
		Limitations:  technicalFileLimitations(coverage, options),
	}
	return manifest
}

func technicalFileLimitations(coverage Coverage, options Options) []string {
	limitations := []string{
		"dependency relationships are top-level: the product depends on every recorded component, and transitive edges are not inferred from download records",
		"SHA-256 values are the first-seen hashes of artifacts fetched through this proxy; components never fetched through the cache carry no hash",
	}
	if coverage.WithSupplier < coverage.Total || coverage.WithLicense < coverage.Total {
		limitations = append(limitations,
			"supplier and license values are operator-declared through [compliance.components]; components without an entry are exported as unknown (NOASSERTION)")
	}
	if options.Profile.Organization == "" {
		limitations = append(limitations, "compliance.organization is not configured; the manufacturer field of the technical file is empty")
	}
	if coverage.WithSHA256 < coverage.Total {
		limitations = append(limitations,
			fmt.Sprintf("%d of %d components have no recorded SHA-256; fetch them through the proxy or enable tamper detection before filing", coverage.Total-coverage.WithSHA256, coverage.Total))
	}
	return limitations
}
