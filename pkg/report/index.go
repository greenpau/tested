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
	"os"

	"github.com/greenpau/tested/pkg/artifact"
)

type indexView struct {
	Title     string
	Outcome   string
	Coverage  *coverageView
	Packages  uint64
	Tests     uint64
	Artifacts int
	Links     []indexLink
}

type indexLink struct {
	Name        string
	Label       string
	Description string
	Format      string
}

var indexLinkCatalog = []struct {
	name        artifact.Name
	label       string
	description string
	format      string
}{
	{
		name:        artifact.TestOutputHTML,
		label:       "Test report",
		description: "Searchable package and test occurrence report.",
		format:      "HTML",
	},
	{
		name:        artifact.CoverageHTML,
		label:       "Coverage source",
		description: "Go-annotated source coverage with tested report styling.",
		format:      "HTML",
	},
	{
		name:        artifact.SummaryJSON,
		label:       "Summary JSON",
		description: "Compact tested/summary/v1 result for automation.",
		format:      "JSON",
	},
	{
		name:        artifact.JUnitXML,
		label:       "JUnit XML",
		description: "Test occurrence exchange for CI systems.",
		format:      "XML",
	},
	{
		name:        artifact.TestOutputJSONL,
		label:       "Raw Go event stream",
		description: "Byte-faithful go test -json evidence.",
		format:      "JSONL",
	},
	{
		name:        artifact.StderrLog,
		label:       "Raw standard error",
		description: "Serious child-process diagnostics.",
		format:      "LOG",
	},
	{
		name:        artifact.CoverageProfile,
		label:       "Coverage profile",
		description: "Native Go statement coverage evidence.",
		format:      "COVER",
	},
}

// RenderIndexHTML writes an index containing only fixed, explicitly available
// managed artifact links.
func (r *Renderer) RenderIndexHTML(
	writer io.Writer,
	input Input,
	available []artifact.Name,
) error {
	if r == nil {
		return errors.New("render index HTML: renderer is nil")
	}
	if writer == nil {
		return errors.New("render index HTML: writer is nil")
	}
	present := make(map[artifact.Name]bool, len(available))
	for _, name := range available {
		present[name] = true
	}
	view := indexView{
		Title:    r.redact(r.title),
		Outcome:  outcome(input),
		Coverage: buildCoverageTotalView(input.Coverage),
		Packages: input.Result.Summary.Packages.Total,
		Tests:    input.Result.Summary.Tests.Total,
	}
	for _, candidate := range indexLinkCatalog {
		if !present[candidate.name] {
			continue
		}
		view.Links = append(view.Links, indexLink{
			Name:        string(candidate.name),
			Label:       candidate.label,
			Description: candidate.description,
			Format:      candidate.format,
		})
	}
	view.Artifacts = len(view.Links)
	if r.assets == nil || r.assets.indexTemplate == nil {
		return errors.New("render index HTML: embedded assets are unavailable")
	}
	if err := r.assets.indexTemplate.ExecuteTemplate(
		writer,
		"index.html",
		view,
	); err != nil {
		return fmt.Errorf("render index HTML: %w", err)
	}
	return nil
}

// PublishIndexHTML discovers existing fixed managed artifacts and atomically
// publishes index.html. It rejects symlink and non-regular link targets.
func (r *Renderer) PublishIndexHTML(
	layout *artifact.Layout,
	input Input,
) error {
	if r == nil {
		return errors.New("publish index HTML: renderer is nil")
	}
	if layout == nil {
		return errors.New("publish index HTML: artifact layout is nil")
	}
	available := make([]artifact.Name, 0, len(indexLinkCatalog))
	for _, candidate := range indexLinkCatalog {
		path, err := layout.Path(candidate.name)
		if err != nil {
			return fmt.Errorf(
				"publish index HTML: resolve %s: %w",
				candidate.name,
				err,
			)
		}
		info, err := os.Lstat(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			continue
		case err != nil:
			return fmt.Errorf(
				"publish index HTML: inspect %s: %w",
				candidate.name,
				err,
			)
		case info.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf(
				"publish index HTML: %s is a symbolic link",
				candidate.name,
			)
		case !info.Mode().IsRegular():
			return fmt.Errorf(
				"publish index HTML: %s is not a regular file",
				candidate.name,
			)
		default:
			available = append(available, candidate.name)
		}
	}
	return r.publish(layout, artifact.IndexHTML, func(writer io.Writer) error {
		return r.RenderIndexHTML(writer, input, available)
	})
}
