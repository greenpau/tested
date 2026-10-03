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
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/greenpau/tested/pkg/protocol"
)

func TestProgressPreservesNormalizedEvidence(t *testing.T) {
	lines := []string{
		`{"Action":"start","Package":"p"}`,
		`{"Action":"run","Package":"p","Test":"TestParent/child"}`,
		`{"Action":"output","Package":"p","Test":"TestParent/child","Output":"hello\n"}`,
		`{"Action":"pause","Package":"p","Test":"TestParent/child"}`,
		`{"Action":"cont","Package":"p","Test":"TestParent/child"}`,
		`{"Action":"pass","Package":"p","Test":"TestParent/child","Elapsed":0.25}`,
		`{"Action":"run","Package":"p","Test":"TestParent/child"}`,
		`{"Action":"skip","Package":"p","Test":"TestParent/child"}`,
		`{"Action":"output","Package":"p","Output":"BenchmarkWork-8 1 2 ns/op\n"}`,
		`{"Action":"build-output","ImportPath":"dependency","Output":"compiler detail\n"}`,
		`{"Action":"build-fail","ImportPath":"dependency"}`,
		`{"Action":"future","Output":"unattributed detail\n"}`,
		`not JSON`,
		`{"Action":"pass","Package":"p"}`,
	}
	for _, limit := range []int64{0, 3} {
		t.Run(string(rune('0'+limit)), func(t *testing.T) {
			live, _ := NewAnalyzer(AnalyzerOptions{MaxOutputBytes: limit})
			reference, _ := NewAnalyzer(AnalyzerOptions{MaxOutputBytes: limit})
			var updates []Progress
			_, err := protocol.Read(strings.NewReader(strings.Join(lines, "\n")+"\n"), protocol.StreamOptions{Handler: func(record protocol.Record) {
				if err := reference.AddRecord(record); err != nil {
					t.Fatal(err)
				}
				update, err := live.AddRecordWithProgress(record)
				if err != nil {
					t.Fatal(err)
				}
				updates = append(updates, update)
			}})
			if err != nil {
				t.Fatal(err)
			}
			if updates[1].Test.Ordinal != 1 || updates[6].Test.Ordinal != 2 {
				t.Fatalf("occurrences lost: %#v", updates)
			}
			if updates[3].Status != StatusPaused || updates[4].Status != StatusRunning || updates[5].Status != StatusPassed || updates[5].Elapsed != 250*time.Millisecond {
				t.Fatalf("lifecycle lost: %#v", updates)
			}
			if updates[8].Scope != OutputTest || updates[8].Test.Name != "BenchmarkWork-8" || updates[8].Status != StatusBenchmarked {
				t.Fatalf("benchmark attribution: %#v", updates[8])
			}
			if updates[9].Scope != OutputBuild || updates[10].Status != StatusFailed {
				t.Fatalf("build attribution: %#v", updates[9:11])
			}
			if updates[11].Scope != OutputUnattributed || updates[12].Diagnostics == 0 {
				t.Fatalf("unknown evidence: %#v", updates[11:13])
			}
			if limit == 3 {
				if updates[2].Output != "hel" || updates[2].OutputBytes != 6 || !updates[2].OutputTruncated {
					t.Fatalf("retained output: %#v", updates[2])
				}
			} else if updates[2].Output != "hello\n" || updates[2].OutputTruncated {
				t.Fatalf("output: %#v", updates[2])
			}
			if counts := updates[13].Tests; counts == nil || counts.Total != 3 || counts.Benchmarked != 1 || counts.Skipped != 1 {
				t.Fatalf("package counts: %#v", counts)
			}
			updates[1].Test.Name = "mutated"
			metadata := RunMetadata{ExitCode: 1}
			if got, want := live.Finalize(metadata), reference.Finalize(metadata); !reflect.DeepEqual(got, want) {
				t.Fatalf("progress changed evidence:\ngot %#v\nwant %#v", got, want)
			}
			if _, err := live.AddRecordWithProgress(protocol.Record{}); !errors.Is(err, ErrFinalized) {
				t.Fatalf("finalized error: %v", err)
			}
		})
	}
}

func TestProgressDoesNotClaimInvalidTransitionPassed(t *testing.T) {
	analyzer := New()
	event := testEvent(1, time.Time{}, protocol.ActionPass, "p", "TestOrphan", "", nil, "")
	update, err := analyzer.AddRecordWithProgress(protocol.Record{Sequence: 1, Event: &event})
	if err != nil {
		t.Fatal(err)
	}
	if update.Status != StatusIncomplete || update.Diagnostics == 0 {
		t.Fatalf("invalid transition appeared successful: %#v", update)
	}
}

func TestProgressFragmentedBenchmarksAndBudgets(t *testing.T) {
	data, err := os.ReadFile("testdata/go1.26-benchmark.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		options AnalyzerOptions
	}{
		{name: "complete"},
		{name: "output", options: AnalyzerOptions{MaxOutputBytes: 5, MaxTotalOutputBytes: 10}},
		{name: "entries", options: AnalyzerOptions{MaxResultEntries: 2}},
		{name: "identities", options: AnalyzerOptions{MaxNormalizedBytes: 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			live, _ := NewAnalyzer(test.options)
			reference, _ := NewAnalyzer(test.options)
			updates := make(map[uint64]Progress)
			_, err := protocol.Read(strings.NewReader(string(data)), protocol.StreamOptions{Handler: func(record protocol.Record) {
				if err := reference.AddRecord(record); err != nil {
					t.Fatal(err)
				}
				update, err := live.AddRecordWithProgress(record)
				if err != nil {
					t.Fatal(err)
				}
				updates[record.Sequence] = update
			}})
			if err != nil {
				t.Fatal(err)
			}
			got, want := live.Finalize(RunMetadata{}), reference.Finalize(RunMetadata{})
			if !reflect.DeepEqual(got, want) {
				t.Fatal("streaming changed normalized evidence")
			}
			for _, pkg := range got.Packages {
				for _, test := range pkg.Tests {
					for _, output := range test.Output {
						update := updates[output.Sequence]
						if !reflect.DeepEqual(update.Test, output.Test) || update.Output != output.Text || update.OutputBytes != output.OriginalBytes {
							t.Fatalf("chunk attributed incorrectly:\nupdate %#v\noutput %#v", update, output)
						}
					}
				}
			}
		})
	}
}
