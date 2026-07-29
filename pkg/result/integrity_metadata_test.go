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
	"strings"
	"testing"
	"time"

	"github.com/greenpau/tested/pkg/protocol"
)

func TestAnalyzerImpossibleOccurrenceTransitions(t *testing.T) {
	base := time.Date(2026, time.July, 29, 16, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		actions []string
	}{
		{name: "orphan pass", actions: []string{protocol.ActionPass}},
		{name: "orphan fail", actions: []string{protocol.ActionFail}},
		{name: "orphan skip", actions: []string{protocol.ActionSkip}},
		{name: "orphan benchmark", actions: []string{protocol.ActionBench}},
		{name: "orphan pause", actions: []string{protocol.ActionPause}},
		{name: "orphan continue", actions: []string{protocol.ActionCont}},
		{
			name: "repeated active run",
			actions: []string{
				protocol.ActionRun,
				protocol.ActionRun,
				protocol.ActionPass,
			},
		},
		{
			name: "duplicate benchmark terminal",
			actions: []string{
				protocol.ActionRun,
				protocol.ActionBench,
				protocol.ActionBench,
			},
		},
		{
			name: "duplicate terminal",
			actions: []string{
				protocol.ActionRun,
				protocol.ActionPass,
				protocol.ActionPass,
			},
		},
		{
			name: "conflicting terminal",
			actions: []string{
				protocol.ActionRun,
				protocol.ActionPass,
				protocol.ActionFail,
			},
		},
		{
			name: "duplicate pause",
			actions: []string{
				protocol.ActionRun,
				protocol.ActionPause,
				protocol.ActionPause,
				protocol.ActionCont,
				protocol.ActionPass,
			},
		},
		{
			name: "continue while running",
			actions: []string{
				protocol.ActionRun,
				protocol.ActionCont,
				protocol.ActionPass,
			},
		},
		{
			name: "terminal while paused",
			actions: []string{
				protocol.ActionRun,
				protocol.ActionPause,
				protocol.ActionPass,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			analyzer := New()
			if err := analyzer.Add(testEvent(
				1,
				base,
				protocol.ActionStart,
				"example.com/transitions",
				"",
				"",
				nil,
				"",
			)); err != nil {
				t.Fatalf("Add(package start) unexpected error: %v", err)
			}
			for i, action := range test.actions {
				sequence := uint64(i + 2)
				event := testEvent(
					sequence,
					base.Add(time.Duration(i+1)*time.Second),
					action,
					"example.com/transitions",
					"TestOne",
					"",
					nil,
					"",
				)
				event.Raw = []byte(`{"Action":"` + action + `"}`)
				if err := analyzer.Add(event); err != nil {
					t.Fatalf("Add(%q) unexpected error: %v", action, err)
				}
			}
			packageSequence := uint64(len(test.actions) + 2)
			if err := analyzer.Add(testEvent(
				packageSequence,
				base.Add(time.Duration(packageSequence)*time.Second),
				protocol.ActionPass,
				"example.com/transitions",
				"",
				"",
				nil,
				"",
			)); err != nil {
				t.Fatalf("Add(package pass) unexpected error: %v", err)
			}

			got := analyzer.Finalize(RunMetadata{})
			if len(got.Packages) != 1 || len(got.Packages[0].Tests) != 1 {
				t.Fatalf("result entities = %#v", got)
			}
			occurrence := got.Packages[0].Tests[0]
			if occurrence.ID.Ordinal != 1 ||
				occurrence.Status != StatusIncomplete ||
				occurrence.IncompleteReason == "" {
				t.Fatalf("corrupt occurrence = %#v", occurrence)
			}
			if got.IntegrityDiagnosticCount != 1 ||
				got.Summary.IntegrityDiagnostics != 1 ||
				len(got.Diagnostics) != 1 ||
				got.Diagnostics[0].Kind != protocol.DiagnosticIntegrity ||
				!strings.Contains(got.Diagnostics[0].Message, "TestOne") {
				t.Fatalf("integrity diagnostics = %#v", got.Diagnostics)
			}
		})
	}
}

func TestAnalyzerOnlyRunAfterTerminalAllocatesRetry(t *testing.T) {
	analyzer := New()
	base := time.Date(2026, time.July, 29, 16, 30, 0, 0, time.UTC)
	events := []protocol.Event{
		testEvent(1, base, protocol.ActionStart, "p", "", "", nil, ""),
		testEvent(2, base.Add(time.Second), protocol.ActionRun, "p", "TestRetry", "", nil, ""),
		testEvent(3, base.Add(2*time.Second), protocol.ActionPass, "p", "TestRetry", "", nil, ""),
		testEvent(4, base.Add(3*time.Second), protocol.ActionFail, "p", "TestRetry", "", nil, ""),
		testEvent(5, base.Add(4*time.Second), protocol.ActionPause, "p", "TestRetry", "", nil, ""),
		testEvent(6, base.Add(5*time.Second), protocol.ActionRun, "p", "TestRetry", "", nil, ""),
		testEvent(7, base.Add(6*time.Second), protocol.ActionPass, "p", "TestRetry", "", nil, ""),
		testEvent(8, base.Add(7*time.Second), protocol.ActionPass, "p", "", "", nil, ""),
	}
	for _, event := range events {
		if err := analyzer.Add(event); err != nil {
			t.Fatalf("Add(%q) unexpected error: %v", event.Action(), err)
		}
	}

	got := analyzer.Finalize(RunMetadata{})
	if len(got.Packages) != 1 || len(got.Packages[0].Tests) != 2 {
		t.Fatalf("retry result = %#v", got)
	}
	first := findTest(t, got.Packages[0], "TestRetry", 1)
	second := findTest(t, got.Packages[0], "TestRetry", 2)
	if first.Status != StatusIncomplete ||
		first.FirstSequence != 2 ||
		first.LastSequence != 5 ||
		second.Status != StatusPassed ||
		second.FirstSequence != 6 ||
		second.LastSequence != 7 {
		t.Fatalf("retry occurrences = first %#v, second %#v", first, second)
	}
	if got.IntegrityDiagnosticCount != 2 {
		t.Fatalf(
			"integrity diagnostic count = %d, want 2: %#v",
			got.IntegrityDiagnosticCount,
			got.Diagnostics,
		)
	}
}

func TestAnalyzerPackageTerminalAndFailedBuildIntegrity(t *testing.T) {
	base := time.Date(2026, time.July, 29, 17, 0, 0, 0, time.UTC)
	tests := []struct {
		name              string
		firstAction       string
		firstFailedBuild  string
		secondAction      string
		secondFailedBuild string
		wantStatus        Status
		wantFailedBuild   string
		wantDiagnostics   uint64
	}{
		{
			name:              "X to empty",
			firstAction:       protocol.ActionFail,
			firstFailedBuild:  "build-X",
			secondAction:      protocol.ActionFail,
			secondFailedBuild: "",
			wantStatus:        StatusFailed,
			wantFailedBuild:   "build-X",
			wantDiagnostics:   1,
		},
		{
			name:              "X to X",
			firstAction:       protocol.ActionFail,
			firstFailedBuild:  "build-X",
			secondAction:      protocol.ActionFail,
			secondFailedBuild: "build-X",
			wantStatus:        StatusFailed,
			wantFailedBuild:   "build-X",
			wantDiagnostics:   1,
		},
		{
			name:              "X to Y",
			firstAction:       protocol.ActionFail,
			firstFailedBuild:  "build-X",
			secondAction:      protocol.ActionFail,
			secondFailedBuild: "build-Y",
			wantStatus:        StatusFailed,
			wantFailedBuild:   "build-X",
			wantDiagnostics:   2,
		},
		{
			name:              "empty to X",
			firstAction:       protocol.ActionFail,
			secondAction:      protocol.ActionFail,
			secondFailedBuild: "build-X",
			wantStatus:        StatusFailed,
			wantFailedBuild:   "build-X",
			wantDiagnostics:   1,
		},
		{
			name:              "pass to fail",
			firstAction:       protocol.ActionPass,
			secondAction:      protocol.ActionFail,
			secondFailedBuild: "build-X",
			wantStatus:        StatusFailed,
			wantFailedBuild:   "build-X",
			wantDiagnostics:   1,
		},
		{
			name:             "fail to pass",
			firstAction:      protocol.ActionFail,
			firstFailedBuild: "build-X",
			secondAction:     protocol.ActionPass,
			wantStatus:       StatusFailed,
			wantFailedBuild:  "build-X",
			wantDiagnostics:  1,
		},
		{
			name:            "duplicate pass",
			firstAction:     protocol.ActionPass,
			secondAction:    protocol.ActionPass,
			wantStatus:      StatusPassed,
			wantDiagnostics: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			analyzer := New()
			events := []protocol.Event{
				testEvent(1, base, protocol.ActionStart, "p", "", "", nil, ""),
				testEvent(
					2,
					base.Add(time.Second),
					test.firstAction,
					"p",
					"",
					"",
					nil,
					test.firstFailedBuild,
				),
				testEvent(
					3,
					base.Add(2*time.Second),
					test.secondAction,
					"p",
					"",
					"",
					nil,
					test.secondFailedBuild,
				),
			}
			for _, event := range events {
				if err := analyzer.Add(event); err != nil {
					t.Fatalf("Add(%q) unexpected error: %v", event.Action(), err)
				}
			}

			got := analyzer.Finalize(RunMetadata{})
			if len(got.Packages) != 1 {
				t.Fatalf("packages = %#v", got.Packages)
			}
			pkg := got.Packages[0]
			if pkg.Status != test.wantStatus ||
				pkg.FailedBuild != test.wantFailedBuild {
				t.Fatalf("package = %#v", pkg)
			}
			if got.IntegrityDiagnosticCount != test.wantDiagnostics ||
				got.Summary.IntegrityDiagnostics != test.wantDiagnostics {
				t.Fatalf("integrity diagnostics = %#v", got.Diagnostics)
			}
			if test.name == "X to Y" {
				foundConflict := false
				for _, diagnostic := range got.Diagnostics {
					if strings.Contains(
						diagnostic.Message,
						"preserving \"build-X\"",
					) {
						foundConflict = true
					}
				}
				if len(got.Diagnostics) != 2 || !foundConflict {
					t.Fatalf("failed-build conflict diagnostics = %#v", got.Diagnostics)
				}
			}
		})
	}
}

func TestAnalyzerAttributeAndArtifactMetadata(t *testing.T) {
	analyzer := New()
	base := time.Date(2026, time.July, 29, 18, 0, 0, 0, time.UTC)
	events := []protocol.Event{
		testEvent(1, base, protocol.ActionStart, "p", "", "", nil, ""),
		metadataEvent(12, 22, base.Add(time.Second), protocol.ActionAttr, "p", "", "z", "last", ""),
		metadataEvent(11, 21, base.Add(2*time.Second), protocol.ActionAttr, "p", "", "a", "first", ""),
		metadataEvent(13, 23, base.Add(3*time.Second), protocol.ActionArtifacts, "p", "", "", "", "/pkg"),
		testEvent(2, base.Add(4*time.Second), protocol.ActionRun, "p", "TestMeta", "", nil, ""),
		metadataEvent(5, 15, base.Add(5*time.Second), protocol.ActionAttr, "p", "TestMeta", "z", "last", ""),
		testEvent(6, base.Add(6*time.Second), protocol.ActionPause, "p", "TestMeta", "", nil, ""),
		metadataEvent(4, 14, base.Add(7*time.Second), protocol.ActionAttr, "p", "TestMeta", "a", "first", ""),
		metadataEvent(7, 17, base.Add(8*time.Second), protocol.ActionArtifacts, "p", "TestMeta", "", "", "/test"),
		testEvent(8, base.Add(9*time.Second), protocol.ActionCont, "p", "TestMeta", "", nil, ""),
		testEvent(9, base.Add(10*time.Second), protocol.ActionPass, "p", "TestMeta", "", nil, ""),
		testEvent(14, base.Add(11*time.Second), protocol.ActionPass, "p", "", "", nil, ""),
	}
	for _, event := range events {
		if err := analyzer.Add(event); err != nil {
			t.Fatalf("Add(%q) unexpected error: %v", event.Action(), err)
		}
	}

	want := analyzer.Finalize(RunMetadata{})
	if want.IntegrityDiagnosticCount != 0 || len(want.Packages) != 1 {
		t.Fatalf("metadata result = %#v", want)
	}
	pkg := want.Packages[0]
	if len(pkg.Attributes) != 2 ||
		pkg.Attributes[0].Sequence != 11 ||
		pkg.Attributes[0].Line != 21 ||
		pkg.Attributes[0].Key != "a" ||
		pkg.Attributes[0].Test != nil ||
		len(pkg.Artifacts) != 1 ||
		pkg.Artifacts[0].Path != "/pkg" ||
		pkg.Artifacts[0].Test != nil {
		t.Fatalf("package metadata = attributes %#v, artifacts %#v", pkg.Attributes, pkg.Artifacts)
	}
	occurrence := findTest(t, pkg, "TestMeta", 1)
	if occurrence.Status != StatusPassed ||
		len(occurrence.Attributes) != 2 ||
		occurrence.Attributes[0].Sequence != 4 ||
		occurrence.Attributes[0].Line != 14 ||
		occurrence.Attributes[0].Key != "a" ||
		occurrence.Attributes[0].Test == nil ||
		*occurrence.Attributes[0].Test != occurrence.ID ||
		len(occurrence.Artifacts) != 1 ||
		occurrence.Artifacts[0].Path != "/test" ||
		occurrence.Artifacts[0].Test == nil ||
		*occurrence.Artifacts[0].Test != occurrence.ID {
		t.Fatalf(
			"test metadata = occurrence %#v, attributes %#v, artifacts %#v",
			occurrence,
			occurrence.Attributes,
			occurrence.Artifacts,
		)
	}

	mutated, ok := analyzer.Package("p")
	if !ok {
		t.Fatal("Package(p) was not found")
	}
	mutated.Attributes[0].Key = "mutated"
	*mutated.Attributes[0].Time = time.Time{}
	mutated.Artifacts[0].Path = "mutated"
	*mutated.Artifacts[0].Time = time.Time{}
	mutated.Tests[0].Attributes[0].Value = "mutated"
	mutated.Tests[0].Attributes[0].Test.Name = "mutated"
	*mutated.Tests[0].Attributes[0].Time = time.Time{}
	mutated.Tests[0].Artifacts[0].Path = "mutated"
	mutated.Tests[0].Artifacts[0].Test.Name = "mutated"
	*mutated.Tests[0].Artifacts[0].Time = time.Time{}

	fresh, ok := analyzer.Package("p")
	if !ok {
		t.Fatal("second Package(p) was not found")
	}
	if fresh.Attributes[0].Key != "a" ||
		fresh.Artifacts[0].Path != "/pkg" ||
		fresh.Tests[0].Attributes[0].Value != "first" ||
		fresh.Tests[0].Attributes[0].Test.Name != "TestMeta" ||
		fresh.Tests[0].Artifacts[0].Path != "/test" ||
		fresh.Tests[0].Artifacts[0].Test.Name != "TestMeta" {
		t.Fatalf("metadata snapshot aliases analyzer state: %#v", fresh)
	}
}

func TestAnalyzerOrphanMetadataIsIncomplete(t *testing.T) {
	base := time.Date(2026, time.July, 29, 19, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		action string
	}{
		{name: "attribute", action: protocol.ActionAttr},
		{name: "artifacts", action: protocol.ActionArtifacts},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			analyzer := New()
			events := []protocol.Event{
				testEvent(1, base, protocol.ActionStart, "p", "", "", nil, ""),
				metadataEvent(
					2,
					2,
					base.Add(time.Second),
					test.action,
					"p",
					"TestOrphan",
					"key",
					"value",
					"/artifact",
				),
				testEvent(3, base.Add(2*time.Second), protocol.ActionRun, "p", "TestOrphan", "", nil, ""),
				testEvent(4, base.Add(3*time.Second), protocol.ActionPass, "p", "TestOrphan", "", nil, ""),
				testEvent(5, base.Add(4*time.Second), protocol.ActionPass, "p", "", "", nil, ""),
			}
			for _, event := range events {
				if err := analyzer.Add(event); err != nil {
					t.Fatalf("Add(%q) unexpected error: %v", event.Action(), err)
				}
			}

			got := analyzer.Finalize(RunMetadata{})
			if len(got.Packages) != 1 || len(got.Packages[0].Tests) != 2 {
				t.Fatalf("orphan metadata result = %#v", got)
			}
			first := findTest(t, got.Packages[0], "TestOrphan", 1)
			second := findTest(t, got.Packages[0], "TestOrphan", 2)
			if first.Status != StatusIncomplete ||
				first.StartedAt != nil ||
				second.Status != StatusPassed ||
				got.IntegrityDiagnosticCount != 1 ||
				len(got.Diagnostics) != 1 ||
				!strings.Contains(got.Diagnostics[0].Message, "orphan test metadata") {
				t.Fatalf(
					"orphan metadata occurrences = first %#v, second %#v, diagnostics %#v",
					first,
					second,
					got.Diagnostics,
				)
			}
			switch test.action {
			case protocol.ActionAttr:
				if len(first.Attributes) != 1 ||
					first.Attributes[0].Key != "key" {
					t.Fatalf("orphan attribute = %#v", first.Attributes)
				}
			case protocol.ActionArtifacts:
				if len(first.Artifacts) != 1 ||
					first.Artifacts[0].Path != "/artifact" {
					t.Fatalf("orphan artifact = %#v", first.Artifacts)
				}
			}
		})
	}
}

func TestAnalyzerMetadataAfterTerminalDoesNotAllocateRetry(t *testing.T) {
	analyzer := New()
	base := time.Date(2026, time.July, 29, 19, 30, 0, 0, time.UTC)
	events := []protocol.Event{
		testEvent(1, base, protocol.ActionStart, "p", "", "", nil, ""),
		testEvent(2, base.Add(time.Second), protocol.ActionRun, "p", "TestClosed", "", nil, ""),
		testEvent(3, base.Add(2*time.Second), protocol.ActionPass, "p", "TestClosed", "", nil, ""),
		metadataEvent(
			4,
			4,
			base.Add(3*time.Second),
			protocol.ActionAttr,
			"p",
			"TestClosed",
			"region",
			"east",
			"",
		),
		metadataEvent(
			5,
			5,
			base.Add(4*time.Second),
			protocol.ActionArtifacts,
			"p",
			"TestClosed",
			"",
			"",
			"/late",
		),
		testEvent(6, base.Add(5*time.Second), protocol.ActionPass, "p", "", "", nil, ""),
	}
	for _, event := range events {
		if err := analyzer.Add(event); err != nil {
			t.Fatalf("Add(%q) unexpected error: %v", event.Action(), err)
		}
	}

	got := analyzer.Finalize(RunMetadata{})
	if len(got.Packages) != 1 || len(got.Packages[0].Tests) != 1 {
		t.Fatalf("late metadata result = %#v", got)
	}
	occurrence := findTest(t, got.Packages[0], "TestClosed", 1)
	if occurrence.Status != StatusIncomplete ||
		occurrence.LastSequence != 5 ||
		len(occurrence.Attributes) != 1 ||
		len(occurrence.Artifacts) != 1 ||
		got.IntegrityDiagnosticCount != 2 {
		t.Fatalf(
			"late metadata occurrence = %#v, diagnostics %#v",
			occurrence,
			got.Diagnostics,
		)
	}
}

func metadataEvent(
	sequence uint64,
	line uint64,
	eventTime time.Time,
	action string,
	pkg string,
	name string,
	key string,
	value string,
	path string,
) protocol.Event {
	return protocol.Event{
		Sequence: sequence,
		Line:     line,
		Kind:     protocol.EventKindTest,
		Test: &protocol.TestEvent{
			Time:        eventTime,
			TimePresent: true,
			Action:      action,
			Package:     pkg,
			Test:        name,
			Key:         key,
			Value:       value,
			Path:        path,
		},
	}
}
