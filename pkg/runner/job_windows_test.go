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
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestAcquireWindowsProcessTree(t *testing.T) {
	const (
		job syscall.Handle = 42
		pid                = 7001
	)
	assignedPID := 0
	released := false
	operations := windowsJobOperations{
		create: func() (syscall.Handle, error) {
			return job, nil
		},
		assign: func(gotJob syscall.Handle, gotPID int) error {
			if gotJob != job {
				t.Fatalf("assign job = %d, want %d", gotJob, job)
			}
			assignedPID = gotPID
			return nil
		},
		close: syscall.CloseHandle,
		terminateAndClose: func(gotJob syscall.Handle, timeout time.Duration) error {
			if gotJob != job || timeout != 25*time.Millisecond {
				t.Fatalf("release job = (%d, %s)", gotJob, timeout)
			}
			released = true
			return nil
		},
	}

	owner, err := acquireWindowsProcessTree(pid, operations)
	if err != nil {
		t.Fatalf("acquireWindowsProcessTree() error = %v", err)
	}
	if owner.job != job || assignedPID != pid {
		t.Fatalf("owner = %#v, assigned PID = %d", owner, assignedPID)
	}
	if err := cleanupOwnedProcessTree(
		&owner,
		&exec.Cmd{},
		25*time.Millisecond,
		true,
	); err != nil {
		t.Fatalf("cleanupOwnedProcessTree() error = %v", err)
	}
	if !released || owner.job != 0 {
		t.Fatalf("release state = (%t, %#v)", released, owner)
	}
}

func TestAcquireWindowsProcessTreePreservesRestrictedJobFailure(t *testing.T) {
	closed := false
	owner, err := acquireWindowsProcessTree(7002, windowsJobOperations{
		create: func() (syscall.Handle, error) {
			return 43, nil
		},
		assign: func(syscall.Handle, int) error {
			return syscall.ERROR_ACCESS_DENIED
		},
		close: func(syscall.Handle) error {
			closed = true
			return nil
		},
		terminateAndClose: func(syscall.Handle, time.Duration) error {
			t.Fatal("terminateAndClose unexpectedly called")
			return nil
		},
	})
	if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) ||
		owner.job != 0 ||
		!closed {
		t.Fatalf(
			"restricted job = (owner %#v, closed %t, error %v)",
			owner,
			closed,
			err,
		)
	}
}

func TestAcquireWindowsProcessTreePreservesAssignmentFailure(t *testing.T) {
	assignErr := errors.New("assignment failed")
	closeErr := errors.New("close failed")
	_, err := acquireWindowsProcessTree(7003, windowsJobOperations{
		create: func() (syscall.Handle, error) {
			return 44, nil
		},
		assign: func(syscall.Handle, int) error {
			return assignErr
		},
		close: func(syscall.Handle) error {
			return closeErr
		},
		terminateAndClose: func(syscall.Handle, time.Duration) error {
			return nil
		},
	})
	if !errors.Is(err, assignErr) || !errors.Is(err, closeErr) {
		t.Fatalf("assignment error = %v, want joined failures", err)
	}
}

func TestWindowsJobInformationLayout(t *testing.T) {
	got := unsafe.Sizeof(jobObjectExtendedLimitInformation{})
	want := uintptr(144)
	if strconv.IntSize == 32 {
		want = 112
	}
	if got != want {
		t.Fatalf("job information size = %d, want %d", got, want)
	}
}

func TestWindowsWaitMilliseconds(t *testing.T) {
	if got := windowsWaitMilliseconds(time.Nanosecond); got != 1 {
		t.Fatalf("nanosecond wait = %d, want 1", got)
	}
	if got := windowsWaitMilliseconds(1500 * time.Microsecond); got != 2 {
		t.Fatalf("rounded wait = %d, want 2", got)
	}
}
