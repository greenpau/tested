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
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

type processTreeOwner struct{}

func acquireProcessTree(_ *exec.Cmd) (processTreeOwner, error) {
	return processTreeOwner{}, nil
}

func interruptProcessTree(cmd *exec.Cmd, gracefulSignal os.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	signal, ok := gracefulSignal.(syscall.Signal)
	if !ok || signal <= 0 {
		signal = syscall.SIGINT
	}
	err := syscall.Kill(-cmd.Process.Pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func killProcessTree(cmd *exec.Cmd, _ time.Duration) error {
	if cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if err == nil {
		return nil
	}
	processErr := cmd.Process.Kill()
	if errors.Is(processErr, os.ErrProcessDone) {
		processErr = nil
	}
	return errors.Join(err, processErr)
}

func cleanupOwnedProcessTree(
	_ *processTreeOwner,
	_ *exec.Cmd,
	_ time.Duration,
	_ bool,
) error {
	// Linux and Darwin clean the process group after a non-reaping exit
	// observation and before Cmd.Wait releases the leader PID. Other Unix
	// platforms deliberately omit natural-exit cleanup. Never address a Unix
	// process group by the leader's numeric PID after reaping.
	return nil
}

func recommendedSignalExitCode(signal os.Signal) int {
	if value, ok := signal.(syscall.Signal); ok && value > 0 {
		return 128 + int(value)
	}
	return 128 + int(syscall.SIGINT)
}

func processStatus(state *os.ProcessState) (int, string) {
	if state == nil {
		return -1, ""
	}
	status, ok := state.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return state.ExitCode(), ""
	}
	signal := status.Signal()
	return 128 + int(signal), signal.String()
}
