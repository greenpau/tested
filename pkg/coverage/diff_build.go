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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/greenpau/tested/pkg/runner"
)

const (
	defaultGitCommand          = "git"
	maxDiffRevisionBytes       = 1024
	maxDiffProfilePathBytes    = 16 << 10
	maxDiffProfilePathTotal    = 8 << 20
	maxDiffProfileFiles        = 10_000
	maxDiffGoListBytes         = 32 << 20
	maxDiffGitMetadataBytes    = 16 << 20
	maxDiffSourceFileBytes     = 8 << 20
	maxDiffSourceTotalBytes    = 64 << 20
	maxDiffPatchFileBytes      = 16 << 20
	maxDiffPatchTotalBytes     = 64 << 20
	maxDiffFilePayloadBytes    = 8 << 20
	maxDiffRetainedTotalBytes  = 64 << 20
	maxDiffJSONBytes           = 128 << 20
	maxDiffCommandStderrBytes  = 64 << 10
	maxDiffTotalStderrBytes    = 2 << 20
	maxDiffGoPackages          = 100_000
	maxDiffGitChangeRecords    = 250_000
	maxDiffHunks               = 100_000
	maxDiffLines               = 1_000_000
	maxDiffSourceLines         = 100_000
	maxDiffPathspecBatchBytes  = 24 << 10
	maxDiffPathspecBatchCount  = 2048
	diffUnavailableNotGoListed = "coverage source is not a Go-listed file in this repository"
	diffUnavailableAmbiguous   = "coverage source maps to multiple repository files"
)

var (
	// ErrInvalidDiffOptions identifies an invalid explicit comparison request.
	ErrInvalidDiffOptions = errors.New("invalid coverage diff options")
	// ErrDiffLimit identifies comparison input or output that exceeds a fixed
	// resource-safety bound.
	ErrDiffLimit = errors.New("coverage diff safety limit exceeded")
	// ErrDiffMapping identifies invalid or ambiguous source mapping evidence.
	ErrDiffMapping = errors.New("coverage diff source mapping failed")
)

type resolvedDiffOptions struct {
	gitCommand     string
	goCommand      string
	projectDir     string
	baseRevision   string
	profileFiles   []string
	interruptGrace time.Duration
}

type diffBuilder struct {
	ctx              context.Context
	options          resolvedDiffOptions
	repositoryRoot   string
	baseCommit       string
	metadataBudget   byteBudget
	sourceBudget     byteBudget
	patchBudget      byteBudget
	stderrBudget     byteBudget
	parsedHunks      int
	parsedLines      int
	projectionLimits diffProjectionLimits
	projection       diffProjectionState
	sourceByProfile  map[string][]sourceCandidate
}

type diffProjectionLimits struct {
	maxHunks         int
	maxLines         int
	maxRetainedBytes int64
	maxEncodedBytes  int64
}

type diffProjectionState struct {
	hunks         int
	lines         int
	retainedBytes int64
	encodedBytes  int64
}

type byteBudget struct {
	maximum int64
	used    int64
}

type boundedCapture struct {
	buffer    bytes.Buffer
	perLimit  int64
	aggregate *byteBudget
	limitName string
}

type sourceCandidate struct {
	absolutePath   string
	repositoryPath string
}

type goListPackage struct {
	ImportPath string
	Dir        string
}

type gitChange struct {
	status  DiffStatus
	oldPath string
	newPath string
}

type sourceDiff struct {
	oldPath       string
	newPath       string
	currentSHA256 string
	status        DiffStatus
	hunks         []DiffHunk
}

func buildDiff(
	ctx context.Context,
	options DiffOptions,
) (result *Diff, resultErr error) {
	resolved, err := resolveDiffOptions(ctx, options)
	if err != nil {
		return nil, err
	}
	builder := &diffBuilder{
		ctx:     ctx,
		options: resolved,
		metadataBudget: byteBudget{
			maximum: maxDiffGoListBytes + maxDiffGitMetadataBytes,
		},
		sourceBudget:     byteBudget{maximum: maxDiffSourceTotalBytes},
		patchBudget:      byteBudget{maximum: maxDiffPatchTotalBytes},
		stderrBudget:     byteBudget{maximum: maxDiffTotalStderrBytes},
		projectionLimits: defaultDiffProjectionLimits(),
	}
	if err := builder.resolveRepository(); err != nil {
		return nil, err
	}
	if err := builder.resolveBaseCommit(); err != nil {
		return nil, err
	}
	if err := diffContextError(ctx); err != nil {
		return nil, err
	}

	diff := &Diff{
		Schema:     DiffSchema,
		BaseCommit: builder.baseCommit,
		Files:      make([]DiffFile, 0, len(resolved.profileFiles)),
	}
	if err := builder.initializeDiffProjection(diff); err != nil {
		return nil, err
	}
	if len(resolved.profileFiles) == 0 {
		return diff, nil
	}

	if err := builder.resolveCoverageSources(); err != nil {
		return nil, err
	}
	if err := diffContextError(ctx); err != nil {
		return nil, err
	}
	currentPaths := builder.currentRepositoryPaths()
	changes := make(map[string]gitChange)
	if len(currentPaths) > 0 {
		changes, err = builder.readGitChanges(currentPaths)
		if err != nil {
			return nil, err
		}
	}

	tempDir, err := os.MkdirTemp("", "tested-coverage-diff-*")
	if err != nil {
		return nil, fmt.Errorf("build coverage diff: create private snapshot directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			result = nil
			resultErr = errors.Join(
				resultErr,
				fmt.Errorf(
					"build coverage diff: remove private snapshot directory: %w",
					err,
				),
			)
		}
	}()
	if err := os.Chmod(tempDir, 0o700); err != nil {
		return nil, fmt.Errorf(
			"build coverage diff: protect private snapshot directory: %w",
			err,
		)
	}

	cache := make(map[string]sourceDiff, len(currentPaths))
	for _, profilePath := range resolved.profileFiles {
		if err := diffContextError(ctx); err != nil {
			return nil, err
		}
		candidates := builder.sourceByProfile[normalizeProfilePath(profilePath)]
		switch len(candidates) {
		case 0:
			if err := builder.appendProjectedDiffFile(diff, unavailableDiffFile(
				profilePath,
				diffUnavailableNotGoListed,
			)); err != nil {
				return nil, err
			}
			continue
		case 1:
		default:
			if err := builder.appendProjectedDiffFile(diff, unavailableDiffFile(
				profilePath,
				diffUnavailableAmbiguous,
			)); err != nil {
				return nil, err
			}
			continue
		}

		candidate := candidates[0]
		comparison, ok := cache[candidate.repositoryPath]
		if !ok {
			comparison, err = builder.compareSource(
				candidate,
				changes[candidate.repositoryPath],
				tempDir,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"build coverage diff for %q: %w",
					boundedDiagnostic(profilePath),
					err,
				)
			}
			cache[candidate.repositoryPath] = comparison
		}
		file := DiffFile{
			ProfilePath:   profilePath,
			OldPath:       comparison.oldPath,
			NewPath:       comparison.newPath,
			CurrentSHA256: comparison.currentSHA256,
			Status:        comparison.status,
			Hunks:         comparison.hunks,
		}
		if err := builder.appendProjectedDiffFile(diff, file); err != nil {
			return nil, err
		}
	}

	sort.Slice(diff.Files, func(i, j int) bool {
		if diff.Files[i].ProfilePath != diff.Files[j].ProfilePath {
			return diff.Files[i].ProfilePath < diff.Files[j].ProfilePath
		}
		return diff.Files[i].NewPath < diff.Files[j].NewPath
	})
	if err := diffContextError(ctx); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(diff)
	if err != nil {
		return nil, fmt.Errorf("build coverage diff: validate JSON payload: %w", err)
	}
	if len(encoded) > maxDiffJSONBytes {
		return nil, fmt.Errorf(
			"%w: serialized diff exceeds %d bytes",
			ErrDiffLimit,
			maxDiffJSONBytes,
		)
	}
	if int64(len(encoded)) != builder.projection.encodedBytes {
		return nil, fmt.Errorf(
			"%w: projected and serialized diff sizes disagree",
			ErrDiffMapping,
		)
	}
	if err := diffContextError(ctx); err != nil {
		return nil, err
	}
	return diff, nil
}

func resolveDiffOptions(
	ctx context.Context,
	options DiffOptions,
) (resolvedDiffOptions, error) {
	if ctx == nil {
		return resolvedDiffOptions{}, fmt.Errorf(
			"%w: context is nil",
			ErrInvalidDiffOptions,
		)
	}
	if err := ctx.Err(); err != nil {
		return resolvedDiffOptions{}, fmt.Errorf("build coverage diff: %w", err)
	}
	projectDir := strings.TrimSpace(options.ProjectDir)
	if projectDir == "" {
		return resolvedDiffOptions{}, fmt.Errorf(
			"%w: project directory is required",
			ErrInvalidDiffOptions,
		)
	}
	projectDir, err := filepath.Abs(projectDir)
	if err != nil {
		return resolvedDiffOptions{}, fmt.Errorf(
			"%w: resolve project directory: %v",
			ErrInvalidDiffOptions,
			err,
		)
	}
	projectDir, err = filepath.EvalSymlinks(projectDir)
	if err != nil {
		return resolvedDiffOptions{}, fmt.Errorf(
			"%w: resolve project directory links: %v",
			ErrInvalidDiffOptions,
			err,
		)
	}
	info, err := os.Stat(projectDir)
	if err != nil {
		return resolvedDiffOptions{}, fmt.Errorf(
			"%w: inspect project directory: %v",
			ErrInvalidDiffOptions,
			err,
		)
	}
	if !info.IsDir() {
		return resolvedDiffOptions{}, fmt.Errorf(
			"%w: project path is not a directory",
			ErrInvalidDiffOptions,
		)
	}

	baseRevision := options.BaseRevision
	if err := validateBaseRevision(baseRevision); err != nil {
		return resolvedDiffOptions{}, err
	}
	gitCommand, err := resolveDiffCommand("GitCommand", options.GitCommand, defaultGitCommand)
	if err != nil {
		return resolvedDiffOptions{}, err
	}
	goCommand, err := resolveDiffCommand("GoCommand", options.GoCommand, defaultGoCommand)
	if err != nil {
		return resolvedDiffOptions{}, err
	}
	if options.InterruptGrace < 0 {
		return resolvedDiffOptions{}, fmt.Errorf(
			"%w: interrupt grace cannot be negative",
			ErrInvalidDiffOptions,
		)
	}
	if len(options.ProfileFiles) > maxDiffProfileFiles {
		return resolvedDiffOptions{}, fmt.Errorf(
			"%w: profile file count exceeds %d",
			ErrDiffLimit,
			maxDiffProfileFiles,
		)
	}
	profileFiles := append([]string(nil), options.ProfileFiles...)
	profilePathBytes := 0
	for i, profilePath := range profileFiles {
		if profilePath == "" {
			return resolvedDiffOptions{}, fmt.Errorf(
				"%w: profile file %d is empty",
				ErrInvalidDiffOptions,
				i,
			)
		}
		if len(profilePath) > maxDiffProfilePathBytes {
			return resolvedDiffOptions{}, fmt.Errorf(
				"%w: profile file %d exceeds %d bytes",
				ErrDiffLimit,
				i,
				maxDiffProfilePathBytes,
			)
		}
		if len(profilePath) > maxDiffProfilePathTotal-profilePathBytes {
			return resolvedDiffOptions{}, fmt.Errorf(
				"%w: aggregate profile paths exceed %d bytes",
				ErrDiffLimit,
				maxDiffProfilePathTotal,
			)
		}
		profilePathBytes += len(profilePath)
		if !utf8.ValidString(profilePath) || strings.IndexByte(profilePath, 0) >= 0 {
			return resolvedDiffOptions{}, fmt.Errorf(
				"%w: profile file %d is not a valid path string",
				ErrInvalidDiffOptions,
				i,
			)
		}
	}
	sort.Strings(profileFiles)
	profileFiles = compactStrings(profileFiles)

	return resolvedDiffOptions{
		gitCommand:     gitCommand,
		goCommand:      goCommand,
		projectDir:     projectDir,
		baseRevision:   baseRevision,
		profileFiles:   profileFiles,
		interruptGrace: options.InterruptGrace,
	}, nil
}

func validateBaseRevision(revision string) error {
	if revision == "" {
		return fmt.Errorf("%w: base revision is required", ErrInvalidDiffOptions)
	}
	if len(revision) > maxDiffRevisionBytes {
		return fmt.Errorf(
			"%w: base revision exceeds %d bytes",
			ErrDiffLimit,
			maxDiffRevisionBytes,
		)
	}
	if !utf8.ValidString(revision) {
		return fmt.Errorf(
			"%w: base revision is not valid UTF-8",
			ErrInvalidDiffOptions,
		)
	}
	if strings.HasPrefix(revision, "-") {
		return fmt.Errorf(
			"%w: base revision cannot begin with '-'",
			ErrInvalidDiffOptions,
		)
	}
	for _, r := range revision {
		if unicode.IsControl(r) {
			return fmt.Errorf(
				"%w: base revision contains a control character",
				ErrInvalidDiffOptions,
			)
		}
	}
	return nil
}

func resolveDiffCommand(name, value, fallback string) (string, error) {
	if value == "" {
		return fallback, nil
	}
	if strings.TrimSpace(value) == "" ||
		!utf8.ValidString(value) ||
		strings.IndexByte(value, 0) >= 0 {
		return "", fmt.Errorf(
			"%w: %s is invalid",
			ErrInvalidDiffOptions,
			name,
		)
	}
	return value, nil
}

func (b *diffBuilder) resolveRepository() error {
	stdout, stderr, exitCode, err := b.runCommand(
		b.options.gitCommand,
		[]string{"rev-parse", "--show-toplevel"},
		maxDiffGitMetadataBytes,
		&b.metadataBudget,
	)
	if err != nil {
		return b.commandError("resolve Git repository", stderr, exitCode, err)
	}
	root, err := parseSingleCommandLine(stdout, "Git repository root")
	if err != nil {
		return err
	}
	if !filepath.IsAbs(root) {
		return fmt.Errorf("%w: Git repository root is not absolute", ErrDiffMapping)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve Git repository root links: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("inspect Git repository root: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: Git repository root is not a directory", ErrDiffMapping)
	}
	if filepath.Clean(root) != filepath.Clean(b.options.projectDir) {
		if _, ok := repositoryRelativePath(root, b.options.projectDir); ok {
			b.repositoryRoot = root
			return nil
		}
		return fmt.Errorf(
			"%w: project directory is outside its Git repository root",
			ErrDiffMapping,
		)
	}
	b.repositoryRoot = root
	return nil
}

func (b *diffBuilder) resolveBaseCommit() error {
	stdout, stderr, exitCode, err := b.runCommand(
		b.options.gitCommand,
		[]string{
			"rev-parse",
			"--verify",
			"--end-of-options",
			b.options.baseRevision + "^{commit}",
		},
		1024,
		&b.metadataBudget,
	)
	if err != nil {
		return b.commandError("resolve Git base revision", stderr, exitCode, err)
	}
	commit, err := parseSingleCommandLine(stdout, "Git base commit")
	if err != nil {
		return err
	}
	if !validObjectID(commit) {
		return fmt.Errorf(
			"%w: Git returned an invalid commit object ID",
			ErrDiffMapping,
		)
	}
	b.baseCommit = strings.ToLower(commit)
	return nil
}

func (b *diffBuilder) resolveCoverageSources() error {
	b.sourceByProfile = make(
		map[string][]sourceCandidate,
		len(b.options.profileFiles),
	)
	packageSet := make(map[string]struct{})
	for _, profilePath := range b.options.profileFiles {
		normalized := normalizeProfilePath(profilePath)
		if isDirectProfilePath(normalized) {
			if candidate, ok := b.resolveSourceCandidate(normalized, ""); ok {
				b.sourceByProfile[normalized] = append(
					b.sourceByProfile[normalized],
					candidate,
				)
			}
			continue
		}
		packageSet[path.Dir(normalized)] = struct{}{}
	}

	packages := make([]string, 0, len(packageSet))
	for packagePath := range packageSet {
		packages = append(packages, packagePath)
	}
	sort.Strings(packages)
	packageDirs := make(map[string][]string, len(packages))
	packageCount := 0
	for offset := 0; offset < len(packages); {
		arguments := []string{"list", "-e", "-json", "--"}
		argumentBytes := 0
		start := offset
		for offset < len(packages) &&
			offset-start < maxDiffPathspecBatchCount &&
			(argumentBytes == 0 ||
				argumentBytes+len(packages[offset])+1 <=
					maxDiffPathspecBatchBytes) {
			arguments = append(arguments, packages[offset])
			argumentBytes += len(packages[offset]) + 1
			offset++
		}
		stdout, stderr, exitCode, err := b.runCommand(
			b.options.goCommand,
			arguments,
			maxDiffGoListBytes,
			&b.metadataBudget,
		)
		if err != nil {
			return b.commandError("list exact Go packages", stderr, exitCode, err)
		}
		decoder := json.NewDecoder(bytes.NewReader(stdout))
		for {
			if err := diffContextError(b.ctx); err != nil {
				return err
			}
			var pkg goListPackage
			err := decoder.Decode(&pkg)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return fmt.Errorf(
					"%w: decode Go package list: %v",
					ErrDiffMapping,
					err,
				)
			}
			packageCount++
			if packageCount > maxDiffGoPackages {
				return fmt.Errorf(
					"%w: Go package count exceeds %d",
					ErrDiffLimit,
					maxDiffGoPackages,
				)
			}
			if _, requested := packageSet[pkg.ImportPath]; requested &&
				pkg.Dir != "" {
				packageDirs[pkg.ImportPath] = append(
					packageDirs[pkg.ImportPath],
					pkg.Dir,
				)
			}
		}
	}
	for packagePath := range packageDirs {
		sort.Strings(packageDirs[packagePath])
		packageDirs[packagePath] = compactStrings(packageDirs[packagePath])
	}
	for _, profilePath := range b.options.profileFiles {
		normalized := normalizeProfilePath(profilePath)
		if isDirectProfilePath(normalized) {
			continue
		}
		packagePath := path.Dir(normalized)
		for _, packageDir := range packageDirs[packagePath] {
			if candidate, ok := b.resolveSourceCandidate(
				path.Base(normalized),
				packageDir,
			); ok {
				b.sourceByProfile[normalized] = append(
					b.sourceByProfile[normalized],
					candidate,
				)
			}
		}
	}
	for profilePath := range b.sourceByProfile {
		sort.Slice(b.sourceByProfile[profilePath], func(i, j int) bool {
			left := b.sourceByProfile[profilePath][i]
			right := b.sourceByProfile[profilePath][j]
			if left.repositoryPath != right.repositoryPath {
				return left.repositoryPath < right.repositoryPath
			}
			return left.absolutePath < right.absolutePath
		})
		b.sourceByProfile[profilePath] = compactSourceCandidates(
			b.sourceByProfile[profilePath],
		)
	}
	return nil
}

func isDirectProfilePath(profilePath string) bool {
	return strings.HasPrefix(profilePath, ".") ||
		filepath.IsAbs(filepath.FromSlash(profilePath))
}

func (b *diffBuilder) resolveSourceCandidate(
	sourcePath, packageDir string,
) (sourceCandidate, bool) {
	if sourcePath == "" {
		return sourceCandidate{}, false
	}
	absolutePath := filepath.FromSlash(sourcePath)
	if !filepath.IsAbs(absolutePath) {
		baseDir := packageDir
		if baseDir == "" {
			baseDir = b.options.projectDir
		} else if !filepath.IsAbs(baseDir) {
			baseDir = filepath.Join(b.options.projectDir, baseDir)
		}
		absolutePath = filepath.Join(baseDir, absolutePath)
	}
	absolutePath, err := filepath.Abs(absolutePath)
	if err != nil {
		return sourceCandidate{}, false
	}
	if err := rejectPathSymlinks(absolutePath); err != nil {
		return sourceCandidate{}, false
	}
	info, err := os.Lstat(absolutePath)
	if err != nil || !info.Mode().IsRegular() {
		return sourceCandidate{}, false
	}
	repositoryPath, ok := repositoryRelativePath(b.repositoryRoot, absolutePath)
	if !ok {
		return sourceCandidate{}, false
	}
	return sourceCandidate{
		absolutePath:   absolutePath,
		repositoryPath: repositoryPath,
	}, true
}

func (b *diffBuilder) currentRepositoryPaths() map[string]struct{} {
	paths := make(map[string]struct{})
	for _, candidates := range b.sourceByProfile {
		if len(candidates) == 1 {
			paths[candidates[0].repositoryPath] = struct{}{}
		}
	}
	return paths
}

func (b *diffBuilder) readGitChanges(
	currentPaths map[string]struct{},
) (map[string]gitChange, error) {
	stdout, stderr, exitCode, err := b.runCommand(
		b.options.gitCommand,
		[]string{
			"--no-pager",
			"diff",
			"--name-status",
			"-z",
			"--find-renames",
			"--no-ext-diff",
			"--diff-filter=AMRT",
			b.baseCommit,
			"--",
		},
		maxDiffGitMetadataBytes,
		&b.metadataBudget,
	)
	if err != nil {
		return nil, b.commandError("inspect Git changes", stderr, exitCode, err)
	}
	changes, err := parseGitChanges(stdout, currentPaths)
	if err != nil {
		return nil, err
	}
	untracked, err := b.readUntrackedSources(currentPaths)
	if err != nil {
		return nil, err
	}
	for repositoryPath := range untracked {
		if _, exists := changes[repositoryPath]; exists {
			return nil, fmt.Errorf(
				"%w: source is both changed and untracked",
				ErrDiffMapping,
			)
		}
		changes[repositoryPath] = gitChange{
			status:  DiffStatusUntracked,
			newPath: repositoryPath,
		}
	}
	return changes, nil
}

func (b *diffBuilder) compareSource(
	candidate sourceCandidate,
	change gitChange,
	tempDir string,
) (sourceDiff, error) {
	current, err := b.readCurrentSource(candidate.absolutePath)
	if err != nil {
		return sourceDiff{}, err
	}
	canonicalCurrent, err := browserCanonicalSource(current)
	if err != nil {
		return sourceDiff{}, err
	}

	comparison := sourceDiff{
		newPath:       candidate.repositoryPath,
		currentSHA256: canonicalSourceSHA256(canonicalCurrent),
	}
	var previous []byte
	switch change.status {
	case DiffStatusAdded:
		comparison.status = DiffStatusAdded
	case DiffStatusModified:
		comparison.oldPath = candidate.repositoryPath
		comparison.status = DiffStatusModified
		previous, err = b.readBaseSource(candidate.repositoryPath)
	case DiffStatusRenamed:
		comparison.oldPath = change.oldPath
		comparison.status = DiffStatusRenamed
		previous, err = b.readBaseSource(change.oldPath)
	case DiffStatusUntracked:
		comparison.status = DiffStatusUntracked
	case "":
		previous, err = b.readBaseSource(candidate.repositoryPath)
		comparison.oldPath = candidate.repositoryPath
		if bytes.Equal(previous, current) {
			comparison.status = DiffStatusUnchanged
		} else {
			comparison.status = DiffStatusModified
		}
	default:
		return sourceDiff{}, fmt.Errorf(
			"%w: unsupported Git status %q",
			ErrDiffMapping,
			change.status,
		)
	}
	if err != nil {
		return sourceDiff{}, err
	}
	canonicalPrevious, err := browserCanonicalSource(previous)
	if err != nil {
		return sourceDiff{}, err
	}
	if bytes.Equal(canonicalPrevious, canonicalCurrent) {
		return comparison, nil
	}
	comparison.hunks, err = b.diffSnapshots(
		tempDir,
		canonicalPrevious,
		canonicalCurrent,
	)
	if err != nil {
		return sourceDiff{}, err
	}
	return comparison, nil
}

func (b *diffBuilder) readCurrentSource(path string) ([]byte, error) {
	if err := rejectPathSymlinks(path); err != nil {
		return nil, fmt.Errorf("validate current coverage source path: %w", err)
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect current coverage source path: %w", err)
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: current coverage source is not regular", ErrDiffMapping)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open current coverage source: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect current coverage source: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: current coverage source is not regular", ErrDiffMapping)
	}
	if !os.SameFile(pathInfo, info) {
		return nil, fmt.Errorf(
			"%w: current coverage source changed before reading",
			ErrDiffMapping,
		)
	}
	if info.Size() > maxDiffSourceFileBytes {
		return nil, fmt.Errorf(
			"%w: current source exceeds %d bytes",
			ErrDiffLimit,
			maxDiffSourceFileBytes,
		)
	}
	content, err := io.ReadAll(io.LimitReader(file, maxDiffSourceFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read current coverage source: %w", err)
	}
	if len(content) > maxDiffSourceFileBytes {
		return nil, fmt.Errorf(
			"%w: current source exceeds %d bytes",
			ErrDiffLimit,
			maxDiffSourceFileBytes,
		)
	}
	if !utf8.Valid(content) {
		return nil, fmt.Errorf("%w: current source is not valid UTF-8", ErrDiffMapping)
	}
	currentInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("reinspect current coverage source: %w", err)
	}
	if info.Size() != currentInfo.Size() ||
		!info.ModTime().Equal(currentInfo.ModTime()) ||
		info.Mode() != currentInfo.Mode() ||
		int64(len(content)) != currentInfo.Size() {
		return nil, fmt.Errorf(
			"%w: current coverage source changed while reading",
			ErrDiffMapping,
		)
	}
	finalPathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("reinspect current coverage source path: %w", err)
	}
	if !finalPathInfo.Mode().IsRegular() || !os.SameFile(info, finalPathInfo) {
		return nil, fmt.Errorf(
			"%w: current coverage source path changed while reading",
			ErrDiffMapping,
		)
	}
	if err := b.sourceBudget.consume(int64(len(content))); err != nil {
		return nil, err
	}
	return content, nil
}

func (b *diffBuilder) readBaseSource(repositoryPath string) ([]byte, error) {
	objectName := b.baseCommit + ":" + repositoryPath
	stdout, stderr, exitCode, err := b.runCommand(
		b.options.gitCommand,
		[]string{"cat-file", "blob", objectName},
		maxDiffSourceFileBytes,
		&b.sourceBudget,
	)
	if err != nil {
		return nil, b.commandError("read Git base source", stderr, exitCode, err)
	}
	if !utf8.Valid(stdout) {
		return nil, fmt.Errorf("%w: base source is not valid UTF-8", ErrDiffMapping)
	}
	return stdout, nil
}

func (b *diffBuilder) diffSnapshots(
	tempDir string,
	previous, current []byte,
) ([]DiffHunk, error) {
	oldPath := filepath.Join(tempDir, "old.go")
	newPath := filepath.Join(tempDir, "new.go")
	if err := writePrivateSnapshot(oldPath, previous); err != nil {
		return nil, err
	}
	if err := writePrivateSnapshot(newPath, current); err != nil {
		return nil, err
	}
	stdout, stderr, exitCode, err := b.runCommand(
		b.options.gitCommand,
		[]string{
			"--no-pager",
			"diff",
			"--no-index",
			"--unified=0",
			"--no-color",
			"--no-ext-diff",
			"--no-textconv",
			"--text",
			"--diff-algorithm=myers",
			"--no-indent-heuristic",
			"--no-prefix",
			"--",
			oldPath,
			newPath,
		},
		maxDiffPatchFileBytes,
		&b.patchBudget,
	)
	if err != nil {
		if errors.Is(err, ErrDiffLimit) || !isOnlyExitCode(err, 1) {
			return nil, b.commandError(
				"compare coverage source snapshots",
				stderr,
				exitCode,
				err,
			)
		}
	} else {
		return nil, fmt.Errorf(
			"%w: Git reported identical snapshots for different source bytes",
			ErrDiffMapping,
		)
	}
	hunks, retainedLines, err := parseZeroContextPatch(
		stdout,
		maxDiffHunks-b.parsedHunks,
		maxDiffLines-b.parsedLines,
	)
	if err != nil {
		return nil, err
	}
	if len(hunks) == 0 {
		return nil, fmt.Errorf(
			"%w: Git returned no changed hunk for different source bytes",
			ErrDiffMapping,
		)
	}
	b.parsedHunks += len(hunks)
	b.parsedLines += retainedLines
	return hunks, nil
}

func (b *diffBuilder) runCommand(
	executable string,
	arguments []string,
	perOutputLimit int64,
	outputBudget *byteBudget,
) ([]byte, string, int, error) {
	stdout := &boundedCapture{
		perLimit:  perOutputLimit,
		aggregate: outputBudget,
		limitName: "command stdout",
	}
	stderr := &boundedCapture{
		perLimit:  maxDiffCommandStderrBytes,
		aggregate: &b.stderrBudget,
		limitName: "command stderr",
	}
	err := runner.RunCommand(b.ctx, runner.CommandOptions{
		Executable:     executable,
		Arguments:      arguments,
		WorkDir:        b.options.projectDir,
		StandardOutput: stdout,
		StandardError:  stderr,
		Environment:    diffCommandEnvironment(),
		InterruptGrace: b.options.interruptGrace,
	})
	exitCode := 0
	if err != nil {
		exitCode = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	return append([]byte(nil), stdout.buffer.Bytes()...),
		stderr.String(),
		exitCode,
		err
}

func (b *diffBuilder) commandError(
	operation, stderr string,
	exitCode int,
	err error,
) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrDiffLimit) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	if contextErr := b.ctx.Err(); contextErr != nil {
		cause := context.Cause(b.ctx)
		if cause == nil {
			cause = contextErr
		}
		err = errors.Join(cause, err)
	}
	detail := strings.TrimSpace(stderr)
	detailSuffix := ""
	if detail != "" {
		detailSuffix = fmt.Sprintf(" (stderr: %q)", detail)
	}
	if exitCode >= 0 {
		return fmt.Errorf(
			"%s: command exited with status %d%s: %w",
			operation,
			exitCode,
			detailSuffix,
			err,
		)
	}
	return fmt.Errorf("%s%s: %w", operation, detailSuffix, err)
}

func (w *boundedCapture) Write(p []byte) (int, error) {
	if w == nil {
		return 0, fmt.Errorf("%w: nil bounded capture", ErrDiffLimit)
	}
	if int64(len(p)) > w.perLimit-int64(w.buffer.Len()) {
		return 0, fmt.Errorf(
			"%w: %s exceeds %d bytes",
			ErrDiffLimit,
			w.limitName,
			w.perLimit,
		)
	}
	if err := w.aggregate.consume(int64(len(p))); err != nil {
		return 0, err
	}
	return w.buffer.Write(p)
}

func (w *boundedCapture) String() string {
	if w == nil {
		return ""
	}
	value := w.buffer.String()
	if len(value) > maxDiagnosticBytes {
		value = value[:maxDiagnosticBytes]
	}
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "\uFFFD")
	}
	return strings.TrimSpace(value)
}

func (b *byteBudget) consume(size int64) error {
	if b == nil || size < 0 || size > b.maximum-b.used {
		return fmt.Errorf("%w: aggregate byte budget exhausted", ErrDiffLimit)
	}
	b.used += size
	return nil
}

func parseSingleCommandLine(data []byte, label string) (string, error) {
	value := strings.TrimSuffix(string(data), "\n")
	value = strings.TrimSuffix(value, "\r")
	if value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("%w: %s is malformed", ErrDiffMapping, label)
	}
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("%w: %s is not valid UTF-8", ErrDiffMapping, label)
	}
	return value, nil
}

func validObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'f') ||
			(r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func repositoryRelativePath(root, target string) (string, bool) {
	relative, err := filepath.Rel(root, target)
	if err != nil ||
		relative == "." ||
		relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(relative) {
		return "", false
	}
	return filepath.ToSlash(relative), true
}

func normalizeProfilePath(value string) string {
	if filepath.Separator == '\\' {
		return strings.ReplaceAll(value, "\\", "/")
	}
	return value
}

func compactStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	output := values[:1]
	for _, value := range values[1:] {
		if value != output[len(output)-1] {
			output = append(output, value)
		}
	}
	return output
}

func compactSourceCandidates(values []sourceCandidate) []sourceCandidate {
	if len(values) < 2 {
		return values
	}
	output := values[:1]
	for _, value := range values[1:] {
		last := output[len(output)-1]
		if value != last {
			output = append(output, value)
		}
	}
	return output
}

func unavailableDiffFile(profilePath, reason string) DiffFile {
	return DiffFile{
		ProfilePath: profilePath,
		Status:      DiffStatusUnavailable,
		Reason:      reason,
	}
}

func boundedDiagnostic(value string) string {
	if len(value) > maxDiagnosticBytes {
		value = value[:maxDiagnosticBytes]
	}
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "\uFFFD")
	}
	return value
}

func writePrivateSnapshot(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create private source snapshot: %w", err)
	}
	written, writeErr := file.Write(content)
	if written != len(content) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write private source snapshot: %w", err)
	}
	return nil
}

func cloneDiffHunks(input []DiffHunk) []DiffHunk {
	if len(input) == 0 {
		return nil
	}
	output := make([]DiffHunk, len(input))
	for i, hunk := range input {
		output[i] = hunk
		if hunk.Lines != nil {
			output[i].Lines = make([]DiffLine, len(hunk.Lines))
			copy(output[i].Lines, hunk.Lines)
		}
	}
	return output
}

type diffFileProjection struct {
	hunks         int
	lines         int
	retainedBytes int64
}

func defaultDiffProjectionLimits() diffProjectionLimits {
	return diffProjectionLimits{
		maxHunks:         maxDiffHunks,
		maxLines:         maxDiffLines,
		maxRetainedBytes: maxDiffRetainedTotalBytes,
		maxEncodedBytes:  maxDiffJSONBytes,
	}
}

func (b *diffBuilder) initializeDiffProjection(diff *Diff) error {
	if b == nil || diff == nil {
		return fmt.Errorf("%w: nil diff projection", ErrDiffMapping)
	}
	if b.projectionLimits.maxHunks < 0 ||
		b.projectionLimits.maxLines < 0 ||
		b.projectionLimits.maxRetainedBytes < 0 ||
		b.projectionLimits.maxEncodedBytes < 0 {
		return fmt.Errorf("%w: invalid diff projection limits", ErrDiffMapping)
	}
	encoded, err := json.Marshal(diff)
	if err != nil {
		return fmt.Errorf("initialize coverage diff projection: %w", err)
	}
	if int64(len(encoded)) > b.projectionLimits.maxEncodedBytes {
		return fmt.Errorf(
			"%w: serialized diff envelope exceeds %d bytes",
			ErrDiffLimit,
			b.projectionLimits.maxEncodedBytes,
		)
	}
	retainedBytes := int64(len(diff.Schema) + len(diff.BaseCommit))
	if retainedBytes > b.projectionLimits.maxRetainedBytes {
		return fmt.Errorf(
			"%w: diff envelope retained data exceeds %d bytes",
			ErrDiffLimit,
			b.projectionLimits.maxRetainedBytes,
		)
	}
	b.projection = diffProjectionState{
		retainedBytes: retainedBytes,
		encodedBytes:  int64(len(encoded)),
	}
	return nil
}

func (b *diffBuilder) appendProjectedDiffFile(diff *Diff, file DiffFile) error {
	if b == nil || diff == nil {
		return fmt.Errorf("%w: nil diff projection", ErrDiffMapping)
	}
	projected, err := measureDiffFileProjection(file)
	if err != nil {
		return err
	}
	if projected.hunks > b.projectionLimits.maxHunks-b.projection.hunks {
		return fmt.Errorf(
			"%w: projected diff hunk count exceeds %d",
			ErrDiffLimit,
			b.projectionLimits.maxHunks,
		)
	}
	if projected.lines > b.projectionLimits.maxLines-b.projection.lines {
		return fmt.Errorf(
			"%w: projected diff line count exceeds %d",
			ErrDiffLimit,
			b.projectionLimits.maxLines,
		)
	}
	if projected.retainedBytes >
		b.projectionLimits.maxRetainedBytes-b.projection.retainedBytes {
		return fmt.Errorf(
			"%w: projected retained diff data exceeds %d bytes",
			ErrDiffLimit,
			b.projectionLimits.maxRetainedBytes,
		)
	}

	// Marshal the immutable, not-yet-cloned view. Per-file retained data is
	// already bounded, and global cardinality was checked above, so this exact
	// encoded-size charge cannot amplify cached hunks in the retained model.
	encoded, err := json.Marshal(file)
	if err != nil {
		return fmt.Errorf("measure projected coverage diff file: %w", err)
	}
	encodedIncrement := int64(len(encoded))
	if len(diff.Files) > 0 {
		encodedIncrement++ // Comma between adjacent array elements.
	}
	if encodedIncrement >
		b.projectionLimits.maxEncodedBytes-b.projection.encodedBytes {
		return fmt.Errorf(
			"%w: projected serialized diff exceeds %d bytes",
			ErrDiffLimit,
			b.projectionLimits.maxEncodedBytes,
		)
	}

	b.projection.hunks += projected.hunks
	b.projection.lines += projected.lines
	b.projection.retainedBytes += projected.retainedBytes
	b.projection.encodedBytes += encodedIncrement
	file.Hunks = cloneDiffHunks(file.Hunks)
	diff.Files = append(diff.Files, file)
	return nil
}

func measureDiffFileProjection(file DiffFile) (diffFileProjection, error) {
	projected := diffFileProjection{}
	values := []string{
		file.ProfilePath,
		file.OldPath,
		file.NewPath,
		file.CurrentSHA256,
		string(file.Status),
		file.Reason,
	}
	for _, value := range values {
		if int64(len(value)) >
			int64(maxDiffFilePayloadBytes)-projected.retainedBytes {
			return diffFileProjection{}, fmt.Errorf(
				"%w: per-file diff payload exceeds %d bytes",
				ErrDiffLimit,
				maxDiffFilePayloadBytes,
			)
		}
		projected.retainedBytes += int64(len(value))
	}
	for _, hunk := range file.Hunks {
		if projected.hunks >= maxDiffHunks {
			return diffFileProjection{}, fmt.Errorf(
				"%w: per-file diff hunk count exceeds %d",
				ErrDiffLimit,
				maxDiffHunks,
			)
		}
		projected.hunks++
		for _, line := range hunk.Lines {
			if projected.lines >= maxDiffLines {
				return diffFileProjection{}, fmt.Errorf(
					"%w: per-file diff line count exceeds %d",
					ErrDiffLimit,
					maxDiffLines,
				)
			}
			projected.lines++
			if int64(len(line.Text)) >
				int64(maxDiffFilePayloadBytes)-projected.retainedBytes {
				return diffFileProjection{}, fmt.Errorf(
					"%w: per-file diff payload exceeds %d bytes",
					ErrDiffLimit,
					maxDiffFilePayloadBytes,
				)
			}
			projected.retainedBytes += int64(len(line.Text))
		}
	}
	return projected, nil
}

func (b *diffBuilder) readUntrackedSources(
	currentPaths map[string]struct{},
) (map[string]struct{}, error) {
	untracked := make(map[string]struct{})
	paths := make([]string, 0, len(currentPaths))
	for repositoryPath := range currentPaths {
		paths = append(paths, repositoryPath)
	}
	sort.Strings(paths)
	for offset := 0; offset < len(paths); {
		arguments := []string{
			"--literal-pathspecs",
			"ls-files",
			"--others",
			"-z",
			"--",
		}
		argumentBytes := 0
		start := offset
		for offset < len(paths) &&
			offset-start < maxDiffPathspecBatchCount &&
			(argumentBytes == 0 ||
				argumentBytes+len(paths[offset])+1 <= maxDiffPathspecBatchBytes) {
			arguments = append(arguments, paths[offset])
			argumentBytes += len(paths[offset]) + 1
			offset++
		}
		stdout, stderr, exitCode, err := b.runCommand(
			b.options.gitCommand,
			arguments,
			maxDiffGitMetadataBytes,
			&b.metadataBudget,
		)
		if err != nil {
			return nil, b.commandError(
				"inspect untracked coverage sources",
				stderr,
				exitCode,
				err,
			)
		}
		batch, err := parseNULTerminatedPaths(stdout)
		if err != nil {
			return nil, err
		}
		for _, repositoryPath := range batch {
			if _, wanted := currentPaths[repositoryPath]; !wanted {
				return nil, fmt.Errorf(
					"%w: Git returned an unrequested untracked source",
					ErrDiffMapping,
				)
			}
			untracked[repositoryPath] = struct{}{}
		}
	}
	return untracked, nil
}

func parseNULTerminatedPaths(data []byte) ([]string, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if data[len(data)-1] != 0 {
		return nil, fmt.Errorf(
			"%w: Git path output is not NUL-terminated",
			ErrDiffMapping,
		)
	}
	reader := newNULTokenReader(data)
	paths := make([]string, 0)
	for reader.more() {
		rawPath, ok := reader.next()
		if !ok {
			return nil, fmt.Errorf(
				"%w: Git path output is truncated",
				ErrDiffMapping,
			)
		}
		if len(paths) >= maxDiffPathspecBatchCount {
			return nil, fmt.Errorf(
				"%w: untracked path count exceeds request batch",
				ErrDiffLimit,
			)
		}
		repositoryPath := normalizeProfilePath(string(rawPath))
		if err := validateGitRepositoryPath(repositoryPath); err != nil {
			return nil, err
		}
		paths = append(paths, repositoryPath)
	}
	return paths, nil
}

func isOnlyExitCode(err error, expected int) bool {
	sawExit := false
	var visit func(error) bool
	visit = func(current error) bool {
		if current == nil {
			return true
		}
		if many, ok := current.(interface{ Unwrap() []error }); ok {
			children := many.Unwrap()
			if len(children) == 0 {
				return false
			}
			for _, child := range children {
				if !visit(child) {
					return false
				}
			}
			return true
		}
		if child := errors.Unwrap(current); child != nil {
			return visit(child)
		}
		exitErr, ok := current.(*exec.ExitError)
		if !ok || exitErr.ExitCode() != expected {
			return false
		}
		sawExit = true
		return true
	}
	return visit(err) && sawExit
}

func diffCommandEnvironment() []string {
	environment := os.Environ()
	filtered := environment[:0]
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if isDiffEnvironmentOverride(key) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(
		filtered,
		"LC_ALL=C",
		"LANG=C",
		"GIT_PAGER=cat",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_NO_LAZY_FETCH=1",
		"GOTOOLCHAIN=local",
		"GOPROXY=off",
		"GONOPROXY=none",
		"GOSUMDB=off",
		"GONOSUMDB=none",
		"GOVCS=*:off",
		"GOFLAGS=",
		"GOENV=off",
	)
}

func isDiffEnvironmentOverride(key string) bool {
	switch strings.ToUpper(key) {
	case "LC_ALL",
		"LANG",
		"GIT_PAGER",
		"GIT_TERMINAL_PROMPT",
		"GIT_OPTIONAL_LOCKS",
		"GIT_NO_LAZY_FETCH",
		"GOTOOLCHAIN",
		"GOPROXY",
		"GONOPROXY",
		"GOSUMDB",
		"GONOSUMDB",
		"GOVCS",
		"GOFLAGS",
		"GOENV":
		return true
	default:
		return false
	}
}

func diffContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		cause := context.Cause(ctx)
		if cause == nil {
			cause = err
		}
		return fmt.Errorf("build coverage diff: %w", cause)
	}
	return nil
}

func canonicalSourceSHA256(source []byte) string {
	digest := sha256.Sum256(source)
	return hex.EncodeToString(digest[:])
}

func browserCanonicalSource(source []byte) ([]byte, error) {
	canonical := source
	if bytes.IndexByte(source, '\r') >= 0 ||
		bytes.IndexByte(source, '\t') >= 0 {
		canonical = make([]byte, 0, len(source))
		for index := 0; index < len(source); index++ {
			switch source[index] {
			case '\t':
				if len(canonical) > maxDiffSourceFileBytes-8 {
					return nil, fmt.Errorf(
						"%w: canonical displayed source exceeds %d bytes",
						ErrDiffLimit,
						maxDiffSourceFileBytes,
					)
				}
				canonical = append(canonical, "        "...)
			case '\r':
				if len(canonical) >= maxDiffSourceFileBytes {
					return nil, fmt.Errorf(
						"%w: canonical displayed source exceeds %d bytes",
						ErrDiffLimit,
						maxDiffSourceFileBytes,
					)
				}
				canonical = append(canonical, '\n')
				if index+1 < len(source) && source[index+1] == '\n' {
					index++
				}
			default:
				if len(canonical) >= maxDiffSourceFileBytes {
					return nil, fmt.Errorf(
						"%w: canonical displayed source exceeds %d bytes",
						ErrDiffLimit,
						maxDiffSourceFileBytes,
					)
				}
				canonical = append(canonical, source[index])
			}
		}
	}
	// HTML's pre element discards one leading LF immediately after its start
	// tag. Go's template places the annotated source directly after that tag.
	if len(canonical) > 0 && canonical[0] == '\n' {
		canonical = canonical[1:]
	}
	lineCount := bytes.Count(canonical, []byte{'\n'})
	if len(canonical) > 0 && canonical[len(canonical)-1] != '\n' {
		lineCount++
	}
	if lineCount > maxDiffSourceLines {
		return nil, fmt.Errorf(
			"%w: displayed source exceeds %d lines",
			ErrDiffLimit,
			maxDiffSourceLines,
		)
	}
	return canonical, nil
}
