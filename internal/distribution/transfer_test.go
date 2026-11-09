package distribution_test

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"depsilo/internal/db"
	"depsilo/internal/distribution"
)

func newTransferKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func publicOf(key ed25519.PrivateKey) ed25519.PublicKey { return key.Public().(ed25519.PublicKey) }

type transferFixture struct {
	source, target *catalogFixture
	signer         ed25519.PrivateKey
	set            *distribution.Set
	objects        []distribution.Object
	contents       [][]byte
}

func newTransferFixture(t *testing.T) *transferFixture {
	t.Helper()
	f := &transferFixture{source: newCatalogFixture(t, distribution.Limits{}), target: newCatalogFixture(t, distribution.Limits{}), signer: newTransferKey(t)}
	f.source.store.ConfigureTransfer(distribution.TransferKeys{Signer: f.signer})
	f.target.store.ConfigureTransfer(distribution.TransferKeys{Trusted: []ed25519.PublicKey{publicOf(f.signer)}})
	// One member exceeds a tar block and one is block-aligned to exercise padding.
	f.contents = [][]byte{bytes.Repeat([]byte("wheel-a "), 100), bytes.Repeat([]byte("b"), 512), []byte("small")}
	for i, data := range f.contents {
		f.objects = append(f.objects, contentObject(data, []string{"alpha", "beta", "gamma"}[i]))
	}
	f.set = stageSet(t, f.source, "python", "v2", f.objects...)
	for i, o := range f.objects {
		putContent(t, f.source, f.set, o, f.contents[i])
	}
	if _, err := f.source.store.Verify(context.Background(), f.set.ID); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *transferFixture) bundle(t *testing.T) []byte {
	t.Helper()
	export, err := f.source.store.PrepareExport(context.Background(), f.set.ID)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := export.Write(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	if int64(out.Len()) != export.Size {
		t.Fatalf("export wrote %d bytes, declared %d", out.Len(), export.Size)
	}
	return out.Bytes()
}

func countRows(t *testing.T, f *catalogFixture, model any) int64 {
	t.Helper()
	var n int64
	if err := f.db.Model(model).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTransferRoundTripPreservesIdentityAndRecordsSigner(t *testing.T) {
	f := newTransferFixture(t)
	raw := f.bundle(t)
	summary, err := distribution.InspectBundle(context.Background(), bytes.NewReader(raw))
	if err != nil || summary.SetID != f.set.ID || summary.ObjectCount != 3 || summary.SignatureVerified || summary.BundleBytes != int64(len(raw)) ||
		summary.SignerKeyID != distribution.KeyID(publicOf(f.signer)) {
		t.Fatalf("inspect: %+v %v", summary, err)
	}
	result, err := f.target.store.Import(context.Background(), bytes.NewReader(raw), "importer")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.SetID != f.set.ID || result.WrittenObjects != 3 || result.ReusedObjects != 0 || result.Verification.ContentStatus != "content_verified" {
		t.Fatalf("unexpected import result: %+v", result)
	}
	set := result.Set
	if set.ProvenanceStatus != "preparer_attested" || set.Provenance == nil || set.Provenance.SignerKeyID != summary.SignerKeyID ||
		set.Provenance.ImportedBy != "importer" || set.Serving || set.InstallValidation != "not_run" {
		t.Fatalf("imported set view: %+v", set)
	}
	for i, o := range f.objects {
		assertStoredContent(t, f.target, o, f.contents[i])
	}
	listed, _, err := f.target.store.List(context.Background(), 10, 0)
	if err != nil || len(listed) != 1 || listed[0].ProvenanceStatus != "preparer_attested" {
		t.Fatalf("list provenance: %+v %v", listed, err)
	}
	again, err := f.target.store.Import(context.Background(), bytes.NewReader(raw), "importer")
	if err != nil || again.ReusedObjects != 3 || again.WrittenObjects != 0 {
		t.Fatalf("repeat import did not reuse content: %+v %v", again, err)
	}
	// Re-exporting an imported set is signed by the re-exporting instance.
	f.target.store.ConfigureTransfer(distribution.TransferKeys{Signer: newTransferKey(t)})
	if _, err := f.target.store.PrepareExport(context.Background(), f.set.ID); err != nil {
		t.Fatalf("re-export imported set: %v", err)
	}
	if err := f.target.store.Delete(context.Background(), f.set.ID); err != nil {
		t.Fatal(err)
	}
	if countRows(t, f.target, &db.DistributionSetProvenance{}) != 0 {
		t.Fatal("delete retained provenance for a removed set")
	}
}

func TestImportNeverChangesTheActiveRevision(t *testing.T) {
	f := newTransferFixture(t)
	old := contentObject([]byte("previous revision"), "previous")
	previous := stageSet(t, f.target, "python", "v1", old)
	putContent(t, f.target, previous, old, []byte("previous revision"))
	if _, err := f.target.store.Activate(context.Background(), previous.ID); err != nil {
		t.Fatal(err)
	}
	raw := f.bundle(t)
	if _, err := f.target.store.Import(context.Background(), bytes.NewReader(raw[:len(raw)/2]), "importer"); err == nil {
		t.Fatal("truncated import succeeded")
	}
	if _, err := f.target.store.Import(context.Background(), bytes.NewReader(raw), "importer"); err != nil {
		t.Fatal(err)
	}
	active, err := f.target.store.Active(context.Background(), "python")
	if err != nil || active.ID != previous.ID {
		t.Fatalf("import changed the active revision: %+v %v", active, err)
	}
}

func TestImportRejectsUntrustedOrTamperedSignaturesBeforeWriting(t *testing.T) {
	f := newTransferFixture(t)
	raw := f.bundle(t)
	cases := map[string]struct {
		trusted []ed25519.PublicKey
		bundle  []byte
	}{
		"no trusted keys":  {nil, raw},
		"different signer": {[]ed25519.PublicKey{publicOf(newTransferKey(t))}, raw},
		"modified manifest": {[]ed25519.PublicKey{publicOf(f.signer)}, rewriteBundle(t, raw, nil, func(m []member) []member {
			m[0].data = bytes.Replace(m[0].data, []byte(`"python"`), []byte(`"pythoN"`), 1)
			return m
		})},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			target := newCatalogFixture(t, distribution.Limits{})
			target.store.ConfigureTransfer(distribution.TransferKeys{Trusted: tc.trusted})
			result, err := target.store.Import(context.Background(), bytes.NewReader(tc.bundle), "importer")
			if !errors.Is(err, distribution.ErrUntrusted) || result != nil {
				t.Fatalf("import = %+v, %v", result, err)
			}
			if countRows(t, target, &db.DistributionSet{}) != 0 || countRows(t, target, &db.DistributionObject{}) != 0 {
				t.Fatal("untrusted bundle mutated the catalog")
			}
		})
	}
}

func TestImportRecoversFromCorruptOrInterruptedContent(t *testing.T) {
	f := newTransferFixture(t)
	raw := f.bundle(t)
	corrupt := rewriteBundle(t, raw, f.signer, func(m []member) []member {
		m[4].data = append([]byte(nil), m[4].data...)
		m[4].data[0] ^= 0xff
		return m
	})
	result, err := f.target.store.Import(context.Background(), bytes.NewReader(corrupt), "importer")
	if !errors.Is(err, distribution.ErrIntegrity) || result == nil || result.SetID != f.set.ID {
		t.Fatalf("corrupt import = %+v, %v", result, err)
	}
	set, err := f.target.store.Get(context.Background(), f.set.ID)
	if err != nil || set.ContentStatus == "content_verified" {
		t.Fatalf("corrupt import exposed a verified set: %+v %v", set, err)
	}
	if set.ProvenanceStatus != "operator_declared" || set.Provenance != nil || countRows(t, f.target, &db.DistributionSetProvenance{}) != 0 {
		t.Fatalf("failed import was attributed to the signer: %+v", set)
	}
	truncated := raw[:len(raw)-1024]
	if _, err := f.target.store.Import(context.Background(), bytes.NewReader(truncated), "importer"); !errors.Is(err, distribution.ErrBundle) {
		t.Fatalf("import without end marker = %v", err)
	}
	if countRows(t, f.target, &db.DistributionSetProvenance{}) != 0 {
		t.Fatal("import missing its end marker recorded provenance")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.target.store.Import(ctx, bytes.NewReader(raw), "importer"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled import error = %v", err)
	}
	result, err = f.target.store.Import(context.Background(), bytes.NewReader(raw), "importer")
	if err != nil || result.ReusedObjects != 3 || result.WrittenObjects != 0 || result.Verification.ContentStatus != "content_verified" ||
		result.Verification.ProvenanceStatus != "preparer_attested" || result.Set.ProvenanceStatus != "preparer_attested" {
		t.Fatalf("retry after corruption: %+v %v", result, err)
	}
}

func TestImportRejectsMalformedArchives(t *testing.T) {
	f := newTransferFixture(t)
	raw := f.bundle(t)
	mutate := func(fn func([]member) []member) []byte { return rewriteBundle(t, raw, f.signer, fn) }
	transfer := func(fn func(map[string]any)) []byte {
		return mutate(func(m []member) []member {
			var value map[string]any
			if err := json.Unmarshal(m[0].data, &value); err != nil {
				t.Fatal(err)
			}
			fn(value)
			m[0].data, _ = json.Marshal(value)
			return m
		})
	}
	cases := map[string][]byte{
		"reordered objects": mutate(func(m []member) []member { m[3], m[4] = m[4], m[3]; return m }),
		"missing object":    mutate(func(m []member) []member { return m[:5] }),
		"duplicate object":  mutate(func(m []member) []member { return append(m, m[5]) }),
		"undeclared member": mutate(func(m []member) []member { return append(m, member{name: "objects/extra", data: []byte("x")}) }),
		"traversal name":    mutate(func(m []member) []member { m[3].name = "../" + m[3].name; return m }),
		"absolute name":     mutate(func(m []member) []member { m[3].name = "/" + m[3].name; return m }),
		"symlink member": mutate(func(m []member) []member {
			m[3].typeflag, m[3].linkname, m[3].data = tar.TypeSymlink, "/etc/passwd", nil
			return m
		}),
		"directory member": mutate(func(m []member) []member {
			return append(m[:3:3], append([]member{{name: "objects/", typeflag: tar.TypeDir}}, m[3:]...)...)
		}),
		"pax records":        mutate(func(m []member) []member { m[3].pax = map[string]string{"comment": "x"}; return m }),
		"short object":       mutate(func(m []member) []member { m[5].data = m[5].data[:2]; return m }),
		"set hash differs":   mutate(func(m []member) []member { m[2].data = append(append([]byte(nil), m[2].data...), ' '); return m }),
		"unknown format":     transfer(func(v map[string]any) { v["format"] = "depsilo/transfer-bundle/v9" }),
		"unknown capability": transfer(func(v map[string]any) { v["required_capabilities"] = []string{"incremental"} }),
		"unknown field":      transfer(func(v map[string]any) { v["baseline"] = "x" }),
		"wrong count":        transfer(func(v map[string]any) { v["object_count"] = 2 }),
		"oversized transfer manifest": mutate(func(m []member) []member {
			m[0].data = append(append([]byte(nil), m[0].data...), bytes.Repeat([]byte(" "), distribution.MaxTransferManifestBytes)...)
			return m
		}),
		"empty set manifest": mutate(func(m []member) []member { m[2].data = nil; return m }),
		"trailing data":      append(append([]byte(nil), raw...), []byte("garbage")...),
		"missing end marker": raw[:len(raw)-1024],
		"partial end marker": raw[:len(raw)-512],
		"not a tar":          []byte("plain text, not a bundle"),
	}
	for name, bundle := range cases {
		t.Run(name, func(t *testing.T) {
			target := newCatalogFixture(t, distribution.Limits{})
			target.store.ConfigureTransfer(distribution.TransferKeys{Trusted: []ed25519.PublicKey{publicOf(f.signer)}})
			result, err := target.store.Import(context.Background(), bytes.NewReader(bundle), "importer")
			if err == nil {
				t.Fatal("malformed bundle imported")
			}
			if !errors.Is(err, distribution.ErrBundle) && !errors.Is(err, distribution.ErrIntegrity) {
				t.Fatalf("unexpected error class: %v", err)
			}
			if result != nil && result.Verification != nil && result.Verification.ContentStatus == "content_verified" {
				t.Fatalf("malformed bundle reached verified state: %+v", result)
			}
			if _, err := distribution.InspectBundle(context.Background(), bytes.NewReader(bundle)); err == nil {
				t.Fatal("inspect accepted a malformed bundle")
			}
		})
	}
	padded := append(append([]byte(nil), raw...), make([]byte, 9*512)...)
	if _, err := f.target.store.Import(context.Background(), bytes.NewReader(padded), "importer"); err != nil {
		t.Fatalf("conventional zero record padding rejected: %v", err)
	}
}

func TestImportEnforcesTargetQuotaBeforeWritingContent(t *testing.T) {
	f := newTransferFixture(t)
	target := newCatalogFixture(t, distribution.Limits{MaxObjectBytes: 600, MaxStorageBytes: 1000})
	target.store.ConfigureTransfer(distribution.TransferKeys{Trusted: []ed25519.PublicKey{publicOf(f.signer)}})
	if _, err := target.store.Import(context.Background(), bytes.NewReader(f.bundle(t)), "importer"); !errors.Is(err, distribution.ErrQuota) {
		t.Fatalf("import over quota = %v", err)
	}
	if countRows(t, target, &db.DistributionObject{}) != 0 || countRows(t, target, &db.DistributionSet{}) != 0 {
		t.Fatal("over-quota import wrote content or catalog rows")
	}
}

func TestExportRequiresSignerAndVerifiedContent(t *testing.T) {
	f := newTransferFixture(t)
	f.source.store.ConfigureTransfer(distribution.TransferKeys{})
	if _, err := f.source.store.PrepareExport(context.Background(), f.set.ID); !errors.Is(err, distribution.ErrSigningUnavailable) {
		t.Fatalf("export without signer = %v", err)
	}
	f.source.store.ConfigureTransfer(distribution.TransferKeys{Signer: f.signer})
	staged := stageSet(t, f.source, "python", "v3", contentObject([]byte("never uploaded"), "missing"))
	if _, err := f.source.store.PrepareExport(context.Background(), staged.ID); !errors.Is(err, distribution.ErrIncomplete) {
		t.Fatalf("export of unverified set = %v", err)
	}
	if _, err := f.source.store.PrepareExport(context.Background(), strings.Repeat("0", 64)); !errors.Is(err, distribution.ErrNotFound) {
		t.Fatalf("export of unknown set = %v", err)
	}
	export, err := f.source.store.PrepareExport(context.Background(), f.set.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Corruption after the size precheck aborts the stream short of Size.
	overwriteContent(t, f.source, f.objects[1], bytes.Repeat([]byte("c"), 512))
	var out bytes.Buffer
	if err := export.Write(context.Background(), &out); !errors.Is(err, distribution.ErrIntegrity) || int64(out.Len()) >= export.Size {
		t.Fatalf("corrupt export = %v after %d/%d bytes", err, out.Len(), export.Size)
	}
}

func TestTransferKeysRoundTripAndRejectAmbiguity(t *testing.T) {
	pemBytes, public, err := distribution.GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "transfer.pem")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := distribution.LoadSigningKey(path)
	if err != nil || !publicOf(loaded).Equal(public) {
		t.Fatalf("load generated key: %v", err)
	}
	parsed, err := distribution.ParseTrustedKeys([]string{distribution.FormatPublicKey(public)})
	if err != nil || len(parsed) != 1 || !parsed[0].Equal(public) {
		t.Fatalf("parse formatted key: %v", err)
	}
	for name, values := range map[string][]string{
		"duplicate":  {distribution.FormatPublicKey(public), distribution.FormatPublicKey(public)},
		"no prefix":  {base64.StdEncoding.EncodeToString(public)},
		"short":      {"ed25519:" + base64.StdEncoding.EncodeToString(public[:16])},
		"not base64": {"ed25519:not base64!"},
	} {
		if _, err := distribution.ParseTrustedKeys(values); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := os.WriteFile(path, append(pemBytes, pemBytes...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := distribution.LoadSigningKey(path); err == nil {
		t.Fatal("accepted a file with two PEM blocks")
	}
}

type member struct {
	name     string
	typeflag byte
	linkname string
	data     []byte
	pax      map[string]string
}

// rewriteBundle re-signs transfer.json with signer (when non-nil) after fn,
// so structural tests exercise checks behind a valid trusted signature.
func rewriteBundle(t *testing.T, raw []byte, signer ed25519.PrivateKey, fn func([]member) []member) []byte {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(raw))
	var members []member
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, member{name: h.Name, typeflag: h.Typeflag, data: data})
	}
	members = fn(members)
	if signer != nil && members[0].name == "transfer.json" && members[1].name == "transfer.sig" {
		var sig distribution.TransferSignature
		if err := json.Unmarshal(members[1].data, &sig); err != nil {
			t.Fatal(err)
		}
		sig.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(signer, members[0].data))
		members[1].data, _ = json.Marshal(sig)
	}
	var out bytes.Buffer
	tw := tar.NewWriter(&out)
	for _, m := range members {
		h := &tar.Header{Name: m.name, Typeflag: m.typeflag, Linkname: m.linkname, Size: int64(len(m.data)), Mode: 0o644, PAXRecords: m.pax}
		if m.typeflag == 0 {
			h.Typeflag = tar.TypeReg
		}
		if h.Typeflag != tar.TypeReg {
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			if _, err := tw.Write(m.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
