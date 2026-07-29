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
	"strings"
	"unicode"
)

func sanitizeUntrusted(value string) string {
	value = strings.ToValidUTF8(value, string(unicode.ReplacementChar))
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return r
		}
		if r < 0x20 || (r >= 0x7F && r <= 0x9F) {
			return unicode.ReplacementChar
		}
		if isDirectionalControl(r) {
			return unicode.ReplacementChar
		}
		return r
	}, value)
}

func sanitizeXML(value string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t', r == '\n', r == '\r':
			return r
		case r >= 0x20 && r <= 0xD7FF:
			return r
		case r >= 0xE000 && r <= 0xFFFD:
			return r
		case r >= 0x10000 && r <= 0x10FFFF:
			return r
		default:
			return unicode.ReplacementChar
		}
	}, value)
}

func neutralizeTerminal(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch r {
		case '\n', '\t':
			builder.WriteRune(r)
		case '\r':
			builder.WriteString(`\r`)
		default:
			if r < 0x20 || (r >= 0x7F && r <= 0x9F) {
				fmt.Fprintf(&builder, `\u%04X`, r)
				continue
			}
			if isDirectionalControl(r) {
				fmt.Fprintf(&builder, `\u%04X`, r)
				continue
			}
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

// SanitizeTerminalText neutralizes untrusted control and directional text
// before any renderer-specific redaction policy exists.
func SanitizeTerminalText(value string) string {
	return neutralizeTerminal(value)
}

func isDirectionalControl(r rune) bool {
	switch {
	case r == '\u061C', r == '\u200E', r == '\u200F':
		return true
	case r >= '\u202A' && r <= '\u202E':
		return true
	case r >= '\u2066' && r <= '\u2069':
		return true
	default:
		return false
	}
}

func neutralizeTerminalInline(value string) string {
	value = neutralizeTerminal(value)
	value = strings.ReplaceAll(value, "\n", `\n`)
	value = strings.ReplaceAll(value, "\t", `\t`)
	return value
}

func escapeMarkdown(value string) string {
	value = neutralizeTerminalInline(value)
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"`", "\\`",
		"~", "\\~",
		"*", "\\*",
		"_", "\\_",
		"{", "\\{",
		"}", "\\}",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		".", "\\.",
		"!", "\\!",
		"|", "\\|",
		">", "\\>",
	)
	return replacer.Replace(value)
}
