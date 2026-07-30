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

package coverage

import (
	"bytes"
	"fmt"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

func parseGitChanges(
	data []byte,
	currentPaths map[string]struct{},
) (map[string]gitChange, error) {
	changes := make(map[string]gitChange)
	if len(data) == 0 {
		return changes, nil
	}
	if data[len(data)-1] != 0 {
		return nil, fmt.Errorf(
			"%w: Git name-status output is not NUL-terminated",
			ErrDiffMapping,
		)
	}
	nextToken := newNULTokenReader(data)
	recordCount := 0
	for nextToken.more() {
		recordCount++
		if recordCount > maxDiffGitChangeRecords {
			return nil, fmt.Errorf(
				"%w: Git change count exceeds %d",
				ErrDiffLimit,
				maxDiffGitChangeRecords,
			)
		}
		statusBytes, ok := nextToken.next()
		if !ok {
			return nil, fmt.Errorf(
				"%w: Git change status is truncated",
				ErrDiffMapping,
			)
		}
		statusToken := string(statusBytes)
		if statusToken == "" {
			return nil, fmt.Errorf(
				"%w: Git change has an empty status",
				ErrDiffMapping,
			)
		}

		var change gitChange
		switch statusToken[0] {
		case 'A':
			change.status = DiffStatusAdded
		case 'M', 'T':
			change.status = DiffStatusModified
		case 'R':
			change.status = DiffStatusRenamed
		default:
			return nil, fmt.Errorf(
				"%w: unsupported Git change status %q",
				ErrDiffMapping,
				boundedDiagnostic(statusToken),
			)
		}
		if change.status == DiffStatusRenamed {
			if len(statusToken) == 1 {
				return nil, fmt.Errorf(
					"%w: Git rename status lacks a similarity score",
					ErrDiffMapping,
				)
			}
			similarity, err := strconv.ParseUint(statusToken[1:], 10, 16)
			if err != nil || similarity > 100 {
				return nil, fmt.Errorf(
					"%w: Git rename similarity score is invalid",
					ErrDiffMapping,
				)
			}
			oldPath, ok := nextToken.next()
			if !ok {
				return nil, fmt.Errorf(
					"%w: Git rename record is truncated",
					ErrDiffMapping,
				)
			}
			newPath, ok := nextToken.next()
			if !ok {
				return nil, fmt.Errorf(
					"%w: Git rename record is truncated",
					ErrDiffMapping,
				)
			}
			change.oldPath = normalizeProfilePath(string(oldPath))
			change.newPath = normalizeProfilePath(string(newPath))
			if err := validateGitRepositoryPath(change.oldPath); err != nil {
				return nil, err
			}
		} else {
			if len(statusToken) != 1 {
				return nil, fmt.Errorf(
					"%w: Git status %q has an unexpected suffix",
					ErrDiffMapping,
					boundedDiagnostic(statusToken),
				)
			}
			newPath, ok := nextToken.next()
			if !ok {
				return nil, fmt.Errorf(
					"%w: Git change record is truncated",
					ErrDiffMapping,
				)
			}
			change.newPath = normalizeProfilePath(string(newPath))
			if change.status == DiffStatusModified {
				change.oldPath = change.newPath
			}
		}
		if err := validateGitRepositoryPath(change.newPath); err != nil {
			return nil, err
		}
		if _, wanted := currentPaths[change.newPath]; !wanted {
			continue
		}
		if _, duplicate := changes[change.newPath]; duplicate {
			return nil, fmt.Errorf(
				"%w: Git returned duplicate changes for one current source",
				ErrDiffMapping,
			)
		}
		changes[change.newPath] = change
	}
	return changes, nil
}

type nulTokenReader struct {
	data   []byte
	offset int
}

func newNULTokenReader(data []byte) *nulTokenReader {
	return &nulTokenReader{data: data}
}

func (r *nulTokenReader) more() bool {
	return r != nil && r.offset < len(r.data)
}

func (r *nulTokenReader) next() ([]byte, bool) {
	if !r.more() {
		return nil, false
	}
	end := bytes.IndexByte(r.data[r.offset:], 0)
	if end < 0 {
		return nil, false
	}
	end += r.offset
	value := r.data[r.offset:end]
	r.offset = end + 1
	return value, true
}

func validateGitRepositoryPath(value string) error {
	if value == "" ||
		!utf8.ValidString(value) ||
		strings.IndexByte(value, 0) >= 0 ||
		strings.HasPrefix(value, "/") ||
		value == "." ||
		value == ".." ||
		strings.HasPrefix(value, "../") ||
		path.Clean(value) != value {
		return fmt.Errorf(
			"%w: Git returned an invalid repository path",
			ErrDiffMapping,
		)
	}
	return nil
}

func parseZeroContextPatch(
	data []byte,
	remainingHunks, remainingLines int,
) ([]DiffHunk, int, error) {
	if remainingHunks < 0 || remainingLines < 0 {
		return nil, 0, fmt.Errorf("%w: diff cardinality exhausted", ErrDiffLimit)
	}
	var (
		hunks           []DiffHunk
		current         *DiffHunk
		oldLinesSeen    int
		newLinesSeen    int
		oldLineNumber   int
		newLineNumber   int
		retainedLines   int
		lastChangedLine *DiffLine
		havePrevious    bool
		previousOldEnd  int
		previousNewEnd  int
	)
	finishHunk := func() error {
		if current == nil {
			return nil
		}
		if oldLinesSeen != current.OldLines ||
			newLinesSeen != current.NewLines {
			return fmt.Errorf(
				"%w: patch hunk line counts do not match its header",
				ErrDiffMapping,
			)
		}
		current = nil
		lastChangedLine = nil
		return nil
	}

	for offset := 0; offset < len(data); {
		lineEnd := bytes.IndexByte(data[offset:], '\n')
		if lineEnd < 0 {
			return nil, 0, fmt.Errorf(
				"%w: patch output has an unterminated record",
				ErrDiffMapping,
			)
		}
		lineEnd += offset
		line := data[offset:lineEnd]
		offset = lineEnd + 1

		if bytes.HasPrefix(line, []byte("@@ ")) {
			if err := finishHunk(); err != nil {
				return nil, 0, err
			}
			if len(hunks) >= remainingHunks {
				return nil, 0, fmt.Errorf(
					"%w: diff hunk count exceeds %d",
					ErrDiffLimit,
					maxDiffHunks,
				)
			}
			hunk, err := parseHunkHeader(string(line))
			if err != nil {
				return nil, 0, err
			}
			remainingLineCapacity := remainingLines - retainedLines
			if hunk.OldLines > remainingLineCapacity ||
				hunk.NewLines > remainingLineCapacity-hunk.OldLines {
				return nil, 0, fmt.Errorf(
					"%w: diff line count exceeds %d",
					ErrDiffLimit,
					maxDiffLines,
				)
			}
			if hunk.OldStart > maxDiffSourceLines ||
				hunk.OldLines > maxDiffSourceLines ||
				hunk.NewStart > maxDiffSourceLines ||
				hunk.NewLines > maxDiffSourceLines ||
				(hunk.OldLines > 0 && hunk.OldStart == 0) ||
				(hunk.NewLines > 0 && hunk.NewStart == 0) ||
				(hunk.OldLines > 0 &&
					hunk.OldStart >
						maxDiffSourceLines-hunk.OldLines+1) ||
				(hunk.NewLines > 0 &&
					hunk.NewStart >
						maxDiffSourceLines-hunk.NewLines+1) {
				return nil, 0, fmt.Errorf(
					"%w: patch hunk exceeds per-source line bounds",
					ErrDiffLimit,
				)
			}
			if (hunk.OldLines > 0 &&
				hunk.OldStart > int(^uint(0)>>1)-hunk.OldLines+1) ||
				(hunk.NewLines > 0 &&
					hunk.NewStart > int(^uint(0)>>1)-hunk.NewLines+1) {
				return nil, 0, fmt.Errorf(
					"%w: patch hunk line range overflows",
					ErrDiffMapping,
				)
			}
			oldEnd := hunk.OldStart + hunk.OldLines
			newEnd := hunk.NewStart + hunk.NewLines
			if havePrevious &&
				(hunk.OldStart < previousOldEnd ||
					hunk.NewStart < previousNewEnd) {
				return nil, 0, fmt.Errorf(
					"%w: patch hunks are not ordered and non-overlapping",
					ErrDiffMapping,
				)
			}
			havePrevious = true
			previousOldEnd = oldEnd
			previousNewEnd = newEnd
			hunk.Lines = make(
				[]DiffLine,
				0,
				hunk.OldLines+hunk.NewLines,
			)
			hunks = append(hunks, hunk)
			current = &hunks[len(hunks)-1]
			oldLinesSeen = 0
			newLinesSeen = 0
			oldLineNumber = current.OldStart
			newLineNumber = current.NewStart
			lastChangedLine = nil
			continue
		}
		if current == nil {
			continue
		}
		if bytes.Equal(line, []byte(`\ No newline at end of file`)) {
			if lastChangedLine == nil || lastChangedLine.NoNewline {
				return nil, 0, fmt.Errorf(
					"%w: misplaced no-newline marker",
					ErrDiffMapping,
				)
			}
			lastChangedLine.NoNewline = true
			continue
		}
		if len(line) == 0 {
			return nil, 0, fmt.Errorf(
				"%w: empty patch line inside a hunk",
				ErrDiffMapping,
			)
		}
		if retainedLines >= remainingLines {
			return nil, 0, fmt.Errorf(
				"%w: diff line count exceeds %d",
				ErrDiffLimit,
				maxDiffLines,
			)
		}
		text := string(line[1:])
		if !utf8.ValidString(text) {
			return nil, 0, fmt.Errorf(
				"%w: patch line is not valid UTF-8",
				ErrDiffMapping,
			)
		}
		if strings.ContainsAny(text, "\r\t") {
			return nil, 0, fmt.Errorf(
				"%w: patch line is not canonical displayed source",
				ErrDiffMapping,
			)
		}
		switch line[0] {
		case '-':
			if oldLinesSeen >= current.OldLines {
				return nil, 0, fmt.Errorf(
					"%w: patch hunk has too many deleted lines",
					ErrDiffMapping,
				)
			}
			current.Lines = append(current.Lines, DiffLine{
				Kind:    DiffLineDelete,
				OldLine: oldLineNumber,
				Text:    text,
			})
			oldLinesSeen++
			oldLineNumber++
		case '+':
			if newLinesSeen >= current.NewLines {
				return nil, 0, fmt.Errorf(
					"%w: patch hunk has too many added lines",
					ErrDiffMapping,
				)
			}
			current.Lines = append(current.Lines, DiffLine{
				Kind:    DiffLineAdd,
				NewLine: newLineNumber,
				Text:    text,
			})
			newLinesSeen++
			newLineNumber++
		default:
			return nil, 0, fmt.Errorf(
				"%w: non-change line appears in a zero-context hunk",
				ErrDiffMapping,
			)
		}
		retainedLines++
		lastChangedLine = &current.Lines[len(current.Lines)-1]
	}
	if err := finishHunk(); err != nil {
		return nil, 0, err
	}
	return hunks, retainedLines, nil
}

func parseHunkHeader(line string) (DiffHunk, error) {
	if !strings.HasPrefix(line, "@@ -") {
		return DiffHunk{}, fmt.Errorf("%w: malformed patch hunk header", ErrDiffMapping)
	}
	end := strings.Index(line[3:], " @@")
	if end < 0 {
		return DiffHunk{}, fmt.Errorf("%w: malformed patch hunk header", ErrDiffMapping)
	}
	end += 3
	ranges := strings.Fields(line[3:end])
	if len(ranges) != 2 ||
		!strings.HasPrefix(ranges[0], "-") ||
		!strings.HasPrefix(ranges[1], "+") {
		return DiffHunk{}, fmt.Errorf("%w: malformed patch hunk ranges", ErrDiffMapping)
	}
	oldStart, oldLines, err := parseHunkRange(ranges[0][1:])
	if err != nil {
		return DiffHunk{}, err
	}
	newStart, newLines, err := parseHunkRange(ranges[1][1:])
	if err != nil {
		return DiffHunk{}, err
	}
	return DiffHunk{
		OldStart: oldStart,
		OldLines: oldLines,
		NewStart: newStart,
		NewLines: newLines,
	}, nil
}

func parseHunkRange(value string) (int, int, error) {
	startText, countText, hasCount := strings.Cut(value, ",")
	if startText == "" || (hasCount && countText == "") {
		return 0, 0, fmt.Errorf("%w: malformed patch hunk range", ErrDiffMapping)
	}
	start, err := strconv.ParseUint(startText, 10, strconv.IntSize)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: invalid patch hunk start", ErrDiffMapping)
	}
	count := uint64(1)
	if hasCount {
		count, err = strconv.ParseUint(countText, 10, strconv.IntSize)
		if err != nil {
			return 0, 0, fmt.Errorf("%w: invalid patch hunk count", ErrDiffMapping)
		}
	}
	return int(start), int(count), nil
}
