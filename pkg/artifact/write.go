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
	"io"
	"os"
	"path/filepath"
)

var errTemporaryArtifactChanged = errors.New(
	"temporary artifact changed before publication",
)

// CreateEvidence safely creates a new raw evidence file with O_EXCL and mode
// 0600. It never truncates evidence that already exists.
func (l *Layout) CreateEvidence(name Name) (*os.File, error) {
	if !isEvidence(name) {
		if _, err := l.Path(name); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %q", ErrNotEvidence, name)
	}
	if err := l.prepareDirectory(); err != nil {
		return nil, err
	}
	path, err := l.Path(name)
	if err != nil {
		return nil, err
	}
	if err := rejectSymlinkComponents(l.OutputDir, path); err != nil {
		return nil, err
	}
	if err := rejectSymlinkOrNonRegular(path); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, privateFileMode)
	if err != nil {
		return nil, fmt.Errorf("create evidence artifact %q: %w", path, err)
	}
	if err := f.Chmod(privateFileMode); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("set evidence artifact permissions on %q: %w", path, err)
	}
	return f, nil
}

// CreateTestOutput creates test_output.jsonl without replacing existing
// evidence.
func (l *Layout) CreateTestOutput() (*os.File, error) {
	return l.CreateEvidence(TestOutputJSONL)
}

// CreateCoverageProfile creates coverage.out without replacing existing
// evidence.
func (l *Layout) CreateCoverageProfile() (*os.File, error) {
	return l.CreateEvidence(CoverageProfile)
}

// CreateStderrLog creates stderr.log without replacing existing evidence.
func (l *Layout) CreateStderrLog() (*os.File, error) {
	return l.CreateEvidence(StderrLog)
}

// CreateRunJSON creates run.json without replacing existing evidence.
func (l *Layout) CreateRunJSON() (*os.File, error) {
	return l.CreateEvidence(RunJSON)
}

// WriteAtomic writes a managed artifact to a same-directory temporary file,
// syncs it, and atomically replaces the destination. A callback failure leaves
// any prior destination unchanged.
func (l *Layout) WriteAtomic(name Name, write func(io.Writer) error) error {
	return l.writeAtomic(name, write, nil)
}

func (l *Layout) writeAtomic(
	name Name,
	write func(io.Writer) error,
	beforePublish func() error,
) error {
	if write == nil {
		return errors.New("write artifact atomically: writer callback is nil")
	}
	if err := l.prepareDirectory(); err != nil {
		return err
	}
	path, err := l.Path(name)
	if err != nil {
		return err
	}
	if err := rejectSymlinkComponents(l.OutputDir, path); err != nil {
		return err
	}
	if err := rejectSymlinkOrNonRegular(path); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(l.OutputDir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary artifact for %q: %w", path, err)
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
		return fmt.Errorf("set temporary artifact permissions for %q: %w", path, err)
	}
	if err := write(tmp); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary artifact for %q: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary artifact for %q: %w", path, err)
	}
	tmpInfo, err := tmp.Stat()
	if err != nil {
		_ = tmp.Close()
		return fmt.Errorf("inspect temporary artifact for %q: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary artifact for %q: %w", path, err)
	}
	// Some filesystems finalize timestamps when the last descriptor closes.
	// Capture that closed state while still requiring the pathname to identify
	// the descriptor-created file with the expected size and permissions.
	closedTmpInfo, err := inspectTemporaryArtifact(tmpPath, tmpInfo, false)
	if err != nil {
		return fmt.Errorf("validate closed temporary artifact for %q: %w", path, err)
	}

	// Run publication-specific validation only after the complete replacement
	// is staged. The destination and temporary paths are checked again below,
	// keeping validation immediately adjacent to the atomic replacement.
	if beforePublish != nil {
		if err := beforePublish(); err != nil {
			return fmt.Errorf(
				"validate before publishing artifact %q: %w",
				path,
				err,
			)
		}
	}

	// Recheck immediately before replacement so a symlink cannot silently turn
	// an intended managed-file replacement into an ambiguous mutation.
	if err := rejectSymlinkComponents(l.OutputDir, path); err != nil {
		return err
	}
	if err := rejectSymlinkOrNonRegular(path); err != nil {
		return err
	}
	if _, err := inspectTemporaryArtifact(tmpPath, closedTmpInfo, true); err != nil {
		return fmt.Errorf("revalidate temporary artifact for %q: %w", path, err)
	}
	if err := replaceFile(tmpPath, path); err != nil {
		return fmt.Errorf("replace artifact %q: %w", path, err)
	}
	removeTemp = false
	return nil
}

// Replace atomically replaces a managed artifact with data.
func (l *Layout) Replace(name Name, data []byte) error {
	return l.WriteAtomic(name, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

func inspectTemporaryArtifact(
	path string,
	expected os.FileInfo,
	compareModTime bool,
) (os.FileInfo, error) {
	if expected == nil {
		return nil, errors.New(
			"inspect temporary artifact: expected file information is nil",
		)
	}
	current, err := os.Lstat(path)
	if err != nil {
		return nil, errors.Join(
			errTemporaryArtifactChanged,
			fmt.Errorf("inspect temporary artifact %q: %w", path, err),
		)
	}
	if current.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf(
			"%w: temporary artifact %q changed to a symbolic link",
			errTemporaryArtifactChanged,
			path,
		)
	}
	if !current.Mode().IsRegular() ||
		!os.SameFile(expected, current) ||
		expected.Size() != current.Size() ||
		expected.Mode().Perm() != current.Mode().Perm() ||
		(compareModTime && !expected.ModTime().Equal(current.ModTime())) {
		return nil, fmt.Errorf(
			"%w: temporary artifact %q no longer identifies the staged file",
			errTemporaryArtifactChanged,
			path,
		)
	}
	return current, nil
}

func rejectSymlinkOrNonRegular(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
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
	return nil
}
