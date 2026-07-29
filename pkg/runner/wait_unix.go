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

//go:build unix

package runner

import (
	"fmt"
	"os/exec"
)

type processExitObserver func(int) error

func waitCommandWithObserver(
	cmd *exec.Cmd,
	state *commandWaitState,
	observe processExitObserver,
	preReapCleanup func() error,
) (error, error) {
	if cmd == nil || cmd.Process == nil {
		return errorsForUnstartedWait(cmd)
	}
	if observe == nil {
		observeErr := fmt.Errorf("process exit observer is nil")
		// No code in this process has started a reaping wait. Disable signaling
		// before falling back to Cmd.Wait because an observer failure cannot
		// establish whether the numeric PID still identifies the child.
		_ = state.beginReap(nil)
		return cmd.Wait(), fmt.Errorf(
			"observe process %d before reap: %w",
			cmd.Process.Pid,
			observeErr,
		)
	}
	if err := observe(cmd.Process.Pid); err != nil {
		// Do not run process-group cleanup after an observation error. In
		// particular, ECHILD may mean host signal policy already reaped the
		// process, in which case the numeric PID is no longer an owned identity.
		_ = state.beginReap(nil)
		return cmd.Wait(), fmt.Errorf(
			"observe process %d before reap: %w",
			cmd.Process.Pid,
			err,
		)
	}
	cleanupErr := state.beginReap(preReapCleanup)
	return cmd.Wait(), cleanupErr
}

func errorsForUnstartedWait(cmd *exec.Cmd) (error, error) {
	if cmd == nil {
		return fmt.Errorf("wait for command: command is nil"), nil
	}
	return fmt.Errorf("wait for command: process is nil"), nil
}
