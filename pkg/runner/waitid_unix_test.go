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
	"os"
	"os/exec"
	"syscall"
	"testing"
	"unsafe"
)

func TestWaitCommandRunsCleanupBeforeReap(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(
		os.Environ(),
		"TESTED_RUNNER_HELPER=status",
		"TESTED_RUNNER_HELPER_EXIT=0",
	)
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start wait helper: %v", err)
	}

	state := newCommandWaitState()
	cleanupCalled := false
	waitErr, cleanupErr := waitCommandWithObserver(
		cmd,
		state,
		observeProcessExit,
		func() error {
			cleanupCalled = true
			if cmd.ProcessState != nil {
				return errors.New("process was reaped before process-group cleanup")
			}
			return nil
		},
	)
	if waitErr != nil || cleanupErr != nil {
		t.Fatalf("wait result = (%v, %v)", waitErr, cleanupErr)
	}
	if !cleanupCalled {
		t.Fatal("pre-reap cleanup was not called")
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Success() {
		t.Fatalf("process state after wait = %#v", cmd.ProcessState)
	}
	select {
	case <-state.startedReaping():
	default:
		t.Fatal("wait state did not publish the transition to reaping")
	}
	attempted, signalErr := state.signal(func() error {
		return errors.New("post-reap signal was attempted")
	})
	if attempted || signalErr != nil {
		t.Fatalf("post-reap signal = (%t, %v), want skipped", attempted, signalErr)
	}
}

func TestWaitCommandObserverFailurePreservesAuthoritativeWait(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(
		os.Environ(),
		"TESTED_RUNNER_HELPER=status",
		"TESTED_RUNNER_HELPER_EXIT=7",
	)
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start wait helper: %v", err)
	}

	observeErr := errors.New("non-reaping observation failed")
	cleanupCalled := false
	state := newCommandWaitState()
	waitErr, cleanupErr := waitCommandWithObserver(
		cmd,
		state,
		func(int) error {
			return observeErr
		},
		func() error {
			cleanupCalled = true
			return nil
		},
	)
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("wait error = %v, want authoritative exit 7", waitErr)
	}
	if !errors.Is(cleanupErr, observeErr) {
		t.Fatalf("cleanup error = %v, want observer error", cleanupErr)
	}
	if cleanupCalled {
		t.Fatal("observer failure ran numeric process-group cleanup")
	}
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 7 {
		t.Fatalf("process state after observer failure = %#v", cmd.ProcessState)
	}
	attempted, signalErr := state.signal(func() error {
		return errors.New("post-reap signal was attempted")
	})
	if attempted || signalErr != nil {
		t.Fatalf("post-reap signal = (%t, %v), want skipped", attempted, signalErr)
	}
}

func TestWaitCommandPreReapFailurePreservesAuthoritativeWait(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(
		os.Environ(),
		"TESTED_RUNNER_HELPER=status",
		"TESTED_RUNNER_HELPER_EXIT=7",
	)
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start wait helper: %v", err)
	}

	preReapErr := errors.New("process-group cleanup failed")
	state := newCommandWaitState()
	waitErr, cleanupErr := waitCommandWithObserver(
		cmd,
		state,
		observeProcessExit,
		func() error {
			if cmd.ProcessState != nil {
				return errors.New("process was reaped before failing cleanup")
			}
			return preReapErr
		},
	)
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("wait error = %v, want authoritative exit 7", waitErr)
	}
	if !errors.Is(cleanupErr, preReapErr) {
		t.Fatalf("cleanup error = %v, want pre-reap error", cleanupErr)
	}
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 7 {
		t.Fatalf("process state after cleanup failure = %#v", cmd.ProcessState)
	}
}

func TestObserveProcessExitRetriesInterruptedWaitID(t *testing.T) {
	calls := 0
	err := observeProcessExitWith(123, func(pid int, info *waitIDInfo) error {
		calls++
		if pid != 123 || info == nil {
			return errors.New("unexpected waitid arguments")
		}
		if calls < 3 {
			return syscall.EINTR
		}
		return nil
	})
	if err != nil {
		t.Fatalf("observeProcessExitWith() error = %v", err)
	}
	if calls != 3 {
		t.Fatalf("waitid call count = %d, want 3", calls)
	}
}

func TestWaitIDInfoStorageCoversSiginfoABI(t *testing.T) {
	if size := unsafe.Sizeof(waitIDInfo{}); size < 128 {
		t.Fatalf("waitIDInfo size = %d, want at least 128", size)
	}
	if alignment := unsafe.Alignof(waitIDInfo{}); alignment < 4 {
		t.Fatalf("waitIDInfo alignment = %d, want at least 4", alignment)
	}
}
