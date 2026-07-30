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
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/greenpau/tested/pkg/coverage"
)

func TestDecorateCoverageHTMLPreservesCanonicalBytes(t *testing.T) {
	renderer, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	presentationBlock := renderer.assets.coverageHead
	canonical := []byte(
		"\n<!DOCTYPE html><html><head><title>coverage</title>" +
			"<style>.cov0{color:red}</style></head><body>" +
			"<pre>&lt;/head&gt; source stays escaped</pre>" +
			`<pre>coverageThemeMarker = id="tested-coverage-theme-v1"</pre>` +
			"<script>void 0</script></body></html>\n",
	)

	var first bytes.Buffer
	if err := renderer.DecorateCoverageHTML(
		context.Background(),
		&singleByteReader{reader: bytes.NewReader(canonical)},
		&first,
	); err != nil {
		t.Fatalf("DecorateCoverageHTML() error = %v", err)
	}
	var second bytes.Buffer
	if err := renderer.DecorateCoverageHTML(
		context.Background(),
		bytes.NewReader(canonical),
		&second,
	); err != nil {
		t.Fatalf("second DecorateCoverageHTML() error = %v", err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("coverage decoration is not byte deterministic")
	}
	themeElement := []byte(`<style id="tested-coverage-theme-v1">`)
	if bytes.Count(first.Bytes(), themeElement) != 1 {
		t.Fatalf("theme element count = %d, want 1", bytes.Count(
			first.Bytes(),
			themeElement,
		))
	}
	restored := bytes.Replace(first.Bytes(), presentationBlock, nil, 1)
	if !bytes.Equal(restored, canonical) {
		t.Fatalf(
			"removing the presentation did not restore canonical bytes\n got: %q\nwant: %q",
			restored,
			canonical,
		)
	}
	for _, expected := range []string{
		`name="viewport"`,
		`content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"`,
		coverageExplorerMarker,
		"--background:",
		"--primary:",
		"@media (prefers-color-scheme: dark)",
		"#files:focus-visible",
		"grid-template-columns: minmax(0, 1fr) auto",
		"scroll-margin-top: 9rem",
		"@media (max-width: 40rem)",
		"@media print",
		"&lt;/head&gt; source stays escaped",
		`coverageThemeMarker = id="tested-coverage-theme-v1"`,
	} {
		if !strings.Contains(first.String(), expected) {
			t.Errorf("decorated coverage HTML missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		"@import",
		"url(",
		"http://",
		"https://",
		"box-shadow:",
	} {
		if strings.Contains(first.String(), forbidden) {
			t.Errorf("decorated coverage HTML contains external resource syntax %q", forbidden)
		}
	}
}

func TestDecorateCoverageHTMLWithDiffEncodesPayload(t *testing.T) {
	renderer, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	diff := &coverage.Diff{
		Schema:     coverage.DiffSchema,
		BaseCommit: strings.Repeat("a", 40),
		Files: []coverage.DiffFile{
			{
				ProfilePath: `example.test/pkg/</script>&` +
					"\u2028\u2029" + `.go`,
				OldPath:       "pkg/old.go",
				NewPath:       "pkg/new.go",
				CurrentSHA256: strings.Repeat("b", 64),
				Status:        coverage.DiffStatusModified,
				Hunks: []coverage.DiffHunk{
					{
						OldStart: 1,
						OldLines: 1,
						NewStart: 1,
						NewLines: 1,
						Lines: []coverage.DiffLine{
							{
								Kind:    coverage.DiffLineDelete,
								OldLine: 1,
								Text:    `const oldValue = "</script>&"`,
							},
							{
								Kind:      coverage.DiffLineAdd,
								NewLine:   1,
								Text:      "const newValue = \"safe\"",
								NoNewline: true,
							},
						},
					},
				},
			},
		},
	}
	injection, err := renderer.renderCoverageHead(diff)
	if err != nil {
		t.Fatalf("renderCoverageHead() error = %v", err)
	}
	for _, expected := range [][]byte{
		[]byte(coverageThemeMarker),
		[]byte(coverageExplorerMarker),
		[]byte(coverageDataMarker),
		[]byte(`\u003c/script\u003e`),
		[]byte(`\u0026`),
		[]byte(`\u2028`),
		[]byte(`\u2029`),
	} {
		if !bytes.Contains(injection, expected) {
			t.Errorf("rendered comparison lacks %q", expected)
		}
	}
	if bytes.Contains(injection, []byte(`</script>&`)) {
		t.Fatal("rendered comparison contains an executable script boundary")
	}

	const dataOpen = `<script id="tested-coverage-data-v1" type="application/json">`
	start := bytes.Index(injection, []byte(dataOpen))
	if start < 0 {
		t.Fatal("rendered comparison lacks data element")
	}
	start += len(dataOpen)
	end := bytes.Index(injection[start:], []byte(`</script>`))
	if end < 0 {
		t.Fatal("rendered comparison data element is not closed")
	}
	var decoded coverage.Diff
	if err := json.Unmarshal(injection[start:start+end], &decoded); err != nil {
		t.Fatalf("decode rendered comparison: %v", err)
	}
	if decoded.Schema != diff.Schema ||
		decoded.BaseCommit != diff.BaseCommit ||
		len(decoded.Files) != 1 ||
		decoded.Files[0].ProfilePath != diff.Files[0].ProfilePath ||
		decoded.Files[0].CurrentSHA256 != diff.Files[0].CurrentSHA256 ||
		len(decoded.Files[0].Hunks) != 1 ||
		len(decoded.Files[0].Hunks[0].Lines) != 2 {
		t.Fatalf("decoded comparison = %#v, want %#v", decoded, diff)
	}

	canonical := []byte(
		"<!doctype html><html><head><title>coverage</title></head>" +
			"<body><pre class=file id=file0>source</pre></body></html>",
	)
	var output bytes.Buffer
	if err := renderer.DecorateCoverageHTMLWithDiff(
		context.Background(),
		bytes.NewReader(canonical),
		&output,
		diff,
	); err != nil {
		t.Fatalf("DecorateCoverageHTMLWithDiff() error = %v", err)
	}
	restored := bytes.Replace(output.Bytes(), injection, nil, 1)
	if !bytes.Equal(restored, canonical) {
		t.Fatal("removing rendered comparison did not restore canonical bytes")
	}

	invalid := *diff
	invalid.Schema = "unexpected"
	if _, err := renderer.renderCoverageHead(&invalid); err == nil {
		t.Fatal("renderCoverageHead(invalid schema) error = nil")
	}
}

func TestDecorateCoverageHTMLRejectsInvalidDocuments(t *testing.T) {
	renderer, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		source     string
		singleByte bool
	}{
		{
			name:   "missing closing head",
			source: "<html><head><title>coverage</title><body></body></html>",
		},
		{
			name:       "duplicate closing head",
			source:     "<html><head></head></head><body></body></html>",
			singleByte: true,
		},
		{
			name: "existing theme marker",
			source: "<html><head><meta " + coverageThemeMarker +
				"></head><body></body></html>",
		},
		{
			name:   "head exceeds limit",
			source: "<html><head>" + strings.Repeat("x", coverageHTMLHeadLimit),
		},
		{
			name: "closing head exceeds limit",
			source: strings.Repeat(
				"x",
				coverageHTMLHeadLimit-len(coverageHeadClose)+1,
			) + string(coverageHeadClose),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			var source io.Reader = strings.NewReader(test.source)
			if test.singleByte {
				source = &singleByteReader{reader: source}
			}
			err := renderer.DecorateCoverageHTML(
				context.Background(),
				source,
				&output,
			)
			if err == nil {
				t.Fatal("DecorateCoverageHTML() error = nil")
			}
		})
	}
}

func TestDecorateCoverageHTMLAcceptsClosingHeadAtLimit(t *testing.T) {
	renderer, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	canonical := []byte(
		strings.Repeat(
			"x",
			coverageHTMLHeadLimit-len(coverageHeadClose),
		) +
			string(coverageHeadClose) +
			strings.Repeat("b", 64<<10),
	)

	var output bytes.Buffer
	if err := renderer.DecorateCoverageHTML(
		context.Background(),
		&initialShortReader{
			reader: bytes.NewReader(canonical),
		},
		&output,
	); err != nil {
		t.Fatalf("DecorateCoverageHTML() error = %v", err)
	}
	restored := bytes.Replace(
		output.Bytes(),
		renderer.assets.coverageHead,
		nil,
		1,
	)
	if !bytes.Equal(restored, canonical) {
		t.Fatal("decoration changed canonical bytes at the head size limit")
	}
}

func TestDecorateCoverageHTMLHandlesTerminalReadResults(t *testing.T) {
	renderer, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	canonical := []byte("<html><head></head><body></body></html>")
	readFailure := errors.New("terminal read failure")

	t.Run("data and EOF", func(t *testing.T) {
		var output bytes.Buffer
		err := renderer.DecorateCoverageHTML(
			context.Background(),
			&dataErrorReader{data: canonical, err: io.EOF},
			&output,
		)
		if err != nil {
			t.Fatalf("DecorateCoverageHTML() error = %v", err)
		}
		restored := bytes.Replace(
			output.Bytes(),
			renderer.assets.coverageHead,
			nil,
			1,
		)
		if !bytes.Equal(restored, canonical) {
			t.Fatal("decoration changed canonical terminal bytes")
		}
	})

	t.Run("data and non-EOF error", func(t *testing.T) {
		var output bytes.Buffer
		err := renderer.DecorateCoverageHTML(
			context.Background(),
			&dataErrorReader{data: canonical, err: readFailure},
			&output,
		)
		if !errors.Is(err, readFailure) {
			t.Fatalf("DecorateCoverageHTML() error = %v, want %v", err, readFailure)
		}
		if output.Len() != 0 {
			t.Fatalf("DecorateCoverageHTML() wrote %d bytes before read failure", output.Len())
		}
	})
}

func TestDecorateCoverageHTMLErrors(t *testing.T) {
	renderer, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	canonical := strings.NewReader("<html><head></head><body></body></html>")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name     string
		renderer *Renderer
		ctx      context.Context
		source   io.Reader
		writer   io.Writer
	}{
		{
			name:     "nil renderer",
			renderer: nil,
			ctx:      context.Background(),
			source:   canonical,
			writer:   io.Discard,
		},
		{
			name:     "nil context",
			renderer: renderer,
			source:   canonical,
			writer:   io.Discard,
		},
		{
			name:     "nil source",
			renderer: renderer,
			ctx:      context.Background(),
			writer:   io.Discard,
		},
		{
			name:     "nil destination",
			renderer: renderer,
			ctx:      context.Background(),
			source:   canonical,
		},
		{
			name:     "cancelled",
			renderer: renderer,
			ctx:      ctx,
			source:   canonical,
			writer:   io.Discard,
		},
		{
			name:     "source error",
			renderer: renderer,
			ctx:      context.Background(),
			source:   fixedErrorReader{err: errors.New("read failure")},
			writer:   io.Discard,
		},
		{
			name:     "destination error",
			renderer: renderer,
			ctx:      context.Background(),
			source: strings.NewReader(
				"<html><head></head><body></body></html>",
			),
			writer: errorWriter{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.renderer.DecorateCoverageHTML(
				test.ctx,
				test.source,
				test.writer,
			); err == nil {
				t.Fatal("DecorateCoverageHTML() error = nil")
			}
		})
	}
}

type singleByteReader struct {
	reader io.Reader
}

func (r *singleByteReader) Read(buffer []byte) (int, error) {
	if len(buffer) > 1 {
		buffer = buffer[:1]
	}
	return r.reader.Read(buffer)
}

type initialShortReader struct {
	reader io.Reader
	read   bool
}

func (r *initialShortReader) Read(buffer []byte) (int, error) {
	if !r.read {
		r.read = true
		if len(buffer) > 1 {
			buffer = buffer[:1]
		}
	}
	return r.reader.Read(buffer)
}

type fixedErrorReader struct {
	err error
}

func (r fixedErrorReader) Read([]byte) (int, error) {
	return 0, r.err
}

type dataErrorReader struct {
	data []byte
	err  error
}

func (r *dataErrorReader) Read(buffer []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	read := copy(buffer, r.data)
	r.data = r.data[read:]
	if len(r.data) > 0 {
		return read, nil
	}
	return read, r.err
}
