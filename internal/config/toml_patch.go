package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
)

type documentEdit struct {
	start       int
	end         int
	replacement []byte
}

type settingsDocumentIndex struct {
	values       map[SettingPath]unstable.Range
	explicit     map[SettingPath]bool
	sectionEnds  map[string]int
	sealedInline map[string]bool
	rootEnd      int
	newline      string
}

func patchSettingsDocument(document []byte, patch SettingsPatch) ([]byte, map[SettingPath]bool, error) {
	index, err := indexSettingsDocument(document)
	if err != nil {
		return nil, nil, err
	}
	edits := make([]documentEdit, 0, len(patch.entries()))
	missingBySection := map[string][]settingPatchEntry{}
	for _, entry := range patch.entries() {
		if raw, ok := index.values[entry.path]; ok {
			edits = append(edits, documentEdit{
				start:       int(raw.Offset),
				end:         int(raw.Offset + raw.Length),
				replacement: renderSettingValue(entry),
			})
			continue
		}
		section, _ := splitSettingPath(entry.path)
		if index.sealedInline[section] {
			return nil, nil, fmt.Errorf("cannot add %s: %s is an inline table", entry.path, section)
		}
		missingBySection[section] = append(missingBySection[section], entry)
	}
	for _, section := range []string{"server", "cache", "auth"} {
		entries := missingBySection[section]
		if len(entries) == 0 {
			continue
		}
		at, inSection := index.sectionEnds[section]
		var b strings.Builder
		if at > 0 && document[at-1] != '\n' {
			b.WriteString(index.newline)
		}
		for _, entry := range entries {
			_, key := splitSettingPath(entry.path)
			if !inSection {
				key = string(entry.path)
				at = index.rootEnd
			}
			fmt.Fprintf(&b, "%s = %s%s", key, renderSettingValue(entry), index.newline)
		}
		edits = append(edits, documentEdit{start: at, end: at, replacement: []byte(b.String())})
	}

	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := append([]byte(nil), document...)
	for _, edit := range edits {
		updated := make([]byte, 0, len(out)-(edit.end-edit.start)+len(edit.replacement))
		updated = append(updated, out[:edit.start]...)
		updated = append(updated, edit.replacement...)
		updated = append(updated, out[edit.end:]...)
		out = updated
	}
	if _, err := decodeConfigDocument(out); err != nil {
		return nil, nil, fmt.Errorf("validate patched config: %w", err)
	}
	after, err := indexSettingsDocument(out)
	if err != nil {
		return nil, nil, err
	}
	return out, after.explicit, nil
}

func indexSettingsDocument(document []byte) (settingsDocumentIndex, error) {
	index := settingsDocumentIndex{
		values:       make(map[SettingPath]unstable.Range),
		explicit:     make(map[SettingPath]bool),
		sectionEnds:  make(map[string]int),
		sealedInline: make(map[string]bool),
		rootEnd:      len(document),
		newline:      "\n",
	}
	if strings.Contains(string(document), "\r\n") {
		index.newline = "\r\n"
	}

	var parser unstable.Parser
	parser.KeepComments = true
	parser.Reset(document)
	var tablePath []string
	foundTable := false
	for parser.NextExpression() {
		expression := parser.Expression()
		switch expression.Kind {
		case unstable.Table, unstable.ArrayTable:
			tablePath = nodeKey(expression)
			lineStart := lineStartAt(document, firstKeyOffset(expression))
			if !foundTable {
				index.rootEnd = lineStart
				foundTable = true
			}
			if len(tablePath) == 1 && isEditableSection(tablePath[0]) {
				index.sectionEnds[tablePath[0]] = lineEndAt(document, lastNodeEnd(expression))
			}
		case unstable.KeyValue:
			path := append(append([]string(nil), tablePath...), nodeKey(expression)...)
			indexValue(&index, expression.Value(), path)
			if len(tablePath) == 1 && isEditableSection(tablePath[0]) {
				index.sectionEnds[tablePath[0]] = lineEndAt(document, lastNodeEnd(expression))
			}
		}
	}
	if err := parser.Error(); err != nil {
		return settingsDocumentIndex{}, fmt.Errorf("parse config document: %w", err)
	}
	return index, nil
}

func sanitizeConfigDocumentForViper(document []byte) ([]byte, error) {
	type parsedExpression struct {
		start    int
		sanitize bool
	}

	var parser unstable.Parser
	parser.KeepComments = true
	parser.Reset(document)
	atRoot := true
	sanitizeTableBlock := false
	expressions := make([]parsedExpression, 0)
	for parser.NextExpression() {
		expression := parser.Expression()
		sanitize := sanitizeTableBlock
		switch expression.Kind {
		case unstable.Table, unstable.ArrayTable:
			atRoot = false
			path := nodeKey(expression)
			sanitizeTableBlock = len(path) > 0 && strings.Contains(path[0], ".")
			sanitize = sanitizeTableBlock
		case unstable.KeyValue:
			key := nodeKey(expression)
			sanitize = sanitizeTableBlock || (atRoot && len(key) == 1 && strings.Contains(key[0], "."))
		}
		expressions = append(expressions, parsedExpression{
			start:    expressionLineStart(document, expression),
			sanitize: sanitize,
		})
	}
	if err := parser.Error(); err != nil {
		return nil, fmt.Errorf("parse config document: %w", err)
	}

	sanitized := append([]byte(nil), document...)
	for i, expression := range expressions {
		if !expression.sanitize {
			continue
		}
		end := len(document)
		if i+1 < len(expressions) {
			end = expressions[i+1].start
		}
		blankDocumentRange(sanitized, expression.start, end)
	}
	return sanitized, nil
}

func expressionLineStart(document []byte, expression *unstable.Node) int {
	at := int(expression.Raw.Offset)
	if expression.Kind == unstable.Table || expression.Kind == unstable.ArrayTable || expression.Kind == unstable.KeyValue {
		at = firstKeyOffset(expression)
	}
	return lineStartAt(document, at)
}

func blankDocumentRange(sanitized []byte, start, end int) {
	for i := start; i < end; i++ {
		if sanitized[i] != '\r' && sanitized[i] != '\n' {
			sanitized[i] = ' '
		}
	}
}

func indexValue(index *settingsDocumentIndex, value *unstable.Node, path []string) {
	if settingPath, ok := canonicalSettingComponents(path); ok {
		index.values[settingPath] = value.Raw
		index.explicit[settingPath] = true
	}
	if value.Kind != unstable.InlineTable {
		return
	}
	if len(path) == 1 && isEditableSection(path[0]) {
		index.sealedInline[path[0]] = true
	}
	children := value.Children()
	for children.Next() {
		child := children.Node()
		if child.Kind != unstable.KeyValue {
			continue
		}
		childPath := append(append([]string(nil), path...), nodeKey(child)...)
		indexValue(index, child.Value(), childPath)
	}
}

func canonicalSettingComponents(components []string) (SettingPath, bool) {
	if len(components) != 2 {
		return "", false
	}
	return canonicalSettingPath(components[0] + "." + components[1])
}

func nodeKey(node *unstable.Node) []string {
	var key []string
	parts := node.Key()
	for parts.Next() {
		key = append(key, string(parts.Node().Data))
	}
	return key
}

func firstKeyOffset(node *unstable.Node) int {
	parts := node.Key()
	if parts.Next() {
		return int(parts.Node().Raw.Offset)
	}
	return 0
}

func lastNodeEnd(node *unstable.Node) int {
	end := int(node.Raw.Offset + node.Raw.Length)
	children := node.Children()
	for children.Next() {
		if childEnd := lastNodeEnd(children.Node()); childEnd > end {
			end = childEnd
		}
	}
	return end
}

func lineStartAt(document []byte, at int) int {
	for at > 0 && document[at-1] != '\n' {
		at--
	}
	return at
}

func lineEndAt(document []byte, at int) int {
	for at < len(document) && document[at] != '\n' {
		at++
	}
	if at < len(document) {
		at++
	}
	return at
}

func canonicalSettingPath(path string) (SettingPath, bool) {
	settingPath := SettingPath(path)
	for _, candidate := range allSettingPaths {
		if settingPath == candidate {
			return settingPath, true
		}
	}
	return "", false
}

func isEditableSection(section string) bool {
	return section == "server" || section == "cache" || section == "auth"
}

func renderSettingValue(entry settingPatchEntry) []byte {
	switch entry.path {
	case SettingServerLogLevel, SettingCacheTTLIndex, SettingCacheTTLBlob, SettingAuthTokenTTL:
		return []byte(strconv.Quote(entry.value.(string)))
	case SettingCacheMaxSizeGB, SettingCacheLRUThreshold:
		return []byte(strconv.Itoa(entry.value.(int)))
	default:
		panic(fmt.Sprintf("unsupported editable setting path %q", entry.path))
	}
}

func splitSettingPath(path SettingPath) (string, string) {
	if _, ok := canonicalSettingPath(string(path)); !ok {
		panic(fmt.Sprintf("unsupported setting path %q", path))
	}
	section, key, ok := strings.Cut(string(path), ".")
	if !ok || !isEditableSection(section) {
		panic(fmt.Sprintf("unsupported editable setting path %q", path))
	}
	return section, key
}
