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
	"testing"
	"time"
)

func TestWaitWindowsCommandDisablesSignalingBeforeReap(t *testing.T) {
	const pid = 7101
	state := newCommandWaitState()
	waitErr := errors.New("authoritative wait result")
	reaped := false
	preReapCleaned := false

	gotWaitErr, cleanupErr := waitWindowsCommandWithObserver(
		pid,
		state,
		func(gotPID int) error {
			if gotPID != pid {
				t.Fatalf("observer PID = %d, want %d", gotPID, pid)
			}
			return nil
		},
		func() error {
			t.Fatal("termination ran after successful observation")
			return nil
		},
		func() error {
			preReapCleaned = true
			return nil
		},
		func() error {
			if !preReapCleaned {
				t.Fatal("reap started before process-tree cleanup")
			}
			reaped = true
			attempted, err := state.signal(func() error {
				return errors.New("numeric signal ran during reap")
			})
			if attempted || err != nil {
				t.Fatalf(
					"signal during reap = (%t, %v), want skipped",
					attempted,
					err,
				)
			}
			return waitErr
		},
		time.Second,
	)
	if !reaped || !errors.Is(gotWaitErr, waitErr) || cleanupErr != nil {
		t.Fatalf(
			"wait result = (reaped %t, wait %v, cleanup %v)",
			reaped,
			gotWaitErr,
			cleanupErr,
		)
	}
}

func TestWaitWindowsCommandObserverFailureStillDisablesSignaling(t *testing.T) {
	const pid = 7102
	state := newCommandWaitState()
	observeErr := errors.New("observation failed")
	terminationErr := errors.New("forced tree termination failed")
	terminated := false

	waitErr, cleanupErr := waitWindowsCommandWithObserver(
		pid,
		state,
		func(int) error {
			return observeErr
		},
		func() error {
			terminated = true
			return terminationErr
		},
		func() error {
			t.Fatal("pre-reap cleanup ran after observer failure")
			return nil
		},
		func() error {
			attempted, err := state.signal(func() error {
				return errors.New("numeric signal ran after observer failure")
			})
			if attempted || err != nil {
				t.Fatalf(
					"signal after observer failure = (%t, %v), want skipped",
					attempted,
					err,
				)
			}
			return nil
		},
		time.Second,
	)
	if waitErr != nil ||
		!terminated ||
		!errors.Is(cleanupErr, observeErr) ||
		!errors.Is(cleanupErr, terminationErr) {
		t.Fatalf(
			"observer failure result = (terminated %t, wait %v, cleanup %v)",
			terminated,
			waitErr,
			cleanupErr,
		)
	}
}

func TestWaitWindowsCommandBoundsReapAfterObservationFailure(t *testing.T) {
	const pid = 7103
	state := newCommandWaitState()
	observeErr := errors.New("observation failed")
	terminationErr := errors.New("termination failed")
	releaseReap := make(chan struct{})
	defer close(releaseReap)

	startedAt := time.Now()
	waitErr, cleanupErr := waitWindowsCommandWithObserver(
		pid,
		state,
		func(int) error {
			return observeErr
		},
		func() error {
			return terminationErr
		},
		nil,
		func() error {
			<-releaseReap
			return nil
		},
		10*time.Millisecond,
	)
	if !errors.Is(waitErr, ErrTerminationTimeout) ||
		!errors.Is(cleanupErr, observeErr) ||
		!errors.Is(cleanupErr, terminationErr) {
		t.Fatalf("bounded observer failure = (%v, %v)", waitErr, cleanupErr)
	}
	if elapsed := time.Since(startedAt); elapsed < 10*time.Millisecond ||
		elapsed >= time.Second {
		t.Fatalf("bounded observer failure duration = %s", elapsed)
	}
}
