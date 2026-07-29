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

//go:build !unix && !windows

package runner

import (
	"os"
	"os/exec"
	"time"
)

func configureProcess(_ *exec.Cmd) {}

type processTreeOwner struct{}

func acquireProcessTree(_ *exec.Cmd) (processTreeOwner, error) {
	return processTreeOwner{}, nil
}

func interruptProcessTree(cmd *exec.Cmd, gracefulSignal os.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	if gracefulSignal == nil {
		gracefulSignal = os.Interrupt
	}
	return cmd.Process.Signal(gracefulSignal)
}

func killProcessTree(cmd *exec.Cmd, _ time.Duration) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

func cleanupOwnedProcessTree(
	_ *processTreeOwner,
	_ *exec.Cmd,
	_ time.Duration,
	_ bool,
) error {
	return nil
}

func recommendedSignalExitCode(signal os.Signal) int {
	if signal != nil && signal.String() == "terminated" {
		return 143
	}
	return 130
}

func processStatus(state *os.ProcessState) (int, string) {
	if state == nil {
		return -1, ""
	}
	return state.ExitCode(), ""
}
