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
	"math/big"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// DefaultMaxLineBytes bounds a single coverage record while remaining well
	// above bufio.Scanner's 64 KiB default.
	DefaultMaxLineBytes = 16 << 20
	// DefaultMaxProfileBytes bounds all raw profile bytes, including record
	// delimiters.
	DefaultMaxProfileBytes = 256 << 20
	// DefaultMaxFiles bounds unique source paths retained by a profile.
	DefaultMaxFiles = 100_000
	// DefaultMaxBlocks bounds unique normalized source blocks retained by a
	// profile.
	DefaultMaxBlocks = 1_000_000
	// MaximumPercentagePrecision bounds exact decimal presentation work.
	MaximumPercentagePrecision = 256
	profileReadBuffer          = 64 << 10
	maxDiagnosticBytes         = 256
)

// Mode describes how execution counts in a Go coverage profile were recorded.
type Mode string

const (
	// ModeSet records whether a statement block ran at least once.
	ModeSet Mode = "set"
	// ModeCount records the number of times a statement block ran.
	ModeCount Mode = "count"
	// ModeAtomic records execution counts using atomic increments.
	ModeAtomic Mode = "atomic"
)

var (
	// ErrMalformedProfile identifies input that does not follow the Go coverage
	// profile grammar.
	ErrMalformedProfile = errors.New("malformed coverage profile")
	// ErrConflictingMode identifies profiles or declarations that disagree on
	// the coverage mode.
	ErrConflictingMode = errors.New("conflicting coverage mode")
	// ErrConflictingBlock identifies matching source coordinates declared with
	// different statement weights.
	ErrConflictingBlock = errors.New("conflicting coverage block")
	// ErrProfileLineTooLong identifies a profile record that exceeds the
	// configured parser bound.
	ErrProfileLineTooLong = errors.New("coverage profile line too long")
	// ErrProfileTooLarge identifies raw profile evidence that exceeds the
	// configured aggregate byte bound.
	ErrProfileTooLarge = errors.New("coverage profile too large")
	// ErrTooManyFiles identifies a profile or merge result with more unique
	// source paths than permitted.
	ErrTooManyFiles = errors.New("too many coverage files")
	// ErrTooManyBlocks identifies a profile or merge result with more unique
	// normalized blocks than permitted.
	ErrTooManyBlocks = errors.New("too many coverage blocks")
	// ErrInvalidLimit identifies a negative parser or merger safety limit.
	ErrInvalidLimit = errors.New("invalid coverage safety limit")
	// ErrInvalidPercentagePrecision identifies an unsupported exact display
	// precision.
	ErrInvalidPercentagePrecision = errors.New(
		"invalid coverage percentage precision",
	)
	// ErrNoProfiles identifies a merge request with no profile evidence.
	ErrNoProfiles = errors.New("no coverage profiles")
)

// ParseOptions controls bounded coverage profile parsing.
type ParseOptions struct {
	// MaxLineBytes is the largest accepted header or body record excluding its
	// optional LF or CRLF delimiter. Zero selects DefaultMaxLineBytes.
	MaxLineBytes int
	// MaxProfileBytes is the largest accepted raw profile, including line
	// delimiters. The parser reads at most one byte beyond this limit. Zero
	// selects DefaultMaxProfileBytes.
	MaxProfileBytes int
	// MaxFiles is the largest number of unique source paths retained. Repeated
	// records for one path consume one file slot. Zero selects DefaultMaxFiles.
	MaxFiles int
	// MaxBlocks is the largest number of unique source-coordinate blocks
	// retained. Duplicate blocks consume one block slot. Zero selects
	// DefaultMaxBlocks.
	MaxBlocks int
}

// MergeOptions controls bounded in-memory profile merging.
type MergeOptions struct {
	// MaxFiles is the largest number of unique source paths in the merged
	// output. Zero selects DefaultMaxFiles.
	MaxFiles int
	// MaxBlocks is the largest number of unique normalized blocks in the
	// merged output. Duplicate blocks consume one block slot. Zero selects
	// DefaultMaxBlocks.
	MaxBlocks int
}

type profileLimits struct {
	maxLineBytes    int
	maxProfileBytes int
	maxFiles        int
	maxBlocks       int
}

type mergeLimits struct {
	maxFiles  int
	maxBlocks int
}

type boundedProfileReader struct {
	source   io.Reader
	maximum  uint64
	consumed uint64
	exceeded bool
}

func (r *boundedProfileReader) Read(p []byte) (int, error) {
	if r == nil || r.source == nil {
		return 0, fmt.Errorf("%w: bounded reader is nil", ErrMalformedProfile)
	}
	if r.exceeded {
		return 0, ErrProfileTooLarge
	}
	if len(p) == 0 {
		return 0, nil
	}

	remaining := r.maximum + 1 - r.consumed
	if uint64(len(p)) > remaining {
		p = p[:int(remaining)]
	}
	n, err := r.source.Read(p)
	if n < 0 || n > len(p) {
		return 0, fmt.Errorf(
			"%w: reader returned invalid byte count %d for %d-byte buffer",
			ErrMalformedProfile,
			n,
			len(p),
		)
	}
	r.consumed += uint64(n)
	if r.consumed > r.maximum {
		r.exceeded = true
		return n, ErrProfileTooLarge
	}
	return n, err
}

func resolveParseOptions(opts ParseOptions) (profileLimits, error) {
	maxLineBytes, err := resolveSafetyLimit(
		"MaxLineBytes",
		opts.MaxLineBytes,
		DefaultMaxLineBytes,
	)
	if err != nil {
		return profileLimits{}, err
	}
	maxProfileBytes, err := resolveSafetyLimit(
		"MaxProfileBytes",
		opts.MaxProfileBytes,
		DefaultMaxProfileBytes,
	)
	if err != nil {
		return profileLimits{}, err
	}
	maxFiles, err := resolveSafetyLimit(
		"MaxFiles",
		opts.MaxFiles,
		DefaultMaxFiles,
	)
	if err != nil {
		return profileLimits{}, err
	}
	maxBlocks, err := resolveSafetyLimit(
		"MaxBlocks",
		opts.MaxBlocks,
		DefaultMaxBlocks,
	)
	if err != nil {
		return profileLimits{}, err
	}
	return profileLimits{
		maxLineBytes:    maxLineBytes,
		maxProfileBytes: maxProfileBytes,
		maxFiles:        maxFiles,
		maxBlocks:       maxBlocks,
	}, nil
}

func resolveMergeOptions(opts MergeOptions) (mergeLimits, error) {
	maxFiles, err := resolveSafetyLimit(
		"MaxFiles",
		opts.MaxFiles,
		DefaultMaxFiles,
	)
	if err != nil {
		return mergeLimits{}, err
	}
	maxBlocks, err := resolveSafetyLimit(
		"MaxBlocks",
		opts.MaxBlocks,
		DefaultMaxBlocks,
	)
	if err != nil {
		return mergeLimits{}, err
	}
	return mergeLimits{
		maxFiles:  maxFiles,
		maxBlocks: maxBlocks,
	}, nil
}

func resolveSafetyLimit(name string, configured, defaultValue int) (int, error) {
	if configured < 0 {
		return 0, fmt.Errorf(
			"%w: %s must not be negative",
			ErrInvalidLimit,
			name,
		)
	}
	if configured == 0 {
		return defaultValue, nil
	}
	return configured, nil
}

// Position identifies a one-based source position.
type Position struct {
	Line   uint64 `json:"line" xml:"line" yaml:"line"`
	Column uint64 `json:"column" xml:"column" yaml:"column"`
}

// Block describes one statement block in a Go coverage profile.
type Block struct {
	Start         Position `json:"start" xml:"start" yaml:"start"`
	End           Position `json:"end" xml:"end" yaml:"end"`
	NumStatements uint64   `json:"num_statements" xml:"num_statements" yaml:"num_statements"`
	Count         uint64   `json:"count" xml:"count" yaml:"count"`
}

// Covered reports whether the block ran at least once.
func (b Block) Covered() bool {
	return b.Count > 0
}

// Totals contains statement-weighted coverage counts.
type Totals struct {
	Statements uint64 `json:"statements" xml:"statements" yaml:"statements"`
	Covered    uint64 `json:"covered" xml:"covered" yaml:"covered"`
}

// Percentage returns a binary floating-point projection of the
// statement-weighted percentage. The boolean is false for a valid empty
// profile so callers do not confuse absent statement weight with genuine
// zero-percent coverage.
//
// Use FormatPercentage when producing deterministic text or when uint64-scale
// totals require exact decimal rounding.
func (t Totals) Percentage() (float64, bool) {
	if t.Statements == 0 {
		return 0, false
	}
	return float64(t.Covered) * 100 / float64(t.Statements), true
}

// FormatPercentage returns the exact statement-weighted percentage rounded to
// decimalPlaces digits after the decimal point. It performs all arithmetic as
// arbitrary-precision integers and rationals, so uint64-scale totals never
// pass through binary floating point. The returned string does not include a
// percent suffix.
func (t Totals) FormatPercentage(decimalPlaces int) (string, error) {
	if decimalPlaces < 0 || decimalPlaces > MaximumPercentagePrecision {
		return "", fmt.Errorf(
			"%w: decimal places must be between 0 and %d",
			ErrInvalidPercentagePrecision,
			MaximumPercentagePrecision,
		)
	}
	if t.Covered > t.Statements {
		return "", fmt.Errorf(
			"%w: covered statements %d exceed total statements %d",
			ErrInvalidTotals,
			t.Covered,
			t.Statements,
		)
	}
	if t.Statements == 0 {
		return "", ErrCoverageUnavailable
	}

	numerator := new(big.Int).SetUint64(t.Covered)
	numerator.Mul(numerator, big.NewInt(100))
	denominator := new(big.Int).SetUint64(t.Statements)
	return new(big.Rat).SetFrac(numerator, denominator).
		FloatString(decimalPlaces), nil
}

// FileSummary contains coverage blocks and statement-weighted totals for one
// source file.
type FileSummary struct {
	Name   string  `json:"name" xml:"name" yaml:"name"`
	Blocks []Block `json:"blocks" xml:"blocks>block" yaml:"blocks"`
	Totals Totals  `json:"totals" xml:"totals" yaml:"totals"`
}

// Profile is the parsed, deterministic representation of a Go coverage
// profile.
type Profile struct {
	Mode  Mode          `json:"mode" xml:"mode" yaml:"mode"`
	Files []FileSummary `json:"files" xml:"files>file" yaml:"files"`
	Total Totals        `json:"total" xml:"total" yaml:"total"`
}

// Summary is an alias for Profile for callers interested only in aggregate
// coverage results.
type Summary = Profile

// Parse reads a standard Go set, count, or atomic coverage profile.
//
// Parsing is bounded and does not use bufio.Scanner, so valid records with long
// generated source paths are not subject to Scanner's 64 KiB token limit.
func Parse(r io.Reader) (*Profile, error) {
	return ParseWithOptions(r, ParseOptions{})
}

// ParseWithOptions reads a standard Go coverage profile with explicit
// per-record, aggregate-byte, unique-file, and unique-block bounds.
func ParseWithOptions(r io.Reader, opts ParseOptions) (*Profile, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: reader is nil", ErrMalformedProfile)
	}
	limits, err := resolveParseOptions(opts)
	if err != nil {
		return nil, err
	}

	source := &boundedProfileReader{
		source:  r,
		maximum: uint64(limits.maxProfileBytes),
	}
	reader := bufio.NewReaderSize(source, profileReadBuffer)
	header, headerErr := readProfileLine(reader, limits.maxLineBytes)
	if source.exceeded {
		return nil, fmt.Errorf(
			"%w: maximum is %d bytes",
			ErrProfileTooLarge,
			limits.maxProfileBytes,
		)
	}
	if headerErr != nil {
		if errors.Is(headerErr, io.EOF) && header == "" {
			return nil, fmt.Errorf("%w: profile is empty", ErrMalformedProfile)
		}
		if !errors.Is(headerErr, io.EOF) {
			return nil, fmt.Errorf("read coverage mode: %w", headerErr)
		}
	}

	mode, err := parseMode(header)
	if err != nil {
		return nil, fmt.Errorf("line 1: %w", err)
	}
	if errors.Is(headerErr, io.EOF) {
		return summarize(mode, nil)
	}

	files := make(profileBlocks)
	uniqueBlocks := 0
	for lineNumber := 2; ; lineNumber++ {
		line, readErr := readProfileLine(reader, limits.maxLineBytes)
		if source.exceeded {
			return nil, fmt.Errorf(
				"read coverage profile line %d: %w: maximum is %d bytes",
				lineNumber,
				ErrProfileTooLarge,
				limits.maxProfileBytes,
			)
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, fmt.Errorf("read coverage profile line %d: %w", lineNumber, readErr)
		}
		if errors.Is(readErr, io.EOF) && line == "" {
			break
		}

		if strings.HasPrefix(strings.TrimSpace(line), "mode:") {
			otherMode, modeErr := parseMode(line)
			if modeErr != nil {
				return nil, fmt.Errorf("line %d: %w", lineNumber, modeErr)
			}
			if otherMode != mode {
				return nil, fmt.Errorf(
					"line %d: %w: declared %q after %q",
					lineNumber,
					ErrConflictingMode,
					otherMode,
					mode,
				)
			}
			return nil, fmt.Errorf(
				"line %d: %w: duplicate mode declaration %q",
				lineNumber,
				ErrMalformedProfile,
				mode,
			)
		}

		name, block, parseErr := parseBlock(line, mode)
		if parseErr != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, parseErr)
		}
		existingBlocks, knownFile := files[name]
		if !knownFile && len(files) >= limits.maxFiles {
			return nil, fmt.Errorf(
				"line %d: %w: maximum is %d",
				lineNumber,
				ErrTooManyFiles,
				limits.maxFiles,
			)
		}
		key := blockKey{Start: block.Start, End: block.End}
		if _, duplicate := existingBlocks[key]; !duplicate {
			if uniqueBlocks >= limits.maxBlocks {
				return nil, fmt.Errorf(
					"line %d: %w: maximum is %d",
					lineNumber,
					ErrTooManyBlocks,
					limits.maxBlocks,
				)
			}
			uniqueBlocks++
		}
		if err := files.add(mode, name, block); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}

		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	return summarize(mode, files)
}

// ParseFile opens and parses a Go coverage profile.
func ParseFile(path string) (*Profile, error) {
	return ParseFileWithOptions(path, ParseOptions{})
}

// ParseFileWithOptions opens and parses a Go coverage profile with explicit
// parser safety bounds.
func ParseFileWithOptions(path string, opts ParseOptions) (*Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open coverage profile %q: %w", path, err)
	}

	profile, parseErr := ParseWithOptions(f, opts)
	closeErr := f.Close()
	if parseErr != nil {
		parseErr = fmt.Errorf("parse coverage profile %q: %w", path, parseErr)
		if closeErr != nil {
			return nil, errors.Join(
				parseErr,
				fmt.Errorf("close coverage profile %q: %w", path, closeErr),
			)
		}
		return nil, parseErr
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close coverage profile %q: %w", path, closeErr)
	}
	return profile, nil
}

// Merge combines parsed profiles without double-counting duplicate statement
// blocks. All profiles must use the same mode. Conservative default limits
// bound the unique files and blocks in the merged output.
func Merge(profiles ...*Profile) (*Profile, error) {
	return MergeWithOptions(MergeOptions{}, profiles...)
}

// MergeWithOptions combines parsed profiles with explicit output-cardinality
// limits. Duplicate source paths and blocks consume one corresponding slot.
func MergeWithOptions(
	opts MergeOptions,
	profiles ...*Profile,
) (*Profile, error) {
	limits, err := resolveMergeOptions(opts)
	if err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, ErrNoProfiles
	}

	var mode Mode
	blocks := make(profileBlocks)
	seenFiles := make(map[string]struct{})
	uniqueBlocks := 0
	for profileIndex, profile := range profiles {
		if profile == nil {
			return nil, fmt.Errorf("%w: profile %d is nil", ErrMalformedProfile, profileIndex+1)
		}
		if !validMode(profile.Mode) {
			return nil, fmt.Errorf(
				"%w: profile %d has unsupported mode %s",
				ErrMalformedProfile,
				profileIndex+1,
				diagnostic(string(profile.Mode)),
			)
		}
		if profileIndex == 0 {
			mode = profile.Mode
		} else if profile.Mode != mode {
			return nil, fmt.Errorf(
				"%w: profile %d uses %q after %q",
				ErrConflictingMode,
				profileIndex+1,
				profile.Mode,
				mode,
			)
		}

		for _, file := range profile.Files {
			if file.Name == "" {
				return nil, fmt.Errorf("%w: profile %d has an empty source path", ErrMalformedProfile, profileIndex+1)
			}
			if len(file.Blocks) == 0 {
				return nil, fmt.Errorf(
					"%w: profile %d source %s has no coverage blocks",
					ErrMalformedProfile,
					profileIndex+1,
					diagnostic(file.Name),
				)
			}
			if _, exists := seenFiles[file.Name]; !exists {
				if len(seenFiles) >= limits.maxFiles {
					return nil, fmt.Errorf(
						"profile %d source %s: %w: maximum is %d",
						profileIndex+1,
						diagnostic(file.Name),
						ErrTooManyFiles,
						limits.maxFiles,
					)
				}
				seenFiles[file.Name] = struct{}{}
			}
			for _, block := range file.Blocks {
				existingBlocks := blocks[file.Name]
				key := blockKey{Start: block.Start, End: block.End}
				if _, duplicate := existingBlocks[key]; !duplicate {
					if uniqueBlocks >= limits.maxBlocks {
						return nil, fmt.Errorf(
							"profile %d source %s: %w: maximum is %d",
							profileIndex+1,
							diagnostic(file.Name),
							ErrTooManyBlocks,
							limits.maxBlocks,
						)
					}
					uniqueBlocks++
				}
				if err := blocks.add(mode, file.Name, block); err != nil {
					return nil, fmt.Errorf(
						"profile %d source %s: %w",
						profileIndex+1,
						diagnostic(file.Name),
						err,
					)
				}
			}
		}
	}
	return summarize(mode, blocks)
}

// Merge combines the receiver and additional parsed profiles.
func (p *Profile) Merge(profiles ...*Profile) (*Profile, error) {
	all := make([]*Profile, 0, len(profiles)+1)
	all = append(all, p)
	all = append(all, profiles...)
	return Merge(all...)
}

// MergeWithOptions combines the receiver and additional profiles using
// explicit output-cardinality limits.
func (p *Profile) MergeWithOptions(
	opts MergeOptions,
	profiles ...*Profile,
) (*Profile, error) {
	all := make([]*Profile, 0, len(profiles)+1)
	all = append(all, p)
	all = append(all, profiles...)
	return MergeWithOptions(opts, all...)
}

func readProfileLine(r *bufio.Reader, maxBytes int) (string, error) {
	line := make([]byte, 0, min(maxBytes, profileReadBuffer))
	for {
		fragment, err := r.ReadSlice('\n')
		switch {
		case err == nil:
			if profileRecordBytes(line, fragment, false) > uint64(maxBytes) {
				return "", fmt.Errorf("%w: maximum is %d bytes", ErrProfileLineTooLong, maxBytes)
			}
			line = append(line, fragment...)
			return string(trimLineEnding(line)), nil
		case errors.Is(err, bufio.ErrBufferFull):
			buffered := uint64(len(line)) + uint64(len(fragment))
			maxBuffered := uint64(maxBytes) + 1
			if buffered > maxBuffered ||
				(buffered == maxBuffered &&
					combinedLastByte(line, fragment) != '\r') {
				return "", fmt.Errorf("%w: maximum is %d bytes", ErrProfileLineTooLong, maxBytes)
			}
			line = append(line, fragment...)
			continue
		case errors.Is(err, io.EOF):
			if profileRecordBytes(line, fragment, true) > uint64(maxBytes) {
				return "", fmt.Errorf("%w: maximum is %d bytes", ErrProfileLineTooLong, maxBytes)
			}
			line = append(line, fragment...)
			return string(trimLineEnding(line)), io.EOF
		default:
			if uint64(len(line))+uint64(len(fragment)) > uint64(maxBytes) {
				return "", fmt.Errorf("%w: maximum is %d bytes", ErrProfileLineTooLong, maxBytes)
			}
			line = append(line, fragment...)
			return string(line), err
		}
	}
}

func profileRecordBytes(prefix, fragment []byte, eof bool) uint64 {
	total := uint64(len(prefix)) + uint64(len(fragment))
	if total == 0 {
		return 0
	}
	last := combinedByteAt(prefix, fragment, total-1)
	switch {
	case last == '\n':
		total--
		if total > 0 && combinedByteAt(prefix, fragment, total-1) == '\r' {
			total--
		}
	case eof && last == '\r':
		// Preserve the parser's established treatment of a final bare CR as a
		// line ending while applying the same payload bound as EOF/LF/CRLF.
		total--
	}
	return total
}

func combinedLastByte(prefix, fragment []byte) byte {
	total := uint64(len(prefix)) + uint64(len(fragment))
	if total == 0 {
		return 0
	}
	return combinedByteAt(prefix, fragment, total-1)
}

func combinedByteAt(prefix, fragment []byte, index uint64) byte {
	if index < uint64(len(prefix)) {
		return prefix[int(index)]
	}
	return fragment[int(index-uint64(len(prefix)))]
}

func trimLineEnding(line []byte) []byte {
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line
}

func parseMode(line string) (Mode, error) {
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != "mode:" {
		return "", fmt.Errorf("%w: expected \"mode: set\", \"mode: count\", or \"mode: atomic\"", ErrMalformedProfile)
	}

	mode := Mode(fields[1])
	if !validMode(mode) {
		return "", fmt.Errorf("%w: unsupported mode %s", ErrMalformedProfile, diagnostic(string(mode)))
	}
	return mode, nil
}

func validMode(mode Mode) bool {
	switch mode {
	case ModeSet, ModeCount, ModeAtomic:
		return true
	default:
		return false
	}
}

func parseBlock(line string, mode Mode) (string, Block, error) {
	var block Block

	prefix, countText, ok := splitLastField(line)
	if !ok {
		return "", block, fmt.Errorf("%w: expected execution count", ErrMalformedProfile)
	}
	prefix, statementsText, ok := splitLastField(prefix)
	if !ok {
		return "", block, fmt.Errorf("%w: expected statement count", ErrMalformedProfile)
	}

	count, err := parseUint(countText, "execution count")
	if err != nil {
		return "", block, err
	}
	if mode == ModeSet && count > 1 {
		return "", block, fmt.Errorf("%w: set-mode execution count must be 0 or 1", ErrMalformedProfile)
	}
	numStatements, err := parseUint(statementsText, "statement count")
	if err != nil {
		return "", block, err
	}
	delimiter := strings.LastIndexByte(prefix, ':')
	if delimiter < 1 || delimiter == len(prefix)-1 {
		return "", block, fmt.Errorf("%w: expected source path and range", ErrMalformedProfile)
	}
	name := prefix[:delimiter]
	sourceRange := prefix[delimiter+1:]

	rangeParts := strings.Split(sourceRange, ",")
	if len(rangeParts) != 2 {
		return "", block, fmt.Errorf("%w: expected one source range comma", ErrMalformedProfile)
	}
	start, err := parsePosition(rangeParts[0])
	if err != nil {
		return "", block, fmt.Errorf("parse start position: %w", err)
	}
	end, err := parsePosition(rangeParts[1])
	if err != nil {
		return "", block, fmt.Errorf("parse end position: %w", err)
	}

	block = Block{
		Start:         start,
		End:           end,
		NumStatements: numStatements,
		Count:         count,
	}
	if err := validateBlock(mode, block); err != nil {
		return "", block, err
	}
	return name, block, nil
}

func parsePosition(value string) (Position, error) {
	var position Position

	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return position, fmt.Errorf("%w: expected line.column", ErrMalformedProfile)
	}
	line, err := parseUint(parts[0], "line")
	if err != nil {
		return position, err
	}
	column, err := parseUint(parts[1], "column")
	if err != nil {
		return position, err
	}
	if line == 0 || column == 0 {
		return position, fmt.Errorf("%w: source positions are one-based", ErrMalformedProfile)
	}
	position.Line = line
	position.Column = column
	return position, nil
}

func parseUint(value, label string) (uint64, error) {
	if value == "" {
		return 0, fmt.Errorf("%w: %s is empty", ErrMalformedProfile, label)
	}
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf(
			"%w: invalid %s %s: %v",
			ErrMalformedProfile,
			label,
			diagnostic(value),
			err,
		)
	}
	return n, nil
}

func splitLastField(value string) (string, string, bool) {
	end := len(value)
	for end > 0 {
		r, size := runeBefore(value, end)
		if !unicode.IsSpace(r) {
			break
		}
		end -= size
	}
	if end == 0 {
		return "", "", false
	}

	start := end
	for start > 0 {
		r, size := runeBefore(value, start)
		if unicode.IsSpace(r) {
			break
		}
		start -= size
	}
	if start == 0 {
		return "", "", false
	}

	headEnd := start
	for headEnd > 0 {
		r, size := runeBefore(value, headEnd)
		if !unicode.IsSpace(r) {
			break
		}
		headEnd -= size
	}
	if headEnd == 0 {
		return "", "", false
	}
	return value[:headEnd], value[start:end], true
}

func runeBefore(value string, end int) (rune, int) {
	r, size := utf8.DecodeLastRuneInString(value[:end])
	if r == utf8.RuneError && size == 0 {
		return utf8.RuneError, 1
	}
	return r, size
}

type blockKey struct {
	Start Position
	End   Position
}

type profileBlocks map[string]map[blockKey]Block

func (p profileBlocks) add(mode Mode, name string, block Block) error {
	if err := validateBlock(mode, block); err != nil {
		return err
	}
	blocks, ok := p[name]
	if !ok {
		blocks = make(map[blockKey]Block)
		p[name] = blocks
	}

	key := blockKey{Start: block.Start, End: block.End}
	existing, ok := blocks[key]
	if !ok {
		blocks[key] = block
		return nil
	}
	if existing.NumStatements != block.NumStatements {
		return fmt.Errorf(
			"%w: %d.%d,%d.%d has statement weights %d and %d",
			ErrConflictingBlock,
			block.Start.Line,
			block.Start.Column,
			block.End.Line,
			block.End.Column,
			existing.NumStatements,
			block.NumStatements,
		)
	}

	switch mode {
	case ModeSet:
		if block.Count > 0 {
			existing.Count = 1
		}
	case ModeCount, ModeAtomic:
		var ok bool
		existing.Count, ok = addUint64(existing.Count, block.Count)
		if !ok {
			return fmt.Errorf(
				"%w: execution count overflows at %d.%d,%d.%d",
				ErrMalformedProfile,
				block.Start.Line,
				block.Start.Column,
				block.End.Line,
				block.End.Column,
			)
		}
	default:
		return fmt.Errorf("%w: unsupported mode %s", ErrMalformedProfile, diagnostic(string(mode)))
	}
	blocks[key] = existing
	return nil
}

func validateBlock(mode Mode, block Block) error {
	if !validMode(mode) {
		return fmt.Errorf("%w: unsupported mode %s", ErrMalformedProfile, diagnostic(string(mode)))
	}
	if block.Start.Line == 0 || block.Start.Column == 0 ||
		block.End.Line == 0 || block.End.Column == 0 {
		return fmt.Errorf("%w: source positions are one-based", ErrMalformedProfile)
	}
	if block.End.Line < block.Start.Line ||
		(block.End.Line == block.Start.Line && block.End.Column < block.Start.Column) {
		return fmt.Errorf("%w: source range ends before it starts", ErrMalformedProfile)
	}
	if mode == ModeSet && block.Count > 1 {
		return fmt.Errorf("%w: set-mode execution count must be 0 or 1", ErrMalformedProfile)
	}
	return nil
}

func summarize(mode Mode, source profileBlocks) (*Profile, error) {
	profile := &Profile{
		Mode:  mode,
		Files: make([]FileSummary, 0, len(source)),
	}

	names := make([]string, 0, len(source))
	for name := range source {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		blockMap := source[name]
		blocks := make([]Block, 0, len(blockMap))
		for _, block := range blockMap {
			blocks = append(blocks, block)
		}
		sort.SliceStable(blocks, func(i, j int) bool {
			return blockLess(blocks[i], blocks[j])
		})

		file := FileSummary{
			Name:   name,
			Blocks: blocks,
		}
		for _, block := range blocks {
			var ok bool
			file.Totals.Statements, ok = addUint64(file.Totals.Statements, block.NumStatements)
			if !ok {
				return nil, fmt.Errorf(
					"%w: statement total overflows for %s",
					ErrMalformedProfile,
					diagnostic(name),
				)
			}
			if block.Covered() {
				file.Totals.Covered, ok = addUint64(file.Totals.Covered, block.NumStatements)
				if !ok {
					return nil, fmt.Errorf(
						"%w: covered statement total overflows for %s",
						ErrMalformedProfile,
						diagnostic(name),
					)
				}
			}
		}

		var ok bool
		profile.Total.Statements, ok = addUint64(profile.Total.Statements, file.Totals.Statements)
		if !ok {
			return nil, fmt.Errorf("%w: total statement count overflows", ErrMalformedProfile)
		}
		profile.Total.Covered, ok = addUint64(profile.Total.Covered, file.Totals.Covered)
		if !ok {
			return nil, fmt.Errorf("%w: total covered statement count overflows", ErrMalformedProfile)
		}
		profile.Files = append(profile.Files, file)
	}
	return profile, nil
}

func blockLess(a, b Block) bool {
	switch {
	case a.Start.Line != b.Start.Line:
		return a.Start.Line < b.Start.Line
	case a.Start.Column != b.Start.Column:
		return a.Start.Column < b.Start.Column
	case a.End.Line != b.End.Line:
		return a.End.Line < b.End.Line
	case a.End.Column != b.End.Column:
		return a.End.Column < b.End.Column
	case a.NumStatements != b.NumStatements:
		return a.NumStatements < b.NumStatements
	default:
		return a.Count < b.Count
	}
}

func addUint64(a, b uint64) (uint64, bool) {
	sum := a + b
	return sum, sum >= a
}

func diagnostic(value string) string {
	if len(value) <= maxDiagnosticBytes {
		return strconv.Quote(value)
	}
	return fmt.Sprintf(
		"%s...[%d bytes total]",
		strconv.Quote(value[:maxDiagnosticBytes]),
		len(value),
	)
}
