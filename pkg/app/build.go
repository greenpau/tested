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
	"strings"
)

const developmentValue = "unknown"

// BuildInfo contains linker-stamped tested build identity.
type BuildInfo struct {
	Version   string `json:"version"`
	GitBranch string `json:"git_branch"`
	GitCommit string `json:"git_commit"`
	BuildUser string `json:"build_user"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
}

// Normalize supplies explicit development values for unstamped fields.
func (info BuildInfo) Normalize() BuildInfo {
	info.Version = valueOr(info.Version, "dev")
	info.GitBranch = valueOr(info.GitBranch, developmentValue)
	info.GitCommit = valueOr(info.GitCommit, developmentValue)
	info.BuildUser = valueOr(info.BuildUser, developmentValue)
	info.BuildDate = valueOr(info.BuildDate, developmentValue)
	info.GoVersion = valueOr(info.GoVersion, runtime.Version())
	return info
}

// String renders a stable multiline version response.
func (info BuildInfo) String() string {
	info = info.Normalize()
	return fmt.Sprintf(
		"tested %s\n  commit: %s\n  branch: %s\n  built: %s by %s\n  go: %s\n",
		info.Version,
		info.GitCommit,
		info.GitBranch,
		info.BuildDate,
		info.BuildUser,
		info.GoVersion,
	)
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
