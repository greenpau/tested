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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/greenpau/tested/pkg/runner"
)

const (
	defaultGoCommand = "go"
	privateFileMode  = 0o600
	privateDirMode   = 0o700
	maxCoverStderr   = 64 << 10
)

var errGeneratedFileChanged = errors.New(
	"generated coverage HTML changed before publication",
)

// HTMLOptions configures annotated HTML generation with "go tool cover".
type HTMLOptions struct {
	// GoCommand selects the Go executable. An empty value selects "go".
	GoCommand string
	// ProjectDir is the tested project root and becomes the command's working
	// directory so package-relative source paths resolve correctly.
	ProjectDir string
	// ProfilePath identifies the coverage profile, relative to ProjectDir when
	// it is not absolute.
	ProfilePath string
	// OutputPath identifies the HTML report, relative to ProjectDir when it is
	// not absolute.
	OutputPath string
	// Decorate optionally copies the Go-authored HTML through a presentation
	// layer before publication. The callback must copy all source bytes it
	// retains to the provided writer and return an error on incomplete output.
	Decorate func(context.Context, io.Reader, io.Writer) error
	// InterruptGrace controls the graceful process-tree cancellation interval.
	// Zero selects the runner's two-second default.
	InterruptGrace time.Duration
}

type generatedFileSnapshot struct {
	identity os.FileInfo
	size     int64
	mode     os.FileMode
	modified time.Time
	digest   [sha256.Size]byte
}

// GenerateHTML invokes the selected "go tool cover", applies an optional
// decorator, and atomically installs its output with mode 0600.
func GenerateHTML(ctx context.Context, opts HTMLOptions) error {
	return generateHTML(ctx, opts, nil)
}

func generateHTML(
	ctx context.Context,
	opts HTMLOptions,
	beforeInstall func(string, generatedFileSnapshot) error,
) error {
	if ctx == nil {
		return errors.New("generate coverage HTML: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("generate coverage HTML: %w", err)
	}
	if opts.ProjectDir == "" {
		return errors.New("generate coverage HTML: project directory is required")
	}
	if opts.ProfilePath == "" {
		return errors.New("generate coverage HTML: profile path is required")
	}
	if opts.OutputPath == "" {
		return errors.New("generate coverage HTML: output path is required")
	}

	rawProjectDir, err := filepath.Abs(opts.ProjectDir)
	if err != nil {
		return fmt.Errorf("resolve project directory %q: %w", opts.ProjectDir, err)
	}
	projectInfo, err := os.Stat(rawProjectDir)
	if err != nil {
		return fmt.Errorf("inspect project directory %q: %w", rawProjectDir, err)
	}
	if !projectInfo.IsDir() {
		return fmt.Errorf("inspect project directory %q: not a directory", rawProjectDir)
	}
	projectDir, err := filepath.EvalSymlinks(rawProjectDir)
	if err != nil {
		return fmt.Errorf("resolve project directory symlinks %q: %w", rawProjectDir, err)
	}
	if err := rejectPathSymlinks(projectDir); err != nil {
		return fmt.Errorf("validate project directory %q: %w", projectDir, err)
	}
	if !filepath.IsAbs(opts.OutputPath) && pathEscapesBase(opts.OutputPath) {
		return fmt.Errorf(
			"generate coverage HTML: relative output path %q escapes the project directory",
			opts.OutputPath,
		)
	}

	profilePath := resolveProjectPath(projectDir, opts.ProfilePath)
	outputPath := resolveProjectPath(projectDir, opts.OutputPath)
	if profilePath == outputPath {
		return fmt.Errorf(
			"generate coverage HTML: output path %q would replace the coverage profile",
			outputPath,
		)
	}
	profileInfo, err := inspectCoverageProfile(profilePath)
	if err != nil {
		return err
	}

	outputDir := filepath.Dir(outputPath)
	if err := rejectPathSymlinks(outputDir); err != nil {
		return fmt.Errorf("validate coverage HTML directory %q: %w", outputDir, err)
	}
	if err := os.MkdirAll(outputDir, privateDirMode); err != nil {
		return fmt.Errorf("create coverage HTML directory %q: %w", outputDir, err)
	}
	if err := rejectPathSymlinks(outputDir); err != nil {
		return fmt.Errorf("validate created coverage HTML directory %q: %w", outputDir, err)
	}
	if err := rejectNonRegularTarget(outputPath); err != nil {
		return err
	}
	if err := rejectProfileAlias(profileInfo, outputPath, "coverage HTML target"); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(outputDir, "."+filepath.Base(outputPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary coverage HTML in %q: %w", outputDir, err)
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(privateFileMode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set temporary coverage HTML permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary coverage HTML: %w", err)
	}

	goCommand := opts.GoCommand
	if goCommand == "" {
		goCommand = defaultGoCommand
	}
	stderr := boundedStderr{limit: maxCoverStderr}
	if err := runner.RunCommand(ctx, runner.CommandOptions{
		Executable: goCommand,
		Arguments: []string{
			"tool",
			"cover",
			"-html=" + profilePath,
			"-o=" + tmpPath,
		},
		WorkDir:        projectDir,
		StandardError:  &stderr,
		InterruptGrace: opts.InterruptGrace,
	}); err != nil {
		return commandError(ctx, goCommand, stderr.String(), err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("generate coverage HTML: %w", err)
	}

	currentProfileInfo, err := inspectCoverageProfile(profilePath)
	if err != nil {
		return err
	}
	if !sameProfileEvidence(profileInfo, currentProfileInfo) {
		return fmt.Errorf("coverage profile %q changed while generating HTML", profilePath)
	}
	if err := rejectPathSymlinks(tmpPath); err != nil {
		return fmt.Errorf("validate generated coverage HTML path %q: %w", tmpPath, err)
	}
	rawSnapshot, err := secureGeneratedFile(tmpPath)
	if err != nil {
		return err
	}
	if os.SameFile(currentProfileInfo, rawSnapshot.identity) {
		return fmt.Errorf("generated coverage HTML %q aliases coverage profile %q", tmpPath, profilePath)
	}
	if opts.Decorate != nil {
		if err := decorateGeneratedHTML(
			ctx,
			tmpPath,
			rawSnapshot,
			opts.Decorate,
		); err != nil {
			return err
		}
		currentProfileInfo, err = inspectCoverageProfile(profilePath)
		if err != nil {
			return err
		}
		if !sameProfileEvidence(profileInfo, currentProfileInfo) {
			return fmt.Errorf(
				"coverage profile %q changed while decorating HTML",
				profilePath,
			)
		}
	}
	tmpSnapshot, err := secureGeneratedFile(tmpPath)
	if err != nil {
		return err
	}
	if os.SameFile(currentProfileInfo, tmpSnapshot.identity) {
		return fmt.Errorf("generated coverage HTML %q aliases coverage profile %q", tmpPath, profilePath)
	}
	if err := rejectPathSymlinks(outputPath); err != nil {
		return fmt.Errorf("validate coverage HTML target %q: %w", outputPath, err)
	}
	if err := rejectNonRegularTarget(outputPath); err != nil {
		return err
	}
	if err := rejectProfileAlias(currentProfileInfo, outputPath, "coverage HTML target"); err != nil {
		return err
	}
	if beforeInstall != nil {
		if err := beforeInstall(tmpPath, tmpSnapshot); err != nil {
			return fmt.Errorf(
				"prepare generated coverage HTML %q for installation: %w",
				tmpPath,
				err,
			)
		}
	}
	if err := revalidateGeneratedFile(tmpPath, tmpSnapshot); err != nil {
		return err
	}
	if err := replaceFile(tmpPath, outputPath); err != nil {
		return fmt.Errorf("install coverage HTML %q: %w", outputPath, err)
	}
	removeTemp = false
	return nil
}

func decorateGeneratedHTML(
	ctx context.Context,
	path string,
	expected generatedFileSnapshot,
	decorate func(context.Context, io.Reader, io.Writer) error,
) error {
	if decorate == nil {
		return nil
	}
	if err := revalidateGeneratedFile(path, expected); err != nil {
		return err
	}

	source, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open canonical coverage HTML %q: %w", path, err)
	}
	sourceInfo, err := source.Stat()
	if err != nil {
		_ = source.Close()
		return fmt.Errorf("inspect canonical coverage HTML %q: %w", path, err)
	}
	if expected.identity == nil ||
		!sourceInfo.Mode().IsRegular() ||
		!os.SameFile(expected.identity, sourceInfo) ||
		expected.size != sourceInfo.Size() ||
		expected.mode != sourceInfo.Mode() ||
		!expected.modified.Equal(sourceInfo.ModTime()) {
		_ = source.Close()
		return fmt.Errorf(
			"%w: canonical coverage HTML %q changed before decoration",
			errGeneratedFileChanged,
			path,
		)
	}

	decorated, err := os.CreateTemp(
		filepath.Dir(path),
		"."+filepath.Base(path)+".theme-*",
	)
	if err != nil {
		_ = source.Close()
		return fmt.Errorf("create decorated coverage HTML beside %q: %w", path, err)
	}
	decoratedPath := decorated.Name()
	removeDecorated := true
	defer func() {
		if removeDecorated {
			_ = os.Remove(decoratedPath)
		}
	}()

	var operationErrors []error
	if err := decorated.Chmod(privateFileMode); err != nil {
		operationErrors = append(
			operationErrors,
			fmt.Errorf("set decorated coverage HTML permissions: %w", err),
		)
	} else if err := decorate(ctx, source, decorated); err != nil {
		operationErrors = append(
			operationErrors,
			fmt.Errorf("decorate generated coverage HTML: %w", err),
		)
	} else if err := decorated.Sync(); err != nil {
		operationErrors = append(
			operationErrors,
			fmt.Errorf("sync decorated coverage HTML: %w", err),
		)
	}
	if err := decorated.Close(); err != nil {
		operationErrors = append(
			operationErrors,
			fmt.Errorf("close decorated coverage HTML: %w", err),
		)
	}
	if err := source.Close(); err != nil {
		operationErrors = append(
			operationErrors,
			fmt.Errorf("close canonical coverage HTML %q: %w", path, err),
		)
	}
	if err := errors.Join(operationErrors...); err != nil {
		return err
	}
	if err := revalidateGeneratedFile(path, expected); err != nil {
		return err
	}
	decoratedSnapshot, err := secureGeneratedFile(decoratedPath)
	if err != nil {
		return fmt.Errorf("secure decorated coverage HTML: %w", err)
	}
	if err := revalidateGeneratedFile(
		decoratedPath,
		decoratedSnapshot,
	); err != nil {
		return err
	}
	if err := revalidateGeneratedFile(path, expected); err != nil {
		return err
	}
	if err := replaceFile(decoratedPath, path); err != nil {
		return fmt.Errorf("stage decorated coverage HTML %q: %w", path, err)
	}
	removeDecorated = false
	return nil
}

func resolveProjectPath(projectDir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(projectDir, path)
}

func pathEscapesBase(path string) bool {
	cleaned := filepath.Clean(path)
	return cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(filepath.Separator))
}

func inspectCoverageProfile(path string) (os.FileInfo, error) {
	if err := rejectPathSymlinks(path); err != nil {
		return nil, fmt.Errorf("validate coverage profile path %q: %w", path, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect coverage profile %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("coverage profile %q is not a regular file", path)
	}
	return info, nil
}

func rejectProfileAlias(profileInfo os.FileInfo, path, label string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s %q: %w", label, path, err)
	}
	if os.SameFile(profileInfo, info) {
		return fmt.Errorf("%s %q aliases the coverage profile", label, path)
	}
	return nil
}

func sameProfileEvidence(before, after os.FileInfo) bool {
	return os.SameFile(before, after) &&
		before.Size() == after.Size() &&
		before.ModTime().Equal(after.ModTime())
}

func rejectPathSymlinks(path string) error {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path %q is not absolute", path)
	}
	root := filepath.VolumeName(path) + string(filepath.Separator)
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("inspect path root %q: %w", root, err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path component %q is a symbolic link", root)
	}

	relative, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("resolve path %q from root %q: %w", path, root, err)
	}
	current := root
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
			return fmt.Errorf("inspect path component %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path component %q is a symbolic link", current)
		}
		if current != path && !info.IsDir() {
			return fmt.Errorf("path component %q is not a directory", current)
		}
	}
	return nil
}

func rejectNonRegularTarget(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect coverage HTML target %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("coverage HTML target %q is a symbolic link", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("coverage HTML target %q is not a regular file", path)
	}
	return nil
}

func secureGeneratedFile(path string) (generatedFileSnapshot, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return generatedFileSnapshot{}, fmt.Errorf(
			"inspect generated coverage HTML %q: %w",
			path,
			err,
		)
	}
	if !info.Mode().IsRegular() {
		return generatedFileSnapshot{}, fmt.Errorf(
			"inspect generated coverage HTML %q: not a regular file",
			path,
		)
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return generatedFileSnapshot{}, fmt.Errorf(
			"open generated coverage HTML %q: %w",
			path,
			err,
		)
	}
	openedInfo, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return generatedFileSnapshot{}, fmt.Errorf(
			"inspect opened coverage HTML %q: %w",
			path,
			err,
		)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		_ = f.Close()
		return generatedFileSnapshot{}, fmt.Errorf(
			"%w: %q changed before securing",
			errGeneratedFileChanged,
			path,
		)
	}
	if err := f.Chmod(privateFileMode); err != nil {
		_ = f.Close()
		return generatedFileSnapshot{}, fmt.Errorf(
			"set generated coverage HTML permissions: %w",
			err,
		)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return generatedFileSnapshot{}, fmt.Errorf(
			"sync generated coverage HTML: %w",
			err,
		)
	}
	snapshot, snapshotErr := snapshotOpenedGeneratedFile(
		f,
		path,
		openedInfo,
	)
	closeErr := f.Close()
	if snapshotErr != nil || closeErr != nil {
		return generatedFileSnapshot{}, errors.Join(
			snapshotErr,
			wrapGeneratedFileCloseError(closeErr, path),
		)
	}
	if err := validateGeneratedPathSnapshot(path, snapshot); err != nil {
		return generatedFileSnapshot{}, err
	}
	return snapshot, nil
}

func revalidateGeneratedFile(
	path string,
	expected generatedFileSnapshot,
) error {
	if err := rejectPathSymlinks(path); err != nil {
		return errors.Join(
			errGeneratedFileChanged,
			fmt.Errorf(
				"revalidate generated coverage HTML path %q: %w",
				path,
				err,
			),
		)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return errors.Join(
			errGeneratedFileChanged,
			fmt.Errorf(
				"inspect generated coverage HTML %q before installation: %w",
				path,
				err,
			),
		)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf(
			"%w: generated coverage HTML %q is no longer a regular file",
			errGeneratedFileChanged,
			path,
		)
	}

	f, err := os.Open(path)
	if err != nil {
		return errors.Join(
			errGeneratedFileChanged,
			fmt.Errorf(
				"open generated coverage HTML %q before installation: %w",
				path,
				err,
			),
		)
	}
	current, snapshotErr := snapshotOpenedGeneratedFile(f, path, info)
	closeErr := f.Close()
	if snapshotErr != nil || closeErr != nil {
		return errors.Join(
			errGeneratedFileChanged,
			snapshotErr,
			wrapGeneratedFileCloseError(closeErr, path),
		)
	}
	if reason := changedGeneratedFile(expected, current); reason != "" {
		return fmt.Errorf(
			"%w: %q changed %s",
			errGeneratedFileChanged,
			path,
			reason,
		)
	}
	return validateGeneratedPathSnapshot(path, current)
}

func snapshotOpenedGeneratedFile(
	f *os.File,
	path string,
	expected os.FileInfo,
) (generatedFileSnapshot, error) {
	if f == nil {
		return generatedFileSnapshot{}, errors.New(
			"snapshot generated coverage HTML: file is nil",
		)
	}
	if expected == nil {
		return generatedFileSnapshot{}, errors.New(
			"snapshot generated coverage HTML: expected file information is nil",
		)
	}
	before, err := f.Stat()
	if err != nil {
		return generatedFileSnapshot{}, fmt.Errorf(
			"inspect opened generated coverage HTML %q: %w",
			path,
			err,
		)
	}
	if !before.Mode().IsRegular() || !os.SameFile(expected, before) {
		return generatedFileSnapshot{}, fmt.Errorf(
			"%w: generated coverage HTML %q changed before hashing",
			errGeneratedFileChanged,
			path,
		)
	}
	if runtime.GOOS != "windows" &&
		before.Mode().Perm() != privateFileMode {
		return generatedFileSnapshot{}, fmt.Errorf(
			"generated coverage HTML %q has permissions %s, want %s",
			path,
			before.Mode().Perm(),
			os.FileMode(privateFileMode),
		)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return generatedFileSnapshot{}, fmt.Errorf(
			"seek generated coverage HTML %q: %w",
			path,
			err,
		)
	}
	hash := sha256.New()
	read, copyErr := io.Copy(hash, f)
	if copyErr != nil {
		return generatedFileSnapshot{}, fmt.Errorf(
			"hash generated coverage HTML %q: %w",
			path,
			copyErr,
		)
	}
	after, err := f.Stat()
	if err != nil {
		return generatedFileSnapshot{}, fmt.Errorf(
			"inspect hashed generated coverage HTML %q: %w",
			path,
			err,
		)
	}
	if !after.Mode().IsRegular() ||
		!os.SameFile(before, after) ||
		after.Size() != before.Size() ||
		after.Mode() != before.Mode() ||
		!after.ModTime().Equal(before.ModTime()) ||
		read != before.Size() {
		return generatedFileSnapshot{}, fmt.Errorf(
			"%w: generated coverage HTML %q changed while hashing",
			errGeneratedFileChanged,
			path,
		)
	}

	current, err := os.Lstat(path)
	if err != nil {
		return generatedFileSnapshot{}, errors.Join(
			errGeneratedFileChanged,
			fmt.Errorf(
				"revalidate generated coverage HTML %q after hashing: %w",
				path,
				err,
			),
		)
	}
	if !current.Mode().IsRegular() ||
		!os.SameFile(after, current) ||
		current.Size() != after.Size() ||
		current.Mode() != after.Mode() ||
		!current.ModTime().Equal(after.ModTime()) {
		return generatedFileSnapshot{}, fmt.Errorf(
			"%w: generated coverage HTML pathname %q changed while hashing",
			errGeneratedFileChanged,
			path,
		)
	}

	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return generatedFileSnapshot{
		identity: current,
		size:     current.Size(),
		mode:     current.Mode(),
		modified: current.ModTime(),
		digest:   digest,
	}, nil
}

func validateGeneratedPathSnapshot(
	path string,
	expected generatedFileSnapshot,
) error {
	current, err := os.Lstat(path)
	if err != nil {
		return errors.Join(
			errGeneratedFileChanged,
			fmt.Errorf(
				"revalidate generated coverage HTML pathname %q: %w",
				path,
				err,
			),
		)
	}
	if current.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf(
			"%w: generated coverage HTML pathname %q became a symbolic link",
			errGeneratedFileChanged,
			path,
		)
	}
	if !current.Mode().IsRegular() ||
		expected.identity == nil ||
		!os.SameFile(expected.identity, current) ||
		expected.size != current.Size() ||
		expected.mode != current.Mode() ||
		!expected.modified.Equal(current.ModTime()) {
		return fmt.Errorf(
			"%w: generated coverage HTML pathname %q no longer identifies "+
				"the validated file",
			errGeneratedFileChanged,
			path,
		)
	}
	return nil
}

func changedGeneratedFile(
	expected generatedFileSnapshot,
	current generatedFileSnapshot,
) string {
	switch {
	case expected.identity == nil || current.identity == nil:
		return "identity"
	case !os.SameFile(expected.identity, current.identity):
		return "identity"
	case expected.size != current.size:
		return "size"
	case expected.mode != current.mode:
		return "mode"
	case !expected.modified.Equal(current.modified):
		return "modification time"
	case expected.digest != current.digest:
		return "contents"
	default:
		return ""
	}
}

func wrapGeneratedFileCloseError(err error, path string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("close generated coverage HTML %q: %w", path, err)
}

func commandError(ctx context.Context, goCommand, stderr string, commandErr error) error {
	detail := strings.TrimSpace(stderr)
	if contextErr := ctx.Err(); contextErr != nil {
		cause := context.Cause(ctx)
		if cause == nil {
			cause = contextErr
		}
		combined := errors.Join(cause, commandErr)
		if detail != "" {
			return fmt.Errorf(
				"run %q tool cover: %w (stderr: %s)",
				goCommand,
				combined,
				detail,
			)
		}
		return fmt.Errorf("run %q tool cover: %w", goCommand, combined)
	}
	if detail != "" {
		return fmt.Errorf(
			"run %q tool cover: %w (stderr: %s)",
			goCommand,
			commandErr,
			detail,
		)
	}
	return fmt.Errorf("run %q tool cover: %w", goCommand, commandErr)
}

type boundedStderr struct {
	buffer  bytes.Buffer
	limit   int
	omitted int64
}

func (w *boundedStderr) Write(p []byte) (int, error) {
	originalLength := len(p)
	remaining := w.limit - w.buffer.Len()
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		_, _ = w.buffer.Write(p[:remaining])
		p = p[remaining:]
	}
	w.omitted += int64(len(p))
	return originalLength, nil
}

func (w *boundedStderr) String() string {
	if w.omitted == 0 {
		return w.buffer.String()
	}
	return fmt.Sprintf("%s\n[stderr truncated: %d bytes omitted]", w.buffer.String(), w.omitted)
}
