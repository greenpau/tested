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
	"errors"
	"fmt"
	"io"

	"github.com/greenpau/tested/pkg/artifact"
	"github.com/greenpau/tested/pkg/coverage"
	"github.com/greenpau/tested/pkg/result"
)

const (
	// SummarySchema is the stable summary.json schema identifier.
	SummarySchema = "tested/summary/v1"
	// DefaultSlowestCount is the default number of slow occurrences rendered.
	DefaultSlowestCount = 10
	// DefaultFailureExcerptBytes bounds all failure excerpts in summary.json.
	DefaultFailureExcerptBytes = 64 << 10
)

// Options configures deterministic artifact rendering.
type Options struct {
	Title                  string
	RedactPatterns         []string
	Slowest                int
	MaxFailureExcerptBytes int
	// BasePackage shortens HTML package labels at exact import-path boundaries.
	BasePackage string
}

// Input is one immutable semantic snapshot and its optional coverage profile.
type Input struct {
	Result     result.Result
	Coverage   *coverage.Profile
	Assessment *Assessment
	// CoverageDiff is the successfully published coverage source comparison.
	CoverageDiff *coverage.Diff
}

// Renderer creates artifact projections without modifying primary evidence.
type Renderer struct {
	title                  string
	basePackage            string
	redactors              redactionExpressions
	slowest                int
	maxFailureExcerptBytes int
	assets                 *reportAssetBundle
}

// New validates options and creates a deterministic renderer.
func New(options Options) (*Renderer, error) {
	if options.Slowest < 0 {
		return nil, fmt.Errorf("create report renderer: slowest count must not be negative")
	}
	if options.MaxFailureExcerptBytes < 0 {
		return nil, fmt.Errorf(
			"create report renderer: failure excerpt bytes must not be negative",
		)
	}

	title := options.Title
	if title == "" {
		title = "tested report"
	}
	maxFailureExcerptBytes := options.MaxFailureExcerptBytes
	if maxFailureExcerptBytes == 0 {
		maxFailureExcerptBytes = DefaultFailureExcerptBytes
	}

	redactors, err := compileRedactors(options.RedactPatterns)
	if err != nil {
		return nil, fmt.Errorf("create report renderer: %w", err)
	}
	assets, err := loadEmbeddedReportAssets()
	if err != nil {
		return nil, fmt.Errorf("create report renderer: %w", err)
	}

	return &Renderer{
		title:                  title,
		basePackage:            options.BasePackage,
		redactors:              redactors,
		slowest:                options.Slowest,
		maxFailureExcerptBytes: maxFailureExcerptBytes,
		assets:                 assets,
	}, nil
}

// Publish renders all report-owned derived artifacts. Raw evidence and the
// manifest remain owned by the caller and artifact package.
func (r *Renderer) Publish(layout *artifact.Layout, input Input) error {
	if err := r.PublishTestOutputHTML(layout, input); err != nil {
		return err
	}
	if err := r.PublishSummaryJSON(layout, input); err != nil {
		return err
	}
	if err := r.PublishJUnitXML(layout, input); err != nil {
		return err
	}
	if err := r.PublishIndexHTML(layout, input); err != nil {
		return err
	}
	return nil
}

// PublishTestOutputHTML atomically publishes test_output.html.
func (r *Renderer) PublishTestOutputHTML(layout *artifact.Layout, input Input) error {
	return r.publish(layout, artifact.TestOutputHTML, func(writer io.Writer) error {
		return r.RenderTestOutputHTML(writer, input)
	})
}

// PublishSummaryJSON atomically publishes summary.json.
func (r *Renderer) PublishSummaryJSON(layout *artifact.Layout, input Input) error {
	return r.publish(layout, artifact.SummaryJSON, func(writer io.Writer) error {
		return r.RenderSummaryJSON(writer, input)
	})
}

// PublishJUnitXML atomically publishes junit.xml.
func (r *Renderer) PublishJUnitXML(layout *artifact.Layout, input Input) error {
	return r.publish(layout, artifact.JUnitXML, func(writer io.Writer) error {
		return r.RenderJUnitXML(writer, input)
	})
}

func (r *Renderer) publish(
	layout *artifact.Layout,
	name artifact.Name,
	render func(io.Writer) error,
) error {
	if r == nil {
		return errors.New("publish report: renderer is nil")
	}
	if layout == nil {
		return errors.New("publish report: artifact layout is nil")
	}
	if err := layout.WriteAtomic(name, render); err != nil {
		return fmt.Errorf("publish %s: %w", name, err)
	}
	return nil
}

func (r *Renderer) redact(value string) string {
	limit := redactionLimit(len(value))
	value = applyRedactors(value, r.redactors)
	value = sanitizeUntrusted(value)
	if len(r.redactors) > 0 && len(value) > limit {
		return redactionOmission
	}
	return value
}

func (r *Renderer) redactOutput(value string, truncated bool) string {
	if truncated && len(r.redactors) > 0 {
		return truncatedRedaction
	}
	return r.redact(value)
}

// SanitizeTerminalText applies configured redaction and neutralizes terminal
// control characters in application-level diagnostics.
func (r *Renderer) SanitizeTerminalText(value string) string {
	if r == nil {
		return neutralizeTerminal(value)
	}
	return neutralizeTerminal(r.redact(value))
}
