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

package artifact

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	// DefaultOutputDir is the compatibility output directory used by tested.
	DefaultOutputDir = ".coverage"

	// TestOutputJSONLName is the raw go test -json evidence stream.
	TestOutputJSONLName = "test_output.jsonl"
	// TestOutputHTMLName is the compatibility HTML test report.
	TestOutputHTMLName = "test_output.html"
	// CoverageProfileName is the standard Go coverage profile.
	CoverageProfileName = "coverage.out"
	// CoverageHTMLName is the Go-authored annotated coverage HTML report.
	CoverageHTMLName = "coverage.html"
	// SummaryJSONName is the machine-readable tested summary.
	SummaryJSONName = "summary.json"
	// ManifestJSONName is the SHA-256 artifact inventory.
	ManifestJSONName = "manifest.json"
	// JUnitXMLName is the JUnit-compatible test report.
	JUnitXMLName = "junit.xml"
	// StderrLogName contains diagnostic standard error from the test process.
	StderrLogName = "stderr.log"
	// RunJSONName contains tested-owned child process outcome evidence.
	RunJSONName = "run.json"
	// IndexHTMLName is the human-readable report entrypoint.
	IndexHTMLName = "index.html"

	privateDirMode  = 0o700
	privateFileMode = 0o600
)

// Name identifies a file managed by tested within an artifact directory.
type Name string

const (
	// TestOutputJSONL identifies the raw go test -json evidence stream.
	TestOutputJSONL Name = TestOutputJSONLName
	// TestOutputHTML identifies the compatibility HTML test report.
	TestOutputHTML Name = TestOutputHTMLName
	// CoverageProfile identifies the standard Go coverage profile.
	CoverageProfile Name = CoverageProfileName
	// CoverageHTML identifies the annotated Go coverage HTML report.
	CoverageHTML Name = CoverageHTMLName
	// SummaryJSON identifies the machine-readable tested summary.
	SummaryJSON Name = SummaryJSONName
	// ManifestJSON identifies the SHA-256 artifact inventory.
	ManifestJSON Name = ManifestJSONName
	// JUnitXML identifies the JUnit-compatible report.
	JUnitXML Name = JUnitXMLName
	// StderrLog identifies captured test-process diagnostics.
	StderrLog Name = StderrLogName
	// RunJSON identifies tested-owned child process outcome evidence.
	RunJSON Name = RunJSONName
	// IndexHTML identifies the human-readable report entrypoint.
	IndexHTML Name = IndexHTMLName
)

var (
	// ErrSymlink identifies an artifact root or managed target that is a
	// symbolic link.
	ErrSymlink = errors.New("artifact path is a symbolic link")
	// ErrHardLink identifies a managed regular file with more than one
	// filesystem name. Such a file cannot be chmodded or hashed as private
	// evidence without also operating on an external alias.
	ErrHardLink = errors.New("artifact path has multiple hard links")
	// ErrUnmanaged identifies a filename outside tested's managed inventory.
	ErrUnmanaged = errors.New("unmanaged artifact")
	// ErrNotEvidence identifies a derived report passed to an evidence-only
	// operation.
	ErrNotEvidence = errors.New("artifact is not raw evidence")
	// ErrUnsafeOutput identifies an output root that could mutate the project
	// root, a parent directory, or a path outside the resolved layout.
	ErrUnsafeOutput = errors.New("unsafe artifact output path")
)

var allNames = []Name{
	ManifestJSON,
	CoverageHTML,
	CoverageProfile,
	IndexHTML,
	JUnitXML,
	RunJSON,
	StderrLog,
	SummaryJSON,
	TestOutputHTML,
	TestOutputJSONL,
}

var evidenceNames = []Name{
	CoverageProfile,
	RunJSON,
	StderrLog,
	TestOutputJSONL,
}

var reportNames = []Name{
	ManifestJSON,
	CoverageHTML,
	IndexHTML,
	JUnitXML,
	SummaryJSON,
	TestOutputHTML,
}

// Layout contains absolute paths for every artifact owned by tested.
type Layout struct {
	WorkDir         string
	OutputDir       string
	CoverageProfile string
	CoverageHTML    string
	TestOutputJSONL string
	TestOutputHTML  string
	SummaryJSON     string
	ManifestJSON    string
	JUnitXML        string
	StderrLog       string
	RunJSON         string
	IndexHTML       string
	pathAnchor      string
}

// Resolve builds an artifact layout. A relative outputDir is resolved against
// the absolute workDir; an empty outputDir selects DefaultOutputDir.
func Resolve(workDir, outputDir string) (*Layout, error) {
	if workDir == "" {
		return nil, errors.New("resolve artifact layout: work directory is required")
	}
	absoluteWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return nil, fmt.Errorf("resolve work directory %q: %w", workDir, err)
	}
	absoluteWorkDir = filepath.Clean(absoluteWorkDir)
	workInfo, err := os.Stat(absoluteWorkDir)
	if err != nil {
		return nil, fmt.Errorf("inspect work directory %q: %w", absoluteWorkDir, err)
	}
	if !workInfo.IsDir() {
		return nil, fmt.Errorf("inspect work directory %q: not a directory", absoluteWorkDir)
	}
	rawWorkDir := absoluteWorkDir
	absoluteWorkDir, err = filepath.EvalSymlinks(rawWorkDir)
	if err != nil {
		return nil, fmt.Errorf("resolve work directory symlinks %q: %w", rawWorkDir, err)
	}

	if outputDir == "" {
		outputDir = DefaultOutputDir
	}
	pathAnchor := absoluteWorkDir
	if !filepath.IsAbs(outputDir) {
		cleanOutputDir := filepath.Clean(outputDir)
		if cleanOutputDir == ".." ||
			strings.HasPrefix(cleanOutputDir, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf(
				"%w: relative output directory %q escapes the work directory",
				ErrUnsafeOutput,
				outputDir,
			)
		}
		outputDir = filepath.Join(absoluteWorkDir, outputDir)
	} else {
		rawOutputDir := filepath.Clean(outputDir)
		common := existingCommonAncestor(rawWorkDir, rawOutputDir)
		canonicalCommon, err := filepath.EvalSymlinks(common)
		if err != nil {
			return nil, fmt.Errorf("resolve output path anchor %q: %w", common, err)
		}
		relativeOutput, err := filepath.Rel(common, rawOutputDir)
		if err != nil {
			return nil, fmt.Errorf("resolve output directory %q: %w", rawOutputDir, err)
		}
		outputDir = filepath.Join(canonicalCommon, relativeOutput)
		pathAnchor = canonicalCommon
	}
	outputDir = filepath.Clean(outputDir)
	if err := validateOutputRoot(absoluteWorkDir, outputDir); err != nil {
		return nil, err
	}
	if err := rejectSymlinkComponents(pathAnchor, outputDir); err != nil {
		return nil, err
	}

	layout := &Layout{
		WorkDir:    absoluteWorkDir,
		OutputDir:  outputDir,
		pathAnchor: pathAnchor,
	}
	layout.CoverageProfile = filepath.Join(outputDir, CoverageProfileName)
	layout.CoverageHTML = filepath.Join(outputDir, CoverageHTMLName)
	layout.TestOutputJSONL = filepath.Join(outputDir, TestOutputJSONLName)
	layout.TestOutputHTML = filepath.Join(outputDir, TestOutputHTMLName)
	layout.SummaryJSON = filepath.Join(outputDir, SummaryJSONName)
	layout.ManifestJSON = filepath.Join(outputDir, ManifestJSONName)
	layout.JUnitXML = filepath.Join(outputDir, JUnitXMLName)
	layout.StderrLog = filepath.Join(outputDir, StderrLogName)
	layout.RunJSON = filepath.Join(outputDir, RunJSONName)
	layout.IndexHTML = filepath.Join(outputDir, IndexHTMLName)
	return layout, nil
}

// ManagedNames returns all managed artifact names in lexical order.
func ManagedNames() []Name {
	names := append([]Name(nil), allNames...)
	sort.Slice(names, func(i, j int) bool {
		return names[i] < names[j]
	})
	return names
}

// EvidenceNames returns the raw evidence names preserved by PrepareReport.
func EvidenceNames() []Name {
	names := append([]Name(nil), evidenceNames...)
	sort.Slice(names, func(i, j int) bool {
		return names[i] < names[j]
	})
	return names
}

// Path returns the absolute path for a managed artifact.
func (l *Layout) Path(name Name) (string, error) {
	if l == nil {
		return "", errors.New("artifact layout is nil")
	}
	var path string
	switch name {
	case CoverageProfile:
		path = l.CoverageProfile
	case CoverageHTML:
		path = l.CoverageHTML
	case TestOutputJSONL:
		path = l.TestOutputJSONL
	case TestOutputHTML:
		path = l.TestOutputHTML
	case SummaryJSON:
		path = l.SummaryJSON
	case ManifestJSON:
		path = l.ManifestJSON
	case JUnitXML:
		path = l.JUnitXML
	case StderrLog:
		path = l.StderrLog
	case RunJSON:
		path = l.RunJSON
	case IndexHTML:
		path = l.IndexHTML
	default:
		return "", fmt.Errorf("%w %q", ErrUnmanaged, name)
	}
	expected := filepath.Join(l.OutputDir, string(name))
	if path != expected || !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: managed target %q", ErrUnsafeOutput, path)
	}
	return path, nil
}

// PrepareRun creates a private artifact directory and removes only managed
// files from an earlier run. Unrelated files and directories are untouched.
func (l *Layout) PrepareRun() error {
	if err := l.prepareDirectory(); err != nil {
		return err
	}
	return l.removeManaged(allNames)
}

// PrepareReport creates a private artifact directory and removes only derived
// managed reports. Raw JSONL, coverage-profile, and stderr evidence is
// preserved for offline report generation.
func (l *Layout) PrepareReport() error {
	return l.prepareReport(false)
}

// PrepareReportWithoutCoverage creates a private artifact directory, preserves
// noncoverage evidence, and removes both derived reports and coverage.out.
// Removing an existing profile unlinks only the managed name; it never chmods
// or hashes an external hard-link alias.
func (l *Layout) PrepareReportWithoutCoverage() error {
	return l.prepareReport(true)
}

func (l *Layout) prepareReport(withoutCoverage bool) error {
	if err := l.prepareDirectory(); err != nil {
		return err
	}
	retained := evidenceNames
	removed := reportNames
	if withoutCoverage {
		retained = []Name{
			RunJSON,
			StderrLog,
			TestOutputJSONL,
		}
		removed = append(append([]Name(nil), reportNames...), CoverageProfile)
	}
	if err := l.validateExisting(retained, true); err != nil {
		return err
	}
	if err := l.validateExisting(removed, false); err != nil {
		return err
	}
	if err := l.secureExisting(retained); err != nil {
		return err
	}
	return l.removeManaged(removed)
}

// RemoveEvidence removes one tested-owned raw evidence artifact. It validates
// the exact target and never follows symbolic links.
func (l *Layout) RemoveEvidence(name Name) error {
	if !isEvidence(name) {
		if _, err := l.Path(name); err != nil {
			return err
		}
		return fmt.Errorf("%w: %q", ErrNotEvidence, name)
	}
	return l.removeManaged([]Name{name})
}

// Remove removes one exact tested-owned managed artifact. It validates the
// target and never follows symbolic links.
func (l *Layout) Remove(name Name) error {
	if _, err := l.Path(name); err != nil {
		return err
	}
	return l.removeManaged([]Name{name})
}

func (l *Layout) prepareDirectory() error {
	if l == nil {
		return errors.New("prepare artifact directory: layout is nil")
	}
	if l.OutputDir == "" {
		return errors.New("prepare artifact directory: output directory is empty")
	}
	if err := validateOutputRoot(l.WorkDir, l.OutputDir); err != nil {
		return err
	}
	if err := rejectSymlinkComponents(l.pathAnchor, l.OutputDir); err != nil {
		return err
	}

	info, err := os.Lstat(l.OutputDir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(l.OutputDir, privateDirMode); err != nil {
			return fmt.Errorf("create artifact directory %q: %w", l.OutputDir, err)
		}
		info, err = os.Lstat(l.OutputDir)
		if err != nil {
			return fmt.Errorf("inspect created artifact directory %q: %w", l.OutputDir, err)
		}
	case err != nil:
		return fmt.Errorf("inspect artifact directory %q: %w", l.OutputDir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: output root %q", ErrSymlink, l.OutputDir)
	}
	if !info.IsDir() {
		return fmt.Errorf("artifact output root %q is not a directory", l.OutputDir)
	}
	if err := secureOpenedPath(
		l.OutputDir,
		info,
		true,
		privateDirMode,
		"artifact directory",
	); err != nil {
		return err
	}
	if err := rejectSymlinkComponents(l.pathAnchor, l.OutputDir); err != nil {
		return err
	}
	return nil
}

func (l *Layout) removeManaged(names []Name) error {
	paths := make([]string, 0, len(names))
	for _, name := range names {
		path, err := l.Path(name)
		if err != nil {
			return err
		}
		if err := rejectSymlinkComponents(l.OutputDir, path); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect managed artifact %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: managed target %q", ErrSymlink, path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("managed artifact %q is not a regular file", path)
		}
		paths = append(paths, path)
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove managed artifact %q: %w", path, err)
		}
	}
	return nil
}

func (l *Layout) secureExisting(names []Name) error {
	for _, name := range names {
		path, err := l.Path(name)
		if err != nil {
			return err
		}
		if err := rejectSymlinkComponents(l.OutputDir, path); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect managed artifact %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: managed target %q", ErrSymlink, path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("managed artifact %q is not a regular file", path)
		}
		if err := rejectMultipleLinks(info, path); err != nil {
			return err
		}
		if err := secureOpenedPath(
			path,
			info,
			false,
			privateFileMode,
			"managed artifact",
		); err != nil {
			return err
		}
	}
	return nil
}

func (l *Layout) validateExisting(names []Name, rejectHardLinks bool) error {
	for _, name := range names {
		path, err := l.Path(name)
		if err != nil {
			return err
		}
		if err := rejectSymlinkComponents(l.OutputDir, path); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect managed artifact %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: managed target %q", ErrSymlink, path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("managed artifact %q is not a regular file", path)
		}
		if rejectHardLinks {
			if err := rejectMultipleLinks(info, path); err != nil {
				return err
			}
		}
	}
	return nil
}

func secureOpenedPath(
	path string,
	expected os.FileInfo,
	wantDirectory bool,
	mode os.FileMode,
	label string,
) error {
	if expected == nil {
		return fmt.Errorf("secure %s %q: expected file information is nil", label, path)
	}
	file, err := os.Open(path)
	if err != nil {
		return errors.Join(
			errArtifactChanged,
			fmt.Errorf("open %s %q for securing: %w", label, path, err),
		)
	}
	closeFile := func() error {
		if err := file.Close(); err != nil {
			return fmt.Errorf("close secured %s %q: %w", label, path, err)
		}
		return nil
	}

	opened, err := file.Stat()
	if err != nil {
		return errors.Join(
			errArtifactChanged,
			fmt.Errorf("inspect opened %s %q: %w", label, path, err),
			closeFile(),
		)
	}
	if opened.IsDir() != wantDirectory ||
		(!wantDirectory && !opened.Mode().IsRegular()) ||
		!os.SameFile(expected, opened) {
		return errors.Join(
			errArtifactChanged,
			fmt.Errorf("%s %q changed before securing", label, path),
			closeFile(),
		)
	}
	if !wantDirectory {
		if err := rejectMultipleLinks(opened, path); err != nil {
			return errors.Join(err, closeFile())
		}
	}
	if err := file.Chmod(mode); err != nil {
		return errors.Join(
			fmt.Errorf("set %s permissions on %q: %w", label, path, err),
			closeFile(),
		)
	}
	secured, err := file.Stat()
	if err != nil {
		return errors.Join(
			errArtifactChanged,
			fmt.Errorf("inspect secured %s %q: %w", label, path, err),
			closeFile(),
		)
	}
	if !wantDirectory {
		if err := rejectMultipleLinks(secured, path); err != nil {
			return errors.Join(
				errArtifactChanged,
				err,
				closeFile(),
			)
		}
	}
	if secured.IsDir() != wantDirectory ||
		(!wantDirectory && !secured.Mode().IsRegular()) ||
		!os.SameFile(opened, secured) ||
		(runtime.GOOS != "windows" && secured.Mode().Perm() != mode.Perm()) {
		return errors.Join(
			errArtifactChanged,
			fmt.Errorf("%s %q changed while securing", label, path),
			closeFile(),
		)
	}
	current, err := os.Lstat(path)
	if err != nil {
		return errors.Join(
			errArtifactChanged,
			fmt.Errorf("revalidate secured %s %q: %w", label, path, err),
			closeFile(),
		)
	}
	if current.Mode()&os.ModeSymlink != 0 {
		return errors.Join(
			errArtifactChanged,
			ErrSymlink,
			fmt.Errorf("secured %s pathname %q changed to a symbolic link", label, path),
			closeFile(),
		)
	}
	if !wantDirectory {
		if err := rejectMultipleLinks(current, path); err != nil {
			return errors.Join(
				errArtifactChanged,
				err,
				closeFile(),
			)
		}
	}
	if current.IsDir() != wantDirectory ||
		(!wantDirectory && !current.Mode().IsRegular()) ||
		!os.SameFile(secured, current) ||
		(runtime.GOOS != "windows" && current.Mode().Perm() != mode.Perm()) {
		return errors.Join(
			errArtifactChanged,
			fmt.Errorf("secured %s pathname %q no longer identifies the opened object", label, path),
			closeFile(),
		)
	}
	return closeFile()
}

func isEvidence(name Name) bool {
	for _, candidate := range evidenceNames {
		if candidate == name {
			return true
		}
	}
	return false
}

func validateOutputRoot(workDir, outputDir string) error {
	if !filepath.IsAbs(workDir) || !filepath.IsAbs(outputDir) {
		return fmt.Errorf("%w: work and output directories must be absolute", ErrUnsafeOutput)
	}
	if filepath.Dir(outputDir) == outputDir {
		return fmt.Errorf("%w: filesystem root %q", ErrUnsafeOutput, outputDir)
	}
	if pathWithin(outputDir, workDir) {
		return fmt.Errorf(
			"%w: output directory %q is the work directory or its ancestor",
			ErrUnsafeOutput,
			outputDir,
		)
	}
	return nil
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative == "." ||
		(relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func existingCommonAncestor(first, second string) string {
	if !strings.EqualFold(filepath.VolumeName(first), filepath.VolumeName(second)) {
		return filepath.VolumeName(second) + string(filepath.Separator)
	}
	candidate := filepath.Clean(first)
	for {
		if pathWithin(candidate, second) {
			return candidate
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return candidate
		}
		candidate = parent
	}
}

func rejectSymlinkComponents(anchor, target string) error {
	if anchor == "" || !filepath.IsAbs(anchor) || !filepath.IsAbs(target) || !pathWithin(anchor, target) {
		return fmt.Errorf("%w: %q is not beneath trusted anchor %q", ErrUnsafeOutput, target, anchor)
	}
	info, err := os.Lstat(anchor)
	if err != nil {
		return fmt.Errorf("inspect artifact path component %q: %w", anchor, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: path component %q", ErrSymlink, anchor)
	}

	relative, err := filepath.Rel(anchor, target)
	if err != nil {
		return fmt.Errorf("resolve artifact path %q beneath %q: %w", target, anchor, err)
	}
	current := anchor
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect artifact path component %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: path component %q", ErrSymlink, current)
		}
		if current != target && !info.IsDir() {
			return fmt.Errorf("artifact path component %q is not a directory", current)
		}
	}
	return nil
}
