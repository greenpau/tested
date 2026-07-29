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

package runstatus

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestEncodeDecode(t *testing.T) {
	started := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	finished := started.Add(2 * time.Second)
	exitCode := 7
	want := Evidence{
		Schema:              Schema,
		Command:             []string{"go", "test", "-json", "./..."},
		WorkDir:             "/project",
		StartedAt:           &started,
		FinishedAt:          &finished,
		DurationNS:          int64(1500 * time.Millisecond),
		ChildStarted:        true,
		ExitCode:            &exitCode,
		Signal:              "interrupt",
		Interrupted:         true,
		Cancellation:        "signal",
		CancellationSignal:  "terminated",
		RecommendedExitCode: 143,
		CaptureComplete:     true,
		Files: []File{
			{
				Name:   "test_output.jsonl",
				Size:   2,
				SHA256: strings.Repeat("a", 64),
			},
			{
				Name:   "stderr.log",
				Size:   0,
				SHA256: strings.Repeat("0", 64),
			},
		},
		Issues: []Issue{{
			Kind:    "runner",
			Message: "wait interrupted",
		}},
	}
	var encoded bytes.Buffer
	if err := Encode(&encoded, want); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if !bytes.HasSuffix(encoded.Bytes(), []byte("\n")) {
		t.Fatalf("Encode() output does not end in newline: %q", encoded.Bytes())
	}
	reordered := want
	reordered.Files = append([]File(nil), want.Files...)
	for left, right := 0, len(reordered.Files)-1; left < right; left, right = left+1, right-1 {
		reordered.Files[left], reordered.Files[right] =
			reordered.Files[right], reordered.Files[left]
	}
	var reorderedEncoded bytes.Buffer
	if err := Encode(&reorderedEncoded, reordered); err != nil {
		t.Fatalf("Encode(reordered) error = %v", err)
	}
	if !bytes.Equal(encoded.Bytes(), reorderedEncoded.Bytes()) {
		t.Fatalf(
			"Encode() depends on file binding order:\n%s\n%s",
			encoded.Bytes(),
			reorderedEncoded.Bytes(),
		)
	}
	if want.Files[0].Name != "test_output.jsonl" {
		t.Fatal("Encode() mutated caller-owned file binding order")
	}
	got, err := Decode(&encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Schema != want.Schema || got.WorkDir != want.WorkDir ||
		got.ExitCode == nil || *got.ExitCode != exitCode ||
		got.DurationNS != want.DurationNS ||
		!got.CaptureComplete || !got.Interrupted ||
		len(got.Issues) != 1 {
		t.Fatalf("Decode() = %#v, want %#v", got, want)
	}
}

func TestDecodeRejectsMalformedEvidence(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "wrong schema", input: evidenceJSON(`"schema":"other"`)},
		{name: "unknown field", input: evidenceJSON(`"extra":1`)},
		{name: "exit without start", input: evidenceJSON(`"exit_code":0`)},
		{name: "negative exit", input: evidenceJSON(`"child_started":true,"exit_code":-1`)},
		{name: "signal without exit", input: startedEvidenceJSON(`"signal":"killed"`)},
		{name: "signal with zero exit", input: startedEvidenceJSON(`"exit_code":0,"signal":"killed"`)},
		{name: "negative duration", input: evidenceJSON(`"duration_ns":-1`)},
		{name: "duration without start", input: evidenceJSON(`"duration_ns":1`)},
		{
			name: "duplicate top-level member",
			input: strings.Replace(
				evidenceJSON(""),
				`"capture_complete":true`,
				`"capture_complete":true,"capture_complete":false`,
				1,
			),
		},
		{
			name: "duplicate nested member",
			input: strings.Replace(
				evidenceJSON(""),
				`"size":0`,
				`"size":0,"size":1`,
				1,
			),
		},
		{
			name: "escaped duplicate member",
			input: strings.Replace(
				evidenceJSON(""),
				`"size":0`,
				`"size":0,"\u0073ize":1`,
				1,
			),
		},
		{
			name: "available policy without coverage binding",
			input: evidenceJSON(
				`"coverage_policy":{"minimum":"1","actual":"50",` +
					`"covered":1,"statements":2,"available":true,` +
					`"satisfied":true}`,
			),
		},
		{name: "multiple values", input: evidenceJSON("") + `{}`},
		{name: "empty issue", input: evidenceJSON(`"issues":[{"kind":"","message":"bad"}]`)},
		{name: "missing binding", input: `{"schema":"tested/run/v1","child_started":false,"capture_complete":true,"files":[]}`},
		{name: "bad digest", input: `{"schema":"tested/run/v1","child_started":false,"capture_complete":true,"files":[{"name":"test_output.jsonl","size":0,"sha256":"abc"}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Decode(strings.NewReader(test.input)); err == nil {
				t.Fatal("Decode() error = nil, want error")
			}
		})
	}
}

func TestValidateCancellationSignalExitMapping(t *testing.T) {
	tests := []struct {
		name      string
		signal    string
		exitCode  int
		wantError bool
	}{
		{
			name:     "interrupt",
			signal:   "interrupt",
			exitCode: 130,
		},
		{
			name:     "terminated",
			signal:   "terminated",
			exitCode: 143,
		},
		{
			name:      "interrupt cannot project term",
			signal:    "interrupt",
			exitCode:  143,
			wantError: true,
		},
		{
			name:      "terminated cannot project interrupt",
			signal:    "terminated",
			exitCode:  130,
			wantError: true,
		},
		{
			name:      "unsupported signal",
			signal:    "killed",
			exitCode:  137,
			wantError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exitCode := test.exitCode
			evidence := Evidence{
				Schema:              Schema,
				ChildStarted:        true,
				ExitCode:            &exitCode,
				Interrupted:         true,
				Cancellation:        "signal",
				CancellationSignal:  test.signal,
				RecommendedExitCode: test.exitCode,
				CaptureComplete:     true,
				Files: []File{{
					Name:   "test_output.jsonl",
					SHA256: strings.Repeat("0", 64),
				}},
			}
			err := evidence.Validate()
			if test.wantError && err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !test.wantError && err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateSignalRequiresKnownNonzeroExit(t *testing.T) {
	zeroExitCode := 0
	failedExitCode := 137
	tests := []struct {
		name      string
		exitCode  *int
		signal    string
		wantError bool
	}{
		{
			name: "no signal without exit",
		},
		{
			name:     "no signal with zero exit",
			exitCode: &zeroExitCode,
		},
		{
			name:      "signal without exit",
			signal:    "killed",
			wantError: true,
		},
		{
			name:      "signal with zero exit",
			exitCode:  &zeroExitCode,
			signal:    "killed",
			wantError: true,
		},
		{
			name:     "signal with failed exit",
			exitCode: &failedExitCode,
			signal:   "killed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := Evidence{
				Schema:          Schema,
				ChildStarted:    true,
				ExitCode:        test.exitCode,
				Signal:          test.signal,
				CaptureComplete: true,
				Files: []File{{
					Name:   "test_output.jsonl",
					SHA256: strings.Repeat("0", 64),
				}},
			}
			err := evidence.Validate()
			if !test.wantError {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			const want = "signal requires a known nonzero exit_code"
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("Validate() error = %q, want containing %q", err, want)
			}
		})
	}
}

func evidenceJSON(extra string) string {
	if extra != "" {
		extra = "," + extra
	}
	return `{"schema":"tested/run/v1","child_started":false,"capture_complete":true,"files":[{"name":"test_output.jsonl","size":0,"sha256":"` +
		strings.Repeat("0", 64) + `"}` + `]` + extra + `}`
}

func startedEvidenceJSON(extra string) string {
	return strings.Replace(
		evidenceJSON(extra),
		`"child_started":false`,
		`"child_started":true`,
		1,
	)
}

func TestDecodeBoundsInput(t *testing.T) {
	input := strings.Repeat(" ", int(MaximumBytes)+1)
	if _, err := Decode(strings.NewReader(input)); err == nil {
		t.Fatal("Decode(oversized) error = nil, want error")
	}
}

func TestEncodeBoundsOutputAndRejectsShortWrites(t *testing.T) {
	evidence := Evidence{
		Schema:          Schema,
		CaptureComplete: true,
		Files: []File{{
			Name:   "test_output.jsonl",
			SHA256: strings.Repeat("0", 64),
		}},
		Issues: []Issue{{
			Kind:    "oversized",
			Message: strings.Repeat("x", int(MaximumBytes)),
		}},
	}
	var output bytes.Buffer
	if err := Encode(&output, evidence); err == nil {
		t.Fatal("Encode(oversized) error = nil, want error")
	}
	if output.Len() != 0 {
		t.Fatalf("Encode(oversized) wrote %d bytes", output.Len())
	}

	evidence.Issues = nil
	if err := Encode(shortWriter{}, evidence); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Encode(short writer) error = %v, want io.ErrShortWrite", err)
	}
}

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) {
	return len(data) / 2, nil
}

func FuzzDecode(f *testing.F) {
	f.Add(evidenceJSON(""))
	f.Add(strings.Replace(
		evidenceJSON(""),
		`"size":0`,
		`"size":0,"size":1`,
		1,
	))
	f.Add(`{"schema":"tested/run/v1","files":[[[[]]]]}`)
	f.Fuzz(func(t *testing.T, input string) {
		_, _ = Decode(strings.NewReader(input))
	})
}
