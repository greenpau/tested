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
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"
)

func TestResolve(t *testing.T) {
	workDir := t.TempDir()
	canonicalWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatal(err)
	}
	absoluteOutput := filepath.Join(t.TempDir(), "absolute-output")
	canonicalOutputParent, err := filepath.EvalSymlinks(filepath.Dir(absoluteOutput))
	if err != nil {
		t.Fatal(err)
	}
	canonicalAbsoluteOutput := filepath.Join(canonicalOutputParent, filepath.Base(absoluteOutput))
	tests := []struct {
		name       string
		workDir    string
		outputDir  string
		wantOutput string
	}{
		{
			name:       "default",
			workDir:    workDir,
			wantOutput: filepath.Join(canonicalWorkDir, DefaultOutputDir),
		},
		{
			name:       "relative",
			workDir:    filepath.Join(workDir, "."),
			outputDir:  "build/reports",
			wantOutput: filepath.Join(canonicalWorkDir, "build", "reports"),
		},
		{
			name:       "absolute",
			workDir:    workDir,
			outputDir:  absoluteOutput,
			wantOutput: canonicalAbsoluteOutput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Resolve(test.workDir, test.outputDir)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if got.OutputDir != test.wantOutput {
				t.Fatalf("OutputDir = %q, want %q", got.OutputDir, test.wantOutput)
			}
			if !filepath.IsAbs(got.WorkDir) || !filepath.IsAbs(got.OutputDir) {
				t.Fatalf("layout paths are not absolute: %#v", got)
			}
			assertLayoutPaths(t, got)
		})
	}

	if _, err := Resolve("", "output"); err == nil {
		t.Fatal("Resolve() with empty work directory error = nil, want error")
	}
	if _, err := Resolve(filepath.Join(workDir, "missing"), "output"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Resolve(missing workdir) error = %v, want os.ErrNotExist", err)
	}
}

func TestResolveRejectsDestructiveOutputRoots(t *testing.T) {
	workDir := t.TempDir()
	volumeRoot := filepath.VolumeName(workDir) + string(filepath.Separator)
	tests := []struct {
		name      string
		outputDir string
	}{
		{name: "work directory", outputDir: "."},
		{name: "work parent", outputDir: filepath.Dir(workDir)},
		{name: "filesystem root", outputDir: volumeRoot},
		{name: "relative traversal", outputDir: filepath.Join("..", "sibling-reports")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Resolve(workDir, test.outputDir); !errors.Is(err, ErrUnsafeOutput) {
				t.Fatalf("Resolve(%q) error = %v, want ErrUnsafeOutput", test.outputDir, err)
			}
		})
	}
}

func TestResolveRejectsSymlinkedOutputAncestors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	workDir := t.TempDir()
	actual := t.TempDir()
	link := filepath.Join(workDir, "linked-output")
	if err := os.Symlink(actual, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Resolve(workDir, filepath.Join("linked-output", "reports")); !errors.Is(err, ErrSymlink) {
		t.Fatalf("Resolve(symlink ancestor) error = %v, want ErrSymlink", err)
	}
}

func TestManagedAndEvidenceNames(t *testing.T) {
	wantManaged := []Name{
		CoverageHTML,
		CoverageProfile,
		IndexHTML,
		JUnitXML,
		ManifestJSON,
		RunJSON,
		StderrLog,
		SummaryJSON,
		TestOutputHTML,
		TestOutputJSONL,
	}
	sort.Slice(wantManaged, func(i, j int) bool { return wantManaged[i] < wantManaged[j] })
	if got := ManagedNames(); !reflect.DeepEqual(got, wantManaged) {
		t.Fatalf("ManagedNames() = %v, want %v", got, wantManaged)
	}

	wantEvidence := []Name{CoverageProfile, RunJSON, StderrLog, TestOutputJSONL}
	sort.Slice(wantEvidence, func(i, j int) bool { return wantEvidence[i] < wantEvidence[j] })
	if got := EvidenceNames(); !reflect.DeepEqual(got, wantEvidence) {
		t.Fatalf("EvidenceNames() = %v, want %v", got, wantEvidence)
	}

	// Returned slices must not expose the package's immutable inventory.
	got := ManagedNames()
	got[0] = Name("mutated")
	if ManagedNames()[0] == Name("mutated") {
		t.Fatal("ManagedNames() returned mutable package state")
	}
}

func TestPrepareRunRemovesOnlyManagedFiles(t *testing.T) {
	layout := newLayout(t)
	if err := os.Mkdir(layout.OutputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(layout.OutputDir, "keep.me")
	if err := os.WriteFile(unrelated, []byte("unrelated"), 0o644); err != nil {
		t.Fatal(err)
	}
	unrelatedDir := filepath.Join(layout.OutputDir, "keep-dir")
	if err := os.Mkdir(unrelatedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range ManagedNames() {
		path, err := layout.Path(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := layout.PrepareRun(); err != nil {
		t.Fatalf("PrepareRun() error = %v", err)
	}
	assertPrivateDir(t, layout.OutputDir)
	for _, name := range ManagedNames() {
		path, err := layout.Path(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still exists after PrepareRun(), err = %v", name, err)
		}
	}
	if data, err := os.ReadFile(unrelated); err != nil || string(data) != "unrelated" {
		t.Fatalf("unrelated file changed: data=%q err=%v", data, err)
	}
	if info, err := os.Stat(unrelatedDir); err != nil || !info.IsDir() {
		t.Fatalf("unrelated directory changed: info=%v err=%v", info, err)
	}
}

func TestPrepareReportPreservesEvidence(t *testing.T) {
	layout := newLayout(t)
	if err := layout.prepareDirectory(); err != nil {
		t.Fatal(err)
	}
	for _, name := range ManagedNames() {
		path, err := layout.Path(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := layout.PrepareReport(); err != nil {
		t.Fatalf("PrepareReport() error = %v", err)
	}
	for _, name := range EvidenceNames() {
		path, err := layout.Path(name)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read preserved %s: %v", name, err)
		}
		if string(data) != string(name) {
			t.Errorf("preserved %s data = %q, want %q", name, data, name)
		}
		assertPrivateFile(t, path)
	}
	for _, name := range reportNames {
		path, err := layout.Path(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("derived report %s still exists, err = %v", name, err)
		}
	}
}

func TestPrepareReportRejectsMultiplyLinkedEvidenceBeforeChmod(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX link counts and permission assertions are not portable to Windows")
	}
	layout := newLayout(t)
	if err := layout.prepareDirectory(); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside-evidence")
	const content = "outside evidence remains private to its owner"
	if err := os.WriteFile(outside, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(outside, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, layout.TestOutputJSONL); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if err := os.WriteFile(layout.IndexHTML, []byte("old report"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := layout.PrepareReport()
	if !errors.Is(err, ErrHardLink) {
		t.Fatalf("PrepareReport() error = %v, want ErrHardLink", err)
	}
	data, readErr := os.ReadFile(outside)
	if readErr != nil || string(data) != content {
		t.Fatalf("outside evidence changed: data=%q err=%v", data, readErr)
	}
	info, statErr := os.Stat(outside)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("outside evidence mode = %s, want 0644", got)
	}
	if _, statErr := os.Stat(layout.IndexHTML); statErr != nil {
		t.Fatalf("derived report was removed before evidence rejection: %v", statErr)
	}
}

func TestPrepareReportWithoutCoverageUnlinksProfileAliasSafely(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX link counts and permission assertions are not portable to Windows")
	}
	layout := newLayout(t)
	if err := layout.prepareDirectory(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		layout.TestOutputJSONL,
		[]byte("{\"Action\":\"pass\"}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside-profile")
	const content = "mode: set\nexample.go:1.1,1.2 1 1\n"
	if err := os.WriteFile(outside, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(outside, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, layout.CoverageProfile); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	for _, path := range []string{layout.CoverageHTML, layout.IndexHTML} {
		if err := os.WriteFile(path, []byte("old report"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := layout.PrepareReportWithoutCoverage(); err != nil {
		t.Fatalf("PrepareReportWithoutCoverage() error = %v", err)
	}
	if _, err := os.Lstat(layout.CoverageProfile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("coverage profile still exists, err = %v", err)
	}
	for _, path := range []string{layout.CoverageHTML, layout.IndexHTML} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("derived report %q still exists, err = %v", path, err)
		}
	}
	data, readErr := os.ReadFile(outside)
	if readErr != nil || string(data) != content {
		t.Fatalf("outside profile changed: data=%q err=%v", data, readErr)
	}
	info, statErr := os.Stat(outside)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("outside profile mode = %s, want 0644", got)
	}
	assertPrivateFile(t, layout.TestOutputJSONL)
}

func TestSecureOpenedPathRejectsReplacementWithoutChmoddingIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("renaming an open file and POSIX permission assertions are not portable to Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact")
	originalPath := filepath.Join(dir, "original")
	replacementPath := filepath.Join(dir, "replacement")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacementPath, []byte("replaced"), 0o644); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, originalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacementPath, path); err != nil {
		t.Fatal(err)
	}

	err = secureOpenedPath(
		path,
		expected,
		false,
		privateFileMode,
		"managed artifact",
	)
	if !errors.Is(err, errArtifactChanged) {
		t.Fatalf("secureOpenedPath() error = %v, want errArtifactChanged", err)
	}
	replacement, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := replacement.Mode().Perm(); got != 0o644 {
		t.Fatalf("replacement mode = %s, want unchanged 0644", got)
	}
	original, err := os.Stat(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := original.Mode().Perm(); got != 0o644 {
		t.Fatalf("unopened original mode = %s, want unchanged 0644", got)
	}
}

func TestRemoveEvidence(t *testing.T) {
	layout := newLayout(t)
	if err := layout.prepareDirectory(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.RunJSON, []byte("status"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := layout.RemoveEvidence(RunJSON); err != nil {
		t.Fatalf("RemoveEvidence() error = %v", err)
	}
	if _, err := os.Lstat(layout.RunJSON); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("run.json still exists after RemoveEvidence(), err = %v", err)
	}
	if err := layout.RemoveEvidence(RunJSON); err != nil {
		t.Fatalf("RemoveEvidence(missing) error = %v", err)
	}
	if err := layout.RemoveEvidence(SummaryJSON); !errors.Is(err, ErrNotEvidence) {
		t.Fatalf("RemoveEvidence(report) error = %v, want ErrNotEvidence", err)
	}
	if err := layout.RemoveEvidence(Name("../escape")); !errors.Is(err, ErrUnmanaged) {
		t.Fatalf("RemoveEvidence(unmanaged) error = %v, want ErrUnmanaged", err)
	}
	if err := os.WriteFile(layout.ManifestJSON, []byte("manifest"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := layout.Remove(ManifestJSON); err != nil {
		t.Fatalf("Remove(manifest) error = %v", err)
	}
	if _, err := os.Lstat(layout.ManifestJSON); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest still exists after Remove(), err = %v", err)
	}
	if err := layout.Remove(Name("../escape")); !errors.Is(err, ErrUnmanaged) {
		t.Fatalf("Remove(unmanaged) error = %v, want ErrUnmanaged", err)
	}
}

func TestPrepareRejectsSymlinksAndNonFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}

	t.Run("output root symlink", func(t *testing.T) {
		workDir := t.TempDir()
		actual := filepath.Join(workDir, "actual")
		if err := os.Mkdir(actual, 0o700); err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(workDir, "output")
		if err := os.Symlink(actual, output); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := Resolve(workDir, output); !errors.Is(err, ErrSymlink) {
			t.Fatalf("Resolve() error = %v, want ErrSymlink", err)
		}
	})

	t.Run("managed target symlink", func(t *testing.T) {
		layout := newLayout(t)
		if err := layout.prepareDirectory(); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, layout.CoverageHTML); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		err := layout.PrepareRun()
		if !errors.Is(err, ErrSymlink) {
			t.Fatalf("PrepareRun() error = %v, want ErrSymlink", err)
		}
		data, readErr := os.ReadFile(outside)
		if readErr != nil || string(data) != "outside" {
			t.Fatalf("symlink target changed: data=%q err=%v", data, readErr)
		}
	})

	t.Run("managed directory", func(t *testing.T) {
		layout := newLayout(t)
		if err := layout.prepareDirectory(); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(layout.CoverageHTML, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := layout.PrepareRun(); err == nil {
			t.Fatal("PrepareRun() error = nil, want non-regular target error")
		}
		if info, err := os.Stat(layout.CoverageHTML); err != nil || !info.IsDir() {
			t.Fatalf("managed directory was removed: info=%v err=%v", info, err)
		}
	})
}

func TestLayoutPathRejectsUnmanagedNames(t *testing.T) {
	layout := newLayout(t)
	tests := []Name{"", "../escape", "test_output.jsonl/child", Name(filepath.Separator)}
	for _, name := range tests {
		t.Run(string(name), func(t *testing.T) {
			if _, err := layout.Path(name); !errors.Is(err, ErrUnmanaged) {
				t.Fatalf("Path(%q) error = %v, want ErrUnmanaged", name, err)
			}
		})
	}
	var nilLayout *Layout
	if _, err := nilLayout.Path(CoverageHTML); err == nil {
		t.Fatal("nil Layout.Path() error = nil, want error")
	}

	outside := filepath.Join(t.TempDir(), SummaryJSONName)
	layout.SummaryJSON = outside
	if _, err := layout.Path(SummaryJSON); !errors.Is(err, ErrUnsafeOutput) {
		t.Fatalf("Path() with mutated target error = %v, want ErrUnsafeOutput", err)
	}
}

func FuzzLayoutPath(f *testing.F) {
	for _, seed := range []string{"", "../escape", CoverageHTMLName, TestOutputJSONLName, "nested/file"} {
		f.Add(seed)
	}
	layout, err := Resolve(f.TempDir(), ".coverage")
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, input string) {
		path, err := layout.Path(Name(input))
		if err != nil {
			return
		}
		if filepath.Dir(path) != layout.OutputDir {
			t.Fatalf("managed path %q escaped output directory %q", path, layout.OutputDir)
		}
	})
}

func newLayout(t *testing.T) *Layout {
	t.Helper()
	layout, err := Resolve(t.TempDir(), DefaultOutputDir)
	if err != nil {
		t.Fatal(err)
	}
	return layout
}

func assertLayoutPaths(t *testing.T, layout *Layout) {
	t.Helper()
	want := map[string]string{
		layout.CoverageProfile: CoverageProfileName,
		layout.CoverageHTML:    CoverageHTMLName,
		layout.TestOutputJSONL: TestOutputJSONLName,
		layout.TestOutputHTML:  TestOutputHTMLName,
		layout.SummaryJSON:     SummaryJSONName,
		layout.ManifestJSON:    ManifestJSONName,
		layout.JUnitXML:        JUnitXMLName,
		layout.StderrLog:       StderrLogName,
		layout.RunJSON:         RunJSONName,
		layout.IndexHTML:       IndexHTMLName,
	}
	if len(want) != len(ManagedNames()) {
		t.Fatalf("layout has %d distinct managed paths, want %d", len(want), len(ManagedNames()))
	}
	for path, base := range want {
		if !filepath.IsAbs(path) || filepath.Dir(path) != layout.OutputDir || filepath.Base(path) != base {
			t.Errorf("managed path %q does not map to %q in %q", path, base, layout.OutputDir)
		}
	}
}

func assertPrivateDir(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("%s mode = %s, want 0700", path, got)
	}
}

func assertPrivateFile(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("%s mode = %s, want 0600", path, got)
	}
}
