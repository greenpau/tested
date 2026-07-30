// Copyright 2026 Paul Greenberg greenpau@outlook.com
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package tag enforces tested's repository-local struct-tag conventions.
package tag

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

var (
	// ErrUnsupportedResource identifies a value that is not a pointer to a
	// struct and therefore cannot be inspected for tag compliance.
	ErrUnsupportedResource = errors.New("unsupported tag compliance resource")
	// ErrInvalidOptions identifies contradictory or malformed compliance
	// options.
	ErrInvalidOptions = errors.New("invalid tag compliance options")
	// ErrNonCompliant identifies a supported struct with one or more tag
	// violations. GetTagCompliance returns the individual violations alongside
	// this error.
	ErrNonCompliant = errors.New("struct tags are not compliant")
)

// Options stores compliance-check policy.
type Options struct {
	// Disabled classifies a struct as intentionally outside serialization-tag
	// compliance. The resource type is still validated.
	Disabled bool
	// DisableTagPresent permits a required tag to be absent. Any present tag is
	// still checked unless DisableTagMismatch is also set.
	DisableTagPresent bool
	// DisableTagMismatch permits any value for a present required tag.
	DisableTagMismatch bool
	// DisableTagOnEmpty omits ",omitempty" from the canonical expected value.
	DisableTagOnEmpty bool
	// AllowFieldMismatch permits a mismatch when the actual tag's name is
	// present in AllowedFields.
	AllowFieldMismatch bool
	// AllowedFields contains exceptional serialized field names. Values are
	// ignored. Prefer FieldTagOverrides for new exact exceptions.
	AllowedFields map[string]interface{}
	// RequiredTags selects the serialization formats to check, in diagnostic
	// order. An empty slice selects json, xml, and yaml.
	RequiredTags []string
	// IgnoreTagOptions compares only the serialized name before the first
	// comma. It is useful for an established schema whose fields deliberately
	// mix required and omitempty behavior.
	IgnoreTagOptions bool
	// FieldTagOverrides defines exact expected values by Go field name and tag
	// name. It is intended for format syntax such as XML nesting.
	FieldTagOverrides map[string]map[string]string
}

// GetTagCompliance checks the immediate exported fields of a pointer-to-struct
// resource. It returns deterministic, field-ordered diagnostics. A typed nil
// pointer is accepted because only its type is inspected.
func GetTagCompliance(resource interface{}, opts *Options) ([]string, error) {
	resourceType, err := resolveResourceType(resource)
	if err != nil {
		return nil, err
	}

	options := Options{}
	if opts != nil {
		options = *opts
	}
	requiredTags, err := resolveRequiredTags(options.RequiredTags)
	if err != nil {
		return nil, err
	}
	if err := validateFieldTagOverrides(
		resourceType,
		requiredTags,
		options.FieldTagOverrides,
	); err != nil {
		return nil, err
	}
	if options.Disabled {
		return nil, nil
	}

	var diagnostics []string
	var suggestions []string
	for i := 0; i < resourceType.NumField(); i++ {
		field := resourceType.Field(i)
		if !field.IsExported() {
			continue
		}

		missing := false
		for _, tagName := range requiredTags {
			expected, overridden := expectedTagValue(
				field.Name,
				tagName,
				options,
			)
			actual, present := field.Tag.Lookup(tagName)
			if !present {
				if options.DisableTagPresent {
					continue
				}
				missing = true
				diagnostics = append(diagnostics, fmt.Sprintf(
					"tag %q not found in %s.%s (%v)",
					tagName,
					qualifiedTypeName(resourceType),
					field.Name,
					field.Type,
				))
				continue
			}
			if actual == "-" || options.DisableTagMismatch {
				continue
			}
			if tagValuesMatch(
				actual,
				expected,
				options.IgnoreTagOptions && !overridden,
			) {
				continue
			}
			if mismatchAllowed(actual, options) {
				continue
			}
			diagnostics = append(diagnostics, fmt.Sprintf(
				"tag %q mismatch in %s.%s (%v): %q (actual) vs. %q (expected)",
				tagName,
				qualifiedTypeName(resourceType),
				field.Name,
				field.Type,
				actual,
				expected,
			))
		}
		if missing {
			suggestions = append(suggestions, fmt.Sprintf(
				"%s %s %s",
				field.Name,
				field.Type,
				makeTags(field.Name, requiredTags, options),
			))
		}
	}

	if len(suggestions) > 0 {
		diagnostics = append(diagnostics, fmt.Sprintf(
			"suggested struct changes to %s:\n%s",
			qualifiedTypeName(resourceType),
			strings.Join(suggestions, "\n"),
		))
	}
	if len(diagnostics) > 0 {
		return diagnostics, fmt.Errorf(
			"%w: struct %q",
			ErrNonCompliant,
			qualifiedTypeName(resourceType),
		)
	}
	return nil, nil
}

func resolveResourceType(resource interface{}) (reflect.Type, error) {
	if resource == nil {
		return nil, fmt.Errorf("%w: got <nil>", ErrUnsupportedResource)
	}
	resourceType := reflect.TypeOf(resource)
	if resourceType.Kind() != reflect.Ptr {
		return nil, fmt.Errorf(
			"%w: expected pointer to struct, got %s",
			ErrUnsupportedResource,
			resourceType.Kind(),
		)
	}
	resourceType = resourceType.Elem()
	if resourceType.Kind() != reflect.Struct {
		return nil, fmt.Errorf(
			"%w: expected pointer to struct, got pointer to %s",
			ErrUnsupportedResource,
			resourceType.Kind(),
		)
	}
	return resourceType, nil
}

func resolveRequiredTags(configured []string) ([]string, error) {
	if len(configured) == 0 {
		return []string{"json", "xml", "yaml"}, nil
	}
	resolved := make([]string, 0, len(configured))
	seen := make(map[string]struct{}, len(configured))
	for _, tagName := range configured {
		if !validTagName(tagName) {
			return nil, fmt.Errorf(
				"%w: invalid required tag name %q",
				ErrInvalidOptions,
				tagName,
			)
		}
		if _, exists := seen[tagName]; exists {
			return nil, fmt.Errorf(
				"%w: duplicate required tag name %q",
				ErrInvalidOptions,
				tagName,
			)
		}
		seen[tagName] = struct{}{}
		resolved = append(resolved, tagName)
	}
	return resolved, nil
}

func validTagName(tagName string) bool {
	if tagName == "" {
		return false
	}
	for _, r := range tagName {
		if r <= ' ' || r == ':' || r == '"' || r == '`' ||
			unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validateFieldTagOverrides(
	resourceType reflect.Type,
	requiredTags []string,
	overrides map[string]map[string]string,
) error {
	if len(overrides) == 0 {
		return nil
	}
	fields := make(map[string]struct{}, resourceType.NumField())
	for i := 0; i < resourceType.NumField(); i++ {
		field := resourceType.Field(i)
		if field.IsExported() {
			fields[field.Name] = struct{}{}
		}
	}
	tags := make(map[string]struct{}, len(requiredTags))
	for _, tagName := range requiredTags {
		tags[tagName] = struct{}{}
	}

	fieldNames := make([]string, 0, len(overrides))
	for fieldName := range overrides {
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)
	for _, fieldName := range fieldNames {
		if _, exists := fields[fieldName]; !exists {
			return fmt.Errorf(
				"%w: override field %q is not an exported immediate field of %s",
				ErrInvalidOptions,
				fieldName,
				qualifiedTypeName(resourceType),
			)
		}
		tagOverrides := overrides[fieldName]
		if len(tagOverrides) == 0 {
			return fmt.Errorf(
				"%w: overrides for %s.%s are empty",
				ErrInvalidOptions,
				qualifiedTypeName(resourceType),
				fieldName,
			)
		}
		tagNames := make([]string, 0, len(tagOverrides))
		for tagName := range tagOverrides {
			tagNames = append(tagNames, tagName)
		}
		sort.Strings(tagNames)
		for _, tagName := range tagNames {
			if _, exists := tags[tagName]; !exists {
				return fmt.Errorf(
					"%w: override tag %q for %s.%s is not required",
					ErrInvalidOptions,
					tagName,
					qualifiedTypeName(resourceType),
					fieldName,
				)
			}
			if tagOverrides[tagName] == "" {
				return fmt.Errorf(
					"%w: override tag %q for %s.%s is empty",
					ErrInvalidOptions,
					tagName,
					qualifiedTypeName(resourceType),
					fieldName,
				)
			}
			if strings.ContainsRune(tagOverrides[tagName], '`') {
				return fmt.Errorf(
					"%w: override tag %q for %s.%s contains a raw literal delimiter",
					ErrInvalidOptions,
					tagName,
					qualifiedTypeName(resourceType),
					fieldName,
				)
			}
		}
	}
	return nil
}

func expectedTagValue(
	fieldName string,
	tagName string,
	opts Options,
) (string, bool) {
	if tagOverrides, exists := opts.FieldTagOverrides[fieldName]; exists {
		if value, exists := tagOverrides[tagName]; exists {
			return value, true
		}
	}
	value := convertFieldToTag(fieldName)
	if !opts.DisableTagOnEmpty {
		value += ",omitempty"
	}
	return value, false
}

func tagValuesMatch(actual, expected string, ignoreOptions bool) bool {
	if !ignoreOptions {
		return actual == expected
	}
	actualName, _, _ := strings.Cut(actual, ",")
	expectedName, _, _ := strings.Cut(expected, ",")
	return actualName == expectedName
}

func mismatchAllowed(actual string, opts Options) bool {
	if !opts.AllowFieldMismatch || opts.AllowedFields == nil {
		return false
	}
	fieldName, _, _ := strings.Cut(actual, ",")
	_, exists := opts.AllowedFields[fieldName]
	return exists
}

func qualifiedTypeName(resourceType reflect.Type) string {
	if resourceType.PkgPath() == "" {
		if resourceType.Name() == "" {
			return "<anonymous>"
		}
		return resourceType.Name()
	}
	return resourceType.PkgPath() + "." + resourceType.Name()
}

func convertFieldToTag(value string) string {
	runes := []rune(value)
	words := make([]string, 0, 4)
	currentWord := make([]rune, 0, len(runes))
	flushCurrentWord := func() {
		if len(currentWord) == 0 {
			return
		}
		words = append(words, strings.ToLower(string(currentWord)))
		currentWord = currentWord[:0]
	}

	for i := 0; i < len(runes); {
		current := runes[i]
		if current == '_' {
			flushCurrentWord()
			i++
			continue
		}
		canStartInitialism := len(currentWord) == 0 ||
			i == 0 ||
			unicode.IsLower(runes[i-1]) ||
			unicode.IsDigit(runes[i-1])
		initialismLength := 0
		if canStartInitialism {
			initialismLength = matchInitialism(runes, i)
		}
		if initialismLength > 0 {
			flushCurrentWord()
			end := i + initialismLength
			if end < len(runes) && runes[end] == 's' &&
				(end+1 == len(runes) ||
					runes[end+1] == '_' ||
					unicode.IsUpper(runes[end+1])) {
				end++
			}
			words = append(
				words,
				strings.ToLower(string(runes[i:end])),
			)
			i = end
			continue
		}
		if unicode.IsUpper(current) && len(currentWord) > 0 {
			previous := runes[i-1]
			nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			acronymPlural := unicode.IsUpper(previous) &&
				i+1 < len(runes) &&
				runes[i+1] == 's' &&
				(i+2 == len(runes) ||
					runes[i+2] == '_' ||
					unicode.IsUpper(runes[i+2]))
			if unicode.IsLower(previous) ||
				unicode.IsDigit(previous) ||
				(unicode.IsUpper(previous) &&
					nextIsLower &&
					!acronymPlural) {
				flushCurrentWord()
			}
		}
		currentWord = append(currentWord, current)
		i++
	}
	flushCurrentWord()
	return strings.Join(words, "_")
}

func matchInitialism(value []rune, start int) int {
	candidates := [...]string{
		"OpenSSH",
		"OAuth2",
		"SHA256",
		"ASCII",
		"HTTPS",
		"JSONL",
		"UUID",
		"GUID",
		"HTML",
		"HTTP",
		"IPv4",
		"IPv6",
		"UTF8",
		"XMPP",
		"XSRF",
		"YAML",
		"ACL",
		"API",
		"CPU",
		"CSS",
		"DNS",
		"EOF",
		"ISBN",
		"JSON",
		"MD5",
		"QPS",
		"RAM",
		"RHS",
		"RPC",
		"SLA",
		"SMTP",
		"SQL",
		"SSH",
		"TCP",
		"TLS",
		"TTL",
		"UDP",
		"UID",
		"URI",
		"URL",
		"XML",
		"XSS",
		"ID",
		"IP",
		"NS",
		"UI",
		"VM",
	}
	longest := 0
	for _, candidate := range candidates {
		candidateRunes := []rune(candidate)
		if len(candidateRunes) <= longest ||
			start+len(candidateRunes) > len(value) {
			continue
		}
		matches := true
		for i, candidateRune := range candidateRunes {
			if value[start+i] != candidateRune {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		end := start + len(candidateRunes)
		if end < len(value) && unicode.IsLower(value[end]) {
			pluralBoundary := value[end] == 's' &&
				(end+1 == len(value) ||
					value[end+1] == '_' ||
					unicode.IsUpper(value[end+1]))
			if !pluralBoundary {
				continue
			}
		}
		longest = len(candidateRunes)
	}
	return longest
}

func makeTags(fieldName string, requiredTags []string, opts Options) string {
	var output strings.Builder
	output.WriteByte('`')
	for i, tagName := range requiredTags {
		if i > 0 {
			output.WriteByte(' ')
		}
		output.WriteString(tagName)
		expected, _ := expectedTagValue(fieldName, tagName, opts)
		output.WriteByte(':')
		output.WriteString(strconv.Quote(expected))
	}
	output.WriteByte('`')
	return output.String()
}
