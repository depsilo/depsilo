package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"depsilo/internal/backup"
	"depsilo/internal/config"
)

func runBackup(args []string) int {
	jsonMode, args := stripJSONFlag(args)
	output, err := backupOutputPath(args)
	if err != nil {
		return printBackupError(jsonMode, err)
	}
	cfg, err := config.Load()
	if err != nil {
		return printBackupError(jsonMode, fmt.Errorf("load config: %w", err))
	}
	if cfg.Database.Driver != "sqlite" {
		return printBackupError(jsonMode, fmt.Errorf("backup supports sqlite databases, got %q", cfg.Database.Driver))
	}
	if cfg.ConfigPath == "" {
		return printBackupError(jsonMode, errors.New("resolved config path is empty"))
	}

	result, err := backup.Create(context.Background(), backup.Paths{
		Config: cfg.ConfigPath, Database: cfg.Database.DSN,
	}, output)
	if err != nil {
		return printBackupError(jsonMode, err)
	}
	if jsonMode {
		printJSON(map[string]any{
			"ok":      true,
			"file":    result.Path,
			"size":    result.Size,
			"message": "Backup created successfully",
		})
		return 0
	}

	fmt.Printf("✓ Backup created: %s (%.1f MB)\n", result.Path, float64(result.Size)/(1024*1024))
	fmt.Println("  Contains: config.toml + a consistent SQLite snapshot")
	fmt.Println("  Cache objects are not included")
	fmt.Println("  Restore with: depsilo restore " + result.Path)
	return 0
}

func runRestore(args []string) int {
	jsonMode, args := stripJSONFlag(args)
	options, err := parseRestoreOptions(args)
	if err != nil {
		if !jsonMode && len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: depsilo restore <backup.tar.gz> [--config-target path] [--database-target path]")
			fmt.Fprintln(os.Stderr, "The Depsilo server that owns the target database must be stopped.")
		}
		return printBackupError(jsonMode, err)
	}

	targets, err := restoreTargets(options)
	if err != nil {
		return printBackupError(jsonMode, err)
	}
	result, err := backup.Restore(context.Background(), options.archive, targets)
	if err != nil {
		return printBackupError(jsonMode, err)
	}
	if jsonMode {
		printJSON(map[string]any{
			"ok":       true,
			"message":  "Restore complete. Restart Depsilo to apply changes.",
			"files":    result.Restored,
			"previous": result.Previous,
		})
		return 0
	}

	fmt.Println("✓ Restore complete!")
	fmt.Println("  Restored:", strings.Join(result.Restored, ", "))
	if len(result.Previous) > 0 {
		fmt.Println("  Previous state retained at:")
		for _, path := range result.Previous {
			fmt.Println("   ", path)
		}
	}
	fmt.Println("  Restart Depsilo to apply changes.")
	return 0
}

func backupOutputPath(args []string) (string, error) {
	output := "depsilo-backup.tar.gz"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--out", "-o":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return "", fmt.Errorf("%s requires an output path", args[i])
			}
			output = args[i+1]
			i++
		default:
			return "", fmt.Errorf("unknown backup argument %q", args[i])
		}
	}
	return output, nil
}

type restoreOptions struct {
	archive        string
	configTarget   string
	databaseTarget string
}

func parseRestoreOptions(args []string) (restoreOptions, error) {
	var options restoreOptions
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config-target", "--database-target":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return restoreOptions{}, fmt.Errorf("%s requires a path", args[i])
			}
			if args[i] == "--config-target" {
				options.configTarget = args[i+1]
			} else {
				options.databaseTarget = args[i+1]
			}
			i++
		default:
			if strings.HasPrefix(args[i], "-") {
				return restoreOptions{}, fmt.Errorf("unknown restore argument %q", args[i])
			}
			if options.archive != "" {
				return restoreOptions{}, fmt.Errorf("unexpected restore argument %q", args[i])
			}
			options.archive = args[i]
		}
	}
	if options.archive == "" {
		return restoreOptions{}, errors.New("missing backup file")
	}
	return options, nil
}

func restoreTargets(options restoreOptions) (backup.Paths, error) {
	cfg, loadErr := config.Load()
	if loadErr == nil {
		if options.configTarget == "" {
			options.configTarget = cfg.ConfigPath
		}
		if options.databaseTarget == "" {
			options.databaseTarget = cfg.Database.DSN
		}
	} else if options.configTarget == "" || options.databaseTarget == "" {
		return backup.Paths{}, fmt.Errorf(
			"load current config: %w; to recover a broken config, provide both --config-target and --database-target",
			loadErr,
		)
	}
	if options.configTarget == "" || options.databaseTarget == "" {
		return backup.Paths{}, errors.New("restore target paths are empty")
	}
	return backup.Paths{Config: options.configTarget, Database: options.databaseTarget}, nil
}

func printBackupError(jsonMode bool, err error) int {
	if jsonMode {
		printJSON(map[string]any{"ok": false, "error": err.Error()})
	} else {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
	return 1
}
