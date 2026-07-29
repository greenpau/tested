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
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	switch os.Getenv("TESTED_RUNNER_HELPER") {
	case "1", "status":
		_, _ = fmt.Fprintln(os.Stdout, `{"Action":"output","Package":"helper","Output":"hello\n"}`)
		code, _ := strconv.Atoi(os.Getenv("TESTED_RUNNER_HELPER_EXIT"))
		os.Exit(code)
	case "chatty":
		payload := strings.Repeat("x", 64<<10)
		for {
			if _, err := fmt.Fprintln(os.Stdout, payload); err != nil {
				os.Exit(98)
			}
		}
	case "chatty-stderr":
		payload := strings.Repeat("x", 64<<10)
		for {
			if _, err := fmt.Fprintln(os.Stderr, payload); err != nil {
				os.Exit(98)
			}
		}
	case "retained-parent":
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), "TESTED_RUNNER_HELPER=retained-child")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "start retained child: %v\n", err)
			os.Exit(97)
		}
		path := os.Getenv("TESTED_RUNNER_PID_FILE")
		if err := os.WriteFile(path, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "write retained child pid: %v\n", err)
			_ = cmd.Process.Kill()
			os.Exit(96)
		}
		os.Exit(0)
	case "detached-pipe-parent":
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), "TESTED_RUNNER_HELPER=retained-child")
		if err := cmd.Start(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "start detached-pipe child: %v\n", err)
			os.Exit(93)
		}
		path := os.Getenv("TESTED_RUNNER_PID_FILE")
		if err := os.WriteFile(path, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "write detached-pipe child pid: %v\n", err)
			_ = cmd.Process.Kill()
			os.Exit(92)
		}
		os.Exit(0)
	case "escaped-pipe-parent":
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), "TESTED_RUNNER_HELPER=retained-child")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		configureEscapedTestProcess(cmd)
		if err := cmd.Start(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "start escaped child: %v\n", err)
			os.Exit(90)
		}
		path := os.Getenv("TESTED_RUNNER_PID_FILE")
		if err := os.WriteFile(path, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "write escaped child pid: %v\n", err)
			_ = cmd.Process.Kill()
			os.Exit(89)
		}
		os.Exit(0)
	case "retained-child":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "await-signal":
		notifications := make(chan os.Signal, 1)
		signal.Notify(notifications, os.Interrupt, syscall.SIGTERM)
		if err := os.WriteFile(
			os.Getenv("TESTED_RUNNER_READY_FILE"),
			[]byte("ready"),
			0o600,
		); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "write signal readiness: %v\n", err)
			os.Exit(95)
		}
		received := <-notifications
		if err := os.WriteFile(
			os.Getenv("TESTED_RUNNER_SIGNAL_FILE"),
			[]byte(received.String()),
			0o600,
		); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "write received signal: %v\n", err)
			os.Exit(94)
		}
		os.Exit(0)
	case "ignore-signals":
		signal.Ignore(os.Interrupt, syscall.SIGTERM)
		if err := os.WriteFile(
			os.Getenv("TESTED_RUNNER_READY_FILE"),
			[]byte("ready"),
			0o600,
		); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "write ignore-signal readiness: %v\n", err)
			os.Exit(91)
		}
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestArguments(t *testing.T) {
	tests := []struct {
		name    string
		options Options
		want    []string
		wantErr string
	}{
		{
			name: "default package",
			options: Options{
				GoCommand: "go",
				WorkDir:   ".",
			},
			want: []string{"test", "-json", "./..."},
		},
		{
			name: "managed flags before exact arguments",
			options: Options{
				GoCommand:       "/opt/go/bin/go",
				WorkDir:         "/project",
				CoverageProfile: "/project/.coverage/coverage.out",
				TestArguments:   []string{"./...", "-args", "-json"},
			},
			want: []string{
				"test",
				"-json",
				"-coverprofile=/project/.coverage/coverage.out",
				"./...",
				"-args",
				"-json",
			},
		},
		{
			name:    "missing go command",
			options: Options{WorkDir: "."},
			wantErr: "go command",
		},
		{
			name:    "missing working directory",
			options: Options{GoCommand: "go"},
			wantErr: "working directory",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Arguments(test.options)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("Arguments() error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Arguments() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Arguments() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRunnerRunPassingModule(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test invokes the Go toolchain")
	}
	workDir := t.TempDir()
	writeFixture(t, filepath.Join(workDir, "go.mod"), "module example.com/fixture\n\ngo 1.25.0\n")
	writeFixture(t, filepath.Join(workDir, "fixture.go"), "package fixture\n\nfunc Sum(a, b int) int { return a + b }\n")
	writeFixture(t, filepath.Join(workDir, "fixture_test.go"), `package fixture

import "testing"

func TestSum(t *testing.T) {
	if Sum(2, 3) != 5 {
		t.Fatal("bad sum")
	}
}
`)

	var stdout strings.Builder
	var stderr strings.Builder
	result, err := New().Run(context.Background(), Options{
		GoCommand:      "go",
		WorkDir:        workDir,
		TestArguments:  []string{"./..."},
		StandardOutput: &stdout,
		StandardError:  &stderr,
	})
	if err != nil {
		t.Fatalf("Run() unexpected error: %v\nstderr: %s", err, stderr.String())
	}
	if result.ExitCode != 0 || result.Interrupted {
		t.Fatalf("Run() result = %#v\nstderr: %s", result, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"Action":"pass"`) {
		t.Fatalf("Run() stdout missing pass event: %s", stdout.String())
	}
}

func TestRunnerRejectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := New().Run(ctx, Options{GoCommand: "go", WorkDir: "."})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if !result.Interrupted || result.ExitCode != -1 ||
		result.Cancellation != CancellationProgrammatic ||
		result.CancellationSignal != "" ||
		result.RecommendedExitCode != 130 {
		t.Fatalf("Run() result = %#v, want programmatic pre-start cancellation", result)
	}
}

func TestRunnerRejectsSignalCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	signalCause := NewSignalCause(syscall.SIGTERM)
	cancel(signalCause)

	result, err := New().Run(ctx, Options{GoCommand: "go", WorkDir: "."})
	if !errors.Is(err, context.Canceled) || !errors.Is(err, signalCause) {
		t.Fatalf("Run() error = %v, want typed signal cancellation", err)
	}
	if !result.Interrupted || result.ExitCode != -1 ||
		result.Cancellation != CancellationSignal ||
		result.CancellationSignal != syscall.SIGTERM.String() ||
		result.RecommendedExitCode != 143 {
		t.Fatalf("Run() result = %#v, want SIGTERM pre-start cancellation", result)
	}
}

func TestCancellationResult(t *testing.T) {
	//nolint:staticcheck // CancellationResult explicitly accepts a nil context.
	if got := CancellationResult(nil); got != (CancellationDetails{}) {
		t.Fatalf("nil context details = %#v, want zero value", got)
	}
	active := CancellationResult(context.Background())
	if active != (CancellationDetails{}) {
		t.Fatalf("active context details = %#v, want zero value", active)
	}

	deadlineCause := errors.New("operation budget exhausted")
	deadlineCtx, cancelDeadline := context.WithDeadlineCause(
		context.Background(),
		time.Now().Add(-time.Second),
		deadlineCause,
	)
	defer cancelDeadline()
	deadline := CancellationResult(deadlineCtx)
	if deadline.Kind != CancellationDeadline ||
		deadline.Signal != "" ||
		deadline.RecommendedExitCode != 130 {
		t.Fatalf("deadline details = %#v", deadline)
	}

	signalCtx, cancelSignal := context.WithCancelCause(context.Background())
	cancelSignal(NewSignalCause(syscall.SIGTERM))
	operator := CancellationResult(signalCtx)
	if operator.Kind != CancellationSignal ||
		operator.Signal != syscall.SIGTERM.String() ||
		operator.RecommendedExitCode != 143 {
		t.Fatalf("operator details = %#v", operator)
	}
}

func TestRunnerStartFailureDoesNotClaimChildStarted(t *testing.T) {
	result, err := New().Run(context.Background(), Options{
		GoCommand: filepath.Join(t.TempDir(), "missing-go"),
		WorkDir:   ".",
	})
	if err == nil {
		t.Fatal("Run() start error = nil")
	}
	if !result.StartedAt.IsZero() {
		t.Fatalf("Run() StartedAt = %s, want zero after start failure", result.StartedAt)
	}
	if result.FinishedAt.IsZero() || result.Duration < 0 {
		t.Fatalf("Run() start-failure timing = %#v", result)
	}
	if result.ExitCode != -1 || result.Interrupted ||
		result.Cancellation != CancellationNone ||
		result.RecommendedExitCode != 0 {
		t.Fatalf("Run() start-failure result = %#v", result)
	}
}

func TestRunnerPreservesProcessStatusWhenOutputCopyFails(t *testing.T) {
	sinkErr := errors.New("output sink failed")
	for _, test := range []struct {
		name string
		exit string
		want int
	}{
		{name: "successful child", exit: "0", want: 0},
		{name: "failing child", exit: "7", want: 7},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := New().Run(context.Background(), Options{
				GoCommand:      os.Args[0],
				WorkDir:        ".",
				StandardOutput: fixedErrorWriter{err: sinkErr},
				Environment: append(
					os.Environ(),
					"TESTED_RUNNER_HELPER=1",
					"TESTED_RUNNER_HELPER_EXIT="+test.exit,
				),
			})
			if !errors.Is(err, sinkErr) {
				t.Fatalf("Run() error = %v, want output sink failure", err)
			}
			if result.ExitCode != test.want {
				t.Fatalf("Run() exit = %d, want %d", result.ExitCode, test.want)
			}
			if result.StartedAt.IsZero() || result.FinishedAt.IsZero() {
				t.Fatalf("Run() omitted process timing: %#v", result)
			}
		})
	}
}

func TestTrackingWriterDrainsAfterFailure(t *testing.T) {
	sinkErr := errors.New("capture failed")
	notifications := make(chan struct{}, 2)
	writer := newTrackingWriter(fixedErrorWriter{err: sinkErr}, func() {
		notifications <- struct{}{}
	})

	for _, data := range [][]byte{[]byte("first"), []byte("second")} {
		n, err := writer.Write(data)
		if err != nil || n != len(data) {
			t.Fatalf("Write(%q) = (%d, %v), want (%d, nil)", data, n, err, len(data))
		}
	}
	if !errors.Is(writer.Err(), sinkErr) {
		t.Fatalf("Err() = %v, want capture failure", writer.Err())
	}
	select {
	case <-notifications:
	default:
		t.Fatal("capture failure notification was not delivered")
	}
	select {
	case <-notifications:
		t.Fatal("capture failure was notified more than once")
	default:
	}
}

func TestTrackingWriterDiscardDetachesSink(t *testing.T) {
	sink := &strings.Builder{}
	writer := newTrackingWriter(sink, nil)
	writer.Discard()

	data := []byte("late child output")
	n, err := writer.Write(data)
	if err != nil || n != len(data) {
		t.Fatalf("Write() = (%d, %v), want (%d, nil)", n, err, len(data))
	}
	if sink.Len() != 0 || writer.Err() != nil {
		t.Fatalf("discarded writer mutated sink or reported error: %q, %v", sink, writer.Err())
	}
}

func TestWaitAfterForcedTerminationIsBounded(t *testing.T) {
	waitCh := make(chan commandWaitResult)
	waitState := newCommandWaitState()
	retryErr := errors.New("retry force kill failed")
	retries := 0
	startedAt := time.Now()
	waitResult, cleanupErr := waitAfterForcedTermination(
		waitCh,
		waitState,
		20*time.Millisecond,
		func() error {
			retries++
			return retryErr
		},
	)
	if !errors.Is(waitResult.waitErr, ErrTerminationTimeout) {
		t.Fatalf("wait error = %v, want ErrTerminationTimeout", waitResult.waitErr)
	}
	if !errors.Is(cleanupErr, retryErr) || retries != 1 {
		t.Fatalf("cleanup = (%v, %d retries), want retry error once", cleanupErr, retries)
	}
	if elapsed := time.Since(startedAt); elapsed < 20*time.Millisecond ||
		elapsed >= time.Second {
		t.Fatalf("bounded wait duration = %s", elapsed)
	}
}

type fixedErrorWriter struct {
	err error
}

func (w fixedErrorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
