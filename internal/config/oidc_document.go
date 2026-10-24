package config

import (
	"bytes"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
)

// Only explicit [auth.oidc] or [auth.sources.<id>] tables are patched.
// Alternative TOML shapes remain readable but require operator normalization:
// guessing at inline/dotted ownership could overwrite unrelated secrets.
func patchOIDCDocument(document []byte, entries []oidcEntry) ([]byte, error) {
	return patchAuthenticationDocument(document, "oidc", entries, false)
}

func patchAuthenticationDocument(document []byte, id string, entries []oidcEntry, remove bool) ([]byte, error) {
	target := "auth.oidc"
	if id != "oidc" {
		target = "auth.sources." + id
	}
	var removals []documentEdit
	var parser unstable.Parser
	parser.KeepComments = true
	parser.Reset(document)
	var table []string
	ranges := make(map[string]documentRange)
	sectionEnd := -1
	ldapSectionEnd := -1
	newline := "\n"
	if bytes.Contains(document, []byte("\r\n")) {
		newline = "\r\n"
	}
	for parser.NextExpression() {
		node := parser.Expression()
		switch node.Kind {
		case unstable.Table, unstable.ArrayTable:
			table = nodeKey(node)
			if strings.HasPrefix(strings.Join(table, "."), target+".") && strings.Join(table, ".") != target+".ldap" {
				return nil, errors.New("nested authentication source tables cannot be edited here")
			}
			if strings.Join(table, ".") == target+".ldap" {
				if node.Kind != unstable.Table {
					return nil, errors.New("LDAP settings must be an explicit table")
				}
				ldapSectionEnd = lineEndAt(document, lastNodeEnd(node))
				removals = append(removals, documentEdit{start: lineStartAt(document, firstKeyOffset(node)), end: ldapSectionEnd})
			}
			if strings.Join(table, ".") == target {
				if node.Kind != unstable.Table {
					return nil, errors.New("authentication source must be an explicit table")
				}
				sectionEnd = lineEndAt(document, lastNodeEnd(node))
				removals = append(removals, documentEdit{start: lineStartAt(document, firstKeyOffset(node)), end: sectionEnd})
			}
		case unstable.KeyValue:
			key := nodeKey(node)
			path := strings.Join(append(append([]string{}, table...), key...), ".")
			owner := strings.Join(table, ".")
			if path == "auth" || path == target || strings.HasPrefix(target, path+".") || strings.HasPrefix(path, target+".") && owner != target && owner != target+".ldap" {
				return nil, errors.New("convert authentication configuration to explicit source tables before editing it here")
			}
			if owner != target && owner != target+".ldap" {
				continue
			}
			if len(key) != 1 {
				return nil, errors.New("dotted authentication source fields cannot be edited here")
			}
			value := node.Value()
			start, end := int(value.Raw.Offset), int(value.Raw.Offset+value.Raw.Length)
			if end <= start || value.Kind == unstable.InlineTable {
				start, end = cascadeInlineValueRange(document, lineStartAt(document, firstKeyOffset(node)), lineEndAt(document, lastNodeEnd(node)))
				if value.Kind == unstable.Array || value.Kind == unstable.InlineTable {
					// Container nodes have no Raw range. Locate their closing
					// delimiter after the last child, including quoted string values.
					closing := byte(']')
					if value.Kind == unstable.InlineTable {
						closing = '}'
					}
					end = lastNodeEnd(value)
					if end < start+1 {
						end = start + 1
					}
					for end < len(document) && (document[end] == ' ' || document[end] == '\t' || document[end] == '\r' || document[end] == '\n' || document[end] == ',') {
						end++
					}
					if end >= len(document) || document[end] != closing {
						return nil, errors.New("cannot locate authentication setting container")
					}
					end++
				}
			}
			field := key[0]
			fieldEnd := lineEndAt(document, end)
			if owner == target+".ldap" {
				field = "ldap." + field
				ldapSectionEnd = fieldEnd
			} else {
				sectionEnd = fieldEnd
			}
			ranges[field] = documentRange{start, end}
			removals = append(removals, documentEdit{start: lineStartAt(document, firstKeyOffset(node)), end: fieldEnd})
		}
	}
	if parser.Error() != nil {
		return nil, errors.New("cannot parse authentication configuration")
	}
	edits := make([]documentEdit, 0, len(entries)+1)
	if remove {
		edits = removals
		entries = nil
	}
	if ldapSectionEnd >= 0 {
		var flattened []oidcEntry
		for _, entry := range entries {
			if entry.field != "ldap" {
				flattened = append(flattened, entry)
				continue
			}
			values := entry.value.(map[string]string)
			for _, field := range ldapFields {
				if value, exists := values[field]; exists {
					flattened = append(flattened, oidcEntry{"ldap." + field, value})
				}
			}
		}
		entries = flattened
	}
	var missing strings.Builder
	var missingLDAP strings.Builder
	for _, entry := range entries {
		value := renderOIDCValue(entry.value)
		if r, ok := ranges[entry.field]; ok {
			edits = append(edits, documentEdit{start: r.start, end: r.end, replacement: value})
		} else if strings.HasPrefix(entry.field, "ldap.") && ldapSectionEnd >= 0 {
			missingLDAP.WriteString(strings.TrimPrefix(entry.field, "ldap.") + " = " + string(value) + newline)
		} else {
			missing.WriteString(entry.field + " = " + string(value) + newline)
		}
	}
	if missingLDAP.Len() > 0 {
		insertion := missingLDAP.String()
		if ldapSectionEnd > 0 && document[ldapSectionEnd-1] != '\n' {
			insertion = newline + insertion
		}
		edits = append(edits, documentEdit{start: ldapSectionEnd, end: ldapSectionEnd, replacement: []byte(insertion)})
	}
	if missing.Len() > 0 {
		insertion := missing.String()
		if sectionEnd < 0 {
			sectionEnd = len(document)
			insertion = "[" + target + "]" + newline + insertion
		}
		if sectionEnd > 0 && document[sectionEnd-1] != '\n' {
			insertion = newline + insertion
		}
		edits = append(edits, documentEdit{start: sectionEnd, end: sectionEnd, replacement: []byte(insertion)})
	}
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := append([]byte{}, document...)
	for _, edit := range edits {
		next := make([]byte, 0, len(out)+len(edit.replacement)-(edit.end-edit.start))
		next = append(next, out[:edit.start]...)
		next = append(next, edit.replacement...)
		next = append(next, out[edit.end:]...)
		out = next
	}
	return out, nil
}

func renderOIDCValue(value any) []byte {
	if fields, ok := value.(map[string]string); ok {
		return renderLDAPValue(fields)
	}
	if items, ok := value.([]string); ok {
		quoted := make([]string, 0, len(items))
		for _, item := range items {
			quoted = append(quoted, strconv.Quote(item))
		}
		return []byte("[" + strings.Join(quoted, ", ") + "]")
	}
	return renderCascadeScalar(value)
}

// Preserve which fields actually came from TOML during pending-removal restore.
// A field absent from the original file must not be filled from its environment.
func authenticationSourceExplicitFields(document []byte, id string) (map[string]bool, error) {
	if _, err := patchAuthenticationDocument(document, id, nil, false); err != nil {
		return nil, err
	}
	target := "auth.oidc"
	if id != "oidc" {
		target = "auth.sources." + id
	}
	fields := map[string]bool{}
	var parser unstable.Parser
	parser.Reset(document)
	table := ""
	for parser.NextExpression() {
		node := parser.Expression()
		switch node.Kind {
		case unstable.Table, unstable.ArrayTable:
			table = strings.Join(nodeKey(node), ".")
			if table == target+".ldap" {
				fields["ldap"] = true
			}
		case unstable.KeyValue:
			key := nodeKey(node)
			if table == target && len(key) == 1 {
				fields[key[0]] = true
			}
		}
	}
	if parser.Error() != nil {
		return nil, errors.New("cannot parse authentication configuration")
	}
	return fields, nil
}
