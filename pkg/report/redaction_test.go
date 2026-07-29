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
	"strings"
	"testing"
)

func TestValidateRedactPatterns(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		wantErr  string
	}{
		{
			name:     "ordinary consuming expression",
			patterns: []string{`secret=[[:alnum:]]+`},
		},
		{
			name:     "contextual zero width accepted at construction",
			patterns: []string{`\b`},
		},
		{
			name:     "invalid expression",
			patterns: []string{"["},
			wantErr:  "compile redaction 1",
		},
		{
			name:     "empty expression",
			patterns: []string{""},
			wantErr:  "must not match empty text",
		},
		{
			name:     "quantified empty expression",
			patterns: []string{"z*"},
			wantErr:  "must not match empty text",
		},
		{
			name:     "anchor matches empty input",
			patterns: []string{"^"},
			wantErr:  "must not match empty text",
		},
		{
			name:     "pattern count",
			patterns: repeatPattern("a", MaximumRedactionPatterns+1),
			wantErr:  "pattern count 33 exceeds limit 32",
		},
		{
			name:     "single pattern bytes",
			patterns: []string{strings.Repeat("a", MaximumRedactionPatternBytes+1)},
			wantErr:  "size 4097 exceeds 4096-byte limit",
		},
		{
			name: "aggregate pattern bytes",
			patterns: repeatPattern(
				strings.Repeat("a", MaximumRedactionPatternBytes),
				MaximumTotalRedactionPatternBytes/MaximumRedactionPatternBytes+1,
			),
			wantErr: "exceed 32768-byte aggregate limit",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateRedactPatterns(test.patterns)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateRedactPatterns() unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf(
					"ValidateRedactPatterns() error = %v, want substring %q",
					err,
					test.wantErr,
				)
			}
		})
	}
}

func TestRedactionAmplificationIsBounded(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		value    string
		want     string
	}{
		{
			name:     "ordinary consuming match",
			patterns: []string{`secret-[[:alpha:]]+`},
			value:    "prefix secret-value suffix",
			want:     "prefix [REDACTED] suffix",
		},
		{
			name:     "beginning anchor retains full input context",
			patterns: []string{`^secret`},
			value:    "secret secret",
			want:     "[REDACTED] secret",
		},
		{
			name:     "dot against hostile long input",
			patterns: []string{"."},
			value:    strings.Repeat("a", 64<<10),
			want:     redactionOmission,
		},
		{
			name:     "repeated dot rules cannot compound",
			patterns: repeatPattern(".", MaximumRedactionPatterns),
			value:    "a",
			want:     redactionOmission,
		},
		{
			name:     "contextual zero width omits complete value",
			patterns: []string{`\b`},
			value:    "secret",
			want:     redactionOmission,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			renderer, err := New(Options{RedactPatterns: test.patterns})
			if err != nil {
				t.Fatalf("New() unexpected error: %v", err)
			}
			got := renderer.redact(test.value)
			if got != test.want {
				t.Fatalf("redact() = %q, want %q", got, test.want)
			}
			if len(got) > redactionLimit(len(test.value)) {
				t.Fatalf(
					"redact() produced %d bytes from %d-byte input; limit %d",
					len(got),
					len(test.value),
					redactionLimit(len(test.value)),
				)
			}
		})
	}
}

func TestRendererRejectsEmptyMatchingRedaction(t *testing.T) {
	_, err := New(Options{RedactPatterns: []string{"z*"}})
	if err == nil || !strings.Contains(err.Error(), "must not match empty text") {
		t.Fatalf("New() error = %v, want empty-match rejection", err)
	}
}

func repeatPattern(pattern string, count int) []string {
	patterns := make([]string, count)
	for index := range patterns {
		patterns[index] = pattern
	}
	return patterns
}
