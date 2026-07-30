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
	"errors"
	"fmt"
	"io"

	"github.com/greenpau/tested/pkg/coverage"
)

const (
	coverageHTMLHeadLimit      = 1 << 20
	coverageHTMLInjectionLimit = 129 << 20
	coverageThemeMarker        = `id="tested-coverage-theme-v1"`
	coverageExplorerMarker     = `id="tested-coverage-explorer-v1"`
	coverageDataMarker         = `id="tested-coverage-data-v1"`
)

var coverageHeadClose = []byte("</head>")

// DecorateCoverageHTML copies Go-authored coverage HTML while inserting
// tested's fixed, self-contained presentation layer before the closing head.
func (r *Renderer) DecorateCoverageHTML(
	ctx context.Context,
	source io.Reader,
	destination io.Writer,
) error {
	return r.decorateCoverageHTML(ctx, source, destination, nil)
}

// DecorateCoverageHTMLWithDiff copies Go-authored coverage HTML while adding
// tested's presentation layer and a safely encoded source comparison model.
// The Go-authored document remains unchanged outside the exact removable head
// injection.
func (r *Renderer) DecorateCoverageHTMLWithDiff(
	ctx context.Context,
	source io.Reader,
	destination io.Writer,
	diff *coverage.Diff,
) error {
	return r.decorateCoverageHTML(ctx, source, destination, diff)
}

func (r *Renderer) decorateCoverageHTML(
	ctx context.Context,
	source io.Reader,
	destination io.Writer,
	diff *coverage.Diff,
) error {
	if r == nil {
		return errors.New("decorate coverage HTML: renderer is nil")
	}
	if ctx == nil {
		return errors.New("decorate coverage HTML: context is nil")
	}
	if source == nil {
		return errors.New("decorate coverage HTML: source is nil")
	}
	if destination == nil {
		return errors.New("decorate coverage HTML: destination is nil")
	}
	if r.assets == nil || r.assets.coverageTemplate == nil ||
		len(r.assets.coverageHead) == 0 {
		return errors.New(
			"decorate coverage HTML: embedded assets are unavailable",
		)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("decorate coverage HTML: %w", context.Cause(ctx))
	}
	injection, err := r.renderCoverageHead(diff)
	if err != nil {
		return err
	}

	head := make([]byte, 0, 32<<10)
	buffer := make([]byte, 32<<10)
	for {
		read, readErr := source.Read(buffer)
		if read > 0 {
			head = append(head, buffer[:read]...)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return fmt.Errorf(
					"decorate coverage HTML: read source: %w",
					readErr,
				)
			}
			if index := bytes.Index(head, coverageHeadClose); index >= 0 {
				if index+len(coverageHeadClose) > coverageHTMLHeadLimit {
					return fmt.Errorf(
						"decorate coverage HTML: closing head exceeds %d bytes",
						coverageHTMLHeadLimit,
					)
				}
				if bytes.Contains(
					head[:index],
					[]byte(coverageThemeMarker),
				) {
					return errors.New(
						"decorate coverage HTML: report already contains the tested theme",
					)
				}
				if bytes.Count(head, coverageHeadClose) != 1 {
					return errors.New(
						"decorate coverage HTML: report contains multiple closing heads",
					)
				}
				if err := writeAll(destination, head[:index]); err != nil {
					return fmt.Errorf("decorate coverage HTML: write head: %w", err)
				}
				if err := writeAll(
					destination,
					injection,
				); err != nil {
					return fmt.Errorf(
						"decorate coverage HTML: write presentation: %w",
						err,
					)
				}
				if err := writeAll(destination, head[index:]); err != nil {
					return fmt.Errorf("decorate coverage HTML: write head suffix: %w", err)
				}
				if errors.Is(readErr, io.EOF) {
					return nil
				}
				if err := copyCoverageHTMLBody(
					ctx,
					destination,
					source,
					head,
				); err != nil {
					return err
				}
				return nil
			}
			if len(head) > coverageHTMLHeadLimit {
				return fmt.Errorf(
					"decorate coverage HTML: closing head exceeds %d bytes",
					coverageHTMLHeadLimit,
				)
			}
			if bytes.Contains(head, []byte(coverageThemeMarker)) {
				return errors.New(
					"decorate coverage HTML: report already contains the tested theme",
				)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return errors.New(
					"decorate coverage HTML: report has no closing head",
				)
			}
			return fmt.Errorf("decorate coverage HTML: read source: %w", readErr)
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("decorate coverage HTML: %w", context.Cause(ctx))
		}
	}
}

func (r *Renderer) renderCoverageHead(diff *coverage.Diff) ([]byte, error) {
	if diff == nil {
		return append([]byte(nil), r.assets.coverageHead...), nil
	}
	if diff.Schema != coverage.DiffSchema {
		return nil, errors.New(
			"decorate coverage HTML: source comparison schema is invalid",
		)
	}
	var output bytes.Buffer
	if err := r.assets.coverageTemplate.ExecuteTemplate(
		&output,
		"coverage_head.html",
		coverageHeadView{Diff: diff},
	); err != nil {
		return nil, fmt.Errorf(
			"decorate coverage HTML: render presentation: %w",
			err,
		)
	}
	data := output.Bytes()
	if len(data) > coverageHTMLInjectionLimit {
		return nil, fmt.Errorf(
			"decorate coverage HTML: rendered presentation exceeds %d bytes",
			coverageHTMLInjectionLimit,
		)
	}
	if bytes.Count(data, []byte(coverageThemeMarker)) != 1 ||
		bytes.Count(data, []byte(coverageExplorerMarker)) != 1 ||
		bytes.Count(data, []byte(coverageDataMarker)) != 1 {
		return nil, errors.New(
			"decorate coverage HTML: rendered presentation markers are invalid",
		)
	}
	if bytes.Contains(data, coverageHeadClose) {
		return nil, errors.New(
			"decorate coverage HTML: rendered presentation closes the document head",
		)
	}
	return append([]byte(nil), data...), nil
}

func copyCoverageHTMLBody(
	ctx context.Context,
	destination io.Writer,
	source io.Reader,
	previous []byte,
) error {
	buffer := make([]byte, 32<<10)
	anchorTail := trailingBytes(previous, len(coverageHeadClose)-1)
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("decorate coverage HTML: %w", context.Cause(ctx))
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			anchorCombined := make([]byte, 0, len(anchorTail)+read)
			anchorCombined = append(anchorCombined, anchorTail...)
			anchorCombined = append(anchorCombined, buffer[:read]...)
			if bytes.Contains(anchorCombined, coverageHeadClose) {
				return errors.New(
					"decorate coverage HTML: report contains multiple closing heads",
				)
			}
			if err := writeAll(destination, buffer[:read]); err != nil {
				return fmt.Errorf("decorate coverage HTML: write body: %w", err)
			}
			anchorTail = trailingBytes(
				anchorCombined,
				len(coverageHeadClose)-1,
			)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return fmt.Errorf("decorate coverage HTML: read body: %w", readErr)
		}
	}
}

func trailingBytes(data []byte, limit int) []byte {
	if limit <= 0 || len(data) == 0 {
		return nil
	}
	if len(data) > limit {
		data = data[len(data)-limit:]
	}
	return append([]byte(nil), data...)
}

func writeAll(destination io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := destination.Write(data)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(data) {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}
