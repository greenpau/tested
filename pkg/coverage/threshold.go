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
	"math/big"
	"strings"
)

const (
	// MaximumThresholdBytes bounds exact decimal policy inputs. The limit is
	// intentionally far beyond useful percentage precision while preventing
	// an archived run status from forcing unbounded big-integer work.
	MaximumThresholdBytes = 256
)

var (
	// ErrInvalidThreshold identifies a minimum-coverage value outside the
	// strict plain-decimal 0..100 percentage grammar.
	ErrInvalidThreshold = errors.New("invalid coverage threshold")
	// ErrCoverageUnavailable identifies totals with no statement weight and
	// therefore no percentage to compare.
	ErrCoverageUnavailable = errors.New("coverage percentage is unavailable")
	// ErrInvalidTotals identifies contradictory weighted coverage counts.
	ErrInvalidTotals = errors.New("invalid coverage totals")
)

// Threshold is an immutable exact minimum-coverage percentage. Its zero value
// is invalid; construct one with ParseThreshold.
type Threshold struct {
	canonical   string
	numerator   *big.Int
	denominator *big.Int
}

// ParseThreshold parses a CLI percentage using the strict grammar
// DIGIT+("."DIGIT+)? and requires an exact value between 0 and 100 inclusive.
// Signs, whitespace, percent suffixes, exponents, NaN, and infinities are not
// accepted.
func ParseThreshold(value string) (Threshold, error) {
	if len(value) > MaximumThresholdBytes {
		return Threshold{}, fmt.Errorf(
			"%w: percentage exceeds %d bytes",
			ErrInvalidThreshold,
			MaximumThresholdBytes,
		)
	}
	whole, fraction, ok := splitPlainDecimal(value)
	if !ok {
		return Threshold{}, fmt.Errorf(
			"%w: expected a plain decimal percentage between 0 and 100, got %s",
			ErrInvalidThreshold,
			diagnostic(value),
		)
	}

	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	fraction = strings.TrimRight(fraction, "0")
	if len(whole) > 3 || (len(whole) == 3 && whole > "100") ||
		(whole == "100" && fraction != "") {
		return Threshold{}, fmt.Errorf(
			"%w: percentage %s is outside 0..100",
			ErrInvalidThreshold,
			diagnostic(value),
		)
	}

	canonical := whole
	if fraction != "" {
		canonical += "." + fraction
	}
	numeratorText := whole + fraction
	numerator, ok := new(big.Int).SetString(numeratorText, 10)
	if !ok {
		return Threshold{}, fmt.Errorf(
			"%w: parse canonical percentage %s",
			ErrInvalidThreshold,
			diagnostic(canonical),
		)
	}
	denominator := big.NewInt(1)
	if fraction != "" {
		denominator.Exp(
			big.NewInt(10),
			big.NewInt(int64(len(fraction))),
			nil,
		)
	}

	return Threshold{
		canonical:   canonical,
		numerator:   numerator,
		denominator: denominator,
	}, nil
}

// Canonical returns the normalized exact decimal without a percent suffix.
// It returns an empty string for an invalid zero-value Threshold.
func (t Threshold) Canonical() string {
	return t.canonical
}

// String returns the normalized exact decimal without a percent suffix.
func (t Threshold) String() string {
	return t.Canonical()
}

// Display returns the normalized exact decimal with a percent suffix. It
// returns an empty string for an invalid zero-value Threshold.
func (t Threshold) Display() string {
	if t.canonical == "" {
		return ""
	}
	return t.canonical + "%"
}

// Compare compares the exact weighted percentage in totals with the threshold.
// It returns -1 when coverage is below the threshold, 0 at exact equality, and
// +1 when coverage is above the threshold.
func (t Threshold) Compare(totals Totals) (int, error) {
	if t.canonical == "" || t.numerator == nil || t.denominator == nil ||
		t.denominator.Sign() <= 0 {
		return 0, ErrInvalidThreshold
	}
	if totals.Covered > totals.Statements {
		return 0, fmt.Errorf(
			"%w: covered statements %d exceed total statements %d",
			ErrInvalidTotals,
			totals.Covered,
			totals.Statements,
		)
	}
	if totals.Statements == 0 {
		return 0, ErrCoverageUnavailable
	}

	// Compare:
	//
	//   100 * covered / statements  ?  numerator / denominator
	//
	// using arbitrary-precision cross products. The source uint64 values and
	// the threshold's exact decimal representation therefore never overflow or
	// pass through binary floating point.
	left := new(big.Int).SetUint64(totals.Covered)
	left.Mul(left, big.NewInt(100))
	left.Mul(left, t.denominator)

	right := new(big.Int).SetUint64(totals.Statements)
	right.Mul(right, t.numerator)
	return left.Cmp(right), nil
}

// SatisfiedBy reports whether the exact weighted percentage in totals is at or
// above the threshold.
func (t Threshold) SatisfiedBy(totals Totals) (bool, error) {
	comparison, err := t.Compare(totals)
	if err != nil {
		return false, err
	}
	return comparison >= 0, nil
}

func splitPlainDecimal(value string) (string, string, bool) {
	if value == "" {
		return "", "", false
	}
	dot := -1
	for i := 0; i < len(value); i++ {
		switch {
		case value[i] >= '0' && value[i] <= '9':
		case value[i] == '.' && dot < 0:
			dot = i
		default:
			return "", "", false
		}
	}
	if dot < 0 {
		return value, "", true
	}
	if dot == 0 || dot == len(value)-1 {
		return "", "", false
	}
	return value[:dot], value[dot+1:], true
}
