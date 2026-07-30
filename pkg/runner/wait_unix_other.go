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

//go:build unix && !linux && !darwin

package runner

import (
	"os/exec"
	"time"
)

func waitCommand(
	cmd *exec.Cmd,
	state *commandWaitState,
	_ time.Duration,
	_ *processTreeOwner,
) (error, error) {
	waitErr := cmd.Wait()
	// This platform has no tested non-reaping observer. Disable all later
	// process-group cleanup after Cmd.Wait rather than risk signaling a reused
	// numeric process-group identifier.
	_ = state.beginReap(nil)
	return waitErr, nil
}
