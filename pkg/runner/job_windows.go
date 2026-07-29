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
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const (
	jobObjectExtendedLimitInformationClass = 9
	jobObjectLimitKillOnClose              = 0x00002000
	processSetQuota                        = 0x00000100
	windowsErrorInvalidParameter           = syscall.Errno(87)
)

var (
	createJobObjectProc = syscall.NewLazyDLL("kernel32.dll").
				NewProc("CreateJobObjectW")
	setInformationJobObjectProc = syscall.NewLazyDLL("kernel32.dll").
					NewProc("SetInformationJobObject")
	assignProcessToJobObjectProc = syscall.NewLazyDLL("kernel32.dll").
					NewProc("AssignProcessToJobObject")
	terminateJobObjectProc = syscall.NewLazyDLL("kernel32.dll").
				NewProc("TerminateJobObject")
)

type jobObjectIOCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobObjectExtendedLimitInformation struct {
	BasicLimitInformation jobObjectBasicLimitInformation
	IOInfo                jobObjectIOCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

type windowsJobOperations struct {
	create            func() (syscall.Handle, error)
	assign            func(syscall.Handle, int) error
	close             func(syscall.Handle) error
	terminateAndClose func(syscall.Handle, time.Duration) error
}

var defaultWindowsJobOperations = windowsJobOperations{
	create:            createKillOnCloseJob,
	assign:            assignProcessToJob,
	close:             syscall.CloseHandle,
	terminateAndClose: terminateAndCloseJob,
}

type processTreeOwner struct {
	job               syscall.Handle
	terminateAndClose func(syscall.Handle, time.Duration) error
}

func acquireProcessTree(cmd *exec.Cmd) (processTreeOwner, error) {
	if cmd.Process == nil {
		return processTreeOwner{}, nil
	}
	return acquireWindowsProcessTree(
		cmd.Process.Pid,
		defaultWindowsJobOperations,
	)
}

func acquireWindowsProcessTree(
	pid int,
	operations windowsJobOperations,
) (processTreeOwner, error) {
	if pid <= 0 {
		return processTreeOwner{}, fmt.Errorf("assign Windows job: invalid PID %d", pid)
	}
	if operations.create == nil || operations.assign == nil ||
		operations.close == nil || operations.terminateAndClose == nil {
		return processTreeOwner{}, fmt.Errorf("assign Windows job: incomplete operations")
	}
	job, err := operations.create()
	if err != nil {
		return processTreeOwner{}, fmt.Errorf("create Windows job: %w", err)
	}
	if err := operations.assign(job, pid); err != nil {
		closeErr := operations.close(job)
		// A process already inside a restrictive host or CI Job Object may
		// reject nested assignment. Windows provides no safe way for tested to
		// relax that host policy, so retain the bounded taskkill fallback.
		if (errors.Is(err, syscall.ERROR_ACCESS_DENIED) ||
			errors.Is(err, windowsErrorInvalidParameter)) &&
			closeErr == nil {
			return processTreeOwner{}, nil
		}
		return processTreeOwner{}, errors.Join(
			fmt.Errorf("assign process %d to Windows job: %w", pid, err),
			wrapWindowsCloseError(closeErr),
		)
	}
	return processTreeOwner{
		job:               job,
		terminateAndClose: operations.terminateAndClose,
	}, nil
}

func cleanupOwnedProcessTree(
	owner *processTreeOwner,
	cmd *exec.Cmd,
	timeout time.Duration,
	leaderReaped bool,
) error {
	if owner != nil && owner.job != 0 {
		job := owner.job
		release := owner.terminateAndClose
		owner.job = 0
		owner.terminateAndClose = nil
		releaseErr := release(job, timeout)
		if releaseErr == nil {
			return nil
		}
		return errors.Join(
			releaseErr,
			fallbackWindowsTreeCleanup(cmd, timeout, leaderReaped),
		)
	}
	return fallbackWindowsTreeCleanup(cmd, timeout, leaderReaped)
}

func fallbackWindowsTreeCleanup(
	cmd *exec.Cmd,
	timeout time.Duration,
	leaderReaped bool,
) error {
	if cmd.Process == nil {
		return nil
	}
	if !leaderReaped {
		return killProcessTree(cmd, timeout)
	}
	// A reaped PID is not a stable tree identity. taskkill is retained as a
	// bounded compatibility attempt, but its "not found" result cannot
	// distinguish an empty tree from a child reparented by a restrictive job.
	_ = taskkillProcessTree(cmd.Process.Pid, timeout)
	return nil
}

func createKillOnCloseJob() (syscall.Handle, error) {
	result, _, callErr := createJobObjectProc.Call(0, 0)
	if result == 0 {
		return 0, windowsCallError("CreateJobObjectW", callErr)
	}
	job := syscall.Handle(result)
	information := jobObjectExtendedLimitInformation{}
	information.BasicLimitInformation.LimitFlags =
		jobObjectLimitKillOnClose
	result, _, callErr = setInformationJobObjectProc.Call(
		uintptr(job),
		jobObjectExtendedLimitInformationClass,
		uintptr(unsafe.Pointer(&information)),
		unsafe.Sizeof(information),
	)
	runtime.KeepAlive(&information)
	if result == 0 {
		setErr := windowsCallError("SetInformationJobObject", callErr)
		return 0, errors.Join(setErr, wrapWindowsCloseError(syscall.CloseHandle(job)))
	}
	return job, nil
}

func assignProcessToJob(job syscall.Handle, pid int) (resultErr error) {
	process, err := syscall.OpenProcess(
		processSetQuota|syscall.PROCESS_TERMINATE,
		false,
		uint32(pid),
	)
	if err != nil {
		return fmt.Errorf("open process %d for job assignment: %w", pid, err)
	}
	defer func() {
		if err := syscall.CloseHandle(process); err != nil {
			resultErr = errors.Join(
				resultErr,
				fmt.Errorf("close process %d handle: %w", pid, err),
			)
		}
	}()
	result, _, callErr := assignProcessToJobObjectProc.Call(
		uintptr(job),
		uintptr(process),
	)
	if result == 0 {
		return windowsCallError("AssignProcessToJobObject", callErr)
	}
	return nil
}

func terminateAndCloseJob(job syscall.Handle, timeout time.Duration) error {
	result, _, callErr := terminateJobObjectProc.Call(uintptr(job), 1)
	var terminationErr error
	if result == 0 {
		terminationErr = windowsCallError("TerminateJobObject", callErr)
	} else {
		event, waitErr := syscall.WaitForSingleObject(
			job,
			windowsWaitMilliseconds(timeout),
		)
		switch {
		case waitErr != nil:
			terminationErr = fmt.Errorf("wait for Windows job: %w", waitErr)
		case event == syscall.WAIT_TIMEOUT:
			terminationErr = fmt.Errorf(
				"wait for Windows job: %w",
				ErrTerminationTimeout,
			)
		case event != syscall.WAIT_OBJECT_0:
			terminationErr = fmt.Errorf(
				"wait for Windows job: unexpected status %d",
				event,
			)
		}
	}
	return errors.Join(
		terminationErr,
		wrapWindowsCloseError(syscall.CloseHandle(job)),
	)
}

func windowsWaitMilliseconds(timeout time.Duration) uint32 {
	if timeout <= 0 {
		timeout = defaultInterruptGrace
	}
	milliseconds := uint64(timeout / time.Millisecond)
	if timeout%time.Millisecond != 0 {
		milliseconds++
	}
	if milliseconds == 0 {
		milliseconds = 1
	}
	const maximumFiniteWait = uint64(^uint32(0) - 1)
	if milliseconds > maximumFiniteWait {
		milliseconds = maximumFiniteWait
	}
	return uint32(milliseconds)
}

func windowsCallError(operation string, callErr error) error {
	if callErr == nil || errors.Is(callErr, syscall.Errno(0)) {
		callErr = syscall.EINVAL
	}
	return fmt.Errorf("%s: %w", operation, callErr)
}

func wrapWindowsCloseError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("close Windows job: %w", err)
}
