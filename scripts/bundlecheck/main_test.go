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

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateBundle(t *testing.T) {
	for _, coverage := range []bool{false, true} {
		t.Run("coverage="+map[bool]string{false: "disabled", true: "enabled"}[coverage], func(t *testing.T) {
			dir := writeBundle(t, coverage)
			report, err := validateBundle(dir, coverage)
			if err != nil {
				t.Fatalf("validateBundle() error = %v", err)
			}
			if report.files != len(expectedNames(coverage)) {
				t.Fatalf(
					"validateBundle() files = %d, want %d",
					report.files,
					len(expectedNames(coverage)),
				)
			}
		})
	}
}

func TestValidateBundleRejectsCorruption(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*testing.T, string)
		wantErr string
	}{
		{
			name: "unexpected artifact",
			mutate: func(t *testing.T, dir string) {
				writeManagedFile(t, filepath.Join(dir, "unexpected.txt"), "extra")
			},
			wantErr: "artifact inventory",
		},
		{
			name: "changed content",
			mutate: func(t *testing.T, dir string) {
				writeManagedFile(
					t,
					filepath.Join(dir, "summary.json"),
					"changed after manifest",
				)
			},
			wantErr: "manifest entry \"summary.json\"",
		},
		{
			name: "unknown manifest field",
			mutate: func(t *testing.T, dir string) {
				path := filepath.Join(dir, manifestName)
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read manifest: %v", err)
				}
				content = []byte(strings.Replace(
					string(content),
					`"version": 1`,
					`"version": 1, "unknown": true`,
					1,
				))
				if err := os.WriteFile(path, content, 0o600); err != nil {
					t.Fatalf("rewrite manifest: %v", err)
				}
			},
			wantErr: "unknown field",
		},
	}

	if runtime.GOOS != "windows" {
		tests = append(tests, struct {
			name    string
			mutate  func(*testing.T, string)
			wantErr string
		}{
			name: "public file mode",
			mutate: func(t *testing.T, dir string) {
				if err := os.Chmod(filepath.Join(dir, "summary.json"), 0o644); err != nil {
					t.Fatalf("chmod summary: %v", err)
				}
			},
			wantErr: "permissions",
		})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := writeBundle(t, true)
			test.mutate(t, dir)
			_, err := validateBundle(dir, true)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf(
					"validateBundle() error = %v, want containing %q",
					err,
					test.wantErr,
				)
			}
		})
	}
}

func writeBundle(t *testing.T, coverage bool) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".coverage")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod bundle: %v", err)
	}

	expected := expectedNames(coverage)
	for _, name := range expected {
		if name == manifestName {
			continue
		}
		content := name + "\n"
		if name == "stderr.log" {
			content = ""
		}
		writeManagedFile(t, filepath.Join(dir, name), content)
	}

	document := manifestDocument{
		Version:   1,
		Algorithm: "sha256",
	}
	for _, name := range expected {
		if name == manifestName {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		hash := sha256.Sum256(content)
		document.Files = append(document.Files, manifestEntry{
			Name:      name,
			Size:      int64(len(content)),
			SHA256:    hex.EncodeToString(hash[:]),
			MediaType: "application/octet-stream",
			Role:      "report",
			Sensitive: true,
		})
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(
		filepath.Join(dir, manifestName),
		encoded,
		0o600,
	); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.Chmod(filepath.Join(dir, manifestName), 0o600); err != nil {
		t.Fatalf("chmod manifest: %v", err)
	}
	return dir
}

func writeManagedFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}
