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
)

const (
	coverageHTMLHeadLimit = 1 << 20
	coverageThemeMarker   = `id="tested-coverage-theme-v1"`
)

var coverageHeadClose = []byte("</head>")

// DecorateCoverageHTML copies Go-authored coverage HTML while inserting
// tested's fixed, self-contained presentation layer before the closing head.
func (r *Renderer) DecorateCoverageHTML(
	ctx context.Context,
	source io.Reader,
	destination io.Writer,
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
	if r.assets == nil || len(r.assets.coverageHead) == 0 {
		return errors.New(
			"decorate coverage HTML: embedded assets are unavailable",
		)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("decorate coverage HTML: %w", context.Cause(ctx))
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
					r.assets.coverageHead,
				); err != nil {
					return fmt.Errorf("decorate coverage HTML: write theme: %w", err)
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
