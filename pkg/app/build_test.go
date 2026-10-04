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
	"runtime/debug"
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

func TestBuildInfoGoMetadata(t *testing.T) {
	goMetadata := &debug.BuildInfo{
		GoVersion: "go1.26.0",
		Main:      debug.Module{Version: "v1.2.3"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.time", Value: "2026-01-02T03:04:05Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	}
	tests := []struct {
		name                  string
		stamps                BuildInfo
		metadata              *debug.BuildInfo
		version, commit, date string
	}{
		{"module and dirty VCS", BuildInfo{}, goMetadata, "1.2.3", "0123456789abcdef-dirty", "2026-01-02T03:04:05Z"},
		{"main defaults", BuildInfo{Version: "dev", GitCommit: "unknown"}, goMetadata, "1.2.3", "0123456789abcdef-dirty", "2026-01-02T03:04:05Z"},
		{"normalized defaults", (BuildInfo{}).Normalize(), goMetadata, "1.2.3", "0123456789abcdef-dirty", "2026-01-02T03:04:05Z"},
		{"stamps win", BuildInfo{Version: "2.0.0", GitCommit: "stamped"}, goMetadata, "2.0.0", "stamped", ""},
		{"module install", BuildInfo{}, &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, "1.2.3", developmentValue, ""},
		{"development", BuildInfo{}, &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "dev", developmentValue, ""},
		{"missing metadata", BuildInfo{}, nil, "dev", developmentValue, ""},
		{"empty metadata", BuildInfo{}, &debug.BuildInfo{}, "dev", developmentValue, ""},
		{"versioned replacement", BuildInfo{}, &debug.BuildInfo{Main: debug.Module{Version: "v1.0.0", Replace: &debug.Module{Version: "v2.0.0"}}}, "2.0.0", developmentValue, ""},
		{"local replacement", BuildInfo{}, &debug.BuildInfo{Main: debug.Module{Version: "v1.0.0", Replace: &debug.Module{Path: "../tested"}}}, "dev", developmentValue, ""},
		{"clean VCS", BuildInfo{}, &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}, {Key: "vcs.modified", Value: "false"}}}, "dev", "abc", ""},
		{"dirty without revision", BuildInfo{}, &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}, {Key: "vcs.time", Value: "2026-01-01T00:00:00Z"}}}, "dev", developmentValue, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.stamps.withGoBuildInfo(tt.metadata)
			if got.Version != tt.version || got.GitCommit != tt.commit || got.CommitDate != tt.date {
				t.Fatalf("resolved identity = %#v", got)
			}
			if got.GitBranch != developmentValue || got.BuildDate != developmentValue || got.BuildUser != developmentValue {
				t.Fatalf("invented provenance: %#v", got)
			}
			if got.GoVersion == "" {
				t.Fatal("missing Go version")
			}
		})
	}
}

func TestBuildInfoPreservesStamps(t *testing.T) {
	stamps := BuildInfo{Version: "1.2.3", GitCommit: "abc", GitBranch: "release", BuildDate: "2026-02-03T00:00:00Z", BuildUser: "builder", GoVersion: "go1.25.0"}
	if got := stamps.withGoBuildInfo(&debug.BuildInfo{GoVersion: "go1.26.0"}); got != stamps {
		t.Fatalf("stamps changed: %#v", got)
	}
	if strings.Contains(stamps.String(), "note:") {
		t.Fatal("complete build should not suggest rebuilding")
	}
}

func TestBuildInfoMissingFieldsAndCommitTime(t *testing.T) {
	info := BuildInfo{Version: "unknown", GitCommit: "unknown", GitBranch: "unknown", BuildUser: "unknown", BuildDate: "unknown", CommitDate: "2026-01-02T03:04:05Z"}
	output := info.String()
	for _, want := range []string{"tested dev\n", "commit: not recorded", "branch: not recorded", "built: not recorded by not recorded", "commit time: 2026-01-02T03:04:05Z", "make install"} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q in %s", want, output)
		}
	}
	if strings.Contains(output, "built: 2026") || strings.Contains(output, "unknown") {
		t.Fatalf("misleading output: %s", output)
	}
}
