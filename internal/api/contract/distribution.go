package contract

import "time"

// DistributionContentStatus is what the last integrity check found for a
// dependency set's declared objects. It is not a provenance or install claim.
type DistributionContentStatus string

const (
	DistributionContentStatusStaged          DistributionContentStatus = "staged"
	DistributionContentStatusContentVerified DistributionContentStatus = "content_verified"
	DistributionContentStatusIncomplete      DistributionContentStatus = "incomplete"
)

// DistributionProvenanceStatus says who vouches for a dependency set: the
// local operator, or a trusted preparer whose signed bundle delivered it.
type DistributionProvenanceStatus string

const (
	DistributionProvenanceStatusOperatorDeclared DistributionProvenanceStatus = "operator_declared"
	DistributionProvenanceStatusPreparerAttested DistributionProvenanceStatus = "preparer_attested"
)

// DistributionObjectKind is the kind of a declared dependency-set object.
type DistributionObjectKind string

const (
	DistributionObjectKindWheel    DistributionObjectKind = "wheel"
	DistributionObjectKindMetadata DistributionObjectKind = "metadata"
)

// DistributionIssueCode is why a declared object failed verification.
type DistributionIssueCode string

const (
	DistributionIssueCodeMissing      DistributionIssueCode = "missing"
	DistributionIssueCodeSizeMismatch DistributionIssueCode = "size_mismatch"
	DistributionIssueCodeHashMismatch DistributionIssueCode = "hash_mismatch"
)

// DistributionTarget is a declared preparation environment, not a
// qualification result.
type DistributionTarget struct {
	OS            string `json:"os"`
	Architecture  string `json:"architecture"`
	PythonVersion string `json:"python_version"`
	PythonABI     string `json:"python_abi"`
	PipVersion    string `json:"pip_version"`
	ClientImage   string `json:"client_image"`
}

// DistributionObject is one content-addressed object a set declares.
type DistributionObject struct {
	SHA256    string                 `json:"sha256"`
	Size      int64                  `json:"size"`
	Kind      DistributionObjectKind `json:"kind"`
	Filename  string                 `json:"filename"`
	Package   string                 `json:"package"`
	Version   string                 `json:"version"`
	SourceURL string                 `json:"source_url,omitempty"`
}

// DistributionManifest is a set's immutable declaration; the set ID is the
// SHA-256 of its exact bytes.
type DistributionManifest struct {
	Format    string               `json:"format"`
	Name      string               `json:"name"`
	Revision  string               `json:"revision"`
	Ecosystem string               `json:"ecosystem"`
	Target    DistributionTarget   `json:"target"`
	Objects   []DistributionObject `json:"objects"`
}

// DistributionProvenance identifies the trusted preparer whose signed
// transfer bundle delivered a set, as decided at import time.
type DistributionProvenance struct {
	Origin      string    `json:"origin"`
	SignerKeyID string    `json:"signer_key_id"`
	ImportedBy  string    `json:"imported_by"`
	ImportedAt  time.Time `json:"imported_at"`
}

// DistributionSet answers GET /admin/distribution/sets/:id and set staging.
// Provenance is present only for an imported set. No install qualification
// runs yet, so InstallValidation is always "not_run" and Serving is false.
type DistributionSet struct {
	ID                string                       `json:"id"`
	Name              string                       `json:"name"`
	Revision          string                       `json:"revision"`
	CreatedAt         time.Time                    `json:"created_at"`
	VerifiedAt        *time.Time                   `json:"verified_at" tstype:"string | null,required"`
	CheckedAt         *time.Time                   `json:"checked_at" tstype:"string | null,required"`
	Manifest          DistributionManifest         `json:"manifest"`
	ContentStatus     DistributionContentStatus    `json:"content_status"`
	ProvenanceStatus  DistributionProvenanceStatus `json:"provenance_status"`
	InstallValidation string                       `json:"install_validation" tstype:"'not_run'"`
	Serving           bool                         `json:"serving"`
	Provenance        *DistributionProvenance      `json:"provenance,omitempty"`
}

// DistributionSetSummary is one set in the catalog list, without its object
// declarations.
type DistributionSetSummary struct {
	ID                string                       `json:"id"`
	Name              string                       `json:"name"`
	Revision          string                       `json:"revision"`
	CreatedAt         time.Time                    `json:"created_at"`
	VerifiedAt        *time.Time                   `json:"verified_at" tstype:"string | null,required"`
	CheckedAt         *time.Time                   `json:"checked_at" tstype:"string | null,required"`
	ContentStatus     DistributionContentStatus    `json:"content_status"`
	ProvenanceStatus  DistributionProvenanceStatus `json:"provenance_status"`
	InstallValidation string                       `json:"install_validation" tstype:"'not_run'"`
	Serving           bool                         `json:"serving"`
	Target            DistributionTarget           `json:"target"`
	ObjectCount       int                          `json:"object_count"`
}

// DistributionStorageUsage is the catalog's retained content against its
// storage quota.
type DistributionStorageUsage struct {
	Bytes      int64 `json:"bytes"`
	QuotaBytes int64 `json:"quota_bytes"`
	Objects    int64 `json:"objects"`
	Sets       int64 `json:"sets"`
}

// DistributionSetListResponse answers GET /admin/distribution/sets. The
// catalog prepares sets but serves no package routes: Scope is always
// "catalog_only" and Serving is false.
type DistributionSetListResponse struct {
	Items   []DistributionSetSummary `json:"items"`
	Total   int64                    `json:"total"`
	Page    int                      `json:"page"`
	PerPage int                      `json:"per_page"`
	Usage   DistributionStorageUsage `json:"usage"`
	Scope   string                   `json:"scope" tstype:"'catalog_only'"`
	Serving bool                     `json:"serving"`
}

// DistributionUploadResult answers PUT
// /admin/distribution/sets/:id/objects/:digest. Reused means the verified
// object was already stored.
type DistributionUploadResult struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Reused bool   `json:"reused"`
}

// DistributionIssue is one declared object that failed verification.
type DistributionIssue struct {
	SHA256 string                `json:"sha256"`
	Code   DistributionIssueCode `json:"code"`
}

// DistributionVerification answers POST /admin/distribution/sets/:id/verify.
type DistributionVerification struct {
	SetID             string                       `json:"set_id"`
	ContentStatus     DistributionContentStatus    `json:"content_status"`
	VerifiedObjects   int                          `json:"verified_objects"`
	Issues            []DistributionIssue          `json:"issues"`
	CheckedAt         time.Time                    `json:"checked_at"`
	ProvenanceStatus  DistributionProvenanceStatus `json:"provenance_status"`
	InstallValidation string                       `json:"install_validation" tstype:"'not_run'"`
	Serving           bool                         `json:"serving"`
}

// DistributionPruneResponse answers POST /admin/distribution/prune.
type DistributionPruneResponse struct {
	RemovedBytes int64  `json:"removed_bytes"`
	Scope        string `json:"scope" tstype:"'catalog_only'"`
	Serving      bool   `json:"serving"`
}

// DistributionExportUsage answers GET /admin/distribution/usage: the current
// UTC month's transfer-bundle export allowance. The limits are absent when
// exports are unlimited; ResetsAt is null when exports are not metered at all.
type DistributionExportUsage struct {
	Unlimited    bool       `json:"unlimited"`
	BundleLimit  int64      `json:"bundle_limit_bytes,omitempty"`
	MonthlyLimit int64      `json:"monthly_limit_bytes,omitempty"`
	UsedBytes    int64      `json:"used_bytes"`
	ResetsAt     *time.Time `json:"resets_at" tstype:"string | null,required"`
}

// DistributionImportResult answers POST /admin/distribution/import. A failed
// import whose signed identity is known carries it under the error body's
// "import" key, so the attempt can be inspected and retried.
type DistributionImportResult struct {
	SetID          string                    `json:"set_id"`
	Name           string                    `json:"name"`
	Revision       string                    `json:"revision"`
	SignerKeyID    string                    `json:"signer_key_id"`
	ObjectCount    int                       `json:"object_count"`
	WrittenObjects int                       `json:"written_objects"`
	ReusedObjects  int                       `json:"reused_objects"`
	Verification   *DistributionVerification `json:"verification,omitempty"`
	Set            *DistributionSet          `json:"set,omitempty"`
}
