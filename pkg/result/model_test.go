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

package result

import (
	"testing"
	"time"

	"github.com/greenpau/tested/pkg/protocol"
)

func TestStatusClassification(t *testing.T) {
	tests := []struct {
		status     Status
		terminal   bool
		successful bool
	}{
		{status: StatusUnknown},
		{status: StatusRunning},
		{status: StatusPaused},
		{status: StatusPassed, terminal: true, successful: true},
		{status: StatusFailed, terminal: true},
		{status: StatusSkipped, terminal: true, successful: true},
		{status: StatusBenchmarked, terminal: true, successful: true},
		{status: StatusIncomplete, terminal: true},
	}
	for _, test := range tests {
		t.Run(string(test.status), func(t *testing.T) {
			if got := test.status.Terminal(); got != test.terminal {
				t.Errorf("Terminal() = %v, want %v", got, test.terminal)
			}
			if got := test.status.Successful(); got != test.successful {
				t.Errorf("Successful() = %v, want %v", got, test.successful)
			}
		})
	}
}

func TestModelHelpers(t *testing.T) {
	id := OccurrenceID{Package: "example.com/p", Name: "TestOne/sub", Ordinal: 3}
	if got := id.String(); got != "example.com/p::TestOne/sub#3" {
		t.Fatalf("String() = %q", got)
	}
	if (Signals{}).Any() {
		t.Fatal("empty Signals.Any() = true")
	}
	if !(Signals{Timeout: true}).Any() {
		t.Fatal("timeout Signals.Any() = false")
	}
}

func TestAnalyzerSnapshotBeforeFinalizeAndTerminalWithoutRun(t *testing.T) {
	analyzer := New()
	start := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	if err := analyzer.Add(testEvent(
		1,
		start,
		protocol.ActionStart,
		"p",
		"",
		"",
		nil,
		"",
	)); err != nil {
		t.Fatalf("Add() start error: %v", err)
	}
	if err := analyzer.Add(testEvent(
		2,
		start.Add(time.Second),
		protocol.ActionOutput,
		"p",
		"TestWithoutRun",
		"panic: final line without newline",
		nil,
		"",
	)); err != nil {
		t.Fatalf("Add() output error: %v", err)
	}
	if err := analyzer.Add(testEvent(
		3,
		start.Add(2*time.Second),
		protocol.ActionPass,
		"p",
		"TestWithoutRun",
		"",
		nil,
		"",
	)); err != nil {
		t.Fatalf("Add() terminal error: %v", err)
	}

	snapshot := analyzer.Snapshot()
	if snapshot.Finalized || !snapshot.Timing.EventEstimated ||
		snapshot.Timing.EventDuration != 2*time.Second {
		t.Fatalf("pre-finalize snapshot = %#v", snapshot)
	}
	test := findTest(t, snapshot.Packages[0], "TestWithoutRun", 1)
	if test.DurationSource != DurationUnknown || test.StartedAt != nil {
		t.Fatalf("terminal-without-run timing = %#v", test)
	}

	final := analyzer.Finalize(RunMetadata{Signal: "terminated"})
	test = findTest(t, final.Packages[0], "TestWithoutRun", 1)
	if !test.Signals.Panic || final.Metadata.Signal != "terminated" {
		t.Fatalf("finalized evidence = test %#v, metadata %#v", test, final.Metadata)
	}
}
