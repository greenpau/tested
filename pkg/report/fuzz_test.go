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
	"bytes"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/greenpau/tested/pkg/result"
)

func FuzzRenderUntrustedText(f *testing.F) {
	for _, seed := range []string{
		"ordinary output",
		`</script><script>alert(1)</script>`,
		"\x00\x01\x1b[31msecret=value",
		"日本語🙂",
		string([]byte{0xff, 0xfe, 'x'}),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		renderer, err := New(Options{
			Title:          value,
			RedactPatterns: []string{`secret=[[:alnum:]]+`},
			Slowest:        1,
		})
		if err != nil {
			t.Fatalf("New() unexpected error: %v", err)
		}
		input := Input{Result: result.Result{
			Finalized: true,
			Metadata:  result.RunMetadata{ExitCode: 1},
			Packages: []result.Package{
				{
					Name:   value,
					Status: result.StatusFailed,
					Tests: []result.TestOccurrence{
						{
							ID: result.OccurrenceID{
								Package: value,
								Name:    value,
								Ordinal: 1,
							},
							Status: result.StatusFailed,
							Output: testOutput(value),
						},
					},
				},
			},
			Summary: result.Summary{
				Packages: result.OutcomeCounts{Total: 1, Failed: 1},
				Tests:    result.OutcomeCounts{Total: 1, Failed: 1},
			},
		}}

		var html bytes.Buffer
		if err := renderer.RenderTestOutputHTML(&html, input); err != nil {
			t.Fatalf("RenderTestOutputHTML(): %v", err)
		}
		if !utf8.Valid(html.Bytes()) {
			t.Fatal("HTML is not valid UTF-8")
		}

		var summary bytes.Buffer
		if err := renderer.RenderSummaryJSON(&summary, input); err != nil {
			t.Fatalf("RenderSummaryJSON(): %v", err)
		}
		if !json.Valid(summary.Bytes()) {
			t.Fatal("summary is not valid JSON")
		}

		var junit bytes.Buffer
		if err := renderer.RenderJUnitXML(&junit, input); err != nil {
			t.Fatalf("RenderJUnitXML(): %v", err)
		}
		var decoded any
		if err := xml.Unmarshal(junit.Bytes(), &decoded); err != nil {
			t.Fatalf("JUnit is not valid XML: %v", err)
		}

		var plain bytes.Buffer
		console, err := NewConsole(ConsoleOptions{
			Writer:         &plain,
			Title:          value,
			Format:         ConsolePlain,
			Color:          ColorNever,
			RedactPatterns: []string{`secret=[[:alnum:]]+`},
		})
		if err != nil {
			t.Fatalf("NewConsole(): %v", err)
		}
		if err := console.Package(input.Result.Packages[0]); err != nil {
			t.Fatalf("Package(): %v", err)
		}
		if err := console.Final(input); err != nil {
			t.Fatalf("Final(): %v", err)
		}
		if strings.ContainsRune(plain.String(), '\x1b') {
			t.Fatal("plain output contains an untrusted escape control")
		}
	})
}

func FuzzRedactionIsBounded(f *testing.F) {
	for _, seed := range []struct {
		pattern string
		value   string
	}{
		{pattern: ".", value: strings.Repeat("a", 4096)},
		{pattern: `\b`, value: "secret"},
		{pattern: `secret=[[:alnum:]]+`, value: "secret=value"},
		{pattern: "z*", value: "zzzz"},
		{pattern: "[", value: "invalid expression"},
		{pattern: "^secret", value: "secret secret"},
	} {
		f.Add(seed.pattern, seed.value)
	}
	f.Fuzz(func(t *testing.T, pattern, value string) {
		renderer, err := New(Options{RedactPatterns: []string{pattern}})
		if err != nil {
			return
		}
		got := renderer.redact(value)
		if len(got) > redactionLimit(len(value)) {
			t.Fatalf(
				"redact() produced %d bytes from %d-byte input; limit %d",
				len(got),
				len(value),
				redactionLimit(len(value)),
			)
		}
	})
}
