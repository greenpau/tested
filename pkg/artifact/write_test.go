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
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCreateEvidenceExclusiveAndPrivate(t *testing.T) {
	tests := []struct {
		name Name
		open func(*Layout) (*os.File, error)
	}{
		{name: TestOutputJSONL, open: (*Layout).CreateTestOutput},
		{name: CoverageProfile, open: (*Layout).CreateCoverageProfile},
		{name: StderrLog, open: (*Layout).CreateStderrLog},
		{name: RunJSON, open: (*Layout).CreateRunJSON},
	}
	for _, test := range tests {
		t.Run(string(test.name), func(t *testing.T) {
			layout := newLayout(t)
			f, err := test.open(layout)
			if err != nil {
				t.Fatalf("CreateEvidence() error = %v", err)
			}
			if _, err := io.WriteString(f, "raw evidence"); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}

			path, err := layout.Path(test.name)
			if err != nil {
				t.Fatal(err)
			}
			assertPrivateDir(t, layout.OutputDir)
			assertPrivateFile(t, path)
			if data, err := os.ReadFile(path); err != nil || string(data) != "raw evidence" {
				t.Fatalf("evidence data = %q, err = %v", data, err)
			}

			if _, err := test.open(layout); !errors.Is(err, os.ErrExist) {
				t.Fatalf("second CreateEvidence() error = %v, want os.ErrExist", err)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "raw evidence" {
				t.Fatalf("existing evidence changed: data=%q err=%v", data, err)
			}
		})
	}
}

func TestCreateEvidenceRejectsReportsAndSymlinks(t *testing.T) {
	layout := newLayout(t)
	for _, name := range []Name{CoverageHTML, TestOutputHTML, SummaryJSON, ManifestJSON, JUnitXML, IndexHTML} {
		t.Run(string(name), func(t *testing.T) {
			if _, err := layout.CreateEvidence(name); !errors.Is(err, ErrNotEvidence) {
				t.Fatalf("CreateEvidence(%q) error = %v, want ErrNotEvidence", name, err)
			}
		})
	}
	if _, err := layout.CreateEvidence(Name("../escape")); !errors.Is(err, ErrUnmanaged) {
		t.Fatalf("CreateEvidence(unmanaged) error = %v, want ErrUnmanaged", err)
	}

	if runtime.GOOS == "windows" {
		return
	}
	if err := layout.prepareDirectory(); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, layout.TestOutputJSONL); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := layout.CreateTestOutput(); !errors.Is(err, ErrSymlink) {
		t.Fatalf("CreateTestOutput() error = %v, want ErrSymlink", err)
	}
}

func TestWriteAtomicSuccessAndCallbackFailure(t *testing.T) {
	layout := newLayout(t)
	if err := layout.Replace(SummaryJSON, []byte("old")); err != nil {
		t.Fatalf("initial Replace() error = %v", err)
	}
	assertPrivateFile(t, layout.SummaryJSON)

	wantErr := errors.New("callback failed")
	err := layout.WriteAtomic(SummaryJSON, func(w io.Writer) error {
		if _, err := io.WriteString(w, "incomplete"); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WriteAtomic() error = %v, want callback error", err)
	}
	if data, err := os.ReadFile(layout.SummaryJSON); err != nil || string(data) != "old" {
		t.Fatalf("destination after callback failure = %q, err = %v", data, err)
	}
	assertNoArtifactTemps(t, layout.OutputDir)

	if err := layout.WriteAtomic(SummaryJSON, func(w io.Writer) error {
		_, err := io.WriteString(w, "new complete value")
		return err
	}); err != nil {
		t.Fatalf("WriteAtomic(success) error = %v", err)
	}
	if data, err := os.ReadFile(layout.SummaryJSON); err != nil || string(data) != "new complete value" {
		t.Fatalf("destination after success = %q, err = %v", data, err)
	}
	assertPrivateFile(t, layout.SummaryJSON)
	assertNoArtifactTemps(t, layout.OutputDir)
}

func TestWriteAtomicValidationAndTargetSafety(t *testing.T) {
	layout := newLayout(t)
	if err := layout.WriteAtomic(SummaryJSON, nil); err == nil {
		t.Fatal("WriteAtomic(nil callback) error = nil, want error")
	}
	if err := layout.Replace(Name("../escape"), []byte("bad")); !errors.Is(err, ErrUnmanaged) {
		t.Fatalf("Replace(unmanaged) error = %v, want ErrUnmanaged", err)
	}

	if runtime.GOOS == "windows" {
		return
	}
	if err := layout.prepareDirectory(); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, layout.SummaryJSON); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := layout.Replace(SummaryJSON, []byte("replacement")); !errors.Is(err, ErrSymlink) {
		t.Fatalf("Replace(symlink) error = %v, want ErrSymlink", err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "outside" {
		t.Fatalf("symlink target changed: data=%q err=%v", data, err)
	}
}

func TestInspectTemporaryArtifactRejectsPathReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "temporary")
	originalPath := filepath.Join(dir, "original")
	replacementPath := filepath.Join(dir, "replacement")
	if err := os.WriteFile(path, []byte("staged"), 0o600); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		replacementPath,
		[]byte("change"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, originalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacementPath, path); err != nil {
		t.Fatal(err)
	}

	if _, err := inspectTemporaryArtifact(
		path,
		expected,
		true,
	); !errors.Is(err, errTemporaryArtifactChanged) {
		t.Fatalf(
			"inspectTemporaryArtifact() error = %v, want "+
				"errTemporaryArtifactChanged",
			err,
		)
	}
}

func assertNoArtifactTemps(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") && strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("temporary artifact remains: %s", entry.Name())
		}
	}
}
