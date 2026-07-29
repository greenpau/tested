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

//go:build windows

package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

var generateConsoleCtrlEvent = syscall.NewLazyDLL("kernel32.dll").
	NewProc("GenerateConsoleCtrlEvent")

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func interruptProcessTree(cmd *exec.Cmd, _ os.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	result, _, callErr := generateConsoleCtrlEvent.Call(
		uintptr(syscall.CTRL_BREAK_EVENT),
		uintptr(cmd.Process.Pid),
	)
	if result == 0 {
		return callErr
	}
	return nil
}

func killProcessTree(cmd *exec.Cmd, timeout time.Duration) error {
	if cmd.Process == nil {
		return nil
	}
	treeErr := taskkillProcessTree(cmd.Process.Pid, timeout)
	processErr := cmd.Process.Kill()
	if errors.Is(processErr, os.ErrProcessDone) {
		return nil
	}
	return errors.Join(treeErr, processErr)
}

func taskkillProcessTree(pid int, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultInterruptGrace
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		"taskkill",
		"/PID",
		strconv.Itoa(pid),
		"/T",
		"/F",
	)
	command.WaitDelay = timeout
	treeErr := command.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		treeErr = fmt.Errorf("terminate process tree with taskkill: %w", ctxErr)
	}
	return treeErr
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
	return state.ExitCode(), ""
}
