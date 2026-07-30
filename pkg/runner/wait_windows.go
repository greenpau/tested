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
	"syscall"
	"time"
)

type windowsProcessExitObserver func(int) error
type windowsCommandReaper func() error

func waitCommand(
	cmd *exec.Cmd,
	state *commandWaitState,
	grace time.Duration,
	owner *processTreeOwner,
) (error, error) {
	if cmd == nil {
		return fmt.Errorf("wait for command: command is nil"), nil
	}
	if cmd.Process == nil {
		return fmt.Errorf("wait for command: process is nil"), nil
	}
	var preReapCleanup func() error
	if owner == nil || owner.job == 0 {
		preReapCleanup = func() error {
			return taskkillProcessTree(cmd.Process.Pid, grace)
		}
	}
	return waitWindowsCommandWithObserver(
		cmd.Process.Pid,
		state,
		observeWindowsProcessExit,
		func() error {
			return killProcessTree(cmd, grace)
		},
		preReapCleanup,
		cmd.Wait,
		grace,
	)
}

func waitWindowsCommandWithObserver(
	pid int,
	state *commandWaitState,
	observe windowsProcessExitObserver,
	terminate func() error,
	preReapCleanup func() error,
	reap windowsCommandReaper,
	reapTimeout time.Duration,
) (error, error) {
	if reap == nil {
		return nil, errors.New("reap Windows command: reaper is nil")
	}
	var observeErr error
	switch {
	case pid <= 0:
		observeErr = fmt.Errorf("invalid PID %d", pid)
	case observe == nil:
		observeErr = errors.New("process exit observer is nil")
	default:
		observeErr = observe(pid)
	}

	var terminationErr error
	if observeErr != nil {
		if terminate == nil {
			terminationErr = errors.New(
				"terminate after Windows process observation failure: " +
					"operation is nil",
			)
		} else {
			_, terminationErr = state.signal(terminate)
		}
	}

	// WaitForSingleObject observes termination without releasing Go's process
	// handle. Cmd.Process retains the original Windows process object until
	// Cmd.Wait, so its PID cannot be recycled while OpenProcess obtains this
	// observer. Disable numeric process-tree signaling before Cmd.Wait releases
	// that stable identity. If observation failed, terminate the owned tree
	// first so the fallback reap cannot block indefinitely with signaling off.
	if observeErr != nil {
		preReapCleanup = nil
	}
	transitionErr := state.beginReap(preReapCleanup)
	var waitErr error
	if observeErr == nil {
		waitErr = reap()
	} else {
		waitErr = boundedWindowsReap(reap, reapTimeout)
	}
	if observeErr != nil {
		observeErr = fmt.Errorf(
			"observe process %d before reap: %w",
			pid,
			observeErr,
		)
	}
	return waitErr, errors.Join(
		observeErr,
		terminationErr,
		transitionErr,
	)
}

func boundedWindowsReap(
	reap windowsCommandReaper,
	timeout time.Duration,
) error {
	if reap == nil {
		return errors.New("reap Windows command: reaper is nil")
	}
	timeout = interruptGrace(timeout)
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- reap()
	}()
	timer := time.NewTimer(timeout)
	defer stopAndDrainTimer(timer)
	select {
	case err := <-waitCh:
		return err
	case <-timer.C:
		return ErrTerminationTimeout
	}
}

func observeWindowsProcessExit(pid int) (resultErr error) {
	if pid <= 0 {
		return fmt.Errorf("observe Windows process: invalid PID %d", pid)
	}
	process, err := syscall.OpenProcess(
		syscall.SYNCHRONIZE,
		false,
		uint32(pid),
	)
	if err != nil {
		return fmt.Errorf("open process %d for exit observation: %w", pid, err)
	}
	defer func() {
		if err := syscall.CloseHandle(process); err != nil {
			resultErr = errors.Join(
				resultErr,
				fmt.Errorf("close process %d observation handle: %w", pid, err),
			)
		}
	}()

	event, err := syscall.WaitForSingleObject(process, syscall.INFINITE)
	switch {
	case err != nil:
		return fmt.Errorf("wait for process %d exit: %w", pid, err)
	case event != syscall.WAIT_OBJECT_0:
		return fmt.Errorf(
			"wait for process %d exit: unexpected status %d",
			pid,
			event,
		)
	default:
		return nil
	}
}
