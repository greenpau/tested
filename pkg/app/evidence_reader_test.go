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

package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greenpau/tested/pkg/artifact"
	"github.com/greenpau/tested/pkg/runstatus"
)

func TestBoundEvidenceReaderVerifiesConsumedDescriptorAndPath(t *testing.T) {
	const content = "bound evidence\n"

	tests := []struct {
		name   string
		mutate func(*testing.T, string)
		wantOK bool
	}{
		{
			name:   "stable",
			wantOK: true,
		},
		{
			name: "path replacement",
			mutate: func(t *testing.T, path string) {
				t.Helper()
				original := path + ".original"
				if err := os.Rename(path, original); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "opened file grows after consumption",
			mutate: func(t *testing.T, path string) {
				t.Helper()
				file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.WriteString("late"); err != nil {
					_ = file.Close()
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), artifact.TestOutputJSONLName)
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			file, err := openRegularEvidence(path)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256([]byte(content))
			reader, err := newBoundEvidenceReader(
				file,
				path,
				runstatus.File{
					Name:   artifact.TestOutputJSONLName,
					Size:   int64(len(content)),
					SHA256: hex.EncodeToString(sum[:]),
				},
			)
			if err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			got, err := io.ReadAll(reader)
			if err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			if string(got) != content {
				_ = file.Close()
				t.Fatalf("ReadAll() = %q, want %q", got, content)
			}
			if test.mutate != nil {
				test.mutate(t, path)
			}
			verifyErr := reader.Verify()
			if closeErr := file.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
			if test.wantOK && verifyErr != nil {
				t.Fatalf("Verify() error = %v", verifyErr)
			}
			if !test.wantOK && verifyErr == nil {
				t.Fatal("Verify() error = nil, want integrity error")
			}
		})
	}
}

func TestVerifyStableOpenedEvidenceRejectsPathAndContentChanges(t *testing.T) {
	const content = "coverage evidence\n"
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
		wantOK bool
	}{
		{name: "stable", wantOK: true},
		{
			name: "path replacement",
			mutate: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "content growth",
			mutate: func(t *testing.T, path string) {
				t.Helper()
				file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.WriteString("late"); err != nil {
					_ = file.Close()
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "coverage.out")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			file, err := openRegularEvidence(path)
			if err != nil {
				t.Fatal(err)
			}
			opened, err := file.Stat()
			if err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			if test.mutate != nil {
				test.mutate(t, path)
			}
			verifyErr := verifyStableOpenedEvidence(
				file,
				path,
				opened,
				int64(len(content)),
			)
			if closeErr := file.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
			if test.wantOK && verifyErr != nil {
				t.Fatalf("verifyStableOpenedEvidence() error = %v", verifyErr)
			}
			if !test.wantOK && verifyErr == nil {
				t.Fatal("verifyStableOpenedEvidence() error = nil, want integrity error")
			}
		})
	}
}

func TestParseCoverageSnapshotBindsExactlyConsumedBytes(t *testing.T) {
	const profileContent = "" +
		"mode: set\n" +
		"example.test/sample.go:1.1,1.20 1 1\n" +
		"example.test/sample.go:2.1,2.20 2 0\n"

	workDir := t.TempDir()
	layout, err := artifact.Resolve(workDir, artifact.DefaultOutputDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(layout.OutputDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		layout.CoverageProfile,
		[]byte(profileContent),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(profileContent))
	evidence := runstatus.Evidence{
		Files: []runstatus.File{{
			Name:   artifact.CoverageProfileName,
			Size:   int64(len(profileContent)),
			SHA256: hex.EncodeToString(sum[:]),
		}},
	}

	profile, snapshotPath, err := parseCoverageSnapshot(
		layout,
		layout.CoverageProfile,
		evidence,
		true,
	)
	if err != nil {
		t.Fatalf("parseCoverageSnapshot() error = %v", err)
	}
	defer func() {
		_ = os.Remove(snapshotPath)
	}()
	if profile.Total.Covered != 1 || profile.Total.Statements != 3 {
		t.Fatalf("profile totals = %#v, want 1/3", profile.Total)
	}
	snapshot, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshot, []byte(profileContent)) {
		t.Fatalf("verified snapshot differs from consumed profile")
	}
	info, err := os.Stat(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode = %04o, want 0600", info.Mode().Perm())
	}

	evidence.Files[0].SHA256 = strings.Repeat("0", 64)
	if _, path, err := parseCoverageSnapshot(
		layout,
		layout.CoverageProfile,
		evidence,
		true,
	); err == nil {
		if path != "" {
			_ = os.Remove(path)
		}
		t.Fatal("parseCoverageSnapshot(tampered binding) error = nil")
	}
	matches, err := filepath.Glob(filepath.Join(
		layout.OutputDir,
		".verified-coverage-*.out",
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0] != snapshotPath {
		t.Fatalf("temporary coverage snapshots = %v, want only %q", matches, snapshotPath)
	}
}
