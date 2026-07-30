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
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestProcessStatusPreservesSignal(t *testing.T) {
	if os.Getenv("TESTED_SIGNAL_HELPER") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(
		ctx,
		os.Args[0],
		"-test.run=TestProcessStatusPreservesSignal",
	)
	cmd.Env = append(os.Environ(), "TESTED_SIGNAL_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start signal helper: %v", err)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("signal helper: %v", err)
	}
	err := cmd.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("helper error = %v, want *exec.ExitError", err)
	}
	code, signal := processStatus(exitErr.ProcessState)
	if code != 128+int(syscall.SIGTERM) {
		t.Fatalf("processStatus() code = %d, want %d", code, 128+int(syscall.SIGTERM))
	}
	if signal == "" {
		t.Fatal("processStatus() omitted signal name")
	}
}
