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
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestModulePackage(t *testing.T) {
	for _, tc := range []struct{ name, content, want string }{
		{"plain", "module example.com/root\n\ngo 1.25\n", "example.com/root"},
		{"comments and whitespace", "// module decoy\r\n\tmodule\t example.com/root // comment\r\n", "example.com/root"},
		{"quoted", "module \"example.com/root\"\n", "example.com/root"},
		{"raw quoted", "module `example.com/root`\n", "example.com/root"},
		{"directive follows require block", "require (\n module v1.0.0\n)\nmodule example.com/root\n", "example.com/root"},
		{"block without separating space", "require(\n module v1.0.0\n)\nmodule example.com/root\n", "example.com/root"},
		{"empty blocks", "require ()\nreplace()\nmodule example.com/root\n", "example.com/root"},
		{"directive precedes replace block", "module example.com/root\nreplace (\n module => ./local\n)\n", "example.com/root"},
		{"duplicate directives", "module example.com/first\nmodule example.com/second\n", ""},
		{"unterminated block", "module example.com/root\nrequire (\n module v1.0.0\n", ""},
		{"no directive", "go 1.25\n", ""},
		{"similar directive", "modulex example.com/root\n", ""},
		{"missing path", "module\n", ""},
		{"extra token", "module example.com/root extra\n", ""},
		{"malformed quote", "module \"example.com/root\n", ""},
		{"absolute", "module /example/root\n", ""},
		{"parent segment", "module example.com/../root\n", ""},
		{"inline comment", "module example.com//comment\n", "example.com"},
		{"trailing separator", "module example.com/root/\n", ""},
		{"oversized", "module example.com/root\n" + strings.Repeat(" ", 1<<20), ""},
		{"exact size bound", "module example.com/root\n" + strings.Repeat(" ", (1<<20)-len("module example.com/root\n")), "example.com/root"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			if got := modulePackage(dir); got != tc.want {
				t.Fatalf("module = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestModulePackageRejectsSymlinkBoundary(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "go.mod")
	if err := os.WriteFile(parent, []byte("module example.com/parent\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(child, "go.mod")
	if err := os.Symlink(parent, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink creation unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if got := modulePackage(child); got != "" {
		t.Fatalf("symlink boundary used module %q", got)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if got := modulePackage(child); got != "" {
		t.Fatalf("dangling symlink boundary used module %q", got)
	}
}

func TestModulePackageHonorsNearestBoundary(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "internal", "tag")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/root\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := modulePackage(child); got != "example.com/root" {
		t.Fatalf("parent module = %q", got)
	}
	nested := filepath.Join(root, "internal", "go.mod")
	for _, content := range []string{"module example.com/nested\n", "go 1.25\n"} {
		if err := os.WriteFile(nested, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		want := ""
		if strings.HasPrefix(content, "module") {
			want = "example.com/nested"
		}
		if got := modulePackage(child); got != want {
			t.Fatalf("nested module = %q, want %q", got, want)
		}
	}
	if err := os.Remove(nested); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if got := modulePackage(child); got != "" {
		t.Fatalf("nonregular module = %q", got)
	}
}
