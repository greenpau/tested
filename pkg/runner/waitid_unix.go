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

//go:build linux || darwin

package runner

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const waitIDProcess = 1 // POSIX P_PID.

// waitIDInfo is an aligned 128-byte destination for siginfo_t. Linux siginfo_t
// is exactly 128 bytes. Darwin's public siginfo_t is smaller, so the same
// storage is sufficient without depending on architecture-specific field
// layouts that tested does not inspect.
type waitIDInfo struct {
	words [16]uint64
}

type waitIDCall func(int, *waitIDInfo) error

func waitCommand(
	cmd *exec.Cmd,
	state *commandWaitState,
	grace time.Duration,
) (error, error) {
	return waitCommandWithObserver(
		cmd,
		state,
		observeProcessExit,
		func() error {
			return cleanupProcessTreeBeforeReap(cmd, grace)
		},
	)
}

func observeProcessExit(pid int) error {
	return observeProcessExitWith(pid, callWaitID)
}

func observeProcessExitWith(pid int, call waitIDCall) error {
	if pid <= 0 {
		return fmt.Errorf("waitid: invalid process ID %d", pid)
	}
	if call == nil {
		return errors.New("waitid: call is nil")
	}
	var info waitIDInfo
	for {
		err := call(pid, &info)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return err
	}
}

func callWaitID(pid int, info *waitIDInfo) error {
	_, _, errno := syscall.Syscall6(
		syscall.SYS_WAITID,
		uintptr(waitIDProcess),
		uintptr(pid),
		uintptr(unsafe.Pointer(info)),
		uintptr(syscall.WEXITED|syscall.WNOWAIT),
		0,
		0,
	)
	runtime.KeepAlive(info)
	if errno != 0 {
		return errno
	}
	return nil
}
