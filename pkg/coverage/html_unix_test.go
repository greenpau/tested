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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestGenerateHTMLCancellationTerminatesRetainedStderrDescendant(
	t *testing.T,
) {
	projectDir := t.TempDir()
	pidPath := filepath.Join(projectDir, "descendant.pid")
	script := writeScript(t, projectDir, "blocking-go", fmt.Sprintf(`#!/bin/sh
trap '' INT TERM
sh -c 'trap "" INT TERM; while :; do sleep 1; done' >&2 &
child=$!
printf '%%s' "$child" > %s
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
	started := time.Now()
	go func() {
		outcomeCh <- GenerateHTML(ctx, HTMLOptions{
			GoCommand:      script,
			ProjectDir:     projectDir,
			ProfilePath:    "coverage.out",
			OutputPath:     "coverage.html",
			InterruptGrace: 100 * time.Millisecond,
		})
	}()

	pid := waitForCoverageDescendantPID(t, pidPath, 2*time.Second)
	defer func() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
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
	if elapsed := time.Since(started); elapsed >= 2*time.Second {
		t.Fatalf("GenerateHTML() cancellation took %s, want less than 2s", elapsed)
	}
	waitForCoverageProcessGone(t, pid, 2*time.Second)

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "prior report" {
		t.Fatalf("destination = %q, want prior report", data)
	}
	assertNoTemporaryFiles(t, projectDir)
}

func waitForCoverageDescendantPID(
	t *testing.T,
	path string,
	timeout time.Duration,
) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr != nil || pid <= 0 {
				t.Fatalf("parse descendant PID %q: %v", data, parseErr)
			}
			return pid
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read descendant PID: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("descendant PID was not written within %s", timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForCoverageProcessGone(
	t *testing.T,
	pid int,
	timeout time.Duration,
) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if err != nil && !errors.Is(err, syscall.EPERM) {
			t.Fatalf("inspect descendant process %d: %v", pid, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("descendant process %d remained after %s", pid, timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
