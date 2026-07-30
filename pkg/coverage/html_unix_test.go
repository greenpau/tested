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

package coverage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

const (
	maxCoveragePIDRecordBytes = 32
	coveragePIDPollInterval   = 10 * time.Millisecond
)

func TestGenerateHTMLCancellationTerminatesRetainedStderrDescendant(
	t *testing.T,
) {
	projectDir := t.TempDir()
	pidPath := filepath.Join(projectDir, "descendant.pid")
	script := writeScript(t, projectDir, "blocking-go", fmt.Sprintf(`#!/bin/sh
set -eu
trap '' INT TERM
sh -c 'trap "" INT TERM; while :; do sleep 1; done' >&2 &
child=$!
pid_file=%s
pid_tmp="${pid_file}.tmp"
printf '%%s\n' "$child" > "$pid_tmp"
mv "$pid_tmp" "$pid_file"
wait "$child"
`, shellQuote(pidPath)))
	output := filepath.Join(projectDir, "coverage.html")
	writeCoverageProfile(t, filepath.Join(projectDir, "coverage.out"))
	if err := os.WriteFile(output, []byte("prior report"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	outcomeCh := make(chan error, 1)
	go func() {
		outcomeCh <- GenerateHTML(ctx, HTMLOptions{
			GoCommand:      script,
			ProjectDir:     projectDir,
			ProfilePath:    "coverage.out",
			OutputPath:     "coverage.html",
			InterruptGrace: 100 * time.Millisecond,
		})
	}()

	pid, err := waitForCoverageDescendantPID(pidPath, 2*time.Second)
	if err != nil {
		cancel()
		select {
		case cleanupErr := <-outcomeCh:
			t.Fatalf(
				"wait for coverage descendant: %v; GenerateHTML cleanup: %v",
				err,
				cleanupErr,
			)
		case <-time.After(2 * time.Second):
			t.Fatalf(
				"wait for coverage descendant: %v; GenerateHTML cleanup timed out",
				err,
			)
		}
	}
	cleanupPID := pid
	defer func() {
		if cleanupPID > 0 {
			_ = syscall.Kill(cleanupPID, syscall.SIGKILL)
		}
	}()
	cancel()

	select {
	case err := <-outcomeCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("GenerateHTML() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("GenerateHTML() did not bound cancellation and pipe draining")
	}
	if err := waitForCoverageProcessGone(pid, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	cleanupPID = 0

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "prior report" {
		t.Fatalf("destination = %q, want prior report", data)
	}
	assertNoTemporaryFiles(t, projectDir)
}

func TestWaitForCoverageDescendantPIDRequiresCompletePublication(
	t *testing.T,
) {
	pidPath := filepath.Join(t.TempDir(), "descendant.pid")
	if err := os.WriteFile(pidPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	writeErrCh := make(chan error, 1)
	go func() {
		time.Sleep(25 * time.Millisecond)
		if err := os.WriteFile(pidPath, []byte("73"), 0o600); err != nil {
			writeErrCh <- err
			return
		}
		time.Sleep(25 * time.Millisecond)
		writeErrCh <- os.WriteFile(pidPath, []byte("731\n"), 0o600)
	}()

	pid, err := waitForCoverageDescendantPID(pidPath, time.Second)
	if writeErr := <-writeErrCh; writeErr != nil {
		t.Fatalf("publish descendant PID: %v", writeErr)
	}
	if err != nil {
		t.Fatalf("waitForCoverageDescendantPID() error = %v", err)
	}
	if pid != 731 {
		t.Fatalf("waitForCoverageDescendantPID() = %d, want 731", pid)
	}
}

func waitForCoverageDescendantPID(
	path string,
	timeout time.Duration,
) (int, error) {
	deadline := time.Now().Add(timeout)
	for {
		pid, ready, err := readCoverageDescendantPID(path)
		if err != nil {
			return 0, err
		}
		if ready {
			return pid, nil
		}
		if !time.Now().Before(deadline) {
			return 0, fmt.Errorf(
				"complete descendant PID was not published within %s",
				timeout,
			)
		}
		delay := min(coveragePIDPollInterval, time.Until(deadline))
		if delay > 0 {
			time.Sleep(delay)
		}
	}
}

func readCoverageDescendantPID(path string) (int, bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("open descendant PID: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(
		file,
		maxCoveragePIDRecordBytes+1,
	))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return 0, false, fmt.Errorf("read descendant PID: %w", err)
	}
	if len(data) > maxCoveragePIDRecordBytes {
		return 0, false, fmt.Errorf(
			"descendant PID record exceeds %d bytes",
			maxCoveragePIDRecordBytes,
		)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return 0, false, nil
	}

	digits := data[:len(data)-1]
	if len(digits) == 0 || digits[0] == '0' {
		return 0, false, fmt.Errorf(
			"invalid descendant PID record %q",
			data,
		)
	}
	for _, value := range digits {
		if value < '0' || value > '9' {
			return 0, false, fmt.Errorf(
				"invalid descendant PID record %q",
				data,
			)
		}
	}
	pid, err := strconv.Atoi(string(digits))
	if err != nil {
		return 0, false, fmt.Errorf(
			"parse descendant PID record %q: %w",
			data,
			err,
		)
	}
	if pid <= 0 {
		return 0, false, fmt.Errorf(
			"invalid descendant PID record %q",
			data,
		)
	}
	return pid, true, nil
}

func waitForCoverageProcessGone(
	pid int,
	timeout time.Duration,
) error {
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if err != nil && !errors.Is(err, syscall.EPERM) {
			return fmt.Errorf("inspect descendant process %d: %w", pid, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"descendant process %d remained after %s",
				pid,
				timeout,
			)
		}
		time.Sleep(coveragePIDPollInterval)
	}
}
