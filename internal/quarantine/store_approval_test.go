package quarantine

import (
	"context"
	"testing"

	"depsilo/internal/db"
)

func TestApprovalUsesDialectIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		ecosystem       string
		approvedPackage string
		approvedVersion string
		requestPackage  string
		requestVersion  string
		storedPackage   string
		storedVersion   string
	}{
		{
			name:            "PyPI package and version aliases",
			ecosystem:       "PyPI",
			approvedPackage: "My_Pkg",
			approvedVersion: "v1.0-1",
			requestPackage:  "my.pkg",
			requestVersion:  "1.0.post1",
			storedPackage:   "my-pkg",
			storedVersion:   "1.0.post1",
		},
		{
			name:            "NuGet package casing and padded release",
			ecosystem:       "NuGet",
			approvedPackage: "Newtonsoft.Json",
			approvedVersion: "1.0",
			requestPackage:  "newtonsoft.json",
			requestVersion:  "1.0.0+build.7",
			storedPackage:   "newtonsoft.json",
			storedVersion:   "1.0.0",
		},
		{
			name:            "SemVer build metadata has equal precedence",
			ecosystem:       "npm",
			approvedPackage: "left-pad",
			approvedVersion: "1.0.0+approved",
			requestPackage:  "left-pad",
			requestVersion:  "1.0.0+requested",
			storedPackage:   "left-pad",
			storedVersion:   "1.0.0+approved",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := NewStore(newLookupDB(t))
			if err := store.Approve(
				context.Background(), test.ecosystem, test.approvedPackage,
				test.approvedVersion, "reviewed by operator", 7,
			); err != nil {
				t.Fatalf("Approve: %v", err)
			}

			approved, err := store.IsApproved(
				context.Background(), test.ecosystem, test.requestPackage, test.requestVersion,
			)
			if err != nil {
				t.Fatalf("IsApproved: %v", err)
			}
			if !approved {
				t.Fatal("dialect-equivalent coordinate was not approved")
			}

			var row db.ApprovedVersion
			if err := store.db.First(&row).Error; err != nil {
				t.Fatalf("read approval: %v", err)
			}
			if row.Ecosystem != stringsLower(test.ecosystem) ||
				row.Package != test.storedPackage || row.Version != test.storedVersion {
				t.Fatalf("stored coordinate = %s:%s@%s, want %s:%s@%s",
					row.Ecosystem, row.Package, row.Version,
					stringsLower(test.ecosystem), test.storedPackage, test.storedVersion)
			}
		})
	}
}

func TestApproveCollapsesEquivalentVersions(t *testing.T) {
	t.Parallel()

	store := NewStore(newLookupDB(t))
	ctx := context.Background()
	if err := store.Approve(ctx, "npm", "left-pad", "1.0.0+first", "first review", 3); err != nil {
		t.Fatalf("first Approve: %v", err)
	}
	if err := store.Approve(ctx, "npm", "left-pad", "1.0.0+second", "second review", 9); err != nil {
		t.Fatalf("second Approve: %v", err)
	}

	var approvals []db.ApprovedVersion
	if err := store.db.Order("id ASC").Find(&approvals).Error; err != nil {
		t.Fatalf("read approvals: %v", err)
	}
	if len(approvals) != 1 {
		t.Fatalf("approval count = %d, want 1", len(approvals))
	}
	if approvals[0].Version != "1.0.0+second" || approvals[0].Reason != "second review" || approvals[0].ApprovedBy != 9 {
		t.Fatalf("approval was not refreshed with canonical latest input: %+v", approvals[0])
	}
}

func TestRevokeRemovesLegacyDialectAliases(t *testing.T) {
	t.Parallel()

	store := NewStore(newLookupDB(t))
	legacy := db.ApprovedVersion{
		Ecosystem: "PyPI", Package: "My_Pkg", Version: "v1.0-1",
		Reason: "legacy approval", ApprovedBy: 2,
	}
	if err := store.db.Create(&legacy).Error; err != nil {
		t.Fatalf("seed legacy approval: %v", err)
	}

	if err := store.Revoke(
		context.Background(), "pypi", "my.pkg", "1.0.post1", "approval no longer needed", 11,
	); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	var count int64
	if err := store.db.Model(&db.ApprovedVersion{}).Count(&count).Error; err != nil {
		t.Fatalf("count approvals: %v", err)
	}
	if count != 0 {
		t.Fatalf("approval count = %d, want 0", count)
	}
	var event db.QuarantineEvent
	if err := store.db.Where("action = ?", ActionRevoked).First(&event).Error; err != nil {
		t.Fatalf("read revoke event: %v", err)
	}
	if event.Ecosystem != "pypi" || event.Package != "my-pkg" || event.Version != "1.0.post1" {
		t.Fatalf("revoke event coordinate is not canonical: %+v", event)
	}
}

func TestApprovalRejectsInvalidCoordinate(t *testing.T) {
	t.Parallel()

	store := NewStore(newLookupDB(t))
	for _, test := range []struct {
		name, ecosystem, pkg, version string
	}{
		{name: "unsupported ecosystem", ecosystem: "unknown", pkg: "pkg", version: "1.0.0"},
		{name: "invalid npm package", ecosystem: "npm", pkg: "Bad Package", version: "1.0.0"},
		{name: "invalid npm version", ecosystem: "npm", pkg: "left-pad", version: "not-semver"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := store.Approve(
				context.Background(), test.ecosystem, test.pkg, test.version, "operator review", 1,
			); err == nil {
				t.Fatal("Approve succeeded, want coordinate validation error")
			}
		})
	}
}

func stringsLower(value string) string {
	if value == "PyPI" {
		return "pypi"
	}
	if value == "NuGet" {
		return "nuget"
	}
	return value
}
