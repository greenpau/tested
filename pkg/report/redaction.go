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

package report

import (
	"fmt"
	"regexp"
)

const (
	// MaximumRedactionPatterns bounds ordered presentation redaction work.
	MaximumRedactionPatterns = 32
	// MaximumRedactionPatternBytes bounds one Go regular expression.
	MaximumRedactionPatternBytes = 4 << 10
	// MaximumTotalRedactionPatternBytes bounds all configured expressions.
	MaximumTotalRedactionPatternBytes = 32 << 10

	redactionReplacement = "[REDACTED]"
	redactionOmission    = "[REDACTED: value omitted]"
	truncatedRedaction   = "[REDACTED: truncated output]"
)

type redactionExpressions []*regexp.Regexp

// ValidateRedactPatterns validates the bounded presentation-redaction
// contract without retaining compiled expressions.
func ValidateRedactPatterns(patterns []string) error {
	_, err := compileRedactors(patterns)
	return err
}

func compileRedactors(patterns []string) (redactionExpressions, error) {
	if len(patterns) > MaximumRedactionPatterns {
		return nil, fmt.Errorf(
			"redaction pattern count %d exceeds limit %d",
			len(patterns),
			MaximumRedactionPatterns,
		)
	}

	totalBytes := 0
	redactors := make(redactionExpressions, 0, len(patterns))
	for index, pattern := range patterns {
		if len(pattern) > MaximumRedactionPatternBytes {
			return nil, fmt.Errorf(
				"redaction %d size %d exceeds %d-byte limit",
				index+1,
				len(pattern),
				MaximumRedactionPatternBytes,
			)
		}
		if len(pattern) > MaximumTotalRedactionPatternBytes-totalBytes {
			return nil, fmt.Errorf(
				"redaction patterns exceed %d-byte aggregate limit",
				MaximumTotalRedactionPatternBytes,
			)
		}
		totalBytes += len(pattern)

		expression, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf(
				"compile redaction %d: %w",
				index+1,
				err,
			)
		}
		if expression.MatchString("") {
			return nil, fmt.Errorf(
				"redaction %d must not match empty text",
				index+1,
			)
		}
		redactors = append(redactors, expression)
	}
	return redactors, nil
}

func applyRedactors(value string, redactors redactionExpressions) string {
	for _, expression := range redactors {
		replaced, omit := replaceAllBounded(value, expression)
		if omit {
			return redactionOmission
		}
		value = replaced
	}
	return value
}

// replaceAllBounded applies one expression without accepting projected output
// beyond the fixed redaction limit. A contextual zero-width match or attempted
// growth beyond that limit omits the complete field so partial secrets cannot
// survive a rejected projection.
func replaceAllBounded(value string, expression *regexp.Regexp) (string, bool) {
	if expression == nil {
		return value, false
	}
	limit := redactionLimit(len(value))
	projectedBytes := len(value)
	matched := false
	omit := false
	replaced := expression.ReplaceAllStringFunc(value, func(match string) string {
		matched = true
		if omit {
			return match
		}
		if match == "" {
			omit = true
			return match
		}
		growth := len(redactionReplacement) - len(match)
		if growth > 0 && growth > limit-projectedBytes {
			omit = true
			return match
		}
		projectedBytes += growth
		return redactionReplacement
	})
	if omit {
		return redactionOmission, true
	}
	if !matched {
		return value, false
	}
	if len(replaced) > limit || len(replaced) != projectedBytes {
		return redactionOmission, true
	}
	return replaced, false
}

func redactionLimit(inputBytes int) int {
	if inputBytes < len(redactionOmission) {
		return len(redactionOmission)
	}
	return inputBytes
}
