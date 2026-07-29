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
	"bytes"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/greenpau/tested/pkg/protocol"
)

func TestAnalyzerPackageSnapshot(t *testing.T) {
	analyzer := New()
	base := time.Date(2026, time.July, 29, 9, 0, 0, 0, time.UTC)
	events := []protocol.Event{
		testEvent(1, base, protocol.ActionStart, "target", "", "", nil, ""),
		testEvent(2, base, protocol.ActionStart, "unrelated", "", "", nil, ""),
		testEvent(3, base.Add(time.Second), protocol.ActionOutput, "target", "", "package output\n", nil, ""),
		testEvent(4, base.Add(2*time.Second), protocol.ActionRun, "target", "TestZulu", "", nil, ""),
		testEvent(5, base.Add(3*time.Second), protocol.ActionOutput, "target", "TestZulu", "zulu output\n", nil, ""),
		testEvent(6, base.Add(4*time.Second), protocol.ActionPass, "target", "TestZulu", "", nil, ""),
		testEvent(7, base.Add(5*time.Second), protocol.ActionRun, "target", "TestAlpha", "", nil, ""),
		testEvent(8, base.Add(6*time.Second), protocol.ActionOutput, "target", "TestAlpha", "alpha one\n", nil, ""),
		testEvent(9, base.Add(7*time.Second), protocol.ActionPass, "target", "TestAlpha", "", nil, ""),
		testEvent(10, base.Add(8*time.Second), protocol.ActionRun, "target", "TestAlpha", "", nil, ""),
		testEvent(11, base.Add(9*time.Second), protocol.ActionOutput, "target", "TestAlpha", "alpha two\n", nil, ""),
		testEvent(12, base.Add(10*time.Second), protocol.ActionPass, "target", "TestAlpha", "", nil, ""),
		testEvent(13, base.Add(11*time.Second), protocol.ActionRun, "target", "TestParent", "", nil, ""),
		testEvent(14, base.Add(12*time.Second), protocol.ActionRun, "target", "TestParent/child", "", nil, ""),
		testEvent(15, base.Add(13*time.Second), protocol.ActionOutput, "target", "TestParent/child", "child output\n", nil, ""),
		testEvent(16, base.Add(14*time.Second), protocol.ActionPass, "target", "TestParent/child", "", nil, ""),
		testEvent(17, base.Add(15*time.Second), protocol.ActionPass, "target", "TestParent", "", nil, ""),
		testEvent(18, base.Add(16*time.Second), protocol.ActionPass, "target", "", "", nil, ""),
	}
	for _, event := range events {
		if err := analyzer.Add(event); err != nil {
			t.Fatalf("Add() unexpected error: %v", err)
		}
	}

	if missing, ok := analyzer.Package("missing"); ok || !reflect.DeepEqual(missing, Package{}) {
		t.Fatalf("Package(missing) = (%#v, %t), want zero value, false", missing, ok)
	}

	want, ok := analyzer.Package("target")
	if !ok {
		t.Fatal("Package(target) was not found")
	}
	if want.Name != "target" || want.Status != StatusPassed {
		t.Fatalf("Package(target) = %#v", want)
	}
	wantIDs := []OccurrenceID{
		{Package: "target", Name: "TestAlpha", Ordinal: 1},
		{Package: "target", Name: "TestAlpha", Ordinal: 2},
		{Package: "target", Name: "TestParent", Ordinal: 1},
		{Package: "target", Name: "TestParent/child", Ordinal: 1},
		{Package: "target", Name: "TestZulu", Ordinal: 1},
	}
	if len(want.Tests) != len(wantIDs) {
		t.Fatalf("Package(target) test count = %d, want %d", len(want.Tests), len(wantIDs))
	}
	for i := range wantIDs {
		if want.Tests[i].ID != wantIDs[i] {
			t.Fatalf("Package(target) test %d = %#v, want %#v", i, want.Tests[i].ID, wantIDs[i])
		}
	}

	mutated, ok := analyzer.Package("target")
	if !ok {
		t.Fatal("second Package(target) was not found")
	}
	*mutated.StartedAt = time.Time{}
	*mutated.FinishedAt = time.Time{}
	mutated.Output[0].Text = "mutated package output"
	*mutated.Output[0].Time = time.Time{}
	mutated.Tests[0].ID.Name = "mutated test"
	*mutated.Tests[0].StartedAt = time.Time{}
	*mutated.Tests[0].FinishedAt = time.Time{}
	mutated.Tests[0].Output[0].Text = "mutated test output"
	*mutated.Tests[0].Output[0].Time = time.Time{}
	mutated.Tests[0].Output[0].Test.Name = "mutated output owner"
	child := mutated.Tests[3]
	if child.Parent == nil {
		t.Fatal("child snapshot has no parent")
	}
	child.Parent.Name = "mutated parent"

	fresh, ok := analyzer.Package("target")
	if !ok {
		t.Fatal("third Package(target) was not found")
	}
	if !reflect.DeepEqual(fresh, want) {
		t.Fatalf("Package() returned aliases into analyzer state:\n fresh: %#v\n  want: %#v", fresh, want)
	}
}

func TestAnalyzerPackageTerminalTransitionIntegrity(t *testing.T) {
	base := time.Date(2026, time.July, 29, 9, 15, 0, 0, time.UTC)
	tests := []struct {
		name         string
		firstAction  string
		secondAction string
		priorStatus  Status
		wantStatus   Status
		wantFinished time.Time
	}{
		{
			name:         "fail to pass",
			firstAction:  protocol.ActionFail,
			secondAction: protocol.ActionPass,
			priorStatus:  StatusFailed,
			wantStatus:   StatusFailed,
			wantFinished: base.Add(time.Second),
		},
		{
			name:         "fail to start",
			firstAction:  protocol.ActionFail,
			secondAction: protocol.ActionStart,
			priorStatus:  StatusFailed,
			wantStatus:   StatusFailed,
			wantFinished: base.Add(time.Second),
		},
		{
			name:         "pass to fail",
			firstAction:  protocol.ActionPass,
			secondAction: protocol.ActionFail,
			priorStatus:  StatusPassed,
			wantStatus:   StatusFailed,
			wantFinished: base.Add(2 * time.Second),
		},
		{
			name:         "pass to skip",
			firstAction:  protocol.ActionPass,
			secondAction: protocol.ActionSkip,
			priorStatus:  StatusPassed,
			wantStatus:   StatusPassed,
			wantFinished: base.Add(time.Second),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			analyzer := New()
			events := []protocol.Event{
				testEvent(
					1,
					base,
					protocol.ActionStart,
					"example.com/integrity",
					"",
					"",
					nil,
					"",
				),
				testEvent(
					2,
					base.Add(time.Second),
					test.firstAction,
					"example.com/integrity",
					"",
					"",
					nil,
					"",
				),
				testEvent(
					3,
					base.Add(2*time.Second),
					test.secondAction,
					"example.com/integrity",
					"",
					"",
					nil,
					"",
				),
			}
			events[2].Raw = []byte(fmt.Sprintf(
				`{"Action":%q,"Package":"example.com/integrity"}`,
				test.secondAction,
			))
			for _, event := range events {
				if err := analyzer.Add(event); err != nil {
					t.Fatalf("Add(%q) unexpected error: %v", event.Action(), err)
				}
			}

			got := analyzer.Finalize(RunMetadata{})
			if len(got.Packages) != 1 {
				t.Fatalf("packages = %#v, want one package", got.Packages)
			}
			pkg := got.Packages[0]
			if pkg.Status != test.wantStatus {
				t.Fatalf("package status = %q, want %q", pkg.Status, test.wantStatus)
			}
			if pkg.FinishedAt == nil || !pkg.FinishedAt.Equal(test.wantFinished) {
				t.Fatalf(
					"package finished_at = %v, want %v",
					pkg.FinishedAt,
					test.wantFinished,
				)
			}
			if got.DiagnosticCount != 1 ||
				got.IntegrityDiagnosticCount != 1 ||
				got.Summary.IntegrityDiagnostics != 1 ||
				len(got.Diagnostics) != 1 {
				t.Fatalf("integrity diagnostic counts = %#v", got)
			}
			diagnostic := got.Diagnostics[0]
			if diagnostic.Kind != protocol.DiagnosticIntegrity ||
				diagnostic.Sequence != 3 ||
				diagnostic.Line != 3 ||
				!strings.Contains(diagnostic.Message, string(test.priorStatus)) ||
				!strings.Contains(diagnostic.Message, test.secondAction) {
				t.Fatalf("integrity diagnostic = %#v", diagnostic)
			}
		})
	}
}

func TestAnalyzerIntegrityDiagnosticCountSurvivesRetentionLimit(t *testing.T) {
	analyzer, err := NewAnalyzer(AnalyzerOptions{MaxDiagnostics: 1})
	if err != nil {
		t.Fatalf("NewAnalyzer() unexpected error: %v", err)
	}
	if err := analyzer.AddRecord(protocol.Record{
		Sequence: 1,
		Line:     1,
		Diagnostic: &protocol.Diagnostic{
			Kind:     protocol.DiagnosticMalformed,
			Sequence: 1,
			Line:     1,
			Message:  "malformed test input",
		},
	}); err != nil {
		t.Fatalf("AddRecord() unexpected error: %v", err)
	}

	base := time.Date(2026, time.July, 29, 9, 17, 0, 0, time.UTC)
	events := []protocol.Event{
		testEvent(2, base, protocol.ActionStart, "example.com/retention", "", "", nil, ""),
		testEvent(3, base.Add(time.Second), protocol.ActionPass, "example.com/retention", "", "", nil, ""),
		testEvent(4, base.Add(2*time.Second), protocol.ActionSkip, "example.com/retention", "", "", nil, ""),
	}
	for _, event := range events {
		if err := analyzer.Add(event); err != nil {
			t.Fatalf("Add(%q) unexpected error: %v", event.Action(), err)
		}
	}

	got := analyzer.Finalize(RunMetadata{})
	if got.DiagnosticCount != 2 ||
		got.IntegrityDiagnosticCount != 1 ||
		got.Summary.IntegrityDiagnostics != 1 ||
		!got.DiagnosticsTruncated ||
		len(got.Diagnostics) != 1 ||
		got.Diagnostics[0].Kind != protocol.DiagnosticMalformed {
		t.Fatalf("diagnostic retention snapshot = %#v", got)
	}
}

func TestAnalyzerPackageOutputAfterTerminal(t *testing.T) {
	base := time.Date(2026, time.July, 29, 9, 20, 0, 0, time.UTC)
	tests := []struct {
		name   string
		action string
		status Status
	}{
		{name: "passed", action: protocol.ActionPass, status: StatusPassed},
		{name: "failed", action: protocol.ActionFail, status: StatusFailed},
		{name: "skipped", action: protocol.ActionSkip, status: StatusSkipped},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			analyzer := New()
			events := []protocol.Event{
				testEvent(1, base, protocol.ActionStart, "example.com/output", "", "", nil, ""),
				testEvent(2, base.Add(time.Second), test.action, "example.com/output", "", "", nil, ""),
				testEvent(3, base.Add(2*time.Second), protocol.ActionOutput, "example.com/output", "", "late output\n", nil, ""),
			}
			for _, event := range events {
				if err := analyzer.Add(event); err != nil {
					t.Fatalf("Add(%q) unexpected error: %v", event.Action(), err)
				}
			}

			got := analyzer.Finalize(RunMetadata{})
			if len(got.Packages) != 1 {
				t.Fatalf("packages = %#v, want one package", got.Packages)
			}
			pkg := got.Packages[0]
			if pkg.Status != test.status ||
				pkg.LastSequence != 3 ||
				len(pkg.Output) != 1 ||
				pkg.Output[0].Text != "late output\n" {
				t.Fatalf("package = %#v", pkg)
			}
			if got.DiagnosticCount != 0 ||
				got.IntegrityDiagnosticCount != 0 ||
				len(got.Diagnostics) != 0 {
				t.Fatalf("output produced diagnostics: %#v", got.Diagnostics)
			}
		})
	}
}

func TestAnalyzerPackageConcurrentSnapshots(t *testing.T) {
	analyzer := New()
	base := time.Date(2026, time.July, 29, 9, 30, 0, 0, time.UTC)
	if err := analyzer.Add(testEvent(
		1,
		base,
		protocol.ActionStart,
		"target",
		"",
		"",
		nil,
		"",
	)); err != nil {
		t.Fatalf("Add(start) unexpected error: %v", err)
	}

	const (
		readers = 4
		writes  = 100
	)
	start := make(chan struct{})
	errors := make(chan error, readers+1)
	var wait sync.WaitGroup

	wait.Add(1)
	go func() {
		defer wait.Done()
		<-start
		for i := 0; i < writes; i++ {
			event := testEvent(
				uint64(i+2),
				base.Add(time.Duration(i+1)*time.Millisecond),
				protocol.ActionOutput,
				"target",
				"",
				fmt.Sprintf("output %d\n", i),
				nil,
				"",
			)
			if err := analyzer.Add(event); err != nil {
				errors <- fmt.Errorf("writer Add(output %d): %w", i, err)
				return
			}
		}
		if err := analyzer.Add(testEvent(
			writes+2,
			base.Add((writes+1)*time.Millisecond),
			protocol.ActionPass,
			"target",
			"",
			"",
			nil,
			"",
		)); err != nil {
			errors <- fmt.Errorf("writer Add(pass): %w", err)
		}
	}()

	for reader := 0; reader < readers; reader++ {
		wait.Add(1)
		go func(reader int) {
			defer wait.Done()
			<-start
			for i := 0; i < writes; i++ {
				pkg, ok := analyzer.Package("target")
				if !ok {
					errors <- fmt.Errorf("reader %d snapshot %d: package missing", reader, i)
					return
				}
				if pkg.Name != "target" {
					errors <- fmt.Errorf("reader %d snapshot %d: package name %q", reader, i, pkg.Name)
					return
				}
				if pkg.StartedAt != nil {
					*pkg.StartedAt = time.Time{}
				}
				if len(pkg.Output) > 0 {
					pkg.Output[0].Text = "reader mutation"
					if pkg.Output[0].Time != nil {
						*pkg.Output[0].Time = time.Time{}
					}
				}
			}
		}(reader)
	}

	close(start)
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}

	got, ok := analyzer.Package("target")
	if !ok || got.Status != StatusPassed || len(got.Output) != writes {
		t.Fatalf("final Package(target) = (%#v, %t)", got, ok)
	}
}

func TestAnalyzerOccurrenceLifecycleAndOrdering(t *testing.T) {
	analyzer := New()
	base := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	events := []protocol.Event{
		testEvent(1, base, protocol.ActionStart, "z.example/pkg", "", "", nil, ""),
		testEvent(2, base.Add(time.Second), protocol.ActionStart, "a.example/pkg", "", "", nil, ""),
		testEvent(3, base.Add(2*time.Second), protocol.ActionRun, "a.example/pkg", "TestRepeat", "", nil, ""),
		testEvent(4, base.Add(3*time.Second), protocol.ActionOutput, "a.example/pkg", "TestRepeat", "WARNING: DATA ", nil, ""),
		testEvent(5, base.Add(4*time.Second), protocol.ActionPause, "a.example/pkg", "TestRepeat", "", nil, ""),
		testEvent(6, base.Add(5*time.Second), protocol.ActionCont, "a.example/pkg", "TestRepeat", "", nil, ""),
		testEvent(7, base.Add(6*time.Second), protocol.ActionOutput, "a.example/pkg", "TestRepeat", "RACE\n", nil, ""),
		testEvent(8, base.Add(7*time.Second), protocol.ActionRun, "a.example/pkg", "TestParent", "", nil, ""),
		testEvent(9, base.Add(8*time.Second), protocol.ActionRun, "a.example/pkg", "TestParent/child", "", nil, ""),
		testEvent(10, base.Add(9*time.Second), protocol.ActionPass, "a.example/pkg", "TestParent/child", "", floatPointer(0.1), ""),
		testEvent(11, base.Add(10*time.Second), protocol.ActionPass, "a.example/pkg", "TestParent", "", nil, ""),
		testEvent(12, base.Add(11*time.Second), protocol.ActionPass, "a.example/pkg", "TestRepeat", "", floatPointer(0.2), ""),
		testEvent(13, base.Add(12*time.Second), protocol.ActionRun, "a.example/pkg", "TestRepeat", "", nil, ""),
		testEvent(14, base.Add(13*time.Second), protocol.ActionOutput, "a.example/pkg", "TestRepeat", "panic: boom\n", nil, ""),
		testEvent(15, base.Add(14*time.Second), protocol.ActionFail, "a.example/pkg", "", "", floatPointer(14), ""),
	}
	for _, event := range events {
		if err := analyzer.Add(event); err != nil {
			t.Fatalf("Add() unexpected error: %v", err)
		}
	}

	wallStart := base.Add(-time.Minute)
	wallFinish := wallStart.Add(20 * time.Second)
	got := analyzer.Finalize(RunMetadata{
		Command:    []string{"go", "test", "-json", "./..."},
		WorkDir:    "/project",
		StartedAt:  wallStart,
		FinishedAt: wallFinish,
		Duration:   17 * time.Second,
		ExitCode:   1,
	})

	if !got.Finalized || len(got.Packages) != 2 {
		t.Fatalf("Result = %#v", got)
	}
	if got.Packages[0].Name != "a.example/pkg" ||
		got.Packages[1].Name != "z.example/pkg" {
		t.Fatalf("package order = %q, %q", got.Packages[0].Name, got.Packages[1].Name)
	}
	aPkg := got.Packages[0]
	if aPkg.Status != StatusFailed || !aPkg.Signals.Race || !aPkg.Signals.Panic {
		t.Fatalf("a package state = %#v", aPkg)
	}
	if got.Packages[1].Status != StatusIncomplete {
		t.Fatalf("z package status = %q, want incomplete", got.Packages[1].Status)
	}
	if len(aPkg.Tests) != 4 {
		t.Fatalf("test occurrences = %d, want 4: %#v", len(aPkg.Tests), aPkg.Tests)
	}

	parent := findTest(t, aPkg, "TestParent", 1)
	child := findTest(t, aPkg, "TestParent/child", 1)
	repeatOne := findTest(t, aPkg, "TestRepeat", 1)
	repeatTwo := findTest(t, aPkg, "TestRepeat", 2)
	if parent.Status != StatusPassed ||
		parent.DurationSource != DurationEventEstimate ||
		parent.Elapsed != 3*time.Second {
		t.Fatalf("parent = %#v", parent)
	}
	if child.Parent == nil || *child.Parent != parent.ID {
		t.Fatalf("child parent = %#v, want %#v", child.Parent, parent.ID)
	}
	if child.DurationSource != DurationGoElapsed ||
		child.Elapsed != 100*time.Millisecond {
		t.Fatalf("child duration = (%v, %q)", child.Elapsed, child.DurationSource)
	}
	if repeatOne.Status != StatusPassed || repeatOne.PauseCount != 1 ||
		!repeatOne.Signals.Race || repeatOne.ID.Ordinal != 1 {
		t.Fatalf("first repetition = %#v", repeatOne)
	}
	if repeatOne.Elapsed != 200*time.Millisecond ||
		repeatOne.DurationSource != DurationGoElapsed {
		t.Fatalf("first repetition duration = (%v, %q)", repeatOne.Elapsed, repeatOne.DurationSource)
	}
	if repeatTwo.Status != StatusIncomplete || repeatTwo.ID.Ordinal != 2 ||
		!repeatTwo.Signals.Panic ||
		repeatTwo.IncompleteReason != "missing terminal event" {
		t.Fatalf("second repetition = %#v", repeatTwo)
	}
	if len(repeatOne.Output) != 2 ||
		repeatOne.Output[0].Scope != OutputTest ||
		repeatOne.Output[0].Test == nil ||
		*repeatOne.Output[0].Test != repeatOne.ID {
		t.Fatalf("first repetition output = %#v", repeatOne.Output)
	}

	if !got.Timing.WallMeasured || got.Timing.WallDuration != 17*time.Second {
		t.Fatalf("wall timing = %#v", got.Timing)
	}
	if !got.Timing.EventEstimated ||
		got.Timing.EventDuration != 14*time.Second ||
		got.Timing.EventTimestamps != uint64(len(events)) {
		t.Fatalf("event timing = %#v", got.Timing)
	}
	if got.Summary.Packages.Total != 2 ||
		got.Summary.Packages.Failed != 1 ||
		got.Summary.Packages.Incomplete != 1 ||
		got.Summary.Tests.Passed != 3 ||
		got.Summary.Tests.Incomplete != 1 ||
		!got.Summary.Race || !got.Summary.Panic {
		t.Fatalf("summary = %#v", got.Summary)
	}

	second := analyzer.Finalize(RunMetadata{ExitCode: 0})
	if second.Metadata.ExitCode != 1 {
		t.Fatalf("idempotent Finalize metadata = %#v", second.Metadata)
	}
	if err := analyzer.Add(testEvent(16, base, protocol.ActionPass, "late", "", "", nil, "")); !errors.Is(err, ErrFinalized) {
		t.Fatalf("Add() after Finalize error = %v", err)
	}
}

func TestAnalyzerOutputBoundsBuildFailureAndSemanticMarkers(t *testing.T) {
	analyzer, err := NewAnalyzer(AnalyzerOptions{
		MaxOutputBytes: 5,
		MaxDiagnostics: 1,
	})
	if err != nil {
		t.Fatalf("NewAnalyzer() unexpected error: %v", err)
	}
	base := time.Date(2026, time.July, 29, 11, 0, 0, 0, time.UTC)
	events := []protocol.Event{
		testEvent(1, base, protocol.ActionStart, "p", "", "", nil, ""),
		testEvent(2, base, protocol.ActionOutput, "p", "", "ééé", nil, ""),
		testEvent(3, base, protocol.ActionOutput, "p", "", "abc (cached) [no test files]", nil, ""),
		testEvent(4, base, protocol.ActionRun, "p", "TestBound", "", nil, ""),
		testEvent(5, base, protocol.ActionOutput, "p", "TestBound", "123456789", nil, ""),
		testEvent(6, base, protocol.ActionOutput, "p", "TestBound", "WARNING: DATA RACE", nil, ""),
		buildEvent(7, protocol.ActionBuildOutput, "p [p.test]", "compile failed badly"),
		buildEvent(8, protocol.ActionBuildFail, "p [p.test]", ""),
		testEvent(9, base, protocol.ActionFail, "p", "", "", floatPointer(0), "p [p.test]"),
	}
	for _, event := range events {
		if err := analyzer.Add(event); err != nil {
			t.Fatalf("Add() unexpected error: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := analyzer.AddRecord(protocol.Record{
			Sequence: uint64(10 + i),
			Line:     uint64(20 + i),
			Diagnostic: &protocol.Diagnostic{
				Kind:     protocol.DiagnosticMalformed,
				Sequence: uint64(10 + i),
				Line:     uint64(20 + i),
				Bytes:    3,
				Preview:  "bad",
				Message:  "bad JSON",
			},
		}); err != nil {
			t.Fatalf("AddRecord() unexpected error: %v", err)
		}
	}

	got := analyzer.Finalize(RunMetadata{ExitCode: 1})
	if len(got.Packages) != 1 || len(got.Builds) != 1 {
		t.Fatalf("entity counts = %d packages, %d builds", len(got.Packages), len(got.Builds))
	}
	pkg := got.Packages[0]
	if pkg.OutputBytes != int64(len("ééé")+len("abc (cached) [no test files]")) ||
		pkg.OutputRetainedBytes != 5 || !pkg.OutputTruncated ||
		len(pkg.Output) != 2 || pkg.Output[0].Text != "éé" ||
		pkg.Output[1].Text != "a" {
		t.Fatalf("package output bound = %#v", pkg)
	}
	if !pkg.Cached || !pkg.NoTests || pkg.FailedBuild != "p [p.test]" {
		t.Fatalf("package semantics = %#v", pkg)
	}
	test := findTest(t, pkg, "TestBound", 1)
	if test.OutputBytes != int64(len("123456789")+len("WARNING: DATA RACE")) ||
		test.OutputRetainedBytes != 5 || !test.OutputTruncated ||
		len(test.Output) != 1 || test.Output[0].Text != "12345" ||
		!test.Signals.Race {
		t.Fatalf("test output bound = %#v", test)
	}
	build := got.Builds[0]
	if build.Status != StatusFailed || build.OutputBytes != int64(len("compile failed badly")) ||
		build.OutputRetainedBytes != 5 || !build.OutputTruncated ||
		len(build.Output) != 1 || build.Output[0].Text != "compi" {
		t.Fatalf("build output bound = %#v", build)
	}
	if got.DiagnosticCount != 2 || len(got.Diagnostics) != 1 ||
		!got.DiagnosticsTruncated || got.Summary.Diagnostics != 2 ||
		got.Summary.DiagnosticsRetained != 1 {
		t.Fatalf("diagnostics = %#v", got)
	}
	if got.Summary.BuildFailures != 1 || got.Summary.Builds.Failed != 1 {
		t.Fatalf("build summary = %#v", got.Summary)
	}

	got.Packages[0].Output[0].Text = "mutated"
	got.Builds[0].Output[0].Text = "mutated"
	fresh := analyzer.Snapshot()
	if fresh.Packages[0].Output[0].Text != "éé" ||
		fresh.Builds[0].Output[0].Text != "compi" {
		t.Fatal("Snapshot() returned aliases into analyzer state")
	}
}

func TestAnalyzerBenchmarksCachedAndNoTests(t *testing.T) {
	analyzer := New()
	base := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	events := []protocol.Event{
		testEvent(1, base, protocol.ActionStart, "bench", "", "", nil, ""),
		testEvent(2, base, protocol.ActionOutput, "bench", "", "BenchmarkFoo-2 100 12 ns/op\n", nil, ""),
		testEvent(3, base, protocol.ActionOutput, "bench", "", "BenchmarkFoo-4 100 10 ns/op\n", nil, ""),
		testEvent(4, base, protocol.ActionOutput, "bench", "", "BenchmarkFoo-2 100 11 ns/op\n", nil, ""),
		testEvent(5, base, protocol.ActionRun, "bench", "BenchmarkBar", "", nil, ""),
		testEvent(6, base, protocol.ActionOutput, "bench", "BenchmarkBar", "log output\n", nil, ""),
		testEvent(7, base, protocol.ActionBench, "bench", "BenchmarkBar", "", nil, ""),
		testEvent(8, base, protocol.ActionOutput, "bench", "", "BenchmarkBar-8 200 8 ns/op\n", nil, ""),
		testEvent(9, base, protocol.ActionPass, "bench", "", "", floatPointer(1), ""),
		testEvent(10, base, protocol.ActionStart, "cached", "", "", nil, ""),
		testEvent(11, time.Time{}, protocol.ActionOutput, "cached", "", "ok  \tcached\t(cached)\n", nil, ""),
		testEvent(12, time.Time{}, protocol.ActionPass, "cached", "", "", floatPointer(0), ""),
		testEvent(13, base, protocol.ActionStart, "empty", "", "", nil, ""),
		testEvent(14, base, protocol.ActionOutput, "empty", "", "?\tempty\t[no test files]\n", nil, ""),
		testEvent(15, base, protocol.ActionSkip, "empty", "", "", floatPointer(0), ""),
	}
	for _, event := range events {
		if err := analyzer.Add(event); err != nil {
			t.Fatalf("Add() unexpected error: %v", err)
		}
	}
	got := analyzer.Finalize(RunMetadata{})
	if len(got.Packages) != 3 ||
		got.Packages[0].Name != "bench" ||
		got.Packages[1].Name != "cached" ||
		got.Packages[2].Name != "empty" {
		t.Fatalf("packages = %#v", got.Packages)
	}
	bench := got.Packages[0]
	if len(bench.Tests) != 4 {
		t.Fatalf("benchmark occurrences = %d: %#v", len(bench.Tests), bench.Tests)
	}
	if findTest(t, bench, "BenchmarkFoo-2", 1).Status != StatusBenchmarked ||
		findTest(t, bench, "BenchmarkFoo-2", 2).Status != StatusBenchmarked ||
		findTest(t, bench, "BenchmarkFoo-4", 1).Status != StatusBenchmarked {
		t.Fatalf("output-only benchmarks = %#v", bench.Tests)
	}
	bar := findTest(t, bench, "BenchmarkBar", 1)
	if bar.Status != StatusBenchmarked || len(bar.Output) != 2 ||
		bar.Output[1].Text != "BenchmarkBar-8 200 8 ns/op\n" {
		t.Fatalf("logged benchmark = %#v", bar)
	}
	if !got.Packages[1].Cached || got.Packages[1].Status != StatusPassed {
		t.Fatalf("cached package = %#v", got.Packages[1])
	}
	if !got.Packages[2].NoTests || got.Packages[2].Status != StatusSkipped {
		t.Fatalf("no-test package = %#v", got.Packages[2])
	}
	if got.Summary.Tests.Benchmarked != 4 ||
		got.Summary.CachedPackages != 1 ||
		got.Summary.PackagesWithoutTests != 1 {
		t.Fatalf("summary = %#v", got.Summary)
	}
}

func TestAnalyzerGo126BenchmarkOutputLifecycle(t *testing.T) {
	input, err := os.ReadFile("testdata/go1.26-benchmark.jsonl")
	if err != nil {
		t.Fatalf("ReadFile() unexpected error: %v", err)
	}
	analyzer := New()
	streamSummary, err := protocol.Read(bytes.NewReader(input), protocol.StreamOptions{
		Handler: func(record protocol.Record) {
			if err := analyzer.AddRecord(record); err != nil {
				t.Errorf("AddRecord() unexpected error: %v", err)
			}
		},
	})
	if err != nil {
		t.Fatalf("Read() unexpected error: %v", err)
	}
	if streamSummary.Events != 15 || streamSummary.DiagnosticCount != 0 {
		t.Fatalf("stream summary = %#v", streamSummary)
	}

	got := analyzer.Finalize(RunMetadata{ExitCode: 0})
	if len(got.Packages) != 1 {
		t.Fatalf("packages = %#v", got.Packages)
	}
	pkg := got.Packages[0]
	if pkg.Status != StatusPassed || len(pkg.Tests) != 4 {
		t.Fatalf("benchmark package = %#v", pkg)
	}

	baseOne := findTest(t, pkg, "BenchmarkSum", 1)
	baseTwo := findTest(t, pkg, "BenchmarkSum", 2)
	cpuOne := findTest(t, pkg, "BenchmarkSum-2", 1)
	cpuTwo := findTest(t, pkg, "BenchmarkSum-2", 2)
	for _, occurrence := range []TestOccurrence{
		baseOne,
		baseTwo,
		cpuOne,
		cpuTwo,
	} {
		if occurrence.Kind != TestKindBenchmark ||
			occurrence.Status != StatusBenchmarked {
			t.Fatalf("benchmark occurrence = %#v", occurrence)
		}
	}
	if len(baseOne.Output) != 3 ||
		baseOne.Output[2].Text != "BenchmarkSum     \t       1\t       167.0 ns/op\n" ||
		len(baseTwo.Output) != 1 ||
		len(cpuOne.Output) != 1 ||
		len(cpuTwo.Output) != 1 {
		t.Fatalf(
			"benchmark output attribution = base1 %#v, base2 %#v, cpu1 %#v, cpu2 %#v",
			baseOne.Output,
			baseTwo.Output,
			cpuOne.Output,
			cpuTwo.Output,
		)
	}
	if got.Summary.Tests.Benchmarked != 4 ||
		got.Summary.Tests.Incomplete != 0 ||
		got.Summary.Tests.Running != 0 ||
		got.Summary.Tests.Paused != 0 ||
		got.Summary.Tests.Unknown != 0 {
		t.Fatalf("benchmark summary = %#v", got.Summary)
	}
}

func TestAnalyzerTestScopedCPUBenchmarkResultFinishesBaseOccurrence(t *testing.T) {
	analyzer := New()
	base := time.Date(2026, time.July, 29, 13, 30, 0, 0, time.UTC)
	events := []protocol.Event{
		testEvent(1, base, protocol.ActionStart, "bench", "", "", nil, ""),
		testEvent(2, base, protocol.ActionRun, "bench", "BenchmarkCPU", "", nil, ""),
		testEvent(3, base, protocol.ActionOutput, "bench", "BenchmarkCPU", "setup\n", nil, ""),
		testEvent(4, base, protocol.ActionOutput, "bench", "BenchmarkCPU", "BenchmarkCPU-4 1 9 ns/op\n", nil, ""),
		testEvent(5, base, protocol.ActionOutput, "bench", "", "BenchmarkCPU-4 1 8 ns/op\n", nil, ""),
		testEvent(6, base, protocol.ActionPass, "bench", "", "", nil, ""),
	}
	for _, event := range events {
		if err := analyzer.Add(event); err != nil {
			t.Fatalf("Add() unexpected error: %v", err)
		}
	}

	got := analyzer.Finalize(RunMetadata{})
	if len(got.Packages) != 1 || len(got.Packages[0].Tests) != 2 {
		t.Fatalf("benchmark result = %#v", got)
	}
	baseOccurrence := findTest(t, got.Packages[0], "BenchmarkCPU", 1)
	cpuOccurrence := findTest(t, got.Packages[0], "BenchmarkCPU-4", 1)
	if baseOccurrence.Status != StatusBenchmarked ||
		len(baseOccurrence.Output) != 2 ||
		cpuOccurrence.Status != StatusBenchmarked ||
		len(cpuOccurrence.Output) != 1 {
		t.Fatalf("benchmark occurrences = base %#v, cpu %#v", baseOccurrence, cpuOccurrence)
	}
}

func TestAnalyzerFinalizeSweepsDetachedNonterminalOccurrences(t *testing.T) {
	analyzer := New()
	pkg := analyzer.ensurePackage("p", 1)
	for ordinal, status := range []Status{
		StatusUnknown,
		StatusRunning,
		StatusPaused,
	} {
		pkg.tests = append(pkg.tests, &testState{
			value: TestOccurrence{
				ID: OccurrenceID{
					Package: "p",
					Name:    fmt.Sprintf("TestDetached%d", ordinal+1),
					Ordinal: 1,
				},
				Kind:   TestKindTest,
				Status: status,
			},
		})
	}

	got := analyzer.Finalize(RunMetadata{})
	if len(got.Packages) != 1 || len(got.Packages[0].Tests) != 3 {
		t.Fatalf("result = %#v", got)
	}
	for _, occurrence := range got.Packages[0].Tests {
		if occurrence.Status != StatusIncomplete ||
			occurrence.IncompleteReason != "missing terminal event" {
			t.Fatalf("detached occurrence = %#v", occurrence)
		}
	}
	if got.Summary.Tests.Incomplete != 3 ||
		got.Summary.Tests.Running != 0 ||
		got.Summary.Tests.Paused != 0 ||
		got.Summary.Tests.Unknown != 0 {
		t.Fatalf("summary = %#v", got.Summary)
	}
}

func TestAnalyzerUnknownActionsAndTimeoutAttribution(t *testing.T) {
	analyzer := New()
	base := time.Date(2026, time.July, 29, 13, 0, 0, 0, time.UTC)
	events := []protocol.Event{
		testEvent(1, base, protocol.ActionStart, "p", "", "", nil, ""),
		testEvent(2, base, protocol.ActionRun, "p", "TestSlow", "", nil, ""),
		testEvent(3, base, "retry", "p", "TestSlow", "retry output\n", nil, ""),
		testEvent(4, base, protocol.ActionOutput, "p", "", "panic: test timed ", nil, ""),
		testEvent(5, base, protocol.ActionOutput, "p", "", "out after 10s\n", nil, ""),
		buildEvent(6, "build-cache-hit", "dep", "cached build\n"),
		testEvent(7, base, protocol.ActionPass, "p", "TestSlow", "", nil, ""),
		testEvent(8, base, protocol.ActionPass, "p", "", "", nil, ""),
	}
	for _, event := range events {
		if err := analyzer.Add(event); err != nil {
			t.Fatalf("Add() unexpected error: %v", err)
		}
	}
	unknown, err := protocol.Decode([]byte(
		`{"Action":"notice","Output":"unattributed output\n","Future":true}`,
	))
	if err != nil {
		t.Fatalf("Decode() unexpected error: %v", err)
	}
	unknown.Sequence = 9
	unknown.Line = 9
	if err := analyzer.Add(unknown); err != nil {
		t.Fatalf("Add() unknown event error: %v", err)
	}
	got := analyzer.Finalize(RunMetadata{})
	slow := findTest(t, got.Packages[0], "TestSlow", 1)
	if !slow.Signals.Timeout || slow.Signals.Panic ||
		!got.Signals.Timeout || got.Signals.Panic {
		t.Fatalf("timeout signals = test %#v, result %#v", slow.Signals, got.Signals)
	}
	if len(slow.Output) != 1 || slow.Output[0].Text != "retry output\n" {
		t.Fatalf("future test output = %#v", slow.Output)
	}
	if len(got.Builds) != 1 || len(got.Builds[0].Output) != 1 {
		t.Fatalf("future build output = %#v", got.Builds)
	}
	if len(got.UnattributedOutput) != 1 ||
		got.UnattributedOutput[0].Scope != OutputUnattributed ||
		got.UnattributedOutput[0].Text != "unattributed output\n" {
		t.Fatalf("unattributed output = %#v", got.UnattributedOutput)
	}
	if len(got.UnknownActions) != 3 ||
		got.UnknownActions[0].Action != "build-cache-hit" ||
		got.UnknownActions[1].Action != "retry" ||
		got.UnknownActions[2].Action != "notice" ||
		got.Summary.UnknownActions != 3 {
		t.Fatalf("unknown actions = %#v, summary %#v", got.UnknownActions, got.Summary)
	}
	if got.DiagnosticCount != 3 || len(got.Diagnostics) != 3 ||
		got.Summary.Diagnostics != 3 {
		t.Fatalf("unknown action diagnostics = %#v", got)
	}
	for _, diagnostic := range got.Diagnostics {
		if diagnostic.Kind != protocol.DiagnosticUnknown {
			t.Fatalf("unknown action diagnostic = %#v", diagnostic)
		}
	}
}

func TestAnalyzerStreamIntegration(t *testing.T) {
	analyzer := New()
	input := []byte(
		`{"Time":"2026-07-29T14:00:00Z","Action":"start","Package":"p"}` + "\n" +
			"not json\n" +
			`{"Time":"2026-07-29T14:00:01Z","Action":"run","Package":"p","Test":"TestOne"}` + "\r\n" +
			`{"Time":"2026-07-29T14:00:02Z","Action":"pass","Package":"p","Test":"TestOne","Elapsed":1}` + "\n" +
			`{"Time":"2026-07-29T14:00:03Z","Action":"pass","Package":"p","Elapsed":3}`,
	)
	var raw bytes.Buffer
	stream := protocol.NewStream(protocol.StreamOptions{
		RawWriter: &raw,
		Handler: func(record protocol.Record) {
			if err := analyzer.AddRecord(record); err != nil {
				t.Errorf("AddRecord() unexpected error: %v", err)
			}
		},
	})
	for offset := 0; offset < len(input); {
		size := 7
		if offset+size > len(input) {
			size = len(input) - offset
		}
		if _, err := stream.Write(input[offset : offset+size]); err != nil {
			t.Fatalf("Write() unexpected error: %v", err)
		}
		offset += size
	}
	streamSummary, err := stream.Finish()
	if err != nil {
		t.Fatalf("Finish() unexpected error: %v", err)
	}
	got := analyzer.Finalize(RunMetadata{})
	if raw.String() != string(input) || streamSummary.Events != 4 ||
		streamSummary.MalformedRecords != 1 {
		t.Fatalf("stream = raw match %v, summary %#v", raw.String() == string(input), streamSummary)
	}
	if len(got.Diagnostics) != 1 || got.DiagnosticCount != 1 ||
		len(got.Packages) != 1 || got.Packages[0].Status != StatusPassed {
		t.Fatalf("result = %#v", got)
	}
	test := findTest(t, got.Packages[0], "TestOne", 1)
	if test.Status != StatusPassed || test.Elapsed != time.Second {
		t.Fatalf("test = %#v", test)
	}
}

func TestAnalyzerUnknownStreamDiagnosticIsCountedOnce(t *testing.T) {
	analyzer := New()
	input := `{"Action":"notice","Output":"future output\n","Future":true}`
	streamSummary, err := protocol.Read(strings.NewReader(input), protocol.StreamOptions{
		Handler: func(record protocol.Record) {
			if err := analyzer.AddRecord(record); err != nil {
				t.Errorf("AddRecord() unexpected error: %v", err)
			}
		},
	})
	if err != nil {
		t.Fatalf("Read() unexpected error: %v", err)
	}
	got := analyzer.Finalize(RunMetadata{})
	if streamSummary.DiagnosticCount != 1 ||
		streamSummary.UnknownRecords != 1 ||
		got.DiagnosticCount != 1 ||
		len(got.Diagnostics) != 1 ||
		got.Diagnostics[0].Kind != protocol.DiagnosticUnknown ||
		got.Summary.UnknownActions != 1 ||
		len(got.UnattributedOutput) != 1 {
		t.Fatalf("unknown stream = summary %#v, result %#v", streamSummary, got)
	}
}

func TestNewAnalyzerValidation(t *testing.T) {
	if _, err := NewAnalyzer(AnalyzerOptions{MaxOutputBytes: -1}); err == nil {
		t.Fatal("NewAnalyzer() accepted negative MaxOutputBytes")
	}
}

func FuzzAnalyzerEventSequence(f *testing.F) {
	f.Add(uint8(3), uint8(1), "TestOne")
	f.Add(uint8(7), uint8(2), "TestParent/child")
	f.Fuzz(func(t *testing.T, countByte, actionByte uint8, name string) {
		if len(name) > 256 {
			t.Skip()
		}
		actions := []string{
			protocol.ActionRun,
			protocol.ActionPause,
			protocol.ActionCont,
			protocol.ActionOutput,
			protocol.ActionPass,
			protocol.ActionFail,
			protocol.ActionSkip,
			protocol.ActionBench,
			"future",
		}
		count := int(countByte%32) + 1
		analyzer, err := NewAnalyzer(AnalyzerOptions{MaxOutputBytes: 64})
		if err != nil {
			t.Fatalf("NewAnalyzer() unexpected error: %v", err)
		}
		for i := 0; i < count; i++ {
			action := actions[(int(actionByte)+i)%len(actions)]
			event := testEvent(
				uint64(i+1),
				time.Unix(int64(i), 0).UTC(),
				action,
				"p",
				name,
				fmt.Sprintf("output-%d", i),
				nil,
				"",
			)
			if err := analyzer.Add(event); err != nil {
				t.Fatalf("Add() unexpected error: %v", err)
			}
		}
		got := analyzer.Finalize(RunMetadata{})
		if !got.Finalized || len(got.Packages) != 1 {
			t.Fatalf("Finalize() = %#v", got)
		}
	})
}

func testEvent(
	sequence uint64,
	eventTime time.Time,
	action string,
	pkg string,
	name string,
	output string,
	elapsed *float64,
	failedBuild string,
) protocol.Event {
	input := &protocol.TestEvent{
		Action:      action,
		Package:     pkg,
		Test:        name,
		Output:      output,
		FailedBuild: failedBuild,
	}
	if !eventTime.IsZero() {
		input.Time = eventTime
		input.TimePresent = true
	}
	if elapsed != nil {
		input.Elapsed = *elapsed
		input.ElapsedPresent = true
	}
	return protocol.Event{
		Sequence: sequence,
		Line:     sequence,
		Kind:     protocol.EventKindTest,
		Test:     input,
	}
}

func buildEvent(
	sequence uint64,
	action string,
	importPath string,
	output string,
) protocol.Event {
	return protocol.Event{
		Sequence: sequence,
		Line:     sequence,
		Kind:     protocol.EventKindBuild,
		Build: &protocol.BuildEvent{
			Action:     action,
			ImportPath: importPath,
			Output:     output,
		},
	}
}

func findTest(
	t *testing.T,
	pkg Package,
	name string,
	ordinal uint64,
) TestOccurrence {
	t.Helper()
	for _, test := range pkg.Tests {
		if test.ID.Name == name && test.ID.Ordinal == ordinal {
			return test
		}
	}
	t.Fatalf("test %s#%d not found in %#v", name, ordinal, pkg.Tests)
	return TestOccurrence{}
}

func floatPointer(value float64) *float64 {
	return &value
}
