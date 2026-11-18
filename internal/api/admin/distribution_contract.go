package admin

import (
	"depsilo/internal/api/contract"
	"depsilo/internal/distribution"
)

func toDistributionTarget(target distribution.Target) contract.DistributionTarget {
	return contract.DistributionTarget{
		OS: target.OS, Architecture: target.Architecture, PythonVersion: target.PythonVersion,
		PythonABI: target.PythonABI, PipVersion: target.PipVersion, ClientImage: target.ClientImage,
	}
}

func toDistributionManifest(manifest distribution.Manifest) contract.DistributionManifest {
	var objects []contract.DistributionObject
	if manifest.Objects != nil {
		objects = make([]contract.DistributionObject, len(manifest.Objects))
		for i, object := range manifest.Objects {
			objects[i] = contract.DistributionObject{
				SHA256: object.SHA256, Size: object.Size, Kind: contract.DistributionObjectKind(object.Kind),
				Filename: object.Filename, Package: object.Package, Version: object.Version, SourceURL: object.SourceURL,
			}
		}
	}
	return contract.DistributionManifest{
		Format: manifest.Format, Name: manifest.Name, Revision: manifest.Revision,
		Ecosystem: manifest.Ecosystem, Target: toDistributionTarget(manifest.Target), Objects: objects,
	}
}

func toDistributionSet(set *distribution.Set) *contract.DistributionSet {
	if set == nil {
		return nil
	}
	response := &contract.DistributionSet{
		ID: set.ID, Name: set.Name, Revision: set.Revision, CreatedAt: set.CreatedAt,
		VerifiedAt: set.VerifiedAt, CheckedAt: set.CheckedAt, Manifest: toDistributionManifest(set.Manifest),
		ContentStatus:     contract.DistributionContentStatus(set.ContentStatus),
		ProvenanceStatus:  contract.DistributionProvenanceStatus(set.ProvenanceStatus),
		InstallValidation: set.InstallValidation,
		Serving:           set.Serving,
	}
	if set.Provenance != nil {
		response.Provenance = &contract.DistributionProvenance{
			Origin: set.Provenance.Origin, SignerKeyID: set.Provenance.SignerKeyID,
			ImportedBy: set.Provenance.ImportedBy, ImportedAt: set.Provenance.ImportedAt,
		}
	}
	return response
}

// toDistributionSetSummary keeps list metadata small even when a manifest
// declares thousands of objects; the full declaration is available through Get.
func toDistributionSetSummary(set distribution.Set) contract.DistributionSetSummary {
	return contract.DistributionSetSummary{
		ID: set.ID, Name: set.Name, Revision: set.Revision, CreatedAt: set.CreatedAt,
		VerifiedAt: set.VerifiedAt, CheckedAt: set.CheckedAt,
		ContentStatus:     contract.DistributionContentStatus(set.ContentStatus),
		ProvenanceStatus:  contract.DistributionProvenanceStatus(set.ProvenanceStatus),
		InstallValidation: set.InstallValidation,
		Serving:           set.Serving, Target: toDistributionTarget(set.Manifest.Target),
		ObjectCount: len(set.Manifest.Objects),
	}
}

func toDistributionVerification(verification *distribution.Verification) *contract.DistributionVerification {
	if verification == nil {
		return nil
	}
	var issues []contract.DistributionIssue
	if verification.Issues != nil {
		issues = make([]contract.DistributionIssue, len(verification.Issues))
		for i, issue := range verification.Issues {
			issues[i] = contract.DistributionIssue{SHA256: issue.SHA256, Code: contract.DistributionIssueCode(issue.Code)}
		}
	}
	return &contract.DistributionVerification{
		SetID: verification.SetID, ContentStatus: contract.DistributionContentStatus(verification.ContentStatus),
		VerifiedObjects: verification.VerifiedObjects, Issues: issues, CheckedAt: verification.CheckedAt,
		ProvenanceStatus:  contract.DistributionProvenanceStatus(verification.ProvenanceStatus),
		InstallValidation: verification.InstallValidation,
		Serving:           verification.Serving,
	}
}

func toDistributionImportResult(result *distribution.ImportResult) contract.DistributionImportResult {
	return contract.DistributionImportResult{
		SetID: result.SetID, Name: result.Name, Revision: result.Revision, SignerKeyID: result.SignerKeyID,
		ObjectCount: result.ObjectCount, WrittenObjects: result.WrittenObjects, ReusedObjects: result.ReusedObjects,
		Verification: toDistributionVerification(result.Verification), Set: toDistributionSet(result.Set),
	}
}

func toDistributionExportUsage(usage distribution.ExportUsage) contract.DistributionExportUsage {
	return contract.DistributionExportUsage{
		Unlimited: usage.Unlimited, BundleLimit: usage.BundleLimit, MonthlyLimit: usage.MonthlyLimit,
		UsedBytes: usage.UsedBytes, ResetsAt: usage.ResetsAt,
	}
}
