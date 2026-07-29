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

package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const defaultInterruptGrace = 2 * time.Second

// ErrTerminationTimeout means the owned child could not be reaped within the
// bounded wait after forced process-tree termination.
var ErrTerminationTimeout = errors.New(
	"timed out waiting for owned child after forced termination",
)

// Options configures one owned go test process.
type Options struct {
	GoCommand       string
	WorkDir         string
	TestArguments   []string
	CoverageProfile string
	StandardOutput  io.Writer
	StandardError   io.Writer
	Environment     []string
	// InterruptGrace also bounds pipe draining after the group leader exits.
	InterruptGrace time.Duration
}

// Result describes the authoritative child-process outcome.
type Result struct {
	Command    []string
	StartedAt  time.Time
	FinishedAt time.Time
	Duration   time.Duration
	// ExitCode and Signal describe the observed child status. ExitCode is -1
	// when no authoritative child status is available.
	ExitCode int
	Signal   string
	// Interrupted is retained for compatibility and is true whenever
	// Cancellation is nonempty.
	Interrupted        bool
	Cancellation       CancellationKind
	CancellationSignal string
	// RecommendedExitCode is the conventional shell projection for
	// cancellation. It is zero when the runner did not cancel the child.
	RecommendedExitCode int
}

// Runner owns the lifetime of one go test child process at a time.
type Runner struct{}

// New creates a test-process runner.
func New() *Runner {
	return &Runner{}
}

// Arguments returns the exact Go arguments for options. tested-managed flags
// precede user arguments so they cannot be reinterpreted after -args.
func Arguments(options Options) ([]string, error) {
	if strings.TrimSpace(options.GoCommand) == "" {
		return nil, fmt.Errorf("go command must not be empty")
	}
	if strings.TrimSpace(options.WorkDir) == "" {
		return nil, fmt.Errorf("working directory must not be empty")
	}
	args := []string{"test", "-json"}
	if options.CoverageProfile != "" {
		args = append(args, "-coverprofile="+options.CoverageProfile)
	}
	args = append(args, options.TestArguments...)
	if len(options.TestArguments) == 0 {
		args = append(args, "./...")
	}
	return args, nil
}

// Run executes go test without a shell. A normal nonzero test/build outcome is
// returned in Result rather than as an infrastructure error.
func (r *Runner) Run(ctx context.Context, options Options) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("context must not be nil")
	}
	args, err := Arguments(options)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Command:  append([]string{options.GoCommand}, args...),
		ExitCode: -1,
	}
	if err := ctx.Err(); err != nil {
		applyCancellation(&result, cancellationFromContext(ctx))
		return result, fmt.Errorf("start go test: %w", context.Cause(ctx))
	}

	cmd := exec.Command(options.GoCommand, args...)
	cmd.Dir = options.WorkDir
	grace := interruptGrace(options.InterruptGrace)
	captureFailureCh := make(chan struct{}, 1)
	notifyCaptureFailure := func() {
		select {
		case captureFailureCh <- struct{}{}:
		default:
		}
	}
	stdout := newTrackingWriter(options.StandardOutput, notifyCaptureFailure)
	stderr := newTrackingWriter(options.StandardError, notifyCaptureFailure)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// WaitDelay bounds the otherwise unlimited wait for descendants that
	// inherited a child pipe and kept it open after the Go command exited.
	cmd.WaitDelay = grace
	if options.Environment != nil {
		cmd.Env = append([]string(nil), options.Environment...)
	}
	configureProcess(cmd)

	startAttemptedAt := time.Now()
	if err := cmd.Start(); err != nil {
		result.FinishedAt = time.Now()
		result.Duration = result.FinishedAt.Sub(startAttemptedAt)
		return result, fmt.Errorf("start go test: %w", err)
	}
	result.StartedAt = startAttemptedAt
	processTree, processTreeErr := acquireProcessTree(cmd)

	waitState := newCommandWaitState()
	waitCh := make(chan commandWaitResult, 1)
	go func() {
		waitErr, preReapErr := waitCommand(cmd, waitState, grace)
		waitCh <- commandWaitResult{
			waitErr:    waitErr,
			cleanupErr: preReapErr,
		}
	}()

	var waitResult commandWaitResult
	var terminationCleanupErr error
	waitCompleted := false
	terminatedForCaptureFailure := false
	select {
	case waitResult = <-waitCh:
	case <-waitState.startedReaping():
		waitResult = <-waitCh
	case <-ctx.Done():
		// Prefer an already available natural exit over cancellation. This
		// narrows the ownership window before any process-group signal and
		// avoids converting a completed test failure into an interrupt.
		select {
		case waitResult = <-waitCh:
			waitCompleted = true
		case <-waitState.startedReaping():
			waitResult = <-waitCh
			waitCompleted = true
		default:
		}
		if !waitCompleted {
			cancellation := cancellationFromContext(ctx)
			var terminationInitiated bool
			waitResult, terminationCleanupErr, terminationInitiated = terminateAndWait(
				cmd,
				waitState,
				waitCh,
				grace,
				cancellation.signal,
			)
			if terminationInitiated {
				applyCancellation(&result, cancellation)
			}
		}
	case <-captureFailureCh:
		// The writer reports failure before its Write call returns. Give a child
		// that is already exiting a short opportunity to publish its natural
		// status before taking ownership of termination.
		settle := captureFailureSettle(grace)
		timer := time.NewTimer(settle)
		select {
		case waitResult = <-waitCh:
			stopAndDrainTimer(timer)
		case <-waitState.startedReaping():
			waitResult = <-waitCh
			stopAndDrainTimer(timer)
		case <-ctx.Done():
			cancellation := cancellationFromContext(ctx)
			var terminationInitiated bool
			waitResult, terminationCleanupErr, terminationInitiated = terminateAndWait(
				cmd,
				waitState,
				waitCh,
				grace,
				cancellation.signal,
			)
			if terminationInitiated {
				applyCancellation(&result, cancellation)
			}
		case <-timer.C:
			waitResult, terminationCleanupErr, terminatedForCaptureFailure =
				terminateAndWait(
					cmd,
					waitState,
					waitCh,
					grace,
					os.Interrupt,
				)
		}
	}

	waitErr := waitResult.waitErr
	cleanupErr := errors.Join(
		processTreeErr,
		waitResult.cleanupErr,
		terminationCleanupErr,
	)
	unreaped := errors.Is(waitErr, ErrTerminationTimeout)
	if unreaped {
		// Cmd.Wait may still own its copy goroutines. Detach them from the
		// evidence sinks before returning so a late completion cannot mutate
		// artifacts while callers publish or hash them.
		stdout.Discard()
		stderr.Discard()
	}
	// Complete platform ownership before callers publish artifacts. Linux and
	// Darwin have already cleaned their group before reaping; Windows retains a
	// stable Job Object for this post-wait step. Unix cleanup must never signal a
	// numeric process-group identifier after the leader was reaped.
	cleanupErr = errors.Join(
		cleanupErr,
		cleanupOwnedProcessTree(&processTree, cmd, grace, !unreaped),
	)

	result.FinishedAt = time.Now()
	result.Duration = result.FinishedAt.Sub(result.StartedAt)
	copyErr := errors.Join(stdout.Err(), stderr.Err())
	if !unreaped {
		result.ExitCode, result.Signal = processStatus(cmd.ProcessState)
	}
	if result.Interrupted {
		if err := errors.Join(
			cleanupErr,
			copyErr,
			nonExitWaitError(waitErr),
		); err != nil {
			return result, fmt.Errorf("terminate cancelled go test: %w", err)
		}
		return result, nil
	}

	if terminatedForCaptureFailure {
		// A status caused by tested terminating the child after evidence capture
		// failed is not an authoritative go test status.
		result.ExitCode = -1
		return result, fmt.Errorf(
			"terminate go test after output capture failure: %w",
			errors.Join(copyErr, cleanupErr, nonExitWaitError(waitErr)),
		)
	}
	if waitErr == nil {
		if result.ExitCode < 0 {
			result.ExitCode = 0
		}
		if copyErr != nil {
			return result, fmt.Errorf("copy go test output: %w", copyErr)
		}
		if cleanupErr != nil {
			return result, fmt.Errorf("clean up go test process tree: %w", cleanupErr)
		}
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		if result.ExitCode < 0 {
			result.ExitCode, result.Signal = processStatus(exitErr.ProcessState)
		}
		if result.ExitCode < 0 {
			result.ExitCode = 1
		}
		if copyErr != nil {
			return result, fmt.Errorf("copy output from failed go test: %w", copyErr)
		}
		if cleanupErr != nil {
			return result, fmt.Errorf("clean up failed go test process tree: %w", cleanupErr)
		}
		return result, nil
	}
	if result.ExitCode < 0 {
		result.ExitCode = -1
	}
	return result, fmt.Errorf(
		"wait for go test: %w",
		errors.Join(waitErr, copyErr, cleanupErr),
	)
}

type trackingWriter struct {
	mu       sync.Mutex
	writer   io.Writer
	notify   func()
	err      error
	discard  bool
	notified bool
}

func newTrackingWriter(writer io.Writer, notify func()) *trackingWriter {
	if writer == nil {
		writer = io.Discard
	}
	return &trackingWriter{writer: writer, notify: notify}
}

func (w *trackingWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	if w.discard {
		w.mu.Unlock()
		return len(data), nil
	}
	n, err := w.writer.Write(data)
	if err == nil && (n < 0 || n > len(data)) {
		err = fmt.Errorf("invalid write count %d for %d bytes", n, len(data))
	} else if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = errors.Join(w.err, err)
		w.discard = true
	}
	notify := w.notify
	shouldNotify := err != nil && !w.notified
	if shouldNotify {
		w.notified = true
	}
	w.mu.Unlock()

	if shouldNotify && notify != nil {
		notify()
	}
	if err != nil {
		// The sink is no longer trustworthy, but the child pipe must continue
		// draining so the child cannot deadlock before the runner terminates it.
		return len(data), nil
	}
	return n, nil
}

func (w *trackingWriter) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func (w *trackingWriter) Discard() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.discard = true
}

func interruptGrace(configured time.Duration) time.Duration {
	if configured <= 0 {
		return defaultInterruptGrace
	}
	return configured
}

func captureFailureSettle(grace time.Duration) time.Duration {
	return grace
}

func terminateAndWait(
	cmd *exec.Cmd,
	waitState *commandWaitState,
	waitCh <-chan commandWaitResult,
	grace time.Duration,
	gracefulSignal os.Signal,
) (commandWaitResult, error, bool) {
	select {
	case waitResult := <-waitCh:
		return waitResult, nil, false
	case <-waitState.startedReaping():
		return <-waitCh, nil, false
	default:
	}

	var cleanupErr error
	terminationInitiated, interruptErr := waitState.signal(func() error {
		return interruptProcessTree(cmd, gracefulSignal)
	})
	if !terminationInitiated {
		return <-waitCh, nil, false
	}
	if interruptErr != nil {
		cleanupErr = interruptErr
		_, killErr := waitState.signal(func() error {
			return killProcessTree(cmd, grace)
		})
		if killErr != nil {
			cleanupErr = errors.Join(cleanupErr, killErr)
		}
	}

	timer := time.NewTimer(grace)
	select {
	case waitResult := <-waitCh:
		stopAndDrainTimer(timer)
		return waitResult, cleanupErr, terminationInitiated
	case <-waitState.startedReaping():
		stopAndDrainTimer(timer)
		return <-waitCh, cleanupErr, terminationInitiated
	case <-timer.C:
		_, killErr := waitState.signal(func() error {
			return killProcessTree(cmd, grace)
		})
		if killErr != nil {
			cleanupErr = errors.Join(cleanupErr, killErr)
		}
		waitResult, retryErr := waitAfterForcedTermination(
			waitCh,
			waitState,
			grace,
			func() error {
				return killProcessTree(cmd, grace)
			},
		)
		return waitResult, errors.Join(cleanupErr, retryErr), terminationInitiated
	}
}

func waitAfterForcedTermination(
	waitCh <-chan commandWaitResult,
	waitState *commandWaitState,
	grace time.Duration,
	retryKill func() error,
) (commandWaitResult, error) {
	timer := time.NewTimer(grace)
	defer stopAndDrainTimer(timer)
	select {
	case waitResult := <-waitCh:
		return waitResult, nil
	case <-waitState.startedReaping():
		return <-waitCh, nil
	case <-timer.C:
		var cleanupErr error
		if retryKill != nil {
			_, cleanupErr = waitState.signal(retryKill)
		}
		return commandWaitResult{waitErr: ErrTerminationTimeout}, cleanupErr
	}
}

func stopAndDrainTimer(timer *time.Timer) {
	if timer == nil || timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

func nonExitWaitError(waitErr error) error {
	if waitErr == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return nil
	}
	return waitErr
}
