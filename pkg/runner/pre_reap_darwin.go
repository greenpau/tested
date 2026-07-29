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

//go:build darwin

package runner

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

func cleanupProcessTreeBeforeReap(cmd *exec.Cmd, grace time.Duration) error {
	err := killProcessTree(cmd, grace)
	// Darwin reports EPERM when the process group contains only the exited,
	// unreaped leader. If any same-user descendant remains signalable, the group
	// signal succeeds instead. Treat this no-live-target result like ESRCH.
	if errors.Is(err, syscall.EPERM) {
		return nil
	}
	return err
}
