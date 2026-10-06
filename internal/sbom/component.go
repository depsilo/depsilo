package sbom

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"depsilo/internal/db"
)

// Component is one package version recorded for a project, enriched with the
// artifact hashes and operator-declared compliance facts available for it.
type Component struct {
	Ecosystem string
	Name      string
	Version   string
	// Hashes are lowercase hex SHA-256 values of the artifacts fetched through
	// the proxy for this coordinate, sorted and deduplicated. A component whose
	// artifacts were never fetched through the cache has no hash.
	Hashes []string
	// Supplier and License come from operator annotations; empty means the
	// technical file reports the component as unknown instead of guessing.
	Supplier string
	License  string
}

// ComponentFacts is one operator-declared package fact.
type ComponentFacts struct {
	Supplier string
	License  string
}

// ComplianceProfile maps operator annotations onto component coordinates.
type ComplianceProfile struct {
	Organization string
	Contact      string
	entries      map[string]ComponentFacts
}

// NewComplianceProfile builds the lookup used by CRA exports. Keys are
// "<ecosystem>:<package>" or "<ecosystem>:<package>@<version>"; the
// version-specific entry wins.
func NewComplianceProfile(organization, contact string, entries map[string]ComponentFacts) ComplianceProfile {
	copied := make(map[string]ComponentFacts, len(entries))
	for key, facts := range entries {
		normalized := normalizeComplianceKey(key)
		if normalized == "" {
			continue
		}
		copied[normalized] = facts
	}
	return ComplianceProfile{Organization: organization, Contact: contact, entries: copied}
}

// Annotate fills supplier/license from the profile when the component does not
// already carry them. The version-specific entry wins per field, and a field
// it does not set falls back to the package-wide entry.
func (p ComplianceProfile) Annotate(component Component) Component {
	if len(p.entries) == 0 {
		return component
	}
	packageKey := strings.ToLower(component.Ecosystem) + ":" + component.Name
	exact := p.entries[packageKey+"@"+component.Version]
	base := p.entries[packageKey]
	if component.Supplier == "" {
		component.Supplier = exact.Supplier
		if component.Supplier == "" {
			component.Supplier = base.Supplier
		}
	}
	if component.License == "" {
		component.License = exact.License
		if component.License == "" {
			component.License = base.License
		}
	}
	return component
}

func normalizeComplianceKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return ""
	}
	ecosystem, rest, found := strings.Cut(key, ":")
	if !found || ecosystem == "" || rest == "" {
		return ""
	}
	return key
}

// Components loads the project's recorded package versions joined with the
// first-seen artifact hashes, then applies the operator compliance profile.
// A non-empty ecosystem limits the result to that adapter identity.
func (g *Generator) Components(ctx context.Context, projectID uint, ecosystem string, profile ComplianceProfile) ([]Component, error) {
	type row struct {
		Ecosystem   string
		PackageName string
		Version     string
		SHA256      *string `gorm:"column:sha256"`
	}
	query := g.db.WithContext(ctx).
		Model(&db.ProjectPackage{}).
		Select("project_packages.ecosystem, project_packages.package_name, project_packages.version, t.sha256").
		Joins("LEFT JOIN tamper_record AS t ON t.ecosystem = project_packages.ecosystem AND t.package = project_packages.package_name AND t.version = project_packages.version").
		Where("project_packages.project_id = ?", projectID)
	if ecosystem != "" {
		query = query.Where("project_packages.ecosystem = ?", ecosystem)
	}
	var rows []row
	if err := query.
		Order("project_packages.ecosystem, project_packages.package_name, project_packages.version, t.sha256").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load project components: %w", err)
	}

	components := make([]Component, 0, len(rows))
	hashesByComponent := make(map[string]map[string]bool)
	indexByComponent := make(map[string]int)
	for _, entry := range rows {
		key := entry.Ecosystem + "\x00" + entry.PackageName + "\x00" + entry.Version
		index, found := indexByComponent[key]
		if !found {
			index = len(components)
			indexByComponent[key] = index
			components = append(components, profile.Annotate(Component{
				Ecosystem: entry.Ecosystem,
				Name:      entry.PackageName,
				Version:   entry.Version,
			}))
		}
		if entry.SHA256 == nil || *entry.SHA256 == "" {
			continue
		}
		if hashesByComponent[key] == nil {
			hashesByComponent[key] = make(map[string]bool, 1)
		}
		hashesByComponent[key][strings.ToLower(*entry.SHA256)] = true
	}
	for key, set := range hashesByComponent {
		hashes := make([]string, 0, len(set))
		for hash := range set {
			hashes = append(hashes, hash)
		}
		sort.Strings(hashes)
		components[indexByComponent[key]].Hashes = hashes
	}
	return components, nil
}
