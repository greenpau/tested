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

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/greenpau/tested/pkg/artifact"
	"github.com/greenpau/tested/pkg/result"
)

type progressRecorder struct {
	messages     []string
	eventWritten bool
	err          error
	stderr       func(string)
}

func (r *progressRecorder) Stage(s string) error                { r.messages = append(r.messages, s); return r.err }
func (r *progressRecorder) Event(result.Progress) (bool, error) { return r.eventWritten, r.err }
func (r *progressRecorder) Stderr(s string) (bool, error) {
	if r.stderr != nil {
		r.stderr(s)
	}
	return r.eventWritten, r.err
}

func TestProgressQuietHeartbeatAndFailureLatch(t *testing.T) {
	now := time.Unix(0, 0)
	recorder := &progressRecorder{eventWritten: true}
	p := startLiveProgress(recorder, false)
	p.enabled = true
	p.now = func() time.Time { return now }
	p.started = now
	p.stage("Running Go tests")
	p.heartbeat(now.Add(9 * time.Second))
	if len(recorder.messages) != 1 {
		t.Fatal("heartbeat arrived early")
	}
	now = now.Add(9 * time.Second)
	p.event(result.Progress{})
	p.heartbeat(now.Add(9 * time.Second))
	if len(recorder.messages) != 1 {
		t.Fatal("recent output did not postpone heartbeat")
	}
	now = now.Add(10 * time.Second)
	p.heartbeat(now)
	if len(recorder.messages) != 2 || !strings.Contains(recorder.messages[1], "19s elapsed, 1 event records") {
		t.Fatalf("heartbeat: %#v", recorder.messages)
	}
	recorder.eventWritten = false // Suppressed/capped details must not starve status.
	now = now.Add(9 * time.Second)
	p.event(result.Progress{})
	now = now.Add(time.Second)
	p.heartbeat(now)
	if len(recorder.messages) != 3 {
		t.Fatal("suppressed detail postponed heartbeat")
	}
	recorder.err = io.ErrClosedPipe
	p.stage("Publishing reports")
	for range 100 {
		p.event(result.Progress{})
		p.stderr("log")
		p.stage("work")
		p.heartbeat(now.Add(time.Hour))
	}
	var outcome executionOutcome
	if !p.collectError(&outcome) || p.collectError(&outcome) || outcome.errors.count() != 1 || !outcome.state.ReportErr {
		t.Fatalf("error latch: %#v", outcome)
	}
	if len(recorder.messages) != 4 {
		t.Fatal("writer retried after failure")
	}
	p.finish(&outcome, nil)
	select {
	case <-p.done:
	default:
		t.Fatal("progress goroutine not joined")
	}
}

func TestProgressCaptureWritesRawFirst(t *testing.T) {
	var raw bytes.Buffer
	recorder := &progressRecorder{err: io.ErrClosedPipe}
	recorder.stderr = func(value string) {
		if raw.String() != value {
			t.Fatal("presentation preceded capture")
		}
	}
	p := startLiveProgress(recorder, false)
	p.enabled = true
	var outcome executionOutcome
	defer p.finish(&outcome, nil)
	capture := progressCapture{raw: &raw, progress: p}
	payload := []byte("compiler\x1b[2J\n")
	n, err := capture.Write(payload)
	if err != nil || n != len(payload) || !bytes.Equal(raw.Bytes(), payload) {
		t.Fatalf("display error damaged raw capture: %d %v %q", n, err, raw.Bytes())
	}
	n, err = capture.Write(payload)
	if err != nil || n != len(payload) || raw.Len() != 2*len(payload) {
		t.Fatal("capture stopped after display failure")
	}
	capture.raw = progressRawFailure{}
	if n, err = capture.Write(payload); n != 0 || !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("raw failure lost: %d %v", n, err)
	}
}

type progressRawFailure struct{}

func (progressRawFailure) Write([]byte) (int, error) { return 0, nil }

const progressHelperHead = `{"Action":"start","Package":"example/progress"}
{"Action":"run","Package":"example/progress","Test":"TestWork/child"}
{"Action":"output","Package":"example/progress","Test":"TestWork/child","Output":"working before completion\n"}
{"Action":"build-output","ImportPath":"example/dependency","Output":"build detail before completion\n"}
`
const progressHelperStderr = "stderr before completion\x1b[2J\n"

func progressHelperTail(fail bool) string {
	action := "pass"
	if fail {
		action = "fail"
	}
	return fmt.Sprintf("{\"Action\":%q,\"Package\":\"example/progress\",\"Test\":\"TestWork/child\",\"Elapsed\":0.1}\n{\"Action\":%q,\"Package\":\"example/progress\",\"Elapsed\":0.1}\n", action, action)
}

// runProgressHelper is selected by TestMain, so the actual test executable can
// stand in for Go without depending on a shell or compiling another helper.
func runProgressHelper() int {
	address := os.Getenv("TESTED_APP_PROGRESS_ADDRESS")
	var connection net.Conn
	if address != "" {
		var err error
		connection, err = net.DialTimeout("tcp", address, 10*time.Second)
		if err != nil {
			return 80
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(20 * time.Second))
	}
	if _, err := io.WriteString(os.Stdout, progressHelperHead); err != nil {
		return 81
	}
	if _, err := io.WriteString(os.Stderr, progressHelperStderr); err != nil {
		return 82
	}
	if connection != nil {
		var release [1]byte
		if _, err := io.ReadFull(connection, release[:]); err != nil {
			return 83
		}
	}
	fail := os.Getenv("TESTED_APP_PROGRESS_HELPER") == "fail"
	if _, err := io.WriteString(os.Stdout, progressHelperTail(fail)); err != nil {
		return 84
	}
	if fail {
		return 1
	}
	return 0
}

type observingProgressWriter struct {
	mu            sync.Mutex
	output        bytes.Buffer
	ready         chan struct{}
	once          sync.Once
	heartbeat     chan struct{}
	heartbeatOnce sync.Once
}

func (w *observingProgressWriter) Write(value []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.output.Write(value)
	got := w.output.String()
	if w.heartbeat != nil && strings.Contains(got, "Still working: Running Go tests") {
		w.heartbeatOnce.Do(func() { close(w.heartbeat) })
	}
	if strings.Contains(got, "[run]") && strings.Contains(got, "working before completion") && strings.Contains(got, "build detail before completion") && strings.Contains(got, "stderr before completion") {
		w.once.Do(func() { close(w.ready) })
	}
	return n, err
}
func (w *observingProgressWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.output.String()
}

func TestExecuteStreamsBeforeChildCompletion(t *testing.T) {
	for _, cancelRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancelRun), func(t *testing.T) {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			work := t.TempDir()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			_ = listener.(*net.TCPListener).SetDeadline(time.Now().Add(15 * time.Second))
			t.Setenv("TESTED_APP_PROGRESS_HELPER", "pass")
			t.Setenv("TESTED_APP_PROGRESS_ADDRESS", listener.Addr().String())
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			output := &observingProgressWriter{ready: make(chan struct{}), heartbeat: make(chan struct{})}
			var stderr bytes.Buffer
			done := make(chan int, 1)
			go func() {
				done <- Execute(ctx, []string{"run", "-C", work, "--no-coverage", "--color", "never", "--go", executable}, output, &stderr, BuildInfo{})
			}()
			// Always join Execute before t.Setenv or the temp directory is cleaned up.
			joined := false
			defer func() {
				cancel()
				if !joined {
					<-done
				}
			}()
			connection, err := listener.Accept()
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			select {
			case <-output.ready:
			case code := <-done:
				joined = true
				t.Fatalf("child completed before streaming: %d, %s", code, stderr.String())
			case <-ctx.Done():
				t.Fatal("live output never arrived")
			}
			root := filepath.Join(work, ".coverage")
			for name, want := range map[string]string{artifact.TestOutputJSONLName: progressHelperHead, artifact.StderrLogName: progressHelperStderr} {
				got, err := os.ReadFile(filepath.Join(root, name))
				if err != nil || string(got) != want {
					t.Fatalf("raw %s before display: %q, %v", name, got, err)
				}
			}
			if strings.Contains(output.String(), "Outcome:") {
				t.Fatal("summary preceded child completion")
			}
			if !cancelRun {
				select {
				case <-output.heartbeat:
				case code := <-done:
					joined = true
					t.Fatalf("child completed before heartbeat: %d", code)
				case <-ctx.Done():
					t.Fatal("quiet-period heartbeat never arrived")
				}
			}
			if cancelRun {
				cancel()
			} else {
				if _, err := connection.Write([]byte{1}); err != nil {
					t.Fatal(err)
				}
			}
			code := <-done
			joined = true
			if cancelRun {
				if code != ExitInterrupted {
					t.Fatalf("cancellation exit=%d, %s", code, stderr.String())
				}
				if _, err := os.Stat(filepath.Join(root, artifact.ManifestJSONName)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("cancelled manifest: %v", err)
				}
				return
			}
			if code != ExitSuccess {
				t.Fatalf("exit=%d, %s", code, stderr.String())
			}
			got, err := os.ReadFile(filepath.Join(root, artifact.TestOutputJSONLName))
			if err != nil || string(got) != progressHelperHead+progressHelperTail(false) {
				t.Fatalf("raw stream changed: %q %v", got, err)
			}
			for _, want := range []string{"[pass]", "Publishing test_output.html", "Publishing summary.json", "Publishing junit.xml", "Publishing index.html", "Hashing artifacts and publishing manifest", "Finished"} {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("missing %q in %s", want, output.String())
				}
			}
			if _, err := os.Stat(filepath.Join(root, artifact.ManifestJSONName)); err != nil {
				t.Fatal(err)
			}
			var offline bytes.Buffer
			if code := Execute(ctx, []string{"report", "-C", work, "--no-coverage"}, &offline, &stderr, BuildInfo{}); code != 0 {
				t.Fatalf("offline exit %d: %s", code, stderr.String())
			}
			if !strings.Contains(offline.String(), "Reading and analyzing captured test events") || strings.Contains(offline.String(), "[run]") || strings.Contains(offline.String(), "working before completion") {
				t.Fatalf("offline replayed historical progress: %s", offline.String())
			}
		})
	}
}

type failOnProgressWriter struct {
	needle string
	failed bool
}

func (w *failOnProgressWriter) Write(value []byte) (int, error) {
	if strings.Contains(string(value), w.needle) {
		w.failed = true
		return 0, io.ErrClosedPipe
	}
	return len(value), nil
}

func TestLiveProgressFailurePreservesEvidence(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{"[run]", "Publishing summary.json", "Hashing artifacts", "[tested] Finished"} {
		for _, childFail := range []bool{false, true} {
			if childFail && needle == "Hashing artifacts" {
				continue
			}
			t.Run(fmt.Sprintf("%s fail=%t", needle, childFail), func(t *testing.T) {
				mode := "pass"
				if childFail {
					mode = "fail"
				}
				t.Setenv("TESTED_APP_PROGRESS_HELPER", mode)
				t.Setenv("TESTED_APP_PROGRESS_ADDRESS", "")
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				work := t.TempDir()
				writer := &failOnProgressWriter{needle: needle}
				var stderr bytes.Buffer
				code := Execute(ctx, []string{"run", "-C", work, "--no-coverage", "--go", executable}, writer, &stderr, BuildInfo{})
				want := ExitInfrastructure
				if childFail {
					want = ExitTestsFailed
				}
				if code != want || !writer.failed {
					t.Fatalf("exit %d want %d, failed=%t, %s", code, want, writer.failed, stderr.String())
				}
				root := filepath.Join(work, ".coverage")
				raw, err := os.ReadFile(filepath.Join(root, artifact.TestOutputJSONLName))
				if err != nil || string(raw) != progressHelperHead+progressHelperTail(childFail) {
					t.Fatalf("capture affected: %q %v", raw, err)
				}
				if _, err := os.Stat(filepath.Join(root, artifact.ManifestJSONName)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("manifest retained: %v", err)
				}
				status, err := os.ReadFile(filepath.Join(root, artifact.RunJSONName))
				if err != nil || bytes.Contains(status, []byte("stream live progress")) {
					t.Fatalf("durable status contaminated: %s %v", status, err)
				}
				summary, err := os.ReadFile(filepath.Join(root, artifact.SummaryJSONName))
				if err != nil || !bytes.Contains(summary, []byte(`"report_failed":true`)) {
					t.Fatalf("summary missing presentation failure: %s %v", summary, err)
				}
				var output bytes.Buffer
				want = 0
				if childFail {
					want = 1
				}
				if code := Execute(ctx, []string{"report", "-C", work, "--no-coverage", "--quiet"}, &output, &stderr, BuildInfo{}); code != want {
					t.Fatalf("offline recovery exit=%d, want=%d: %s", code, want, stderr.String())
				}
			})
		}
	}
}

func TestExecuteLivePresentationModes(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TESTED_APP_PROGRESS_HELPER", "pass")
	t.Setenv("TESTED_APP_PROGRESS_ADDRESS", "")
	cases := []struct {
		name    string
		options []string
		live    bool
		logs    bool
		json    bool
	}{
		{name: "plain", live: true, logs: true},
		{name: "markdown", options: []string{"--format", "markdown"}, live: true, logs: true},
		{name: "quiet", options: []string{"--quiet"}},
		{name: "json", options: []string{"--format", "json"}, json: true},
		{name: "redacted", options: []string{"--redact", "working before completion"}, live: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			work := t.TempDir()
			args := append([]string{"run", "-C", work, "--no-coverage", "--color", "never", "--go", executable}, test.options...)
			var output, stderr bytes.Buffer
			if code := Execute(ctx, args, &output, &stderr, BuildInfo{}); code != 0 {
				t.Fatalf("exit %d: %s", code, stderr.String())
			}
			got := output.String()
			if strings.Contains(got, "Preparing test artifacts") != test.live || strings.Contains(got, "working before completion") != test.logs || strings.Contains(got, "stderr before completion") != test.logs {
				t.Fatalf("unexpected live projection: %s", got)
			}
			if test.live {
				context, event := "[package] example/progress", "[run] test TestWork/child"
				if test.name == "markdown" {
					context, event = "**package** example/progress", "**run** test TestWork/child"
				}
				if !strings.Contains(got, context) || !strings.Contains(got, event) ||
					strings.Contains(got, "example/progress::TestWork") {
					t.Fatalf("package context was lost or repeated in test lines: %s", got)
				}
			}
			if test.json {
				decoder := json.NewDecoder(strings.NewReader(got))
				var summary map[string]any
				if err := decoder.Decode(&summary); err != nil {
					t.Fatal(err)
				}
				if summary["schema"] != "tested/summary/v1" {
					t.Fatalf("bad schema: %#v", summary)
				}
				if err := decoder.Decode(&summary); !errors.Is(err, io.EOF) {
					t.Fatalf("extra JSON output: %v", err)
				}
			} else if !strings.Contains(got, "Outcome:") {
				t.Fatal("final summary missing")
			}
			for name, want := range map[string]string{artifact.TestOutputJSONLName: progressHelperHead + progressHelperTail(false), artifact.StderrLogName: progressHelperStderr} {
				got, err := os.ReadFile(filepath.Join(work, ".coverage", name))
				if err != nil || string(got) != want {
					t.Fatalf("raw evidence affected by format: %q %v", got, err)
				}
			}
		})
	}
}
