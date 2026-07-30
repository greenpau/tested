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
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	runnerTestReadinessTimeout   = 10 * time.Second
	runnerTestCompletionTimeout  = 10 * time.Second
	runnerTestCancellationBudget = 5 * time.Second
)

func TestRunnerTerminatesChattyChildAfterCaptureFailure(t *testing.T) {
	sinkErr := errors.New("event evidence is unavailable")
	for _, test := range []struct {
		name   string
		mode   string
		stdout io.Writer
		stderr io.Writer
	}{
		{
			name:   "standard output",
			mode:   "chatty",
			stdout: fixedErrorWriter{err: sinkErr},
			stderr: io.Discard,
		},
		{
			name:   "standard error",
			mode:   "chatty-stderr",
			stdout: io.Discard,
			stderr: fixedErrorWriter{err: sinkErr},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			result, err := New().Run(ctx, Options{
				GoCommand:      os.Args[0],
				WorkDir:        ".",
				StandardOutput: test.stdout,
				StandardError:  test.stderr,
				Environment: append(
					os.Environ(),
					"TESTED_RUNNER_HELPER="+test.mode,
				),
				InterruptGrace: 100 * time.Millisecond,
			})
			if !errors.Is(err, sinkErr) {
				t.Fatalf("Run() error = %v, want capture failure", err)
			}
			if ctx.Err() != nil {
				t.Fatalf("Run() exceeded its bounded termination window: %v", ctx.Err())
			}
			if result.Interrupted {
				t.Fatalf(
					"Run() marked infrastructure termination as user interruption: %#v",
					result,
				)
			}
			if result.ExitCode != -1 {
				t.Fatalf("Run() exit = %d, want non-authoritative -1", result.ExitCode)
			}
			if result.Duration <= 0 || result.Duration >= 2*time.Second {
				t.Fatalf("Run() duration = %s, want bounded termination", result.Duration)
			}
		})
	}
}

func TestRunnerCancelsActiveProcessWithinBound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := New().Run(ctx, Options{
		GoCommand:      os.Args[0],
		WorkDir:        ".",
		StandardOutput: io.Discard,
		StandardError:  io.Discard,
		Environment: append(
			os.Environ(),
			"TESTED_RUNNER_HELPER=chatty",
		),
		InterruptGrace: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run() cancellation error = %v", err)
	}
	if !result.Interrupted ||
		result.Cancellation != CancellationDeadline ||
		result.CancellationSignal != "" ||
		result.RecommendedExitCode != 130 ||
		result.ExitCode != 128+int(syscall.SIGINT) ||
		result.Signal != syscall.SIGINT.String() {
		t.Fatalf("Run() result = %#v, want deadline cancellation", result)
	}
	if result.Duration <= 0 || result.Duration >= 2*time.Second {
		t.Fatalf("Run() duration = %s, want bounded cancellation", result.Duration)
	}
}

func TestRunnerPropagatesOperatorSignal(t *testing.T) {
	for _, test := range []struct {
		name string
		send syscall.Signal
		exit int
	}{
		{name: "interrupt", send: syscall.SIGINT, exit: 130},
		{name: "terminate", send: syscall.SIGTERM, exit: 143},
	} {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			readyFile := filepath.Join(tempDir, "ready")
			signalFile := filepath.Join(tempDir, "signal")
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(context.Canceled)

			type runOutcome struct {
				result Result
				err    error
			}
			outcomeCh := make(chan runOutcome, 1)
			go func() {
				result, err := New().Run(ctx, Options{
					GoCommand:      os.Args[0],
					WorkDir:        ".",
					StandardOutput: io.Discard,
					StandardError:  io.Discard,
					Environment: append(
						os.Environ(),
						"TESTED_RUNNER_HELPER=await-signal",
						"TESTED_RUNNER_READY_FILE="+readyFile,
						"TESTED_RUNNER_SIGNAL_FILE="+signalFile,
					),
					InterruptGrace: 2 * time.Second,
				})
				outcomeCh <- runOutcome{result: result, err: err}
			}()

			waitForReadinessMarker(
				t,
				readyFile,
				runnerTestReadinessTimeout,
			)
			cancelledAt := time.Now()
			cancel(NewSignalCause(test.send))

			var outcome runOutcome
			select {
			case outcome = <-outcomeCh:
			case <-time.After(runnerTestCompletionTimeout):
				t.Fatal("Run() did not complete after operator signal")
			}
			if elapsed := time.Since(cancelledAt); elapsed >=
				runnerTestCancellationBudget {
				t.Fatalf(
					"Run() cancellation took %s, want less than %s",
					elapsed,
					runnerTestCancellationBudget,
				)
			}
			if outcome.err != nil {
				t.Fatalf("Run() signal cancellation error = %v", outcome.err)
			}
			if !outcome.result.Interrupted ||
				outcome.result.Cancellation != CancellationSignal ||
				outcome.result.CancellationSignal != test.send.String() ||
				outcome.result.RecommendedExitCode != test.exit {
				t.Fatalf(
					"Run() result = %#v, want signal cancellation exit %d",
					outcome.result,
					test.exit,
				)
			}
			if outcome.result.ExitCode != 0 || outcome.result.Signal != "" {
				t.Fatalf(
					"Run() child status = (%d, %q), want handled signal and exit 0",
					outcome.result.ExitCode,
					outcome.result.Signal,
				)
			}

			data, err := os.ReadFile(signalFile)
			if err != nil {
				t.Fatalf("read received signal: %v", err)
			}
			if got := strings.TrimSpace(string(data)); got != test.send.String() {
				t.Fatalf("child received signal %q, want %q", got, test.send)
			}
		})
	}
}

func TestRunnerBoundsIgnoredOperatorSignal(t *testing.T) {
	readyFile := filepath.Join(t.TempDir(), "ready")
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(context.Canceled)

	type runOutcome struct {
		result Result
		err    error
	}
	outcomeCh := make(chan runOutcome, 1)
	go func() {
		result, err := New().Run(ctx, Options{
			GoCommand:      os.Args[0],
			WorkDir:        ".",
			StandardOutput: io.Discard,
			StandardError:  io.Discard,
			Environment: append(
				os.Environ(),
				"TESTED_RUNNER_HELPER=ignore-signals",
				"TESTED_RUNNER_READY_FILE="+readyFile,
			),
			InterruptGrace: 100 * time.Millisecond,
		})
		outcomeCh <- runOutcome{result: result, err: err}
	}()

	waitForReadinessMarker(t, readyFile, runnerTestReadinessTimeout)
	cancelledAt := time.Now()
	cancel(NewSignalCause(syscall.SIGTERM))

	var outcome runOutcome
	select {
	case outcome = <-outcomeCh:
	case <-time.After(runnerTestCompletionTimeout):
		t.Fatal("Run() did not force a child that ignored the operator signal")
	}
	if elapsed := time.Since(cancelledAt); elapsed >=
		runnerTestCancellationBudget {
		t.Fatalf(
			"Run() cancellation took %s, want less than %s",
			elapsed,
			runnerTestCancellationBudget,
		)
	}
	if outcome.err != nil {
		t.Fatalf("Run() ignored-signal cancellation error = %v", outcome.err)
	}
	if !outcome.result.Interrupted ||
		outcome.result.Cancellation != CancellationSignal ||
		outcome.result.CancellationSignal != syscall.SIGTERM.String() ||
		outcome.result.RecommendedExitCode != 143 {
		t.Fatalf("Run() result = %#v, want bounded SIGTERM cancellation", outcome.result)
	}
	if outcome.result.ExitCode != 128+int(syscall.SIGKILL) ||
		outcome.result.Signal != syscall.SIGKILL.String() {
		t.Fatalf(
			"Run() forced child status = (%d, %q), want SIGKILL",
			outcome.result.ExitCode,
			outcome.result.Signal,
		)
	}
	if outcome.result.Duration <= 0 {
		t.Fatalf("Run() duration = %s, want a positive duration", outcome.result.Duration)
	}
}

func TestRunnerCleansRetainedPipeProcessGroupBeforeReap(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "retained-child.pid")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := New().Run(ctx, Options{
		GoCommand:      os.Args[0],
		WorkDir:        ".",
		StandardOutput: io.Discard,
		StandardError:  io.Discard,
		Environment: append(
			os.Environ(),
			"TESTED_RUNNER_HELPER=retained-parent",
			"TESTED_RUNNER_PID_FILE="+pidFile,
		),
		InterruptGrace: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run() process-group cleanup error = %v", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("Run() exceeded its pipe wait bound: %v", ctx.Err())
	}
	if result.ExitCode != 0 || result.Interrupted {
		t.Fatalf("Run() result = %#v, want naturally exited group leader", result)
	}

	data, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("read retained child PID: %v", readErr)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if parseErr != nil || pid <= 0 {
		t.Fatalf("parse retained child PID %q: %v", data, parseErr)
	}
	cleanupPID := pid
	defer func() {
		if cleanupPID > 0 {
			_ = syscall.Kill(cleanupPID, syscall.SIGKILL)
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		probeErr := syscall.Kill(pid, 0)
		if errors.Is(probeErr, syscall.ESRCH) {
			cleanupPID = 0
			break
		}
		if probeErr != nil {
			t.Fatalf("probe retained child %d: %v", pid, probeErr)
		}
		if time.Now().After(deadline) {
			t.Fatalf("retained child %d survived process-group cleanup", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRunnerWaitDelayStillBoundsEscapedRetainedPipe(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "escaped-child.pid")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := New().Run(ctx, Options{
		GoCommand:      os.Args[0],
		WorkDir:        ".",
		StandardOutput: io.Discard,
		StandardError:  io.Discard,
		Environment: append(
			os.Environ(),
			"TESTED_RUNNER_HELPER=escaped-pipe-parent",
			"TESTED_RUNNER_PID_FILE="+pidFile,
		),
		InterruptGrace: 100 * time.Millisecond,
	})
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("Run() error = %v, want exec.ErrWaitDelay", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("Run() exceeded its pipe wait bound: %v", ctx.Err())
	}
	if result.ExitCode != 0 || result.Interrupted {
		t.Fatalf("Run() result = %#v, want naturally exited group leader", result)
	}

	data, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("read escaped child PID: %v", readErr)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if parseErr != nil || pid <= 0 {
		t.Fatalf("parse escaped child PID %q: %v", data, parseErr)
	}
	if killErr := syscall.Kill(pid, syscall.SIGKILL); killErr != nil &&
		!errors.Is(killErr, syscall.ESRCH) {
		t.Fatalf("clean escaped child %d: %v", pid, killErr)
	}
}

func TestRunnerCleansDescendantThatClosedInheritedPipes(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "detached-pipe-child.pid")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := New().Run(ctx, Options{
		GoCommand:      os.Args[0],
		WorkDir:        ".",
		StandardOutput: io.Discard,
		StandardError:  io.Discard,
		Environment: append(
			os.Environ(),
			"TESTED_RUNNER_HELPER=detached-pipe-parent",
			"TESTED_RUNNER_PID_FILE="+pidFile,
		),
		InterruptGrace: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run() descendant cleanup error = %v", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("Run() exceeded its cleanup bound: %v", ctx.Err())
	}
	if result.ExitCode != 0 || result.Interrupted {
		t.Fatalf("Run() result = %#v, want naturally exited group leader", result)
	}

	data, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("read detached-pipe child PID: %v", readErr)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if parseErr != nil || pid <= 0 {
		t.Fatalf("parse detached-pipe child PID %q: %v", data, parseErr)
	}
	cleanupPID := pid
	defer func() {
		if cleanupPID > 0 {
			_ = syscall.Kill(cleanupPID, syscall.SIGKILL)
		}
	}()
	waitForProcessExit(t, pid, 2*time.Second)
	cleanupPID = 0
}

// waitForReadinessMarker observes presence only. The helpers install their
// signal handler before creating the marker, and no marker content is part of
// this synchronization contract.
func waitForReadinessMarker(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat %s: %v", path, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForProcessExit(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		probeErr := syscall.Kill(pid, 0)
		if errors.Is(probeErr, syscall.ESRCH) {
			return
		}
		if probeErr != nil {
			t.Fatalf("probe process %d: %v", pid, probeErr)
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d survived process-group cleanup", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
