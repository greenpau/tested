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
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

const developmentValue = "not recorded"

// BuildInfo contains tested build identity. Explicit linker stamps take
// precedence over Go's embedded module and VCS metadata.
type BuildInfo struct {
	Version    string `json:"version"`
	GitBranch  string `json:"git_branch"`
	GitCommit  string `json:"git_commit"`
	BuildUser  string `json:"build_user"`
	BuildDate  string `json:"build_date"`
	GoVersion  string `json:"go_version"`
	CommitDate string `json:"commit_date,omitempty"`
}

// Normalize supplies explicit values for metadata the build did not record.
func (info BuildInfo) Normalize() BuildInfo {
	info.Version = buildValueOr(info.Version, "dev")
	info.GitBranch = buildValueOr(info.GitBranch, developmentValue)
	info.GitCommit = buildValueOr(info.GitCommit, developmentValue)
	info.BuildUser = buildValueOr(info.BuildUser, developmentValue)
	info.BuildDate = buildValueOr(info.BuildDate, developmentValue)
	info.GoVersion = valueOr(info.GoVersion, runtime.Version())
	return info
}

// String renders a stable multiline version response.
func (info BuildInfo) String() string {
	info = info.Normalize()
	output := fmt.Sprintf(
		"tested %s\n  commit: %s\n  branch: %s\n  built: %s by %s\n  go: %s\n",
		info.Version,
		info.GitCommit,
		info.GitBranch,
		info.BuildDate,
		info.BuildUser,
		info.GoVersion,
	)
	if info.CommitDate != "" {
		output += fmt.Sprintf("  commit time: %s\n", info.CommitDate)
	}
	if info.GitCommit == developmentValue || info.GitBranch == developmentValue ||
		info.BuildDate == developmentValue || info.BuildUser == developmentValue {
		output += "  note: use make install from a Git checkout to record full build metadata\n"
	}
	return output
}

func (info BuildInfo) withRuntime() BuildInfo {
	metadata, _ := debug.ReadBuildInfo()
	return info.withGoBuildInfo(metadata)
}

// withGoBuildInfo uses only immutable executable metadata. In particular,
// vcs.time is a commit timestamp, never evidence of when the binary was built.
func (info BuildInfo) withGoBuildInfo(metadata *debug.BuildInfo) BuildInfo {
	if metadata == nil {
		return info.Normalize()
	}
	module := metadata.Main
	if module.Replace != nil {
		module = *module.Replace
	}
	if buildValueOr(info.Version, "dev") == "dev" &&
		module.Version != "" && module.Version != "(devel)" {
		info.Version = strings.TrimPrefix(module.Version, "v")
	}
	var revision, commitDate string
	var modified bool
	for _, setting := range metadata.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.time":
			commitDate = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if buildValueOr(info.GitCommit, "") == "" && revision != "" {
		info.GitCommit = revision
		if modified {
			info.GitCommit += "-dirty"
		}
		info.CommitDate = commitDate
	}
	info.GoVersion = valueOr(info.GoVersion, metadata.GoVersion)
	return info.Normalize()
}

func buildValueOr(value, fallback string) string {
	switch strings.TrimSpace(value) {
	case "", "unknown", developmentValue:
		return fallback
	default:
		return value
	}
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
