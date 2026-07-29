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
	"strings"
	"testing"
)

func TestBuildInfoString(t *testing.T) {
	output := (BuildInfo{
		Version:   "1.2.3",
		GitBranch: "main",
		GitCommit: "abc123",
		BuildUser: "builder",
		BuildDate: "2026-07-29",
		GoVersion: "go1.25.0",
	}).String()
	for _, expected := range []string{
		"tested 1.2.3",
		"commit: abc123",
		"branch: main",
		"built: 2026-07-29 by builder",
		"go: go1.25.0",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("String() missing %q: %s", expected, output)
		}
	}
}

func TestBuildInfoNormalize(t *testing.T) {
	info := (BuildInfo{}).Normalize()
	if info.Version != "dev" || info.GitCommit != developmentValue ||
		info.GoVersion == "" {
		t.Fatalf("Normalize() = %#v", info)
	}
}
