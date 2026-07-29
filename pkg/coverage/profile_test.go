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
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestParseModesAndStatementWeightedTotals(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMode  Mode
		wantCount uint64
	}{
		{
			name: "set",
			input: strings.Join([]string{
				"mode: set",
				"z.example/file.go:1.1,1.2 100 1",
				"a.example/file.go:2.1,2.2 9 0",
				"a.example/file.go:1.1,1.2 1 1",
				"",
			}, "\n"),
			wantMode:  ModeSet,
			wantCount: 1,
		},
		{
			name: "count",
			input: strings.Join([]string{
				"mode: count",
				"z.example/file.go:1.1,1.2 100 7",
				"a.example/file.go:2.1,2.2 9 0",
				"a.example/file.go:1.1,1.2 1 3",
			}, "\r\n"),
			wantMode:  ModeCount,
			wantCount: 3,
		},
		{
			name: "atomic",
			input: strings.Join([]string{
				"mode: atomic",
				"z.example/file.go:1.1,1.2 100 42",
				"a.example/file.go:2.1,2.2 9 0",
				"a.example/file.go:1.1,1.2 1 5",
				"",
			}, "\n"),
			wantMode:  ModeAtomic,
			wantCount: 5,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Parse(strings.NewReader(test.input))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if got.Mode != test.wantMode {
				t.Fatalf("Mode = %q, want %q", got.Mode, test.wantMode)
			}
			if len(got.Files) != 2 {
				t.Fatalf("len(Files) = %d, want 2", len(got.Files))
			}
			if got.Files[0].Name != "a.example/file.go" || got.Files[1].Name != "z.example/file.go" {
				t.Fatalf("file order = %q, %q, want lexical order", got.Files[0].Name, got.Files[1].Name)
			}
			if got.Files[0].Blocks[0].Start.Line != 1 {
				t.Fatalf("block order starts at line %d, want line 1", got.Files[0].Blocks[0].Start.Line)
			}
			if got.Files[0].Blocks[0].Count != test.wantCount {
				t.Fatalf("first a.example count = %d, want %d", got.Files[0].Blocks[0].Count, test.wantCount)
			}

			assertTotals(t, got.Files[0].Totals, 10, 1, 10)
			assertTotals(t, got.Files[1].Totals, 100, 100, 100)
			assertTotals(t, got.Total, 110, 101, 10100.0/110.0)
			totalPercent, _ := got.Total.Percentage()
			firstPercent, _ := got.Files[0].Totals.Percentage()
			secondPercent, _ := got.Files[1].Totals.Percentage()
			if totalPercent == (firstPercent+secondPercent)/2 {
				t.Fatal("total percentage was computed by averaging per-file percentages")
			}
		})
	}
}

func TestParseColonBearingAndLongPaths(t *testing.T) {
	longPath := "example.com/" + strings.Repeat("generated/", 7000) + "file.go"
	if len(longPath) <= 64*1024 {
		t.Fatalf("test path length = %d, expected more than 64 KiB", len(longPath))
	}
	input := fmt.Sprintf(
		"mode: count\nC:\\work\\src\\pkg:file.go:1.2,3.4 2 1\nexample.com/team/repo:generated/file.go:5.6,7.8 3 0\n%s:9.10,11.12 4 2\n",
		longPath,
	)

	got, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	wantNames := []string{
		`C:\work\src\pkg:file.go`,
		"example.com/team/repo:generated/file.go",
		longPath,
	}
	sort.Strings(wantNames)
	if len(got.Files) != len(wantNames) {
		t.Fatalf("len(Files) = %d, want %d", len(got.Files), len(wantNames))
	}
	for i, want := range wantNames {
		if got.Files[i].Name != want {
			t.Errorf("Files[%d].Name = %q, want %q", i, got.Files[i].Name, want)
		}
	}
}

func TestParseHeaderOnlyWithoutNewline(t *testing.T) {
	got, err := Parse(strings.NewReader("mode: set"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got.Mode != ModeSet || len(got.Files) != 0 || got.Total != (Totals{}) {
		t.Fatalf("Parse() = %#v, want empty set profile", got)
	}
	if percent, ok := got.Total.Percentage(); ok || percent != 0 {
		t.Fatalf("empty Percentage() = (%v, %t), want (0, false)", percent, ok)
	}
}

func TestTotalsFormatPercentageUsesExactRationalArithmetic(t *testing.T) {
	for _, test := range []struct {
		name       string
		totals     Totals
		places     int
		want       string
		wantTarget error
	}{
		{
			name: "uint64 scale just below one hundred",
			totals: Totals{
				Statements: math.MaxUint64,
				Covered:    math.MaxUint64 - 1,
			},
			places: 24,
			want:   "99.999999999999999994578989",
		},
		{
			name: "uint64 scale one covered statement",
			totals: Totals{
				Statements: math.MaxUint64,
				Covered:    1,
			},
			places: 24,
			want:   "0.000000000000000005421011",
		},
		{
			name:   "exact rounding",
			totals: Totals{Statements: 6, Covered: 1},
			places: 2,
			want:   "16.67",
		},
		{
			name:       "coverage unavailable",
			totals:     Totals{},
			places:     2,
			wantTarget: ErrCoverageUnavailable,
		},
		{
			name: "contradictory totals",
			totals: Totals{
				Statements: 1,
				Covered:    2,
			},
			places:     2,
			wantTarget: ErrInvalidTotals,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.totals.FormatPercentage(test.places)
			if test.wantTarget != nil {
				if !errors.Is(err, test.wantTarget) {
					t.Fatalf(
						"FormatPercentage() error = %v, want errors.Is(_, %v)",
						err,
						test.wantTarget,
					)
				}
				return
			}
			if err != nil {
				t.Fatalf("FormatPercentage() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("FormatPercentage() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTotalsFormatPercentageBoundsPrecision(t *testing.T) {
	totals := Totals{Statements: 1, Covered: 1}
	for _, places := range []int{-1, MaximumPercentagePrecision + 1} {
		if _, err := totals.FormatPercentage(places); !errors.Is(
			err,
			ErrInvalidPercentagePrecision,
		) {
			t.Fatalf(
				"FormatPercentage(%d) error = %v, want "+
					"ErrInvalidPercentagePrecision",
				places,
				err,
			)
		}
	}
	if got, err := totals.FormatPercentage(MaximumPercentagePrecision); err != nil {
		t.Fatalf("FormatPercentage(maximum) error = %v", err)
	} else if len(got) != len("100.")+MaximumPercentagePrecision {
		t.Fatalf(
			"FormatPercentage(maximum) length = %d, want %d",
			len(got),
			len("100.")+MaximumPercentagePrecision,
		)
	}
}

func TestParseMergesDuplicateBlocks(t *testing.T) {
	tests := []struct {
		name      string
		mode      Mode
		counts    []uint64
		wantCount uint64
	}{
		{name: "set logical or", mode: ModeSet, counts: []uint64{0, 1, 0}, wantCount: 1},
		{name: "count addition", mode: ModeCount, counts: []uint64{2, 3, 4}, wantCount: 9},
		{name: "atomic addition", mode: ModeAtomic, counts: []uint64{10, 20}, wantCount: 30},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var input strings.Builder
			fmt.Fprintf(&input, "mode: %s\n", test.mode)
			for _, count := range test.counts {
				fmt.Fprintf(&input, "file.go:1.1,2.2 7 %d\n", count)
			}
			got, err := Parse(strings.NewReader(input.String()))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if len(got.Files) != 1 || len(got.Files[0].Blocks) != 1 {
				t.Fatalf("duplicate blocks were not merged: %#v", got.Files)
			}
			if got.Files[0].Blocks[0].Count != test.wantCount {
				t.Fatalf("merged count = %d, want %d", got.Files[0].Blocks[0].Count, test.wantCount)
			}
			if got.Total.Statements != 7 {
				t.Fatalf("statement weight = %d, want duplicate weight counted once as 7", got.Total.Statements)
			}
		})
	}
}

func TestParseDuplicateBlockFailures(t *testing.T) {
	max := fmt.Sprintf("%d", uint64(math.MaxUint64))
	tests := []struct {
		name   string
		input  string
		target error
	}{
		{
			name:   "conflicting weights",
			input:  "mode: count\nfile.go:1.1,2.2 7 1\nfile.go:1.1,2.2 8 1\n",
			target: ErrConflictingBlock,
		},
		{
			name: "count overflow",
			input: fmt.Sprintf(
				"mode: count\nfile.go:1.1,2.2 7 %s\nfile.go:1.1,2.2 7 1\n",
				max,
			),
			target: ErrMalformedProfile,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(test.input))
			if !errors.Is(err, test.target) {
				t.Fatalf("Parse() error = %v, want errors.Is(_, %v)", err, test.target)
			}
		})
	}
}

func TestParseWithOptionsBoundsLines(t *testing.T) {
	_, err := ParseWithOptions(
		strings.NewReader("mode: count\nfile.go:1.1,1.2 1 1\n"),
		ParseOptions{MaxLineBytes: len("mode: count")},
	)
	if !errors.Is(err, ErrProfileLineTooLong) {
		t.Fatalf("ParseWithOptions() error = %v, want ErrProfileLineTooLong", err)
	}
	_, err = ParseWithOptions(strings.NewReader("mode: set"), ParseOptions{MaxLineBytes: -1})
	if !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("ParseWithOptions(negative) error = %v, want ErrInvalidLimit", err)
	}
}

func TestParseWithOptionsExcludesLineDelimitersFromBound(t *testing.T) {
	const header = "mode: set"
	for _, test := range []struct {
		name  string
		input string
	}{
		{name: "EOF", input: header},
		{name: "LF", input: header + "\n"},
		{name: "CRLF", input: header + "\r\n"},
		{name: "bare CR at EOF", input: header + "\r"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseWithOptions(
				strings.NewReader(test.input),
				ParseOptions{MaxLineBytes: len(header)},
			)
			if err != nil {
				t.Fatalf("ParseWithOptions() error = %v", err)
			}
			if got.Mode != ModeSet {
				t.Fatalf("Mode = %q, want %q", got.Mode, ModeSet)
			}
		})
	}

	for _, test := range []struct {
		name  string
		input string
	}{
		{name: "EOF", input: header + "x"},
		{name: "LF", input: header + "x\n"},
		{name: "CRLF", input: header + "x\r\n"},
		{name: "bare CR at EOF", input: header + "x\r"},
	} {
		t.Run("reject overlong "+test.name, func(t *testing.T) {
			_, err := ParseWithOptions(
				strings.NewReader(test.input),
				ParseOptions{MaxLineBytes: len(header)},
			)
			if !errors.Is(err, ErrProfileLineTooLong) {
				t.Fatalf(
					"ParseWithOptions() error = %v, want ErrProfileLineTooLong",
					err,
				)
			}
		})
	}
}

func TestReadProfileLineBoundsCRLFAcrossBufferBoundary(t *testing.T) {
	payload := strings.Repeat("x", profileReadBuffer-1)
	reader := bufio.NewReaderSize(
		strings.NewReader(payload+"\r\n"),
		profileReadBuffer,
	)
	got, err := readProfileLine(reader, len(payload))
	if err != nil {
		t.Fatalf("readProfileLine() error = %v", err)
	}
	if got != payload {
		t.Fatalf("readProfileLine() length = %d, want %d", len(got), len(payload))
	}

	reader = bufio.NewReaderSize(
		strings.NewReader(payload+"x\r\n"),
		profileReadBuffer,
	)
	_, err = readProfileLine(reader, len(payload))
	if !errors.Is(err, ErrProfileLineTooLong) {
		t.Fatalf(
			"readProfileLine() error = %v, want ErrProfileLineTooLong",
			err,
		)
	}
}

func TestParseWithOptionsBoundsAggregateProfileBytes(t *testing.T) {
	input := []byte(
		"mode: count\n" +
			"a.go:1.1,1.2 1 1\n" +
			"b.go:2.1,2.2 2 0\n",
	)
	for _, test := range []struct {
		name        string
		maximumRead int
		eofWithData bool
	}{
		{name: "one byte short reads", maximumRead: 1},
		{name: "short final read with EOF", maximumRead: 3, eofWithData: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &trackingShortReader{
				data:        input,
				maximumRead: test.maximumRead,
				eofWithData: test.eofWithData,
			}
			got, err := ParseWithOptions(reader, ParseOptions{
				MaxProfileBytes: len(input),
			})
			if err != nil {
				t.Fatalf("ParseWithOptions() error = %v", err)
			}
			if reader.offset != len(input) {
				t.Fatalf(
					"source bytes read = %d, want exact profile size %d",
					reader.offset,
					len(input),
				)
			}
			if len(got.Files) != 2 {
				t.Fatalf("len(Files) = %d, want 2", len(got.Files))
			}
		})
	}

	maximum := len(input) - 1
	oversized := append(append([]byte(nil), input...), []byte(strings.Repeat("ignored", 1000))...)
	reader := &trackingShortReader{
		data:        oversized,
		maximumRead: 7,
	}
	_, err := ParseWithOptions(reader, ParseOptions{
		MaxProfileBytes: maximum,
	})
	if !errors.Is(err, ErrProfileTooLarge) {
		t.Fatalf(
			"ParseWithOptions() error = %v, want ErrProfileTooLarge",
			err,
		)
	}
	if got, want := reader.offset, maximum+1; got != want {
		t.Fatalf(
			"oversized source bytes read = %d, want bounded probe of %d",
			got,
			want,
		)
	}
}

func TestParseWithOptionsBoundsUniqueFilesAndBlocks(t *testing.T) {
	t.Run("exact files and blocks", func(t *testing.T) {
		input := "mode: count\n" +
			"a.go:1.1,1.2 1 1\n" +
			"a.go:2.1,2.2 1 0\n" +
			"b.go:1.1,1.2 1 1\n"
		got, err := ParseWithOptions(strings.NewReader(input), ParseOptions{
			MaxFiles:  2,
			MaxBlocks: 3,
		})
		if err != nil {
			t.Fatalf("ParseWithOptions() error = %v", err)
		}
		if len(got.Files) != 2 ||
			len(got.Files[0].Blocks)+len(got.Files[1].Blocks) != 3 {
			t.Fatalf("bounded profile = %#v, want two files and three blocks", got)
		}
	})

	t.Run("file over limit", func(t *testing.T) {
		input := "mode: set\n" +
			"a.go:1.1,1.2 1 1\n" +
			"b.go:1.1,1.2 1 1\n"
		_, err := ParseWithOptions(strings.NewReader(input), ParseOptions{
			MaxFiles: 1,
		})
		if !errors.Is(err, ErrTooManyFiles) {
			t.Fatalf(
				"ParseWithOptions() error = %v, want ErrTooManyFiles",
				err,
			)
		}
	})

	t.Run("repeated file consumes one slot", func(t *testing.T) {
		input := "mode: set\n" +
			"a.go:1.1,1.2 1 1\n" +
			"a.go:2.1,2.2 1 0\n"
		got, err := ParseWithOptions(strings.NewReader(input), ParseOptions{
			MaxFiles:  1,
			MaxBlocks: 2,
		})
		if err != nil {
			t.Fatalf("ParseWithOptions() error = %v", err)
		}
		if len(got.Files) != 1 || len(got.Files[0].Blocks) != 2 {
			t.Fatalf("repeated-file profile = %#v", got)
		}
	})

	t.Run("block over limit", func(t *testing.T) {
		input := "mode: set\n" +
			"a.go:1.1,1.2 1 1\n" +
			"a.go:2.1,2.2 1 1\n"
		_, err := ParseWithOptions(strings.NewReader(input), ParseOptions{
			MaxBlocks: 1,
		})
		if !errors.Is(err, ErrTooManyBlocks) {
			t.Fatalf(
				"ParseWithOptions() error = %v, want ErrTooManyBlocks",
				err,
			)
		}
	})

	t.Run("duplicate block consumes one slot", func(t *testing.T) {
		input := "mode: count\n" +
			"a.go:1.1,1.2 1 1\n" +
			"a.go:1.1,1.2 1 2\n"
		got, err := ParseWithOptions(strings.NewReader(input), ParseOptions{
			MaxFiles:  1,
			MaxBlocks: 1,
		})
		if err != nil {
			t.Fatalf("ParseWithOptions() error = %v", err)
		}
		if got.Files[0].Blocks[0].Count != 3 {
			t.Fatalf(
				"duplicate block count = %d, want merged count 3",
				got.Files[0].Blocks[0].Count,
			)
		}
	})
}

func TestParseWithOptionsRejectsNegativeAggregateLimits(t *testing.T) {
	for _, test := range []struct {
		name string
		opts ParseOptions
	}{
		{name: "line bytes", opts: ParseOptions{MaxLineBytes: -1}},
		{name: "profile bytes", opts: ParseOptions{MaxProfileBytes: -1}},
		{name: "files", opts: ParseOptions{MaxFiles: -1}},
		{name: "blocks", opts: ParseOptions{MaxBlocks: -1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseWithOptions(
				strings.NewReader("mode: set\n"),
				test.opts,
			)
			if !errors.Is(err, ErrInvalidLimit) {
				t.Fatalf(
					"ParseWithOptions() error = %v, want ErrInvalidLimit",
					err,
				)
			}
		})
	}
}

func TestDefaultProfileLimitsAreNonzero(t *testing.T) {
	if DefaultMaxLineBytes <= 0 ||
		DefaultMaxProfileBytes <= 0 ||
		DefaultMaxFiles <= 0 ||
		DefaultMaxBlocks <= 0 {
		t.Fatalf(
			"default limits = line:%d profile:%d files:%d blocks:%d",
			DefaultMaxLineBytes,
			DefaultMaxProfileBytes,
			DefaultMaxFiles,
			DefaultMaxBlocks,
		)
	}
	limits, err := resolveParseOptions(ParseOptions{})
	if err != nil {
		t.Fatalf("resolveParseOptions() error = %v", err)
	}
	if limits.maxLineBytes != DefaultMaxLineBytes ||
		limits.maxProfileBytes != DefaultMaxProfileBytes ||
		limits.maxFiles != DefaultMaxFiles ||
		limits.maxBlocks != DefaultMaxBlocks {
		t.Fatalf("resolveParseOptions() = %#v, want exported defaults", limits)
	}
}

func TestMergeProfiles(t *testing.T) {
	first := mustParse(t, "mode: count\nb.go:1.1,1.2 2 1\na.go:1.1,1.2 3 4\n")
	second := mustParse(t, "mode: count\na.go:1.1,1.2 3 5\nc.go:1.1,1.2 7 0\n")

	got, err := Merge(first, second)
	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}
	if len(got.Files) != 3 ||
		got.Files[0].Name != "a.go" ||
		got.Files[1].Name != "b.go" ||
		got.Files[2].Name != "c.go" {
		t.Fatalf("merged files are not deterministic: %#v", got.Files)
	}
	if got.Files[0].Blocks[0].Count != 9 {
		t.Fatalf("duplicate merged count = %d, want 9", got.Files[0].Blocks[0].Count)
	}
	if got.Total.Statements != 12 || got.Total.Covered != 5 {
		t.Fatalf("merged totals = %#v, want 12 statements and 5 covered", got.Total)
	}

	methodGot, err := first.Merge(second)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%#v", methodGot) != fmt.Sprintf("%#v", got) {
		t.Fatalf("Profile.Merge() = %#v, want %#v", methodGot, got)
	}
}

func TestMergeWithOptionsBoundsUniqueOutput(t *testing.T) {
	aFirst := mustParse(t, "mode: count\na.go:1.1,1.2 1 1\n")
	aDuplicate := mustParse(t, "mode: count\na.go:1.1,1.2 1 2\n")
	aSecond := mustParse(t, "mode: count\na.go:2.1,2.2 1 1\n")
	bFirst := mustParse(t, "mode: count\nb.go:1.1,1.2 1 1\n")

	t.Run("exact unique output", func(t *testing.T) {
		got, err := MergeWithOptions(
			MergeOptions{MaxFiles: 2, MaxBlocks: 2},
			aFirst,
			bFirst,
		)
		if err != nil {
			t.Fatalf("MergeWithOptions() error = %v", err)
		}
		if len(got.Files) != 2 ||
			len(got.Files[0].Blocks)+len(got.Files[1].Blocks) != 2 {
			t.Fatalf("MergeWithOptions() = %#v, want two files and blocks", got)
		}
	})

	t.Run("duplicate names and blocks consume one slot", func(t *testing.T) {
		got, err := MergeWithOptions(
			MergeOptions{MaxFiles: 1, MaxBlocks: 1},
			aFirst,
			aDuplicate,
		)
		if err != nil {
			t.Fatalf("MergeWithOptions() error = %v", err)
		}
		if len(got.Files) != 1 ||
			len(got.Files[0].Blocks) != 1 ||
			got.Files[0].Blocks[0].Count != 3 {
			t.Fatalf("duplicate merged output = %#v", got)
		}
	})

	t.Run("file over limit", func(t *testing.T) {
		_, err := MergeWithOptions(
			MergeOptions{MaxFiles: 1},
			aFirst,
			bFirst,
		)
		if !errors.Is(err, ErrTooManyFiles) {
			t.Fatalf(
				"MergeWithOptions() error = %v, want ErrTooManyFiles",
				err,
			)
		}
	})

	t.Run("block over limit", func(t *testing.T) {
		_, err := MergeWithOptions(
			MergeOptions{MaxBlocks: 1},
			aFirst,
			aSecond,
		)
		if !errors.Is(err, ErrTooManyBlocks) {
			t.Fatalf(
				"MergeWithOptions() error = %v, want ErrTooManyBlocks",
				err,
			)
		}
	})

	t.Run("method variant", func(t *testing.T) {
		got, err := aFirst.MergeWithOptions(
			MergeOptions{MaxFiles: 1, MaxBlocks: 1},
			aDuplicate,
		)
		if err != nil {
			t.Fatalf("Profile.MergeWithOptions() error = %v", err)
		}
		if got.Files[0].Blocks[0].Count != 3 {
			t.Fatalf(
				"Profile.MergeWithOptions() count = %d, want 3",
				got.Files[0].Blocks[0].Count,
			)
		}
	})
}

func TestMergeWithOptionsRejectsNegativeLimits(t *testing.T) {
	profile := mustParse(t, "mode: set\na.go:1.1,1.2 1 1\n")
	for _, opts := range []MergeOptions{
		{MaxFiles: -1},
		{MaxBlocks: -1},
	} {
		_, err := MergeWithOptions(opts, profile)
		if !errors.Is(err, ErrInvalidLimit) {
			t.Fatalf(
				"MergeWithOptions(%#v) error = %v, want ErrInvalidLimit",
				opts,
				err,
			)
		}
	}
}

func TestMergeProfileFailures(t *testing.T) {
	setProfile := mustParse(t, "mode: set\nfile.go:1.1,1.2 1 1\n")
	countProfile := mustParse(t, "mode: count\nfile.go:1.1,1.2 1 1\n")
	conflictingWeight := mustParse(t, "mode: count\nfile.go:1.1,1.2 2 1\n")

	tests := []struct {
		name     string
		profiles []*Profile
		target   error
	}{
		{name: "none", target: ErrNoProfiles},
		{name: "nil", profiles: []*Profile{nil}, target: ErrMalformedProfile},
		{name: "mixed modes", profiles: []*Profile{setProfile, countProfile}, target: ErrConflictingMode},
		{
			name:     "conflicting weight",
			profiles: []*Profile{countProfile, conflictingWeight},
			target:   ErrConflictingBlock,
		},
		{
			name: "invalid block",
			profiles: []*Profile{{
				Mode: ModeSet,
				Files: []FileSummary{{
					Name:   "file.go",
					Blocks: []Block{{}},
				}},
			}},
			target: ErrMalformedProfile,
		},
		{
			name: "empty file summary",
			profiles: []*Profile{{
				Mode: ModeSet,
				Files: []FileSummary{{
					Name: "empty.go",
				}},
			}},
			target: ErrMalformedProfile,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Merge(test.profiles...)
			if !errors.Is(err, test.target) {
				t.Fatalf("Merge() error = %v, want errors.Is(_, %v)", err, test.target)
			}
		})
	}
}

func TestParseRejectsMalformedProfiles(t *testing.T) {
	max := fmt.Sprintf("%d", uint64(math.MaxUint64))
	tests := []struct {
		name   string
		input  string
		target error
	}{
		{name: "nil reader", target: ErrMalformedProfile},
		{name: "empty", input: "", target: ErrMalformedProfile},
		{name: "blank", input: "\n", target: ErrMalformedProfile},
		{name: "bad header", input: "mode:set\n", target: ErrMalformedProfile},
		{name: "unsupported mode", input: "mode: race\n", target: ErrMalformedProfile},
		{name: "blank record", input: "mode: set\n\n", target: ErrMalformedProfile},
		{name: "duplicate mode", input: "mode: set\nmode: set\n", target: ErrMalformedProfile},
		{name: "conflicting mode", input: "mode: set\nmode: count\n", target: ErrConflictingMode},
		{name: "missing count", input: "mode: count\nfile.go:1.1,1.2 1\n", target: ErrMalformedProfile},
		{name: "negative count", input: "mode: count\nfile.go:1.1,1.2 1 -1\n", target: ErrMalformedProfile},
		{name: "set count over one", input: "mode: set\nfile.go:1.1,1.2 1 2\n", target: ErrMalformedProfile},
		{name: "missing source", input: "mode: count\n:1.1,1.2 1 1\n", target: ErrMalformedProfile},
		{name: "missing comma", input: "mode: count\nfile.go:1.1-1.2 1 1\n", target: ErrMalformedProfile},
		{name: "extra comma", input: "mode: count\nfile.go:1.1,1.2,1.3 1 1\n", target: ErrMalformedProfile},
		{name: "bad start", input: "mode: count\nfile.go:one.1,1.2 1 1\n", target: ErrMalformedProfile},
		{name: "zero position", input: "mode: count\nfile.go:0.1,1.2 1 1\n", target: ErrMalformedProfile},
		{name: "reverse range", input: "mode: count\nfile.go:2.1,1.2 1 1\n", target: ErrMalformedProfile},
		{
			name: "per-file total overflow",
			input: fmt.Sprintf(
				"mode: count\nfile.go:1.1,1.2 %s 1\nfile.go:2.1,2.2 1 1\n",
				max,
			),
			target: ErrMalformedProfile,
		},
		{
			name: "global total overflow",
			input: fmt.Sprintf(
				"mode: count\na.go:1.1,1.2 %s 1\nb.go:1.1,1.2 1 1\n",
				max,
			),
			target: ErrMalformedProfile,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var reader io.Reader
			if test.name != "nil reader" {
				reader = strings.NewReader(test.input)
			}
			_, err := Parse(reader)
			if err == nil {
				t.Fatal("Parse() error = nil, want error")
			}
			if !errors.Is(err, test.target) {
				t.Fatalf("Parse() error = %v, want errors.Is(_, %v)", err, test.target)
			}
		})
	}
}

func TestParseAcceptsGoZeroStatementBlocks(t *testing.T) {
	profile, err := ParseFile(filepath.Join("testdata", "zero_statement.out"))
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	if len(profile.Files) != 2 ||
		len(profile.Files[0].Blocks) != 2 ||
		len(profile.Files[1].Blocks) != 2 {
		t.Fatalf("zero-statement fixture blocks = %#v, want two files with two blocks each", profile.Files)
	}
	if profile.Files[0].Blocks[0].NumStatements != 0 ||
		profile.Files[0].Blocks[0].Count != 1 ||
		profile.Files[1].Blocks[0].NumStatements != 0 ||
		profile.Files[1].Blocks[0].Count != 0 {
		t.Fatalf("covered and uncovered zero-statement blocks were not preserved: %#v", profile.Files)
	}
	if profile.Total.Statements != 2 || profile.Total.Covered != 1 {
		t.Fatalf("weighted totals = %#v, want zero-weight blocks ignored and 1/2 coverage", profile.Total)
	}
	percentage, available := profile.Total.Percentage()
	if !available || percentage != 50 {
		t.Fatalf("Percentage() = (%v, %t), want (50, true)", percentage, available)
	}
}

func TestParsePropagatesReaderFailure(t *testing.T) {
	wantErr := errors.New("read failure")
	reader := io.MultiReader(
		strings.NewReader("mode: set\n"),
		errorReader{err: wantErr},
	)
	_, err := Parse(reader)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Parse() error = %v, want errors.Is(_, %v)", err, wantErr)
	}
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "coverage.out")
	if err := os.WriteFile(path, []byte("mode: atomic\nfile.go:1.1,1.2 1 9\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	if got.Mode != ModeAtomic || got.Total.Covered != 1 {
		t.Fatalf("ParseFile() = %#v, want one covered atomic statement", got)
	}

	_, err = ParseFile(filepath.Join(dir, "missing.out"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ParseFile(missing) error = %v, want os.ErrNotExist", err)
	}
}

func TestBlockCovered(t *testing.T) {
	tests := []struct {
		count uint64
		want  bool
	}{
		{count: 0, want: false},
		{count: 1, want: true},
		{count: math.MaxUint64, want: true},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("count_%d", test.count), func(t *testing.T) {
			if got := (Block{Count: test.count}).Covered(); got != test.want {
				t.Fatalf("Covered() = %t, want %t", got, test.want)
			}
		})
	}
}

func FuzzParse(f *testing.F) {
	seeds := []string{
		"",
		"mode: set\n",
		"mode: count\nfile.go:1.1,1.2 1 3\n",
		"mode: atomic\r\nC:\\src\\file.go:1.1,1.2 1 9\r\n",
		"mode: set\nfile.go:1.1,1.2 1 -1\n",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		profile, err := Parse(strings.NewReader(input))
		if err != nil {
			return
		}
		if profile.Mode != ModeSet && profile.Mode != ModeCount && profile.Mode != ModeAtomic {
			t.Fatalf("successful parse returned invalid mode %q", profile.Mode)
		}
		if profile.Total.Covered > profile.Total.Statements {
			t.Fatalf("covered %d exceeds statements %d", profile.Total.Covered, profile.Total.Statements)
		}
		for i, file := range profile.Files {
			if i > 0 && profile.Files[i-1].Name > file.Name {
				t.Fatalf("files are not sorted: %q before %q", profile.Files[i-1].Name, file.Name)
			}
			if file.Totals.Covered > file.Totals.Statements {
				t.Fatalf(
					"%q covered %d exceeds statements %d",
					file.Name,
					file.Totals.Covered,
					file.Totals.Statements,
				)
			}
		}
	})
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

type trackingShortReader struct {
	data        []byte
	offset      int
	maximumRead int
	eofWithData bool
}

func (r *trackingShortReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	maximum := r.maximumRead
	if maximum <= 0 || maximum > len(p) {
		maximum = len(p)
	}
	if remaining := len(r.data) - r.offset; maximum > remaining {
		maximum = remaining
	}
	copy(p, r.data[r.offset:r.offset+maximum])
	r.offset += maximum
	if r.eofWithData && r.offset == len(r.data) {
		return maximum, io.EOF
	}
	return maximum, nil
}

func assertTotals(t *testing.T, got Totals, statements, covered uint64, percent float64) {
	t.Helper()
	gotPercent, ok := got.Percentage()
	if got.Statements != statements || got.Covered != covered || !ok || math.Abs(gotPercent-percent) > 1e-12 {
		t.Fatalf(
			"Totals = {%d, %d, %.12f}, want {%d, %d, %.12f}",
			got.Statements,
			got.Covered,
			gotPercent,
			statements,
			covered,
			percent,
		)
	}
}

func mustParse(t *testing.T, input string) *Profile {
	t.Helper()
	profile, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	return profile
}
