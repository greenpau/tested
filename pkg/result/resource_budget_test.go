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
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/greenpau/tested/pkg/protocol"
)

func TestAnalyzerAggregateOutputBudget(t *testing.T) {
	analyzer, err := NewAnalyzer(AnalyzerOptions{
		MaxTotalOutputBytes: 5,
	})
	if err != nil {
		t.Fatalf("NewAnalyzer() unexpected error: %v", err)
	}
	base := time.Date(2026, time.July, 29, 20, 0, 0, 0, time.UTC)
	events := []protocol.Event{
		testEvent(1, base, protocol.ActionStart, "p", "", "", nil, ""),
		testEvent(2, base.Add(time.Second), protocol.ActionOutput, "p", "", "abc", nil, ""),
		testEvent(3, base.Add(2*time.Second), protocol.ActionRun, "p", "TestOne", "", nil, ""),
		testEvent(4, base.Add(3*time.Second), protocol.ActionOutput, "p", "TestOne", "éé", nil, ""),
		testEvent(5, base.Add(4*time.Second), protocol.ActionPass, "p", "TestOne", "", nil, ""),
		buildEvent(6, protocol.ActionBuildOutput, "p [p.test]", "xyz"),
		buildEvent(7, protocol.ActionBuildFail, "p [p.test]", ""),
		testEvent(8, base.Add(5*time.Second), protocol.ActionPass, "p", "", "", nil, ""),
	}
	for _, event := range events {
		if err := analyzer.Add(event); err != nil {
			t.Fatalf("Add(%q) unexpected error: %v", event.Action(), err)
		}
	}

	got := analyzer.Finalize(RunMetadata{})
	if got.Incomplete ||
		got.TotalOutputBytes != 10 ||
		got.TotalOutputRetainedBytes != 5 ||
		!got.TotalOutputTruncated ||
		got.NormalizedEntries != 6 ||
		got.NormalizedEntriesRetained != 5 ||
		!got.NormalizedEntriesTruncated {
		t.Fatalf("aggregate resource facts = %#v", got)
	}
	if got.Summary.TotalOutputBytes != got.TotalOutputBytes ||
		got.Summary.TotalOutputRetainedBytes != got.TotalOutputRetainedBytes ||
		got.Summary.TotalOutputTruncated != got.TotalOutputTruncated ||
		got.Summary.NormalizedEntries != got.NormalizedEntries ||
		got.Summary.NormalizedEntriesRetained != got.NormalizedEntriesRetained ||
		got.Summary.NormalizedEntriesTruncated != got.NormalizedEntriesTruncated ||
		got.Summary.Incomplete {
		t.Fatalf("summary resource facts = %#v", got.Summary)
	}

	pkg := got.Packages[0]
	if len(pkg.Output) != 1 ||
		pkg.Output[0].Text != "abc" ||
		pkg.OutputRetainedBytes != 3 ||
		pkg.OutputTruncated {
		t.Fatalf("package output = %#v", pkg)
	}
	occurrence := findTest(t, pkg, "TestOne", 1)
	if len(occurrence.Output) != 1 ||
		occurrence.Output[0].Text != "é" ||
		occurrence.OutputBytes != 4 ||
		occurrence.OutputRetainedBytes != 2 ||
		!occurrence.OutputTruncated {
		t.Fatalf("test output = %#v", occurrence)
	}
	if len(got.Builds) != 1 ||
		len(got.Builds[0].Output) != 0 ||
		got.Builds[0].OutputBytes != 3 ||
		got.Builds[0].OutputRetainedBytes != 0 ||
		!got.Builds[0].OutputTruncated {
		t.Fatalf("build output = %#v", got.Builds)
	}
	if got.IntegrityDiagnosticCount != 0 {
		t.Fatalf("output truncation emitted integrity diagnostics: %#v", got.Diagnostics)
	}
}

func TestAnalyzerBenchmarkOccurrencesIgnoreOutputRetention(t *testing.T) {
	const benchmarkLine = "BenchmarkStable 1 9 ns/op\n"
	for _, test := range []struct {
		name                  string
		maxTotalOutputBytes   int64
		wantRetainedBytes     int64
		wantOutputTruncated   bool
		wantRetainedEntryLess bool
	}{
		{
			name:                "unlimited",
			wantRetainedBytes:   2 * int64(len(benchmarkLine)),
			wantOutputTruncated: false,
		},
		{
			name:                  "aggregate output clipped",
			maxTotalOutputBytes:   1,
			wantRetainedBytes:     1,
			wantOutputTruncated:   true,
			wantRetainedEntryLess: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			analyzer, err := NewAnalyzer(AnalyzerOptions{
				MaxTotalOutputBytes: test.maxTotalOutputBytes,
			})
			if err != nil {
				t.Fatalf("NewAnalyzer() unexpected error: %v", err)
			}
			base := time.Date(
				2026,
				time.July,
				29,
				20,
				30,
				0,
				0,
				time.UTC,
			)
			events := []protocol.Event{
				testEvent(
					1,
					base,
					protocol.ActionStart,
					"bench",
					"",
					"",
					nil,
					"",
				),
				testEvent(
					2,
					base,
					protocol.ActionOutput,
					"bench",
					"",
					benchmarkLine,
					nil,
					"",
				),
				testEvent(
					3,
					base,
					protocol.ActionOutput,
					"bench",
					"",
					benchmarkLine,
					nil,
					"",
				),
				testEvent(
					4,
					base,
					protocol.ActionPass,
					"bench",
					"",
					"",
					nil,
					"",
				),
			}
			for _, event := range events {
				if err := analyzer.Add(event); err != nil {
					t.Fatalf("Add(%q) unexpected error: %v", event.Action(), err)
				}
			}

			got := analyzer.Finalize(RunMetadata{})
			if got.Incomplete ||
				len(got.Packages) != 1 ||
				len(got.Packages[0].Tests) != 2 ||
				got.Summary.Tests.Benchmarked != 2 ||
				got.TotalOutputRetainedBytes != test.wantRetainedBytes ||
				got.TotalOutputTruncated != test.wantOutputTruncated {
				t.Fatalf("benchmark projection = %#v", got)
			}
			if test.wantRetainedEntryLess &&
				got.NormalizedEntriesRetained >= got.NormalizedEntries {
				t.Fatalf("clipped benchmark entry facts = %#v", got)
			}
		})
	}
}

func TestAnalyzerResultEntryBudgetStopsSemanticGrowth(t *testing.T) {
	const limit = int64(4)
	analyzer, err := NewAnalyzer(AnalyzerOptions{
		MaxResultEntries: limit,
	})
	if err != nil {
		t.Fatalf("NewAnalyzer() unexpected error: %v", err)
	}
	base := time.Date(2026, time.July, 29, 21, 0, 0, 0, time.UTC)
	overflow := metadataEvent(
		5,
		5,
		base.Add(4*time.Second),
		protocol.ActionArtifacts,
		"p",
		"TestOne",
		"",
		"",
		"/omitted",
	)
	overflow.Raw = []byte(strings.Repeat("x", 4096))
	events := []protocol.Event{
		testEvent(1, base, protocol.ActionStart, "p", "", "", nil, ""),
		testEvent(2, base.Add(time.Second), protocol.ActionOutput, "p", "", "one", nil, ""),
		testEvent(3, base.Add(2*time.Second), protocol.ActionRun, "p", "TestOne", "", nil, ""),
		metadataEvent(
			4,
			4,
			base.Add(3*time.Second),
			protocol.ActionAttr,
			"p",
			"TestOne",
			"kept",
			"yes",
			"",
		),
		overflow,
		testEvent(6, base.Add(5*time.Second), protocol.ActionOutput, "p", "TestOne", "two", nil, ""),
		testEvent(7, base.Add(6*time.Second), protocol.ActionPass, "p", "TestOne", "", nil, ""),
		testEvent(8, base.Add(7*time.Second), protocol.ActionOutput, "p", "", "three", nil, ""),
		testEvent(9, base.Add(8*time.Second), protocol.ActionPass, "p", "", "", nil, ""),
	}
	for _, event := range events {
		if err := analyzer.Add(event); err != nil {
			t.Fatalf("Add(%q) unexpected error: %v", event.Action(), err)
		}
	}

	sequence := uint64(10)
	for i := 0; i < 128; i++ {
		if err := analyzer.Add(testEvent(
			sequence,
			base,
			protocol.ActionRun,
			"p",
			fmt.Sprintf("TestDropped%d", i),
			"",
			nil,
			"",
		)); err != nil {
			t.Fatalf("Add(dropped test %d) unexpected error: %v", i, err)
		}
		sequence++
	}
	for i := 0; i < 128; i++ {
		if err := analyzer.Add(testEvent(
			sequence,
			base,
			protocol.ActionStart,
			fmt.Sprintf("dropped/package/%d", i),
			"",
			"",
			nil,
			"",
		)); err != nil {
			t.Fatalf("Add(dropped package %d) unexpected error: %v", i, err)
		}
		sequence++
	}
	for i := 0; i < 128; i++ {
		if err := analyzer.Add(buildEvent(
			sequence,
			protocol.ActionBuildOutput,
			fmt.Sprintf("dropped-build-%d", i),
			"x",
		)); err != nil {
			t.Fatalf("Add(dropped build %d) unexpected error: %v", i, err)
		}
		sequence++
	}
	for i := 0; i < 128; i++ {
		if err := analyzer.Add(protocol.Event{
			Sequence: sequence,
			Line:     sequence,
			Kind:     protocol.EventKindUnknown,
			Unknown: &protocol.UnknownEvent{
				Action: fmt.Sprintf("future-%d", i),
			},
		}); err != nil {
			t.Fatalf("Add(dropped unknown action %d) unexpected error: %v", i, err)
		}
		sequence++
	}

	if len(analyzer.packages) != 1 ||
		len(analyzer.builds) != 0 ||
		len(analyzer.ordinals) != 1 ||
		len(analyzer.latest) != 1 ||
		len(analyzer.unknownActions) != 0 {
		t.Fatalf(
			"bounded analyzer maps = packages %d, builds %d, ordinals %d, latest %d, unknown %d",
			len(analyzer.packages),
			len(analyzer.builds),
			len(analyzer.ordinals),
			len(analyzer.latest),
			len(analyzer.unknownActions),
		)
	}

	got := analyzer.Finalize(RunMetadata{})
	if !got.Incomplete ||
		!got.Summary.Incomplete ||
		!got.NormalizedEntriesTruncated ||
		!got.Summary.NormalizedEntriesTruncated ||
		got.NormalizedEntriesRetained != uint64(limit) ||
		got.Summary.NormalizedEntriesRetained != uint64(limit) ||
		got.NormalizedEntries != 647 {
		t.Fatalf("entry budget facts = %#v", got)
	}
	if got.TotalOutputBytes != 139 ||
		got.TotalOutputRetainedBytes != 3 ||
		!got.TotalOutputTruncated {
		t.Fatalf("entry-limited output facts = %#v", got)
	}
	if len(got.Packages) != 1 ||
		len(got.Packages[0].Tests) != 1 ||
		len(got.Packages[0].Attributes) != 0 ||
		len(got.Packages[0].Tests[0].Attributes) != 1 ||
		len(got.Packages[0].Tests[0].Artifacts) != 0 ||
		len(got.Builds) != 0 ||
		len(got.UnknownActions) != 0 {
		t.Fatalf("retained semantic entries = %#v", got)
	}
	if got.Packages[0].Status != StatusPassed ||
		got.Packages[0].Tests[0].Status != StatusPassed ||
		got.Packages[0].OutputBytes != 8 ||
		got.Packages[0].OutputRetainedBytes != 3 ||
		!got.Packages[0].OutputTruncated ||
		got.Packages[0].Tests[0].OutputBytes != 3 ||
		got.Packages[0].Tests[0].OutputRetainedBytes != 0 ||
		!got.Packages[0].Tests[0].OutputTruncated {
		t.Fatalf("existing entity completion and output = %#v", got.Packages[0])
	}

	capacityDiagnostics := 0
	for _, diagnostic := range got.Diagnostics {
		if strings.Contains(
			diagnostic.Message,
			"normalized result entry capacity",
		) {
			capacityDiagnostics++
			if diagnostic.Kind != protocol.DiagnosticIntegrity ||
				diagnostic.Sequence != 5 ||
				len(diagnostic.Preview) > protocol.DefaultMaxDiagnosticBytes ||
				!diagnostic.Truncated {
				t.Fatalf("capacity diagnostic = %#v", diagnostic)
			}
		}
	}
	if capacityDiagnostics != 1 ||
		got.IntegrityDiagnosticCount != 1 ||
		got.Summary.IntegrityDiagnostics != 1 {
		t.Fatalf("capacity diagnostics = %#v", got.Diagnostics)
	}
}

func TestAnalyzerUnlimitedAggregateBudgetsAndSnapshotFacts(t *testing.T) {
	analyzer, err := NewAnalyzer(AnalyzerOptions{
		MaxTotalOutputBytes: 0,
		MaxResultEntries:    0,
	})
	if err != nil {
		t.Fatalf("NewAnalyzer() unexpected error: %v", err)
	}
	base := time.Date(2026, time.July, 29, 22, 0, 0, 0, time.UTC)
	events := []protocol.Event{
		testEvent(1, base, protocol.ActionStart, "p", "", "", nil, ""),
		testEvent(2, base.Add(time.Second), protocol.ActionRun, "p", "TestOne", "", nil, ""),
		metadataEvent(3, 3, base.Add(2*time.Second), protocol.ActionAttr, "p", "TestOne", "k", "v", ""),
		metadataEvent(4, 4, base.Add(3*time.Second), protocol.ActionArtifacts, "p", "TestOne", "", "", "/a"),
		testEvent(5, base.Add(4*time.Second), protocol.ActionOutput, "p", "TestOne", "abc", nil, ""),
		testEvent(6, base.Add(5*time.Second), protocol.ActionPass, "p", "TestOne", "", nil, ""),
		testEvent(7, base.Add(6*time.Second), protocol.ActionPass, "p", "", "", nil, ""),
	}
	for _, event := range events {
		if err := analyzer.Add(event); err != nil {
			t.Fatalf("Add(%q) unexpected error: %v", event.Action(), err)
		}
	}

	first := analyzer.Finalize(RunMetadata{})
	if first.Incomplete ||
		first.TotalOutputBytes != 3 ||
		first.TotalOutputRetainedBytes != 3 ||
		first.TotalOutputTruncated ||
		first.NormalizedEntries != 5 ||
		first.NormalizedEntriesRetained != 5 ||
		first.NormalizedEntriesTruncated {
		t.Fatalf("unlimited resource facts = %#v", first)
	}
	first.TotalOutputBytes = 0
	first.TotalOutputRetainedBytes = 0
	first.NormalizedEntries = 0
	first.NormalizedEntriesRetained = 0
	first.Summary.TotalOutputBytes = 0
	first.Summary.NormalizedEntries = 0

	fresh := analyzer.Snapshot()
	if fresh.TotalOutputBytes != 3 ||
		fresh.TotalOutputRetainedBytes != 3 ||
		fresh.NormalizedEntries != 5 ||
		fresh.NormalizedEntriesRetained != 5 ||
		fresh.Summary.TotalOutputBytes != 3 ||
		fresh.Summary.NormalizedEntries != 5 {
		t.Fatalf("snapshot aggregate facts aliased prior snapshot: %#v", fresh)
	}
}

func TestAnalyzerNormalizedStringBudgetBoundsMetadataAndIdentities(t *testing.T) {
	const (
		normalizedLimit = int64(22)
		metadataEvents  = 64
	)
	analyzer, err := NewAnalyzer(AnalyzerOptions{
		MaxResultEntries:   1 << 30,
		MaxNormalizedBytes: normalizedLimit,
	})
	if err != nil {
		t.Fatalf("NewAnalyzer() unexpected error: %v", err)
	}
	base := time.Date(2026, time.July, 29, 23, 0, 0, 0, time.UTC)
	events := []protocol.Event{
		testEvent(1, base, protocol.ActionStart, "pkg", "", "", nil, ""),
		testEvent(2, base.Add(time.Second), protocol.ActionRun, "pkg", "T", "", nil, ""),
		metadataEvent(3, 3, base.Add(2*time.Second), protocol.ActionAttr, "pkg", "T", "key", "value", ""),
		metadataEvent(4, 4, base.Add(3*time.Second), protocol.ActionArtifacts, "pkg", "T", "", "", "xx"),
	}
	for _, event := range events {
		if err := analyzer.Add(event); err != nil {
			t.Fatalf("Add(%q) unexpected error: %v", event.Action(), err)
		}
	}

	huge := strings.Repeat("h", 1<<20)
	sequence := uint64(5)
	for i := 0; i < metadataEvents; i++ {
		if err := analyzer.Add(metadataEvent(
			sequence,
			sequence,
			base,
			protocol.ActionAttr,
			"pkg",
			"T",
			huge,
			huge,
			"",
		)); err != nil {
			t.Fatalf("Add(large metadata %d) unexpected error: %v", i, err)
		}
		sequence++
	}
	if err := analyzer.Add(testEvent(
		sequence,
		base,
		protocol.ActionOutput,
		"pkg",
		"T",
		"payload",
		nil,
		"",
	)); err != nil {
		t.Fatalf("Add(output) unexpected error: %v", err)
	}
	sequence++
	if err := analyzer.Add(testEvent(
		sequence,
		base,
		protocol.ActionPass,
		"pkg",
		"T",
		"",
		nil,
		"",
	)); err != nil {
		t.Fatalf("Add(test pass) unexpected error: %v", err)
	}
	sequence++
	if err := analyzer.Add(testEvent(
		sequence,
		base,
		protocol.ActionFail,
		"pkg",
		"",
		"",
		nil,
		"zz",
	)); err != nil {
		t.Fatalf("Add(package fail) unexpected error: %v", err)
	}
	sequence++
	if err := analyzer.AddRecord(protocol.Record{
		Sequence: sequence,
		Line:     sequence,
		Diagnostic: &protocol.Diagnostic{
			Kind:     protocol.DiagnosticMalformed,
			Sequence: sequence,
			Line:     sequence,
			Preview:  huge,
			Message:  huge,
		},
	}); err != nil {
		t.Fatalf("AddRecord(large diagnostic) unexpected error: %v", err)
	}

	got := analyzer.Finalize(RunMetadata{})
	if !got.Incomplete ||
		!got.NormalizedEntriesTruncated ||
		got.NormalizedEntries != metadataEvents+5 ||
		got.NormalizedEntriesRetained != 3 ||
		!got.NormalizedBytesTruncated ||
		got.NormalizedBytes <= normalizedLimit ||
		got.NormalizedBytesRetained != normalizedLimit {
		t.Fatalf("normalized byte facts = %#v", got)
	}
	if got.Summary.NormalizedBytes != got.NormalizedBytes ||
		got.Summary.NormalizedBytesRetained != got.NormalizedBytesRetained ||
		!got.Summary.NormalizedBytesTruncated ||
		!got.Summary.Incomplete {
		t.Fatalf("normalized summary facts = %#v", got.Summary)
	}
	if len(got.Packages) != 1 ||
		got.Packages[0].FailedBuild != "" ||
		len(got.Packages[0].Tests) != 1 ||
		len(got.Packages[0].Tests[0].Attributes) != 1 ||
		len(got.Packages[0].Tests[0].Artifacts) != 0 ||
		len(got.Packages[0].Tests[0].Output) != 0 ||
		got.Packages[0].Tests[0].OutputBytes != int64(len("payload")) ||
		!got.Packages[0].Tests[0].OutputTruncated {
		t.Fatalf("normalized-byte-limited model = %#v", got.Packages)
	}
	if got.TotalOutputBytes != int64(len("payload")) ||
		got.TotalOutputRetainedBytes != 0 ||
		!got.TotalOutputTruncated {
		t.Fatalf("normalized-byte-limited output = %#v", got)
	}

	resourceDiagnostics := 0
	largeDiagnosticFound := false
	for _, diagnostic := range got.Diagnostics {
		if strings.Contains(
			diagnostic.Message,
			"normalized-string byte capacity",
		) {
			resourceDiagnostics++
		}
		if diagnostic.Kind == protocol.DiagnosticMalformed {
			largeDiagnosticFound = true
			if len(diagnostic.Message) > diagnosticMessageBytes ||
				len(diagnostic.Preview) > protocol.DefaultMaxDiagnosticBytes ||
				!diagnostic.Truncated {
				t.Fatalf("bounded external diagnostic = %#v", diagnostic)
			}
		}
	}
	if resourceDiagnostics != 1 ||
		got.IntegrityDiagnosticCount != 1 ||
		!largeDiagnosticFound {
		t.Fatalf("normalized capacity diagnostics = %#v", got.Diagnostics)
	}
}

func TestAnalyzerTestMetadataCountsDuplicatedPackageIdentity(t *testing.T) {
	analyzer, err := NewAnalyzer(AnalyzerOptions{
		MaxNormalizedBytes: 12,
	})
	if err != nil {
		t.Fatalf("NewAnalyzer() unexpected error: %v", err)
	}
	base := time.Date(2026, time.July, 29, 23, 30, 0, 0, time.UTC)
	events := []protocol.Event{
		testEvent(1, base, protocol.ActionStart, "p", "", "", nil, ""),
		testEvent(2, base, protocol.ActionRun, "p", "T", "", nil, ""),
		metadataEvent(
			3,
			3,
			base,
			protocol.ActionAttr,
			"p",
			"T",
			"k",
			"v",
			"",
		),
		metadataEvent(
			4,
			4,
			base,
			protocol.ActionArtifacts,
			"p",
			"T",
			"",
			"",
			"x",
		),
		testEvent(5, base, protocol.ActionPass, "p", "T", "", nil, ""),
		testEvent(6, base, protocol.ActionPass, "p", "", "", nil, ""),
	}
	for _, event := range events {
		if err := analyzer.Add(event); err != nil {
			t.Fatalf("Add(%q) unexpected error: %v", event.Action(), err)
		}
	}

	got := analyzer.Finalize(RunMetadata{})
	if got.Incomplete ||
		got.NormalizedEntries != 4 ||
		got.NormalizedEntriesRetained != 4 ||
		got.NormalizedBytes != 12 ||
		got.NormalizedBytesRetained != 12 ||
		got.NormalizedBytesTruncated ||
		len(got.Packages) != 1 ||
		len(got.Packages[0].Tests) != 1 ||
		len(got.Packages[0].Tests[0].Attributes) != 1 ||
		len(got.Packages[0].Tests[0].Artifacts) != 1 {
		t.Fatalf("test metadata field-byte accounting = %#v", got)
	}
}

func TestAnalyzerNormalizedStringBudgetBoundsRunMetadata(t *testing.T) {
	analyzer, err := NewAnalyzer(AnalyzerOptions{
		MaxNormalizedBytes: 10,
	})
	if err != nil {
		t.Fatalf("NewAnalyzer() unexpected error: %v", err)
	}
	got := analyzer.Finalize(RunMetadata{
		Command: []string{"go", "test"},
		WorkDir: "/work",
		Signal:  "TERM",
	})
	if !got.Incomplete ||
		got.NormalizedEntries != 2 ||
		got.NormalizedEntriesRetained != 2 ||
		got.NormalizedEntriesTruncated ||
		got.NormalizedBytes != 15 ||
		got.NormalizedBytesRetained != 10 ||
		!got.NormalizedBytesTruncated {
		t.Fatalf("run metadata budget facts = %#v", got)
	}
	if len(got.Metadata.Command) != 2 ||
		got.Metadata.Command[0] != "go" ||
		got.Metadata.Command[1] != "test" ||
		got.Metadata.WorkDir != "" ||
		got.Metadata.Signal != "TERM" {
		t.Fatalf("bounded run metadata = %#v", got.Metadata)
	}
	if got.IntegrityDiagnosticCount != 1 ||
		len(got.Diagnostics) != 1 ||
		!strings.Contains(
			got.Diagnostics[0].Message,
			"normalized-string byte capacity",
		) {
		t.Fatalf("run metadata capacity diagnostic = %#v", got.Diagnostics)
	}
}

func TestAnalyzerRunCommandArgumentsUseAtomicBudgets(t *testing.T) {
	emptyArguments := make([]string, 1024)
	tests := []struct {
		name                         string
		options                      AnalyzerOptions
		command                      []string
		wantCommand                  []string
		wantEntries                  uint64
		wantEntriesRetained          uint64
		wantNormalizedBytes          int64
		wantNormalizedBytesRetained  int64
		wantNormalizedBytesTruncated bool
	}{
		{
			name:                "empty arguments consume entry capacity",
			options:             AnalyzerOptions{MaxResultEntries: 2},
			command:             emptyArguments,
			wantCommand:         []string{"", ""},
			wantEntries:         uint64(len(emptyArguments)),
			wantEntriesRetained: 2,
		},
		{
			name: "string exhaustion omits the remaining suffix",
			options: AnalyzerOptions{
				MaxResultEntries:   10,
				MaxNormalizedBytes: 1,
			},
			command:                      []string{"a", "bc", ""},
			wantCommand:                  []string{"a"},
			wantEntries:                  3,
			wantEntriesRetained:          1,
			wantNormalizedBytes:          3,
			wantNormalizedBytesRetained:  1,
			wantNormalizedBytesTruncated: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			analyzer, err := NewAnalyzer(test.options)
			if err != nil {
				t.Fatalf("NewAnalyzer() unexpected error: %v", err)
			}
			got := analyzer.Finalize(RunMetadata{
				Command: test.command,
			})
			if !got.Incomplete ||
				!got.NormalizedEntriesTruncated ||
				got.NormalizedEntries != test.wantEntries ||
				got.NormalizedEntriesRetained !=
					test.wantEntriesRetained ||
				got.NormalizedBytes != test.wantNormalizedBytes ||
				got.NormalizedBytesRetained !=
					test.wantNormalizedBytesRetained ||
				got.NormalizedBytesTruncated !=
					test.wantNormalizedBytesTruncated ||
				!slices.Equal(got.Metadata.Command, test.wantCommand) {
				t.Fatalf("bounded run command = %#v", got)
			}
			if got.IntegrityDiagnosticCount != 1 {
				t.Fatalf(
					"run command capacity diagnostics = %#v",
					got.Diagnostics,
				)
			}
		})
	}
}

func TestAnalyzerDroppedOutputObservesEveryIdentityField(t *testing.T) {
	base := time.Date(2026, time.July, 30, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name                string
		event               protocol.Event
		wantNormalizedBytes int64
	}{
		{
			name: "package output",
			event: testEvent(
				2,
				base,
				protocol.ActionOutput,
				"pkg",
				"",
				"x",
				nil,
				"",
			),
			wantNormalizedBytes: 7,
		},
		{
			name: "test output",
			event: testEvent(
				2,
				base,
				protocol.ActionOutput,
				"pkg",
				"T",
				"x",
				nil,
				"",
			),
			wantNormalizedBytes: 11,
		},
		{
			name: "build output",
			event: buildEvent(
				2,
				protocol.ActionBuildOutput,
				"imp",
				"x",
			),
			wantNormalizedBytes: 7,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			analyzer, err := NewAnalyzer(AnalyzerOptions{
				MaxResultEntries: 1,
			})
			if err != nil {
				t.Fatalf("NewAnalyzer() unexpected error: %v", err)
			}
			if err := analyzer.Add(testEvent(
				1,
				base,
				protocol.ActionStart,
				"k",
				"",
				"",
				nil,
				"",
			)); err != nil {
				t.Fatalf("Add(filler) unexpected error: %v", err)
			}
			if err := analyzer.Add(test.event); err != nil {
				t.Fatalf("Add(output) unexpected error: %v", err)
			}

			got := analyzer.Finalize(RunMetadata{})
			if !got.Incomplete ||
				got.NormalizedEntries != 3 ||
				got.NormalizedEntriesRetained != 1 ||
				!got.NormalizedEntriesTruncated ||
				got.NormalizedBytes != test.wantNormalizedBytes ||
				got.NormalizedBytesRetained != 1 ||
				!got.NormalizedBytesTruncated ||
				got.TotalOutputBytes != 1 ||
				got.TotalOutputRetainedBytes != 0 ||
				!got.TotalOutputTruncated {
				t.Fatalf("dropped output accounting = %#v", got)
			}
		})
	}
}

func TestAnalyzerIgnoresEmptyOutputRegardlessOfLimits(t *testing.T) {
	tests := []struct {
		name    string
		options AnalyzerOptions
	}{
		{name: "unlimited"},
		{
			name: "per scope",
			options: AnalyzerOptions{
				MaxOutputBytes: 1,
			},
		},
		{
			name: "aggregate",
			options: AnalyzerOptions{
				MaxTotalOutputBytes: 1,
			},
		},
		{
			name: "both",
			options: AnalyzerOptions{
				MaxOutputBytes:      1,
				MaxTotalOutputBytes: 1,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			analyzer, err := NewAnalyzer(test.options)
			if err != nil {
				t.Fatalf("NewAnalyzer() unexpected error: %v", err)
			}
			base := time.Date(2026, time.July, 30, 1, 0, 0, 0, time.UTC)
			events := []protocol.Event{
				testEvent(
					1,
					base,
					protocol.ActionStart,
					"p",
					"",
					"",
					nil,
					"",
				),
				testEvent(
					2,
					base,
					protocol.ActionOutput,
					"p",
					"",
					"",
					nil,
					"",
				),
				testEvent(
					3,
					base,
					protocol.ActionPass,
					"p",
					"",
					"",
					nil,
					"",
				),
			}
			for _, event := range events {
				if err := analyzer.Add(event); err != nil {
					t.Fatalf(
						"Add(%q) unexpected error: %v",
						event.Action(),
						err,
					)
				}
			}

			got := analyzer.Finalize(RunMetadata{})
			if got.Incomplete ||
				got.TotalOutputBytes != 0 ||
				got.TotalOutputRetainedBytes != 0 ||
				got.TotalOutputTruncated ||
				got.NormalizedEntries != 1 ||
				got.NormalizedEntriesRetained != 1 ||
				got.NormalizedEntriesTruncated ||
				len(got.Packages) != 1 ||
				len(got.Packages[0].Output) != 0 {
				t.Fatalf("empty output accounting = %#v", got)
			}
		})
	}
}

func TestValidUTF8PrefixBytes(t *testing.T) {
	tests := []struct {
		name  string
		value string
		limit int
		want  int
	}{
		{name: "empty", value: "", limit: 10, want: 0},
		{name: "negative limit", value: "abc", limit: -1, want: 0},
		{name: "ascii limit", value: "abc", limit: 2, want: 2},
		{name: "limit beyond value", value: "abc", limit: 10, want: 3},
		{name: "split multibyte rune", value: "éx", limit: 1, want: 0},
		{name: "complete multibyte rune", value: "éx", limit: 2, want: 2},
		{
			name:  "invalid byte stops prefix",
			value: string([]byte{'a', 0xff, 'b'}),
			limit: 3,
			want:  1,
		},
		{
			name:  "incomplete trailing rune stops prefix",
			value: string([]byte{'a', 0xe2, 0x82}),
			limit: 3,
			want:  1,
		},
		{
			name:  "encoded replacement rune is valid",
			value: "\uFFFDx",
			limit: 3,
			want:  3,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validUTF8PrefixBytes(
				test.value,
				test.limit,
			); got != test.want {
				t.Fatalf(
					"validUTF8PrefixBytes(%q, %d) = %d, want %d",
					test.value,
					test.limit,
					got,
					test.want,
				)
			}
		})
	}
}

func TestNewAnalyzerRejectsNegativeBudgets(t *testing.T) {
	tests := []struct {
		name    string
		options AnalyzerOptions
	}{
		{
			name:    "per-scope output",
			options: AnalyzerOptions{MaxOutputBytes: -1},
		},
		{
			name:    "aggregate output",
			options: AnalyzerOptions{MaxTotalOutputBytes: -1},
		},
		{
			name:    "result entries",
			options: AnalyzerOptions{MaxResultEntries: -1},
		},
		{
			name:    "normalized strings",
			options: AnalyzerOptions{MaxNormalizedBytes: -1},
		},
		{
			name:    "semantic line",
			options: AnalyzerOptions{MaxSemanticLineBytes: -1},
		},
		{
			name:    "diagnostics",
			options: AnalyzerOptions{MaxDiagnostics: -1},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewAnalyzer(test.options); err == nil {
				t.Fatalf("NewAnalyzer(%#v) accepted negative budget", test.options)
			}
		})
	}
}
