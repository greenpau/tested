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
	"os/exec"
	"strings"
	"time"
)

// CommandOptions configures one auxiliary process owned by tested.
type CommandOptions struct {
	Executable     string
	Arguments      []string
	WorkDir        string
	StandardOutput io.Writer
	StandardError  io.Writer
	Environment    []string
	// InterruptGrace also bounds pipe draining after the group leader exits.
	InterruptGrace time.Duration
}

// RunCommand executes one argv-based auxiliary command while owning its
// complete process tree. Context cancellation requests a graceful interrupt,
// waits for InterruptGrace, force-terminates the tree, drains owned pipes, and
// reaps the group leader before returning when the operating system permits.
func RunCommand(ctx context.Context, options CommandOptions) error {
	if ctx == nil {
		return errors.New("run owned command: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("run owned command: %w", context.Cause(ctx))
	}
	if strings.TrimSpace(options.Executable) == "" {
		return errors.New("run owned command: executable is required")
	}
	if strings.TrimSpace(options.WorkDir) == "" {
		return errors.New("run owned command: working directory is required")
	}

	cmd := exec.Command(options.Executable, options.Arguments...)
	cmd.Dir = options.WorkDir
	stdout := newTrackingWriter(options.StandardOutput, nil)
	stderr := newTrackingWriter(options.StandardError, nil)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if options.Environment != nil {
		cmd.Env = append([]string(nil), options.Environment...)
	}
	grace := interruptGrace(options.InterruptGrace)
	// WaitDelay bounds the otherwise unlimited wait for descendants that
	// inherited a child pipe and kept it open after the group leader exited.
	cmd.WaitDelay = grace
	configureProcess(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf(
			"start owned command %q: %w",
			options.Executable,
			err,
		)
	}
	processTree, processTreeErr := acquireProcessTree(cmd)

	waitState := newCommandWaitState()
	waitCh := make(chan commandWaitResult, 1)
	go func() {
		waitErr, preReapErr := waitCommand(
			cmd,
			waitState,
			grace,
			&processTree,
		)
		waitCh <- commandWaitResult{
			waitErr:    waitErr,
			cleanupErr: preReapErr,
		}
	}()

	var waitResult commandWaitResult
	var terminationCleanupErr error
	cancelled := false
	select {
	case waitResult = <-waitCh:
	case <-waitState.startedReaping():
		waitResult = <-waitCh
	case <-ctx.Done():
		// Prefer an already observed natural exit over cancellation. Once
		// reaping starts, process-group signaling is no longer safe.
		select {
		case waitResult = <-waitCh:
		case <-waitState.startedReaping():
			waitResult = <-waitCh
		default:
			cancellation := cancellationFromContext(ctx)
			var terminationInitiated bool
			waitResult, terminationCleanupErr, terminationInitiated =
				terminateAndWait(
					cmd,
					waitState,
					waitCh,
					grace,
					cancellation.signal,
				)
			cancelled = terminationInitiated
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
		// Cmd.Wait may still own copier goroutines. Detach them from caller
		// writers so late completion cannot mutate published artifacts.
		stdout.Discard()
		stderr.Discard()
	}
	cleanupErr = errors.Join(
		cleanupErr,
		cleanupOwnedProcessTree(&processTree, cmd, grace, !unreaped),
	)
	copyErr := errors.Join(stdout.Err(), stderr.Err())

	if cancelled {
		cause := context.Cause(ctx)
		if cause == nil {
			cause = context.Canceled
		}
		return fmt.Errorf(
			"cancel owned command %q: %w",
			options.Executable,
			errors.Join(
				cause,
				cleanupErr,
				copyErr,
				nonExitWaitError(waitErr),
			),
		)
	}
	if waitErr != nil {
		return fmt.Errorf(
			"wait for owned command %q: %w",
			options.Executable,
			errors.Join(waitErr, copyErr, cleanupErr),
		)
	}
	if err := errors.Join(copyErr, cleanupErr); err != nil {
		return fmt.Errorf(
			"complete owned command %q: %w",
			options.Executable,
			err,
		)
	}
	return nil
}
