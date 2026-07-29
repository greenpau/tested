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
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRunCommandExecutesExactArgv(t *testing.T) {
	var stdout bytes.Buffer
	err := RunCommand(context.Background(), CommandOptions{
		Executable:     os.Args[0],
		Arguments:      []string{"-test.run=^$"},
		WorkDir:        ".",
		StandardOutput: &stdout,
		Environment: append(
			os.Environ(),
			"TESTED_RUNNER_HELPER=status",
			"TESTED_RUNNER_HELPER_EXIT=0",
		),
	})
	if err != nil {
		t.Fatalf("RunCommand() error = %v", err)
	}
	if !strings.Contains(stdout.String(), `"Action":"output"`) {
		t.Fatalf("RunCommand() stdout = %q, want helper output", stdout.String())
	}
}

func TestRunCommandReturnsExitError(t *testing.T) {
	err := RunCommand(context.Background(), CommandOptions{
		Executable: os.Args[0],
		Arguments:  []string{"-test.run=^$"},
		WorkDir:    ".",
		Environment: append(
			os.Environ(),
			"TESTED_RUNNER_HELPER=status",
			"TESTED_RUNNER_HELPER_EXIT=17",
		),
	})
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("RunCommand() error = %v, want *exec.ExitError", err)
	}
	if got := exitErr.ExitCode(); got != 17 {
		t.Fatalf("RunCommand() exit = %d, want 17", got)
	}
}

func TestRunCommandValidationAndPreCancellation(t *testing.T) {
	//nolint:staticcheck // The nil-context rejection is part of this API's contract.
	if err := RunCommand(nil, CommandOptions{}); err == nil {
		t.Fatal("RunCommand(nil) error = nil, want validation error")
	}
	if err := RunCommand(
		context.Background(),
		CommandOptions{WorkDir: "."},
	); err == nil {
		t.Fatal("RunCommand(missing executable) error = nil, want error")
	}
	if err := RunCommand(
		context.Background(),
		CommandOptions{Executable: os.Args[0]},
	); err == nil {
		t.Fatal("RunCommand(missing workdir) error = nil, want error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RunCommand(ctx, CommandOptions{
		Executable: os.Args[0],
		WorkDir:    ".",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunCommand(cancelled) error = %v, want context.Canceled", err)
	}
}
