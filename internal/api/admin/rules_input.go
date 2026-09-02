package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"

	"depsilo/internal/ecosystem"
	"depsilo/internal/packagepolicy"
	packagerules "depsilo/internal/rules"
)

const maxRuleRequestBodyBytes int64 = 64 << 10

type ruleInput struct {
	Ecosystem   string `json:"ecosystem"`
	PackageName string `json:"package_name"`
	Version     string `json:"version"`
	Action      string `json:"action"`
	Reason      string `json:"reason"`
}

type ruleUpdateInput struct {
	Ecosystem   *string `json:"ecosystem"`
	PackageName *string `json:"package_name"`
	Version     *string `json:"version"`
	Action      *string `json:"action"`
	Reason      *string `json:"reason"`
}

type ruleTestInput struct {
	Ecosystem string `json:"ecosystem"`
	Package   string `json:"package"`
	Version   string `json:"version"`
}

func decodeRuleJSON(c *gin.Context, destination any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRuleRequestBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain one JSON object")
		}
		return err
	}
	return nil
}

func (input *ruleInput) normalizeAndValidate() error {
	input.Ecosystem = strings.ToLower(strings.TrimSpace(input.Ecosystem))
	input.PackageName = strings.TrimSpace(input.PackageName)
	input.Version = normalizeRuleVersion(input.Version)
	input.Action = strings.ToLower(strings.TrimSpace(input.Action))
	input.Reason = strings.TrimSpace(input.Reason)
	if err := validateRuleEcosystem(input.Ecosystem); err != nil {
		return err
	}
	if err := validateRulePackage(input.PackageName); err != nil {
		return err
	}
	if err := validateRuleVersion(input.Version); err != nil {
		return err
	}
	if input.Action != "allow" && input.Action != "deny" {
		return errors.New("action must be allow or deny")
	}
	return validateRuleReason(input.Reason)
}

func (input *ruleUpdateInput) normalizeAndValidate() (packagerules.RulePatch, error) {
	var patch packagerules.RulePatch
	hasUpdate := false
	if input.Ecosystem != nil {
		value := strings.ToLower(strings.TrimSpace(*input.Ecosystem))
		if err := validateRuleEcosystem(value); err != nil {
			return packagerules.RulePatch{}, err
		}
		patch.Ecosystem = &value
		hasUpdate = true
	}
	if input.PackageName != nil {
		value := strings.TrimSpace(*input.PackageName)
		if err := validateRulePackage(value); err != nil {
			return packagerules.RulePatch{}, err
		}
		patch.PackageName = &value
		hasUpdate = true
	}
	if input.Version != nil {
		value := normalizeRuleVersion(*input.Version)
		if err := validateRuleVersion(value); err != nil {
			return packagerules.RulePatch{}, err
		}
		patch.Version = &value
		hasUpdate = true
	}
	if input.Action != nil {
		value := strings.ToLower(strings.TrimSpace(*input.Action))
		if value != "allow" && value != "deny" {
			return packagerules.RulePatch{}, errors.New("action must be allow or deny")
		}
		patch.Action = &value
		hasUpdate = true
	}
	if input.Reason != nil {
		value := strings.TrimSpace(*input.Reason)
		if err := validateRuleReason(value); err != nil {
			return packagerules.RulePatch{}, err
		}
		patch.Reason = &value
		hasUpdate = true
	}
	if !hasUpdate {
		return packagerules.RulePatch{}, errors.New("at least one editable rule field is required")
	}
	return patch, nil
}

func (input *ruleTestInput) normalizeAndValidate() error {
	input.Ecosystem = strings.ToLower(strings.TrimSpace(input.Ecosystem))
	input.Package = strings.TrimSpace(input.Package)
	input.Version = strings.TrimSpace(input.Version)
	if err := validateRuleEcosystem(input.Ecosystem); err != nil {
		return err
	}
	if input.Ecosystem == "*" {
		return errors.New("test ecosystem must identify a concrete ecosystem")
	}
	if input.Package == "" || len(input.Package) > 256 || containsRuleControl(input.Package) {
		return errors.New("package must be non-empty, at most 256 characters, and contain no control characters")
	}
	if len(input.Version) > 128 || containsRuleControl(input.Version) {
		return errors.New("version must be at most 128 characters and contain no control characters")
	}
	dialect, err := packagepolicy.DialectFor(input.Ecosystem)
	if err != nil {
		return err
	}
	if _, err := dialect.NormalizePackageName(input.Package); err != nil {
		return fmt.Errorf("invalid %s package name: %w", input.Ecosystem, err)
	}
	if input.Version != "" {
		if err := dialect.ValidateVersion(input.Version); err != nil {
			return fmt.Errorf("invalid %s version: %w", input.Ecosystem, err)
		}
	}
	return nil
}

func validateRuleEcosystem(value string) error {
	if value == "*" {
		return nil
	}
	for _, definition := range ecosystem.RuleDefinitions() {
		if definition.Name == value {
			return nil
		}
	}
	return fmt.Errorf("unsupported rule ecosystem %q", value)
}

func validateRulePackage(value string) error {
	if value == "" || len(value) > 256 || containsRuleControl(value) {
		return errors.New("package_name must be non-empty, at most 256 characters, and contain no control characters")
	}
	if strings.Count(value, "*") > 1 || strings.Contains(value, "*") && value != "*" && !strings.HasSuffix(value, "*") {
		return errors.New("package_name supports only an exact name, *, or a trailing-prefix wildcard")
	}
	return nil
}

func normalizeRuleVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "*"
	}
	return value
}

func validateRuleVersion(value string) error {
	if value == "" || len(value) > 128 || containsRuleControl(value) {
		return errors.New("version must be non-empty, at most 128 characters, and contain no control characters")
	}
	if strings.ContainsAny(value, "\r\n") {
		return errors.New("version must be a wildcard, exact version, or supported comparison")
	}
	for _, operator := range []string{"<=", ">=", "<", ">"} {
		if strings.HasPrefix(value, operator) {
			if strings.TrimSpace(strings.TrimPrefix(value, operator)) == "" {
				return errors.New("version comparison requires a target")
			}
			return nil
		}
	}
	if strings.HasPrefix(value, "=") || strings.HasPrefix(value, "!") || strings.HasPrefix(value, "~") {
		return errors.New("version must be a wildcard, exact version, or one of <, <=, >, >=")
	}
	return nil
}

func validateRuleReason(value string) error {
	if len(value) > 512 || containsRuleControl(value) {
		return errors.New("reason must be at most 512 characters and contain no control characters")
	}
	return nil
}

func containsRuleControl(value string) bool {
	return strings.ContainsFunc(value, unicode.IsControl)
}
