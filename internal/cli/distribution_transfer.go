package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"depsilo/internal/distribution"
)

// transferClient drops the overall request timeout of the catalog client: a
// multi-gigabyte bundle can legitimately take longer. Interrupting the command
// cancels the context. Redirects remain disabled.
func transferClient(client *http.Client) *http.Client {
	clone := *client
	clone.Timeout = 0
	return &clone
}

func distributionKeygen(jsonMode bool, outPath string) int {
	pemBytes, public, err := distribution.GenerateSigningKey()
	if err != nil {
		return distributionCLIError(jsonMode, errors.New("cannot generate a transfer signing key"))
	}
	file, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return distributionCLIError(jsonMode, errors.New("cannot create the key file; it must not already exist"))
	}
	_, writeErr := file.Write(pemBytes)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(outPath)
		return distributionCLIError(jsonMode, errors.New("cannot write the key file"))
	}
	publicKey, keyID := distribution.FormatPublicKey(public), distribution.KeyID(public)
	if jsonMode {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"private_key_file": outPath, "public_key": publicKey, "key_id": keyID})
		return 0
	}
	fmt.Printf("Transfer signing key written to %s (mode 0600). Keep it private.\n\n", outPath)
	fmt.Printf("Exporting instance config.toml:\n  [distribution]\n  transfer_signing_key_file = %q\n\n", outPath)
	fmt.Printf("Importing instance config.toml (restart required):\n  [distribution]\n  trusted_transfer_keys = [%q]\n\n", publicKey)
	fmt.Printf("Key ID: %s\n", keyID)
	return 0
}

// exportDistribution saves the bundle beside its destination and publishes it
// only after the whole stream passed structure, length and hash checks. An
// existing destination is never replaced.
func exportDistribution(ctx context.Context, client *http.Client, endpoint, outPath string) ([]byte, error) {
	if _, err := os.Lstat(outPath); err == nil {
		return nil, errors.New("output file already exists; choose a new path")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("cannot inspect the output path")
	}
	resp, err := distributionSend(ctx, transferClient(client), http.MethodGet, endpoint, "application/json", nil, -1)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return distributionResponse(resp)
	}
	temp, err := os.CreateTemp(filepath.Dir(outPath), ".depsilo-bundle-*")
	if err != nil {
		return nil, errors.New("cannot create a temporary file beside the output path")
	}
	tempPath := temp.Name()
	published := false
	defer func() {
		if !published {
			_ = temp.Close()
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return nil, errors.New("cannot restrict temporary bundle permissions")
	}
	written := &countingWriter{writer: temp}
	summary, err := distribution.InspectBundle(ctx, io.TeeReader(resp.Body, written))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("downloaded bundle is incomplete or failed verification; nothing was saved")
	}
	// Drain anything InspectBundle did not need so the saved file and the
	// declared Content-Length can be compared exactly.
	if _, err := io.Copy(written, resp.Body); err != nil {
		return nil, errors.New("bundle download ended unexpectedly; nothing was saved")
	}
	if resp.ContentLength >= 0 && written.n != resp.ContentLength {
		return nil, errors.New("bundle length differs from the server declaration; nothing was saved")
	}
	if err := temp.Sync(); err != nil {
		return nil, errors.New("cannot flush the bundle to disk")
	}
	if err := temp.Close(); err != nil {
		return nil, errors.New("cannot close the bundle file")
	}
	if err := publishBundle(tempPath, outPath); err != nil {
		return nil, err
	}
	published = true
	return json.Marshal(map[string]any{"bundle": outPath, "set_id": summary.SetID, "name": summary.Name, "revision": summary.Revision,
		"signer_key_id": summary.SignerKeyID, "object_count": summary.ObjectCount, "total_bytes": summary.TotalBytes,
		"bundle_bytes": written.n, "content_verified": true, "signature_verified": false,
		"note": "the importing instance verifies the signature against its trusted_transfer_keys"})
}

// linkFile is replaceable in tests that simulate filesystems without links.
var linkFile = os.Link

// publishBundle prefers a hard link, which cannot replace a destination created
// meanwhile. exFAT/FAT32 removable media and many network shares, the usual
// offline transfer targets, lack hard links; there it falls back to rename
// after a fresh existence check, accepting that narrow race over failing.
func publishBundle(tempPath, outPath string) error {
	err := linkFile(tempPath, outPath)
	if err == nil {
		_ = os.Remove(tempPath)
		return nil
	}
	if errors.Is(err, os.ErrExist) {
		return errors.New("output file appeared during export; nothing was replaced")
	}
	if _, statErr := os.Lstat(outPath); !errors.Is(statErr, os.ErrNotExist) {
		return errors.New("output file appeared during export; nothing was replaced")
	}
	if err := os.Rename(tempPath, outPath); err != nil {
		return errors.New("cannot publish the bundle at the output path")
	}
	return nil
}

type countingWriter struct {
	writer io.Writer
	n      int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.n += int64(n)
	return n, err
}

func importDistribution(ctx context.Context, client *http.Client, endpoint, bundlePath string) ([]byte, error) {
	info, err := os.Lstat(bundlePath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("bundle must be a regular file, not a symlink")
	}
	file, err := os.Open(bundlePath)
	if err != nil {
		return nil, errors.New("cannot open bundle file")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("bundle file changed while opening")
	}
	resp, err := distributionSend(ctx, transferClient(client), http.MethodPost, endpoint, "application/x-tar", file, opened.Size())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return distributionResponse(resp)
}
