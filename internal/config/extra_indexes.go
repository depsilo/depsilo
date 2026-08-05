package config

import (
	"fmt"
	"strings"

	"depsilo/internal/httpnamespace"
)

const maxExtraIndexNameBytes = 128

const ExtraIndexKindPyTorch = "pytorch"

func builtinExtraIndexPresets() []ExtraIndexConfig {
	return []ExtraIndexConfig{
		{
			Name:       "pytorch",
			Kind:       ExtraIndexKindPyTorch,
			Path:       "pypi-torch",
			SimplePath: "/",
			Upstreams: []UpstreamConfig{
				{
					Name:      "pytorch",
					URL:       "https://download.pytorch.org/whl",
					Priority:  1,
					ProbeMode: "passive",
				},
			},
		},
	}
}

// resolveExtraIndexPresets preserves operator declaration order. A same-name
// entry overlays the preset so omitted layout fields inherit safe defaults; a
// canonical-path match remains fully operator-owned for upgrade compatibility.
// Other enabled presets are appended in catalog order.
func resolveExtraIndexPresets(policy ExtraIndexPresets, operator []ExtraIndexConfig) ([]ExtraIndexConfig, error) {
	presets := builtinExtraIndexPresets()
	catalog := make(map[string]ExtraIndexConfig, len(presets))
	for _, preset := range presets {
		catalog[strings.ToLower(preset.Name)] = preset
	}

	disabled := make(map[string]bool, len(policy.Disabled))
	for index, name := range policy.Disabled {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || trimmed != name {
			return nil, fmt.Errorf("extra_index_presets.disabled[%d] must name a built-in preset without surrounding whitespace", index)
		}
		key := strings.ToLower(name)
		if _, exists := catalog[key]; !exists {
			return nil, fmt.Errorf("extra_index_presets.disabled[%d] names unknown preset %q", index, name)
		}
		if disabled[key] {
			return nil, fmt.Errorf("extra_index_presets.disabled[%d] duplicates preset %q", index, name)
		}
		disabled[key] = true
	}

	resolved := append(make([]ExtraIndexConfig, 0, len(operator)+len(presets)), operator...)
	if policy.Enabled != nil && !*policy.Enabled {
		return resolved, nil
	}

	claimed := make(map[string]bool, len(operator))
	for index := range resolved {
		operatorName := strings.ToLower(resolved[index].Name)
		operatorPath := normalizedExtraIndexRouteIdentity(resolved[index].Path)
		for _, preset := range presets {
			key := strings.ToLower(preset.Name)
			if disabled[key] {
				continue
			}
			if operatorName == key {
				resolved[index] = overlayExtraIndexPreset(preset, resolved[index])
				claimed[key] = true
				break
			}
			// Older configurations may have used a different display name for
			// the route that later became built in. Treat the canonical route as
			// operator-owned so an upgrade never appends a duplicate and fails
			// startup. A route that still points at the preset's upstream also
			// inherits omitted layout fields; a repurposed route preserves every
			// operator field.
			if operatorPath != "" && operatorPath == normalizedExtraIndexRouteIdentity(preset.Path) {
				if extraIndexUsesPresetUpstream(resolved[index], preset) {
					resolved[index] = overlayExtraIndexPreset(preset, resolved[index])
				}
				claimed[key] = true
				break
			}
		}
	}
	for _, preset := range presets {
		key := strings.ToLower(preset.Name)
		if claimed[key] || disabled[key] {
			continue
		}
		resolved = append(resolved, preset)
	}
	return resolved, nil
}

func overlayExtraIndexPreset(preset, operator ExtraIndexConfig) ExtraIndexConfig {
	merged := preset
	merged.Name = operator.Name
	if operator.Kind != "" {
		merged.Kind = operator.Kind
	}
	if operator.Path != "" {
		merged.Path = operator.Path
	}
	if operator.SimplePath != "" {
		merged.SimplePath = operator.SimplePath
	}
	if len(operator.Upstreams) > 0 {
		merged.Upstreams = append([]UpstreamConfig(nil), operator.Upstreams...)
	}
	return merged
}

func normalizedExtraIndexRouteIdentity(value string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(value), "/"))
}

func extraIndexUsesPresetUpstream(index, preset ExtraIndexConfig) bool {
	if len(index.Upstreams) == 0 || len(preset.Upstreams) == 0 {
		return false
	}
	presetURLs := make(map[string]struct{}, len(preset.Upstreams))
	for _, configured := range preset.Upstreams {
		presetURLs[strings.TrimRight(strings.TrimSpace(configured.URL), "/")] = struct{}{}
	}
	for _, configured := range index.Upstreams {
		if _, ok := presetURLs[strings.TrimRight(strings.TrimSpace(configured.URL), "/")]; ok {
			return true
		}
	}
	return false
}

func normalizeExtraIndexes(indexes []ExtraIndexConfig) error {
	reservedRoots := httpnamespace.ExtraIndexReservedRoots()
	reservedRoutes := make(map[string]string, len(reservedRoots))
	for _, route := range reservedRoots {
		root := strings.TrimPrefix(route, "/")
		reservedRoutes[strings.ToLower(root)] = route
	}
	seenRoutes := make(map[string]int, len(indexes))
	seenNames := make(map[string]int, len(indexes))

	for index := range indexes {
		kind := strings.TrimSpace(indexes[index].Kind)
		if kind != indexes[index].Kind || kind != "" && kind != ExtraIndexKindPyTorch {
			return fmt.Errorf(
				"extra_indexes[%d].kind must be %q when set",
				index,
				ExtraIndexKindPyTorch,
			)
		}

		name := indexes[index].Name
		trimmedName := strings.TrimSpace(name)
		if trimmedName == "" {
			return fmt.Errorf("extra_indexes[%d].name must not be empty", index)
		}
		if name != trimmedName {
			return fmt.Errorf(
				"extra_indexes[%d].name %q must not contain leading or trailing whitespace; "+
					"rename it explicitly so existing cache identity changes are visible",
				index,
				name,
			)
		}
		if len(name) > maxExtraIndexNameBytes {
			return fmt.Errorf(
				"extra_indexes[%d].name must be at most %d bytes",
				index,
				maxExtraIndexNameBytes,
			)
		}
		if !validExtraIndexName(name) {
			return fmt.Errorf(
				"extra_indexes[%d].name %q must be a URL-safe slug that starts and ends with a letter or digit",
				index,
				name,
			)
		}
		nameKey := strings.ToLower(name)
		if previous, duplicate := seenNames[nameKey]; duplicate {
			return fmt.Errorf(
				"extra_indexes[%d].name %q duplicates extra_indexes[%d].name; choose a unique name",
				index,
				name,
				previous,
			)
		}
		seenNames[nameKey] = index

		route := strings.Trim(strings.TrimSpace(indexes[index].Path), "/")
		indexes[index].Path = route
		if route == "" {
			return fmt.Errorf("extra_indexes[%d].path must not be empty", index)
		}
		if !validExtraIndexRoute(route) {
			return fmt.Errorf(
				"extra_indexes[%d].path %q must contain only URL-safe literal path segments; "+
					`use letters, digits, ".", "_", and "-", without ".", "..", wildcards, escapes, or empty segments`,
				index,
				route,
			)
		}
		routeKey := strings.ToLower(route)
		if previous, duplicate := seenRoutes[routeKey]; duplicate {
			return fmt.Errorf(
				"extra_indexes[%d].path %q duplicates extra_indexes[%d].path; choose a unique path",
				index,
				route,
				previous,
			)
		}
		for previousRoute, previous := range seenRoutes {
			if inExtraIndexProtocolSubtree(routeKey, previousRoute) ||
				inExtraIndexProtocolSubtree(previousRoute, routeKey) {
				return fmt.Errorf(
					"extra_indexes[%d].path %q and extra_indexes[%d].path %q "+
						`must not use another extra index's reserved "/simple" or "/files" subtree; choose disjoint paths`,
					index,
					route,
					previous,
					indexes[previous].Path,
				)
			}
			if indexes[previous].Kind == ExtraIndexKindPyTorch && strings.HasPrefix(routeKey, previousRoute+"/") ||
				indexes[index].Kind == ExtraIndexKindPyTorch && strings.HasPrefix(previousRoute, routeKey+"/") {
				return fmt.Errorf(
					"extra_indexes[%d].path %q overlaps the channel namespace owned by extra_indexes[%d].path %q",
					index,
					route,
					previous,
					indexes[previous].Path,
				)
			}
		}
		seenRoutes[routeKey] = index

		root, _, _ := strings.Cut(route, "/")
		if reservedRoute, reserved := reservedRoutes[strings.ToLower(root)]; reserved {
			return fmt.Errorf(
				`extra_indexes[%d].path %q conflicts with reserved route %q; choose a different path`,
				index,
				route,
				reservedRoute,
			)
		}

		simplePath, err := normalizeExtraIndexSimplePath(indexes[index].SimplePath)
		if err != nil {
			return fmt.Errorf("extra_indexes[%d].simple_path: %w", index, err)
		}
		indexes[index].SimplePath = simplePath
	}
	return nil
}

func normalizeExtraIndexSimplePath(raw string) (string, error) {
	route := strings.Trim(strings.TrimSpace(raw), "/")
	if route == "" {
		if strings.TrimSpace(raw) == "/" {
			return "/", nil
		}
		return "/simple", nil
	}
	if !validExtraIndexRoute(route) {
		return "", fmt.Errorf("must contain only URL-safe literal path segments")
	}
	return "/" + route, nil
}

func validExtraIndexRoute(route string) bool {
	for _, segment := range strings.Split(route, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, character := range segment {
			if character >= 'a' && character <= 'z' ||
				character >= 'A' && character <= 'Z' ||
				character >= '0' && character <= '9' ||
				character == '.' ||
				character == '_' ||
				character == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func inExtraIndexProtocolSubtree(route, owner string) bool {
	for _, segment := range []string{"simple", "files"} {
		reserved := owner + "/" + segment
		if route == reserved || strings.HasPrefix(route, reserved+"/") {
			return true
		}
	}
	return false
}

func validExtraIndexName(name string) bool {
	if name == "" {
		return false
	}
	if !asciiAlphaNumeric(name[0]) || !asciiAlphaNumeric(name[len(name)-1]) {
		return false
	}
	for index := range len(name) {
		character := name[index]
		if asciiAlphaNumeric(character) ||
			character == '.' ||
			character == '_' ||
			character == '-' {
			continue
		}
		return false
	}
	return true
}

func asciiAlphaNumeric(character byte) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9'
}
