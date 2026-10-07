package config

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
)

// cascadeDocumentIndex records the byte ranges patchCascadeDocument may edit.
// Only the [cascade] table and its [[cascade.peers]] array tables are ever
// touched, so comments and operator-owned keys elsewhere survive verbatim.
type cascadeDocumentIndex struct {
	hasSection bool
	sectionEnd int
	values     map[string]cascadeKeyRange
	peerBlocks []documentRange
	rootEnd    int
	newline    string
}

type cascadeKeyRange struct {
	valueStart, valueEnd int
	lineStart, lineEnd   int
}

type documentRange struct {
	start, end int
}

type cascadeKeyValue struct {
	key   string
	value any
}

func patchCascadeDocument(
	document []byte,
	settings CascadeConfig,
	patch CascadeConfigPatch,
) ([]byte, error) {
	index, err := indexCascadeDocument(document)
	if err != nil {
		return nil, err
	}

	edits := make([]documentEdit, 0, len(index.values)+len(index.peerBlocks)+1)
	inserts := map[int]string{}
	addInsert := func(at int, text string) {
		if text == "" {
			return
		}
		inserts[at] += text
	}

	updates := make([]cascadeKeyValue, 0, 5)
	if patch.Enabled != nil {
		updates = append(updates, cascadeKeyValue{"enabled", settings.Enabled})
	}
	if patch.SharedToken != nil && settings.Token != "" {
		updates = append(updates, cascadeKeyValue{"token", settings.Token})
	}
	if patch.MaxHops != nil {
		updates = append(updates, cascadeKeyValue{"max_hops", settings.MaxHops})
	}
	if patch.MaxTTL != nil {
		updates = append(updates, cascadeKeyValue{"max_ttl", compactDuration(settings.MaxTTL)})
	}
	if patch.AllowInsecureHTTP != nil {
		updates = append(updates, cascadeKeyValue{"allow_insecure_http", settings.AllowInsecureHTTP})
	}
	if patch.SharedToken != nil && settings.Token == "" {
		if existing, ok := index.values["token"]; ok {
			edits = append(edits, documentEdit{start: existing.lineStart, end: existing.lineEnd})
		}
	}
	missing := make([]cascadeKeyValue, 0, len(updates))
	for _, update := range updates {
		if existing, ok := index.values[update.key]; ok {
			edits = append(edits, documentEdit{
				start:       existing.valueStart,
				end:         existing.valueEnd,
				replacement: renderCascadeScalar(update.value),
			})
			continue
		}
		missing = append(missing, update)
	}

	peersChanged := patch.Peers != nil
	peersHandled := false
	if !index.hasSection {
		// A missing section is created in one edit so scalars and peers cannot
		// be inserted at conflicting offsets.
		var block strings.Builder
		if len(missing) > 0 || (peersChanged && len(settings.Peers) > 0) {
			block.WriteString("[cascade]" + index.newline)
			block.WriteString(renderCascadeKeys(missing, index.newline))
			if peersChanged && len(settings.Peers) > 0 {
				if len(missing) > 0 {
					block.WriteString(index.newline)
				}
				block.WriteString(renderCascadePeers(settings.Peers, index.newline))
			}
			addInsert(index.rootEnd, withLeadingBlankLine(document, index.rootEnd, block.String(), index.newline))
		}
		peersHandled = true
	} else {
		if len(missing) > 0 {
			addInsert(index.sectionEnd, renderCascadeKeys(missing, index.newline))
		}
		if peersChanged {
			rendered := renderCascadePeers(settings.Peers, index.newline)
			if len(index.peerBlocks) > 0 {
				first := index.peerBlocks[0]
				last := index.peerBlocks[len(index.peerBlocks)-1]
				replacement := rendered
				if replacement == "" && last.end < len(document) {
					replacement = index.newline
				}
				if replacement != "" && last.end < len(document) &&
					!strings.HasSuffix(replacement, index.newline+index.newline) {
					replacement += index.newline
				}
				edits = append(edits, documentEdit{start: first.start, end: last.end, replacement: []byte(replacement)})
				peersHandled = true
			}
		}
	}
	if peersChanged && !peersHandled && len(settings.Peers) > 0 {
		text := renderCascadePeers(settings.Peers, index.newline)
		if existing := inserts[index.sectionEnd]; existing != "" {
			text = index.newline + text
		}
		addInsert(index.sectionEnd, text)
	}

	for at, text := range inserts {
		edits = append(edits, documentEdit{start: at, end: at, replacement: []byte(text)})
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
	return out, nil
}

func indexCascadeDocument(document []byte) (cascadeDocumentIndex, error) {
	index := cascadeDocumentIndex{
		values:  make(map[string]cascadeKeyRange),
		rootEnd: len(document),
		newline: "\n",
	}
	if bytes.Contains(document, []byte("\r\n")) {
		index.newline = "\r\n"
	}

	var parser unstable.Parser
	parser.KeepComments = true
	parser.Reset(document)
	type tableEntry struct {
		path  []string
		start int
	}
	tables := make([]tableEntry, 0)
	var tablePath []string
	foundTable := false
	for parser.NextExpression() {
		expression := parser.Expression()
		switch expression.Kind {
		case unstable.Table, unstable.ArrayTable:
			path := nodeKey(expression)
			start := lineStartAt(document, firstKeyOffset(expression))
			if !foundTable {
				index.rootEnd = start
				foundTable = true
			}
			tables = append(tables, tableEntry{path: path, start: start})
			tablePath = path
			if len(path) == 1 && path[0] == "cascade" {
				index.hasSection = true
				index.sectionEnd = lineEndAt(document, lastNodeEnd(expression))
			}
		case unstable.KeyValue:
			key := nodeKey(expression)
			if len(tablePath) == 0 && len(key) == 1 && key[0] == "cascade" {
				return cascadeDocumentIndex{}, fmt.Errorf(
					"cascade is an inline table; convert it to a [cascade] table before editing it here",
				)
			}
			if len(tablePath) != 1 || tablePath[0] != "cascade" || len(key) != 1 {
				continue
			}
			value := expression.Value()
			valueStart := int(value.Raw.Offset)
			valueEnd := int(value.Raw.Offset + value.Raw.Length)
			lineStart := lineStartAt(document, firstKeyOffset(expression))
			lineEnd := lineEndAt(document, lastNodeEnd(expression))
			if valueEnd <= valueStart {
				// The unstable parser does not expose a Raw range for boolean
				// values. Fall back to the token after '=' on the same line.
				valueStart, valueEnd = cascadeInlineValueRange(document, lineStart, lineEnd)
			}
			index.values[key[0]] = cascadeKeyRange{
				valueStart: valueStart,
				valueEnd:   valueEnd,
				lineStart:  lineStart,
				lineEnd:    lineEnd,
			}
			index.sectionEnd = lineEnd
		}
	}
	if err := parser.Error(); err != nil {
		return cascadeDocumentIndex{}, fmt.Errorf("parse cascade configuration: %w", err)
	}
	for i, table := range tables {
		if len(table.path) != 2 || table.path[0] != "cascade" || table.path[1] != "peers" {
			continue
		}
		end := len(document)
		if i+1 < len(tables) {
			end = tables[i+1].start
		}
		index.peerBlocks = append(index.peerBlocks, documentRange{start: table.start, end: end})
	}
	return index, nil
}

// cascadeInlineValueRange returns the token between '=' and the end of an
// inline value (whitespace, comment, or line end). It is used when the parser
// node carries no Raw range.
func cascadeInlineValueRange(document []byte, lineStart, lineEnd int) (int, int) {
	contentEnd := lineEnd
	for contentEnd > lineStart && (document[contentEnd-1] == '\n' || document[contentEnd-1] == '\r') {
		contentEnd--
	}
	equals := bytes.IndexByte(document[lineStart:contentEnd], '=')
	if equals < 0 {
		return lineStart, lineStart
	}
	start := lineStart + equals + 1
	for start < contentEnd && (document[start] == ' ' || document[start] == '\t') {
		start++
	}
	end := start
	for end < contentEnd && document[end] != ' ' && document[end] != '\t' && document[end] != '#' {
		end++
	}
	return start, end
}

func renderCascadeKeys(keys []cascadeKeyValue, newline string) string {
	if len(keys) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, entry := range keys {
		builder.WriteString(entry.key)
		builder.WriteString(" = ")
		builder.Write(renderCascadeScalar(entry.value))
		builder.WriteString(newline)
	}
	return builder.String()
}

func renderCascadeScalar(value any) []byte {
	switch typed := value.(type) {
	case bool:
		if typed {
			return []byte("true")
		}
		return []byte("false")
	case int:
		return []byte(strconv.Itoa(typed))
	case string:
		return []byte(strconv.Quote(typed))
	default:
		panic(fmt.Sprintf("unsupported cascade value type %T", value))
	}
}

func renderCascadePeers(peers []CascadePeerConfig, newline string) string {
	var builder strings.Builder
	for index, peer := range peers {
		if index > 0 {
			builder.WriteString(newline)
		}
		builder.WriteString("[[cascade.peers]]" + newline)
		builder.WriteString(fmt.Sprintf("name = %s%s", strconv.Quote(peer.Name), newline))
		builder.WriteString(fmt.Sprintf("url = %s%s", strconv.Quote(peer.URL), newline))
		if peer.Token != "" {
			builder.WriteString(fmt.Sprintf("token = %s%s", strconv.Quote(peer.Token), newline))
		}
		if peer.ForwardCredentials {
			builder.WriteString("forward_credentials = true" + newline)
		}
	}
	return builder.String()
}

func withLeadingBlankLine(document []byte, at int, text, newline string) string {
	if at == 0 {
		return text
	}
	if at >= 2 && string(document[at-2:at]) == newline+newline {
		return text
	}
	return newline + text
}
