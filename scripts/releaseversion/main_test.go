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
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseVersion(t *testing.T) {
	for _, value := range []string{"1.0.0", "1.9.99\n", "1.18446744073709551615.18446744073709551615\n"} {
		t.Run(value, func(t *testing.T) {
			v, err := parseVersion(value)
			if err != nil || v.String() != strings.TrimSuffix(value, "\n") {
				t.Fatalf("parseVersion(%q) = %v, %v", value, v, err)
			}
		})
	}
	for _, value := range []string{
		"", "0.1.0", "2.0.0", "01.2.3", "1.02.3", "1.2.03", "1.2", "1.2.3.4",
		"v1.2.3", "1.2.3-dev", "1.2.3+build", "1.-2.3", "1.+2.3", "1. 2.3",
		" 1.2.3", "1.2.3 ", "1.2.3\r\n", "1.2.3\n\n", "1.2.3\n2.0.0",
		"1.18446744073709551616.0", "1.0.18446744073709551616", "1.٢.3", "1.2.3\x00",
	} {
		t.Run("reject "+value, func(t *testing.T) {
			if _, err := parseVersion(value); err == nil {
				t.Fatalf("accepted invalid VERSION %q", value)
			}
		})
	}
}

func TestNextReleaseVersion(t *testing.T) {
	for _, tc := range []struct{ current, kind, want string }{
		{"1.0.0", "patch", "1.0.1"}, {"1.9.99", "patch", "1.9.100"},
		{"1.9.99", "minor", "1.10.0"}, {"1.9.0", "minor", "1.10.0"},
		{"1.0.18446744073709551615", "patch", ""},
		{"1.18446744073709551615.0", "minor", ""},
		{"1.0.18446744073709551615", "minor", "1.1.0"},
		{"1.18446744073709551615.0", "patch", "1.18446744073709551615.1"},
		{"1.2.3", "major", ""}, {"1.2.3", "", ""},
	} {
		t.Run(tc.current+" "+tc.kind, func(t *testing.T) {
			v, err := parseVersion(tc.current)
			if err != nil {
				t.Fatal(err)
			}
			next, err := v.next(tc.kind)
			if (err != nil) != (tc.want == "") || (err == nil && next.String() != tc.want) {
				t.Fatalf("next = %s, %v; want %q", next, err, tc.want)
			}
		})
	}
}

func TestReleaseVersionCommand(t *testing.T) {
	file := filepath.Join(t.TempDir(), "VERSION")
	if err := os.WriteFile(file, []byte("1.2.3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"check"}, "1.2.3\n"}, {[]string{"check", "v1.2.3"}, "1.2.3\n"},
		{[]string{"next", "patch"}, "1.2.4\n"}, {[]string{"next", "minor"}, "1.3.0\n"},
		{nil, ""}, {[]string{"check", "v2.2.3"}, ""}, {[]string{"next"}, ""},
		{[]string{"next", "major"}, ""}, {[]string{"next", "minor", "extra"}, ""},
		{[]string{"check", "1.2.3"}, ""}, {[]string{"--unknown"}, ""},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var output bytes.Buffer
			err := run(append([]string{"-file", file}, tc.args...), &output)
			if (err != nil) != (tc.want == "") || output.String() != tc.want {
				t.Fatalf("run = %q, %v; want %q", output.String(), err, tc.want)
			}
		})
	}
}

func TestReleaseVersionFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "VERSION")
	for _, name := range []string{file, dir} {
		if _, err := readVersion(name); err == nil {
			t.Fatalf("accepted missing or nonregular file %s", name)
		}
	}
	if err := os.WriteFile(file, []byte("1.2.3\n"+strings.Repeat(" ", 100)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readVersion(file); err == nil {
		t.Fatal("accepted oversized version file")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := readVersion(link); err == nil {
		t.Fatal("accepted symlink")
	}
}
