package cli

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"depsilo/internal/config"
)

// ── runBackup ────────────────────────────────────────────────────

func runBackup(args []string) int {
	jsonMode, args := stripJSONFlag(args)

	outFile := "depsilo-backup.tar.gz"
	for i := 0; i < len(args); i++ {
		if (args[i] == "--out" || args[i] == "-o") && i+1 < len(args) {
			outFile = args[i+1]
			i++
		}
	}

	// Load config to find paths
	cfg, err := config.Load()
	if err != nil {
		if jsonMode {
			printJSON(map[string]any{"ok": false, "error": fmt.Sprintf("load config: %v", err)})
		} else {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			fmt.Fprintln(os.Stderr, "  Make sure config.toml exists or set DEPSILO_CONFIG env var.")
		}
		return 1
	}

	configPath := cfg.ConfigPath
	if configPath == "" {
		configPath = "config.toml"
	}
	dbPath := cfg.Database.DSN

	// Verify source files exist
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if jsonMode {
			printJSON(map[string]any{"ok": false, "error": fmt.Sprintf("config file not found: %s", configPath)})
		} else {
			fmt.Fprintf(os.Stderr, "Error: config file not found: %s\n", configPath)
		}
		return 1
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		if jsonMode {
			printJSON(map[string]any{"ok": false, "error": fmt.Sprintf("database not found: %s", dbPath)})
		} else {
			fmt.Fprintf(os.Stderr, "Error: database not found: %s\n", dbPath)
		}
		return 1
	}

	// Create output file
	f, err := os.Create(outFile)
	if err != nil {
		if jsonMode {
			printJSON(map[string]any{"ok": false, "error": fmt.Sprintf("create output: %v", err)})
		} else {
			fmt.Fprintf(os.Stderr, "Error creating %s: %v\n", outFile, err)
		}
		return 1
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	// Add manifest
	manifest := map[string]any{
		"version":    "1",
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"files":      []string{"config.toml", "depsilo.db"},
	}
	manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")
	addToTar(tw, "manifest.json", manifestJSON)

	// Add config.toml
	if err := addFileToTar(tw, configPath, "config.toml"); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Add database
	if err := addFileToTar(tw, dbPath, "depsilo.db"); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Close writers
	tw.Close()
	gw.Close()
	f.Close()

	if jsonMode {
		info, _ := os.Stat(outFile)
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		printJSON(map[string]any{
			"ok":      true,
			"file":    outFile,
			"size":    size,
			"message": "Backup created successfully",
		})
	} else {
		info, _ := os.Stat(outFile)
		fmt.Printf("✓ Backup created: %s", outFile)
		if info != nil {
			fmt.Printf(" (%.1f MB)", float64(info.Size())/(1024*1024))
		}
		fmt.Println()
		fmt.Println("  Contains: config.toml + depsilo.db")
		fmt.Println("  Restore with: depsilo restore " + outFile)
	}
	return 0
}

// ── runRestore ───────────────────────────────────────────────────

func runRestore(args []string) int {
	jsonMode, args := stripJSONFlag(args)

	if len(args) < 1 {
		if jsonMode {
			printJSON(map[string]any{"ok": false, "error": "missing backup file", "usage": "depsilo restore <backup.tar.gz>"})
		} else {
			fmt.Fprintln(os.Stderr, "Usage: depsilo restore <backup.tar.gz>")
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "Restores config.toml and database from a backup.")
			fmt.Fprintln(os.Stderr, "The server must be stopped before restoring.")
		}
		return 1
	}

	backupFile := args[0]

	// Check backup exists
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		if jsonMode {
			printJSON(map[string]any{"ok": false, "error": fmt.Sprintf("backup file not found: %s", backupFile)})
		} else {
			fmt.Fprintf(os.Stderr, "Error: backup file not found: %s\n", backupFile)
		}
		return 1
	}

	// Open backup
	f, err := os.Open(backupFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening %s: %v\n", backupFile, err)
		return 1
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s is not a valid gzip file: %v\n", backupFile, err)
		return 1
	}
	defer gr.Close()
	tr := tar.NewReader(gr)

	// Load config to find target paths
	cfg, _ := config.Load()
	configTarget := cfg.ConfigPath
	if configTarget == "" {
		configTarget = "config.toml"
	}
	dbTarget := cfg.Database.DSN

	// Extract to temp dir first, then move
	tmpDir, err := os.MkdirTemp("", "depsilo-restore-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp dir: %v\n", err)
		return 1
	}
	defer os.RemoveAll(tmpDir)

	var manifest map[string]any
	restored := []string{}

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading backup: %v\n", err)
			return 1
		}

		switch header.Name {
		case "manifest.json":
			decoder := json.NewDecoder(tr)
			decoder.Decode(&manifest)
		case "config.toml", "depsilo.db":
			tmpPath := filepath.Join(tmpDir, header.Name)
			out, err := os.Create(tmpPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error extracting %s: %v\n", header.Name, err)
				return 1
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				fmt.Fprintf(os.Stderr, "Error extracting %s: %v\n", header.Name, err)
				return 1
			}
			out.Close()
			restored = append(restored, header.Name)
		}
	}

	if manifest == nil {
		fmt.Fprintln(os.Stderr, "Warning: no manifest.json found in backup (old format?)")
	}

	if len(restored) == 0 {
		fmt.Fprintln(os.Stderr, "Error: backup contains no restorable files")
		return 1
	}

	// Move files to target locations
	for _, name := range restored {
		src := filepath.Join(tmpDir, name)
		var dst string
		switch name {
		case "config.toml":
			dst = configTarget
		case "depsilo.db":
			dst = dbTarget
		}

		// Back up existing file
		if _, err := os.Stat(dst); err == nil {
			backupOld := dst + ".pre-restore.bak"
			if err := os.Rename(dst, backupOld); err != nil {
				fmt.Fprintf(os.Stderr, "Error backing up existing %s: %v\n", dst, err)
				return 1
			}
			fmt.Printf("  Backed up existing %s → %s\n", filepath.Base(dst), filepath.Base(backupOld))
		}

		// Ensure parent directory exists
		if dir := filepath.Dir(dst); dir != "." {
			os.MkdirAll(dir, 0755)
		}

		if err := os.Rename(src, dst); err != nil {
			// Try copy if rename fails (cross-device)
			if err := copyFile(src, dst); err != nil {
				fmt.Fprintf(os.Stderr, "Error restoring %s: %v\n", dst, err)
				return 1
			}
		}
	}

	if jsonMode {
		printJSON(map[string]any{
			"ok":      true,
			"message": "Restore complete. Restart Depsilo to apply changes.",
			"files":   restored,
		})
	} else {
		fmt.Println("✓ Restore complete!")
		fmt.Println("  Restored:", strings.Join(restored, ", "))
		if cfg.Server.Port > 0 {
			fmt.Println()
			fmt.Println("  Restart Depsilo to apply changes:")
			fmt.Println("    depsilo stop && depsilo start --daemon")
		}
	}
	return 0
}

// ── tar helpers ──────────────────────────────────────────────────

func addToTar(tw *tar.Writer, name string, data []byte) error {
	header := &tar.Header{
		Name:    name,
		Size:    int64(len(data)),
		Mode:    0644,
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write tar header for %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("write tar body for %s: %w", name, err)
	}
	return nil
}

func addFileToTar(tw *tar.Writer, srcPath, tarName string) error {
	info, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("stat %s: %w", srcPath, err)
	}

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("create tar header for %s: %w", srcPath, err)
	}
	header.Name = tarName

	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write tar header for %s: %w", tarName, err)
	}

	f, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", srcPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("write tar body for %s: %w", tarName, err)
	}
	return nil
}

func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	_, err = io.Copy(d, s)
	return err
}
