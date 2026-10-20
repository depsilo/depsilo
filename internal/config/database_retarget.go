package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
)

// RetargetSQLiteDatabase updates database.dsn while preserving all unrelated
// TOML bytes. Restore uses it when an operator deliberately installs a backup
// database at a location different from the one recorded in the archive.
// The returned document has already passed the normal strict config decoder.
func RetargetSQLiteDatabase(document []byte, target string) ([]byte, bool, error) {
	if strings.TrimSpace(target) == "" {
		return nil, false, fmt.Errorf("database restore target is empty")
	}
	cfg, err := decodeConfigDocument(document)
	if err != nil {
		return nil, false, err
	}
	if cfg.Database.Driver != "sqlite" {
		return nil, false, fmt.Errorf("database restore target requires sqlite config, got %q", cfg.Database.Driver)
	}
	currentPath, err := sqliteDocumentFilePath(cfg.Database.DSN)
	if err != nil {
		return nil, false, fmt.Errorf("resolve configured database.dsn: %w", err)
	}
	targetPath, err := sqliteDocumentFilePath(target)
	if err != nil {
		return nil, false, fmt.Errorf("resolve database restore target: %w", err)
	}
	if sameDocumentPath(currentPath, targetPath) {
		return append([]byte(nil), document...), false, nil
	}

	updated, err := patchDatabaseDSN(document, targetPath)
	if err != nil {
		return nil, false, err
	}
	if _, err := decodeConfigDocument(updated); err != nil {
		return nil, false, fmt.Errorf("validate retargeted config: %w", err)
	}
	return updated, true, nil
}

func patchDatabaseDSN(document []byte, target string) ([]byte, error) {
	return patchConfigString(document, "database", "dsn", target)
}

func patchConfigString(document []byte, section, field, target string) ([]byte, error) {
	var parser unstable.Parser
	parser.KeepComments = true
	parser.Reset(document)
	tablePath := []string(nil)
	rootEnd := len(document)
	foundTable := false
	sectionEnd := -1
	inlineTable := false
	inlineInsertAt := 0
	inlineNonempty := false
	var valueRange *unstable.Range

	for parser.NextExpression() {
		expression := parser.Expression()
		switch expression.Kind {
		case unstable.Table, unstable.ArrayTable:
			tablePath = nodeKey(expression)
			if !foundTable {
				rootEnd = lineStartAt(document, firstKeyOffset(expression))
				foundTable = true
			}
			if len(tablePath) == 1 && tablePath[0] == section {
				sectionEnd = lineEndAt(document, lastNodeEnd(expression))
			}
		case unstable.KeyValue:
			path := append(append([]string(nil), tablePath...), nodeKey(expression)...)
			if len(path) == 1 && path[0] == section && expression.Value().Kind == unstable.InlineTable {
				inlineInsertAt = int(expression.Value().Raw.Offset + 1)
				children := expression.Value().Children()
				inlineNonempty = children.Next()
			}
			findConfigStringValue(expression.Value(), path, section, field, &valueRange, &inlineTable)
			if len(tablePath) == 1 && tablePath[0] == section {
				sectionEnd = lineEndAt(document, lastNodeEnd(expression))
			}
		}
	}
	if err := parser.Error(); err != nil {
		return nil, fmt.Errorf("parse config document: %w", err)
	}

	replacement := []byte(strconv.Quote(target))
	if valueRange != nil {
		start := int(valueRange.Offset)
		end := int(valueRange.Offset + valueRange.Length)
		updated := make([]byte, 0, len(document)-(end-start)+len(replacement))
		updated = append(updated, document[:start]...)
		updated = append(updated, replacement...)
		updated = append(updated, document[end:]...)
		return updated, nil
	}
	if inlineTable {
		if section == "auth" {
			addition := " " + field + " = " + string(replacement)
			if inlineNonempty {
				addition += ","
			}
			addition += " "
			updated := append([]byte(nil), document[:inlineInsertAt]...)
			updated = append(updated, addition...)
			return append(updated, document[inlineInsertAt:]...), nil
		}
		return nil, fmt.Errorf("cannot add %s.%s while %s is an inline table", section, field, section)
	}

	newline := "\n"
	if strings.Contains(string(document), "\r\n") {
		newline = "\r\n"
	}
	at := rootEnd
	key := section + "." + field
	if sectionEnd >= 0 {
		at = sectionEnd
		key = field
	}
	var addition strings.Builder
	if at > 0 && document[at-1] != '\n' {
		addition.WriteString(newline)
	}
	fmt.Fprintf(&addition, "%s = %s%s", key, replacement, newline)
	updated := make([]byte, 0, len(document)+addition.Len())
	updated = append(updated, document[:at]...)
	updated = append(updated, addition.String()...)
	updated = append(updated, document[at:]...)
	return updated, nil
}

func findConfigStringValue(value *unstable.Node, path []string, section, field string, found **unstable.Range, inlineTable *bool) {
	if len(path) == 2 && path[0] == section && path[1] == field {
		raw := value.Raw
		*found = &raw
		return
	}
	if value.Kind != unstable.InlineTable {
		return
	}
	if len(path) == 1 && path[0] == section {
		*inlineTable = true
	}
	children := value.Children()
	for children.Next() {
		child := children.Node()
		if child.Kind != unstable.KeyValue {
			continue
		}
		childPath := append(append([]string(nil), path...), nodeKey(child)...)
		findConfigStringValue(child.Value(), childPath, section, field, found, inlineTable)
	}
}

func sqliteDocumentFilePath(dsn string) (string, error) {
	if strings.TrimSpace(dsn) == "" || dsn == ":memory:" {
		return "", fmt.Errorf("database.dsn must name a file-backed SQLite database")
	}
	path := dsn
	if strings.HasPrefix(dsn, "file:") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			return "", err
		}
		if strings.EqualFold(parsed.Query().Get("mode"), "memory") || parsed.Opaque == ":memory:" {
			return "", fmt.Errorf("database.dsn must not use an in-memory SQLite database")
		}
		if parsed.Host != "" && parsed.Host != "localhost" {
			return "", fmt.Errorf("database.dsn must not use a remote file URI host")
		}
		switch {
		case parsed.Path != "":
			path = parsed.Path
		case parsed.Opaque != "":
			path, err = url.PathUnescape(parsed.Opaque)
			if err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("SQLite DSN has no database path")
		}
	} else if index := strings.IndexByte(path, '?'); index >= 0 {
		path = path[:index]
	}
	absolute, err := filepath.Abs(filepath.FromSlash(path))
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func sameDocumentPath(left, right string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
