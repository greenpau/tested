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
	"strings"

	"github.com/greenpau/tested/pkg/result"
)

const liveDetailLimit = 4 << 20
const liveLineLimit = 4 << 10

// Stage writes an orchestration stage or periodic status. Stage messages remain
// available after the detail budget fills. Quiet and JSON suppress all live output.
func (c *Console) Stage(message string) error {
	if c == nil {
		return errors.New("render console stage: console is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.quiet || c.format == ConsoleJSON {
		return nil
	}
	c.livePackageSet = false
	return writeConsole(c.writer, c.liveLine("tested", message))
}

// Event writes an incremental normalized update. The bool reports whether any
// live bytes were written, so suppressed details do not postpone a heartbeat.
func (c *Console) Event(update result.Progress) (bool, error) {
	if c == nil {
		return false, errors.New("render console event: console is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.quiet || c.format == ConsoleJSON || c.liveLimited {
		return false, nil
	}
	var text strings.Builder
	label := string(update.Scope)
	if update.Test != nil {
		label += fmt.Sprintf(" %s [occurrence %d]", c.renderer.redact(update.Test.Name), update.Test.Ordinal)
	}
	if update.Diagnostics > 0 {
		text.WriteString(c.liveLine("diagnostic", fmt.Sprintf("%d new integrity diagnostics; inspect the final report", update.Diagnostics)))
	}
	switch update.Action {
	case "start", "run", "pause", "cont", "pass", "fail", "skip", "bench", "build-fail":
		message := label + " — " + string(update.Status)
		if knownDuration(update.Elapsed, update.DurationSource) {
			message += " (" + update.Elapsed.String()
			if update.DurationSource != "" {
				message += ", " + string(update.DurationSource)
			}
			message += ")"
		}
		if counts := update.Tests; counts != nil {
			message += fmt.Sprintf(" — %d tests, %d failed, %d skipped, %d benchmarked, %d incomplete", counts.Total, counts.Failed, counts.Skipped, counts.Benchmarked, counts.Incomplete)
		}
		text.WriteString(c.formatLiveLine(update.Action, message))
	case "attr", "artifacts":
		text.WriteString(c.formatLiveLine(update.Action, label))
	}
	if update.OutputBytes > 0 || update.Output != "" {
		text.WriteString(c.logLines(label, update.Output))
	}
	if update.OutputTruncated && !c.outputClipped {
		c.outputClipped = true
		text.WriteString(c.liveLine("tested", "Log preview truncated by result limits; complete bytes remain in test_output.jsonl"))
	}
	if text.Len() == 0 {
		return false, nil
	}
	// Package identity is context for a contiguous group, not a prefix on every
	// test/log line. Compare original identities so redaction collisions still
	// produce a new context. Emit context and its event in one bounded write.
	scoped := update.Package != "" || update.Scope == result.OutputPackage ||
		update.Scope == result.OutputTest || update.Scope == result.OutputBuild
	detail := text.String()
	if scoped && (!c.livePackageSet || c.livePackage != update.Package) {
		name := c.renderer.redact(update.Package)
		if update.Package == "" {
			name = "(package name unavailable)"
		}
		detail = c.formatLiveLine("package", name) + detail
	}
	written, err := c.writeDetail(detail)
	if err != nil {
		c.livePackageSet = false
	} else if written && !c.liveLimited {
		c.livePackage, c.livePackageSet = update.Package, scoped
	}
	return written, err
}

// Stderr displays already captured child stderr. It never owns raw capture.
func (c *Console) Stderr(value string) (bool, error) {
	if c == nil {
		return false, errors.New("render console stderr: console is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.quiet || c.format == ConsoleJSON || c.liveLimited {
		return false, nil
	}
	written, err := c.writeDetail(c.logLines("stderr", value))
	if err != nil || written {
		c.livePackageSet = false
	}
	return written, err
}

func (c *Console) logLines(label, value string) string {
	// Arbitrary regexes can match across records or read boundaries. Rendering
	// chunks independently would expose fragments of a configured secret.
	if len(c.renderer.redactors) > 0 {
		if c.logsOmitted {
			return ""
		}
		c.logsOmitted = true
		return c.liveLine("tested", "Live log text omitted while redaction is enabled; raw evidence is unchanged")
	}
	clipped := len(value) > 16*1024
	value, _ = truncateUTF8(value, 16*1024)
	var text strings.Builder
	for value != "" {
		if text.Len() >= 64*1024 {
			clipped = true
			break
		}
		line, rest, _ := strings.Cut(value, "\n")
		text.WriteString(c.liveLine("log", label+": "+line))
		value = rest
	}
	if clipped {
		text.WriteString(c.liveLine("tested", "Log event preview truncated; complete bytes remain in raw evidence"))
	}
	return text.String()
}

func (c *Console) liveLine(kind, value string) string {
	return c.formatLiveLine(kind, c.renderer.redact(value))
}

// formatLiveLine accepts a composition of already redacted fields. Redacting
// after prefixing breaks anchored rules; applying rules twice can reprocess an
// omission marker. Keep field redaction separate from destination escaping.
func (c *Console) formatLiveLine(kind, value string) string {
	if c.format == ConsoleMarkdown {
		value = strings.NewReplacer("&", "&amp;", "<", "&lt;").Replace(value)
		value = escapeMarkdown(value)
	} else {
		value = neutralizeTerminalInline(value)
	}
	// Bound after escaping, which may expand hostile text.
	if len(value) > liveLineLimit-80 {
		value, _ = truncateUTF8(value, liveLineLimit-100)
		value += " [preview truncated]"
	}
	if c.format == ConsoleMarkdown {
		return "- **" + kind + "** " + value + "\n"
	}
	return "[" + kind + "] " + value + "\n"
}

func (c *Console) writeDetail(value string) (bool, error) {
	if value == "" {
		return false, nil
	}
	if len(value) > liveDetailLimit-c.liveBytes {
		c.liveLimited = true
		return true, writeConsole(c.writer, c.liveLine("tested", "Live detail limit reached (4 MiB); status and stages continue, complete logs remain in raw evidence"))
	}
	c.liveBytes += len(value)
	return true, writeConsole(c.writer, value)
}
