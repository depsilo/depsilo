package npm

import (
	"crypto/sha256"
	"encoding/hex"
	"mime"
	"strings"
)

const installV1MediaType = "application/vnd.npm.install-v1+json"

type acceptMediaRange struct {
	mediaType string
	typeName  string
	subtype   string
	quality   int
}

func MetadataCacheKey(packageName string) string {
	return "npm/" + strings.ToLower(packageName) + "/metadata.json"
}

func ScopedMetadataCacheKey(scope, packageName string) string {
	return "npm/@" + strings.ToLower(scope) + "/" + strings.ToLower(packageName) + "/metadata.json"
}

func metadataCacheKeyForAccept(baseKey, accept string) string {
	// npm 7+ requests the abbreviated install-v1 representation. v0.9 stored
	// that real-client response at baseKey, so keep this one representation on
	// the legacy key during an in-place upgrade. Full/default and every other
	// negotiated form move to hashed keys and can no longer overwrite it.
	if acceptsInstallV1(accept) {
		return baseKey
	}
	digest := sha256.Sum256([]byte(accept))
	const suffix = "/metadata.json"
	return strings.TrimSuffix(baseKey, suffix) + "/__accept__/" + hex.EncodeToString(digest[:]) + suffix
}

func acceptsInstallV1(accept string) bool {
	ranges, ok := parseAcceptMediaRanges(accept)
	if !ok {
		return false
	}

	installQuality := 0
	explicitInstall := false
	for _, mediaRange := range ranges {
		if mediaRange.mediaType == installV1MediaType {
			explicitInstall = true
			installQuality = mediaRange.quality
			break
		}
	}
	if !explicitInstall || installQuality == 0 {
		return false
	}

	return installQuality > representationQuality(ranges, "application", "json")
}

func parseAcceptMediaRanges(accept string) ([]acceptMediaRange, bool) {
	if strings.TrimSpace(accept) == "" || strings.ContainsAny(accept, "\"\\") {
		return nil, false
	}

	items := strings.Split(accept, ",")
	ranges := make([]acceptMediaRange, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(item))
		if err != nil {
			return nil, false
		}
		mediaType = strings.ToLower(mediaType)
		parts := strings.Split(mediaType, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" || (parts[0] == "*" && parts[1] != "*") {
			return nil, false
		}
		if _, duplicate := seen[mediaType]; duplicate {
			return nil, false
		}
		seen[mediaType] = struct{}{}

		quality := 1000
		for name, value := range params {
			if name != "q" || len(params) != 1 {
				return nil, false
			}
			parsedQuality, valid := parseAcceptQuality(value)
			if !valid {
				return nil, false
			}
			quality = parsedQuality
		}
		ranges = append(ranges, acceptMediaRange{
			mediaType: mediaType,
			typeName:  parts[0],
			subtype:   parts[1],
			quality:   quality,
		})
	}
	return ranges, true
}

func parseAcceptQuality(value string) (int, bool) {
	integer, fraction, hasFraction := strings.Cut(value, ".")
	if !hasFraction {
		fraction = ""
	}
	if len(fraction) > 3 || (integer == "" && fraction == "") {
		return 0, false
	}
	for _, digit := range fraction {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}

	switch integer {
	case "", "0":
		quality := 0
		for index := 0; index < 3; index++ {
			quality *= 10
			if index < len(fraction) {
				quality += int(fraction[index] - '0')
			}
		}
		return quality, true
	case "1":
		if strings.Trim(fraction, "0") != "" {
			return 0, false
		}
		return 1000, true
	default:
		return 0, false
	}
}

func representationQuality(ranges []acceptMediaRange, typeName, subtype string) int {
	bestSpecificity := -1
	quality := 0
	for _, mediaRange := range ranges {
		if mediaRange.typeName != "*" && mediaRange.typeName != typeName {
			continue
		}
		if mediaRange.subtype != "*" && mediaRange.subtype != subtype {
			continue
		}

		specificity := 0
		if mediaRange.typeName != "*" {
			specificity++
		}
		if mediaRange.subtype != "*" {
			specificity++
		}
		if specificity > bestSpecificity {
			bestSpecificity = specificity
			quality = mediaRange.quality
		}
	}
	return quality
}

func TarballCacheKey(packageName, filename string) string {
	return "npm/" + strings.ToLower(packageName) + "/-/" + filename
}

func ScopedTarballCacheKey(scope, packageName, filename string) string {
	return "npm/@" + strings.ToLower(scope) + "/" + strings.ToLower(packageName) + "/-/" + filename
}
