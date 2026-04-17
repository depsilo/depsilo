# SBOM Generation

## Overview

Generate Software Bill of Materials (SBOM) documents from project download records. Supports both SPDX 2.3 and CycloneDX 1.5 formats. Exports via API endpoint and Web UI button. Pro feature.

**Depends on:** Project Management & Download Tracking (Spec A must be implemented first).

## SBOM Formats

### SPDX 2.3 (JSON)

SPDX is the Linux Foundation standard, required by US federal procurement (EO 14028).

Output structure:
```json
{
  "spdxVersion": "SPDX-2.3",
  "dataLicense": "CC0-1.0",
  "SPDXID": "SPDXRef-DOCUMENT",
  "name": "ai-platform-sbom",
  "documentNamespace": "https://depsilo.com/spdx/ai-platform/2026-04-17",
  "creationInfo": {
    "created": "2026-04-17T10:00:00Z",
    "creators": ["Tool: Depsilo"],
    "licenseListVersion": "3.22"
  },
  "packages": [
    {
      "SPDXID": "SPDXRef-Package-pypi-requests-2.31.0",
      "name": "requests",
      "versionInfo": "2.31.0",
      "downloadLocation": "https://pypi.org/project/requests/2.31.0/",
      "supplier": "NOASSERTION",
      "externalRefs": [
        {
          "referenceCategory": "PACKAGE-MANAGER",
          "referenceType": "purl",
          "referenceLocator": "pkg:pypi/requests@2.31.0"
        }
      ],
      "primaryPackagePurpose": "LIBRARY"
    }
  ],
  "relationships": [
    {
      "spdxElementId": "SPDXRef-DOCUMENT",
      "relationshipType": "DESCRIBES",
      "relatedSpdxElement": "SPDXRef-Package-pypi-requests-2.31.0"
    }
  ]
}
```

### CycloneDX 1.5 (JSON)

CycloneDX is the OWASP standard, popular in security-focused organizations.

Output structure:
```json
{
  "bomFormat": "CycloneDX",
  "specVersion": "1.5",
  "serialNumber": "urn:uuid:generated-uuid",
  "version": 1,
  "metadata": {
    "timestamp": "2026-04-17T10:00:00Z",
    "tools": [
      {
        "vendor": "Depsilo",
        "name": "Depsilo SBOM Generator",
        "version": "0.3.0"
      }
    ],
    "component": {
      "type": "application",
      "name": "ai-platform",
      "version": "snapshot-2026-04-17"
    }
  },
  "components": [
    {
      "type": "library",
      "name": "requests",
      "version": "2.31.0",
      "purl": "pkg:pypi/requests@2.31.0",
      "bom-ref": "pypi-requests-2.31.0"
    }
  ]
}
```

## Package URL (purl)

Both formats use Package URL (purl) as the canonical package identifier. Mapping:

| Ecosystem | purl Format |
|-----------|-------------|
| pypi | `pkg:pypi/{name}@{version}` |
| npm | `pkg:npm/{name}@{version}` or `pkg:npm/%40{scope}/{name}@{version}` |
| go | `pkg:golang/{module}@{version}` |
| cargo | `pkg:cargo/{name}@{version}` |
| maven | `pkg:maven/{group}/{artifact}@{version}` |
| rubygems | `pkg:gem/{name}@{version}` |
| composer | `pkg:composer/{vendor}/{name}@{version}` |
| nuget | `pkg:nuget/{name}@{version}` |
| conda | `pkg:conda/{name}@{version}` |
| cran | `pkg:cran/{name}@{version}` |
| helm | `pkg:helm/{name}@{version}` |
| apt | `pkg:deb/debian/{name}@{version}` |
| docker | `pkg:docker/{name}@{version}` |

Packages with empty version use `@unknown` in purl.

## Backend

### Package: `internal/sbom/`

#### generator.go

Core SBOM generation logic.

```go
type Generator struct {
    db *gorm.DB
}

func (g *Generator) GenerateSPDX(project *db.Project, packages []db.ProjectPackage) ([]byte, error)
func (g *Generator) GenerateCycloneDX(project *db.Project, packages []db.ProjectPackage) ([]byte, error)
```

Both methods:
1. Query ProjectPackage records for the project
2. Convert each record to the appropriate format with purl
3. Serialize to JSON
4. Return the bytes

#### purl.go

Package URL generation.

```go
func FormatPURL(ecosystem, packageName, version string) string
```

Maps Depsilo ecosystem names to purl types and formats the identifier.

### API Endpoint

Add to the project admin routes (Pro):

```
GET /api/v1/admin/projects/:id/sbom?format=spdx|cyclonedx
```

Query parameters:
- `format`: `spdx` (default) or `cyclonedx`
- `ecosystem`: optional filter (e.g., `pypi` — only include Python packages)

Response:
- Content-Type: `application/json`
- Content-Disposition: `attachment; filename="{slug}-sbom-{date}.{format}.json"`
- Body: the SBOM JSON document

### Vulnerability Enrichment

If the security intelligence feature is enabled (Vulnerability table has data), SBOM entries are enriched:

**SPDX:** Add `externalRefs` with `SECURITY` category pointing to OSV IDs.

**CycloneDX:** Add `vulnerabilities` array at the document level, cross-referencing components by `bom-ref`.

This enrichment is best-effort — if no vulnerability data exists, the SBOM is still valid without it.

## Frontend

### Project Detail Page Enhancement

On the project detail page (`/admin/projects/:id`), add an "Export SBOM" section:

- Dropdown select: SPDX / CycloneDX
- Optional: ecosystem filter select (All / pypi / npm / ...)
- "Download SBOM" button
- Below: package count summary ("142 packages across 3 ecosystems")

### Implementation

```typescript
// api.ts
exportSbom: (projectId: number, params: { format: string; ecosystem?: string }) =>
  api.get(`/admin/projects/${projectId}/sbom`, {
    params,
    responseType: 'blob',
  }),
```

Button click triggers download via blob URL.

## i18n Keys

Add to both `en.ts` and `zh.ts`:

```
sbom.export / 导出 SBOM
sbom.format / 格式
sbom.spdx / SPDX 2.3
sbom.cyclonedx / CycloneDX 1.5
sbom.download / 下载 SBOM
sbom.filterEcosystem / 筛选生态
sbom.allEcosystems / 全部生态
sbom.packageSummary / %d 个包，%d 个生态 / %d packages across %d ecosystems
sbom.generating / 正在生成...
sbom.includeVulnerabilities / 包含漏洞信息
```

## Files Changed

| File | Action |
|------|--------|
| `internal/sbom/generator.go` | Create: SPDX + CycloneDX generation |
| `internal/sbom/purl.go` | Create: Package URL formatting |
| `internal/api/admin/projects.go` | Add: ExportSBOM handler |
| `web/src/admin/pages/Projects.tsx` | Add: SBOM export UI on detail page |
| `web/src/lib/api.ts` | Add: exportSbom method |
| `web/src/i18n/en.ts` + `zh.ts` | Add: sbom i18n keys |

## Scope Boundaries

- JSON output only (no SPDX tag-value or CycloneDX XML)
- No SBOM signing or integrity verification
- No SBOM diff (comparing two snapshots)
- No automated SBOM push to external systems
- No license field population (needs separate license scanning feature)
- Vulnerability enrichment is optional and best-effort
- All packages are listed as direct dependencies (DESCRIBES relationship) — no dependency tree
