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
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestParseThresholdCanonicalizesPlainPercentages(t *testing.T) {
	tests := []struct {
		input     string
		canonical string
		display   string
	}{
		{input: "0", canonical: "0", display: "0%"},
		{input: "00", canonical: "0", display: "0%"},
		{input: "000.000", canonical: "0", display: "0%"},
		{input: "0.0001", canonical: "0.0001", display: "0.0001%"},
		{input: "000.0100", canonical: "0.01", display: "0.01%"},
		{input: "82.5", canonical: "82.5", display: "82.5%"},
		{input: "082.5000", canonical: "82.5", display: "82.5%"},
		{
			input:     "99.999999999999999999999999999999999999",
			canonical: "99.999999999999999999999999999999999999",
			display:   "99.999999999999999999999999999999999999%",
		},
		{input: "100", canonical: "100", display: "100%"},
		{input: "000100.000000", canonical: "100", display: "100%"},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, err := ParseThreshold(test.input)
			if err != nil {
				t.Fatalf("ParseThreshold() unexpected error: %v", err)
			}
			if got.Canonical() != test.canonical ||
				got.String() != test.canonical ||
				got.Display() != test.display {
				t.Fatalf(
					"threshold = canonical %q, string %q, display %q; want %q, %q, %q",
					got.Canonical(),
					got.String(),
					got.Display(),
					test.canonical,
					test.canonical,
					test.display,
				)
			}

			reparsed, err := ParseThreshold(got.Canonical())
			if err != nil {
				t.Fatalf("ParseThreshold(canonical) unexpected error: %v", err)
			}
			if reparsed.Canonical() != got.Canonical() {
				t.Fatalf(
					"canonical form is not idempotent: %q became %q",
					got.Canonical(),
					reparsed.Canonical(),
				)
			}
		})
	}
}

func TestParseThresholdRejectsNonPlainAndOutOfRangeValues(t *testing.T) {
	tests := []string{
		"",
		".",
		".5",
		"5.",
		"1.2.3",
		" 50",
		"50 ",
		"5 0",
		"+50",
		"-0",
		"50%",
		"NaN",
		"nan",
		"Inf",
		"-Inf",
		"Infinity",
		"1e2",
		"1E2",
		"0x64",
		"1_0",
		"１００",
		"100.000000000000000000000000000000000001",
		"100.01",
		"101",
		"999999999999999999999999999999999999999999999999999999",
		strings.Repeat("9", MaximumThresholdBytes+1),
		"\n50",
	}

	for _, input := range tests {
		t.Run(fmt.Sprintf("%q", input), func(t *testing.T) {
			_, err := ParseThreshold(input)
			if !errors.Is(err, ErrInvalidThreshold) {
				t.Fatalf(
					"ParseThreshold(%q) error = %v, want ErrInvalidThreshold",
					input,
					err,
				)
			}
		})
	}
}

func TestThresholdCompareUsesExactOverflowSafeArithmetic(t *testing.T) {
	const maxUint64 = ^uint64(0)

	tests := []struct {
		name          string
		threshold     string
		totals        Totals
		wantCompare   int
		wantSatisfied bool
	}{
		{
			name:          "zero boundary equality",
			threshold:     "0",
			totals:        Totals{Covered: 0, Statements: 1},
			wantCompare:   0,
			wantSatisfied: true,
		},
		{
			name:          "simple boundary equality",
			threshold:     "12.5",
			totals:        Totals{Covered: 1, Statements: 8},
			wantCompare:   0,
			wantSatisfied: true,
		},
		{
			name:          "uint64 scale boundary equality",
			threshold:     "50",
			totals:        Totals{Covered: maxUint64 / 2, Statements: maxUint64 - 1},
			wantCompare:   0,
			wantSatisfied: true,
		},
		{
			name:          "one unit below half at uint64 maximum",
			threshold:     "50",
			totals:        Totals{Covered: maxUint64 / 2, Statements: maxUint64},
			wantCompare:   -1,
			wantSatisfied: false,
		},
		{
			name:          "one unit above half at uint64 maximum",
			threshold:     "50",
			totals:        Totals{Covered: maxUint64/2 + 1, Statements: maxUint64},
			wantCompare:   1,
			wantSatisfied: true,
		},
		{
			name:          "full coverage at uint64 maximum",
			threshold:     "100",
			totals:        Totals{Covered: maxUint64, Statements: maxUint64},
			wantCompare:   0,
			wantSatisfied: true,
		},
		{
			name:          "one uncovered at uint64 maximum",
			threshold:     "100",
			totals:        Totals{Covered: maxUint64 - 1, Statements: maxUint64},
			wantCompare:   -1,
			wantSatisfied: false,
		},
		{
			name:          "repeating third above finite decimal",
			threshold:     "33.333333333333333333",
			totals:        Totals{Covered: 1, Statements: 3},
			wantCompare:   1,
			wantSatisfied: true,
		},
		{
			name:          "repeating third below larger finite decimal",
			threshold:     "33.333333333333333334",
			totals:        Totals{Covered: 1, Statements: 3},
			wantCompare:   -1,
			wantSatisfied: false,
		},
		{
			name:          "uint64 ratio above nearby decimal",
			threshold:     "99.99999999999999999",
			totals:        Totals{Covered: maxUint64 - 1, Statements: maxUint64},
			wantCompare:   1,
			wantSatisfied: true,
		},
		{
			name:          "uint64 ratio below nearby decimal",
			threshold:     "99.999999999999999999",
			totals:        Totals{Covered: maxUint64 - 1, Statements: maxUint64},
			wantCompare:   -1,
			wantSatisfied: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			threshold := mustThreshold(t, test.threshold)
			gotCompare, err := threshold.Compare(test.totals)
			if err != nil {
				t.Fatalf("Compare() unexpected error: %v", err)
			}
			if gotCompare != test.wantCompare {
				t.Fatalf("Compare() = %d, want %d", gotCompare, test.wantCompare)
			}
			gotSatisfied, err := threshold.SatisfiedBy(test.totals)
			if err != nil {
				t.Fatalf("SatisfiedBy() unexpected error: %v", err)
			}
			if gotSatisfied != test.wantSatisfied {
				t.Fatalf(
					"SatisfiedBy() = %t, want %t",
					gotSatisfied,
					test.wantSatisfied,
				)
			}
		})
	}
}

func TestThresholdComparisonRejectsUnavailableOrInvalidState(t *testing.T) {
	valid := mustThreshold(t, "80")
	tests := []struct {
		name      string
		threshold Threshold
		totals    Totals
		target    error
	}{
		{
			name:      "zero value threshold",
			threshold: Threshold{},
			totals:    Totals{Covered: 8, Statements: 10},
			target:    ErrInvalidThreshold,
		},
		{
			name:      "no statements",
			threshold: valid,
			totals:    Totals{},
			target:    ErrCoverageUnavailable,
		},
		{
			name:      "covered with no statements",
			threshold: valid,
			totals:    Totals{Covered: 1},
			target:    ErrInvalidTotals,
		},
		{
			name:      "covered exceeds statements",
			threshold: valid,
			totals:    Totals{Covered: 11, Statements: 10},
			target:    ErrInvalidTotals,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.threshold.Compare(test.totals)
			if !errors.Is(err, test.target) {
				t.Fatalf("Compare() error = %v, want %v", err, test.target)
			}
			_, err = test.threshold.SatisfiedBy(test.totals)
			if !errors.Is(err, test.target) {
				t.Fatalf("SatisfiedBy() error = %v, want %v", err, test.target)
			}
		})
	}
}

func FuzzParseThreshold(f *testing.F) {
	for _, seed := range []string{
		"0",
		"82.5",
		"000100.000",
		"100.0001",
		"NaN",
		"1e2",
		".5",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		threshold, err := ParseThreshold(input)
		if err != nil {
			return
		}
		canonical := threshold.Canonical()
		if canonical == "" ||
			strings.ContainsAny(canonical, "eE+-% \t\r\n") {
			t.Fatalf("invalid canonical threshold %q", canonical)
		}
		reparsed, err := ParseThreshold(canonical)
		if err != nil || reparsed.Canonical() != canonical {
			t.Fatalf(
				"canonical threshold %q reparsed as %q with error %v",
				canonical,
				reparsed.Canonical(),
				err,
			)
		}
		const maxUint64 = ^uint64(0)
		comparison, err := threshold.Compare(Totals{
			Covered:    maxUint64,
			Statements: maxUint64,
		})
		if err != nil || comparison < 0 {
			t.Fatalf("100%% coverage comparison = (%d, %v)", comparison, err)
		}
	})
}

func mustThreshold(t *testing.T, input string) Threshold {
	t.Helper()
	threshold, err := ParseThreshold(input)
	if err != nil {
		t.Fatalf("ParseThreshold(%q) unexpected error: %v", input, err)
	}
	return threshold
}
