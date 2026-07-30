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
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/greenpau/tested/pkg/protocol"
)

const (
	// DefaultMaxDiagnostics bounds diagnostics retained by the normalized
	// model. Protocol and summary counts still describe discarded entries.
	DefaultMaxDiagnostics  = 1000
	signalTailBytes        = 256
	maxBenchmarkFragments  = 8192
	unknownActionBytes     = 256
	diagnosticMessageBytes = 1024
	diagnosticValueBytes   = 256
)

// ErrFinalized is returned when a caller attempts to add evidence after the
// analyzer has been finalized.
var ErrFinalized = errors.New("result analyzer is finalized")

// AnalyzerOptions configures bounded normalized state. MaxOutputBytes applies
// independently to every test occurrence, package, build, and the
// unattributed output bucket. MaxTotalOutputBytes applies across every output
// scope, and MaxResultEntries bounds retained packages, builds, occurrences,
// metadata, output chunks, and unknown-action summaries. MaxNormalizedBytes
// bounds dynamically retained identity and metadata strings independently
// from output text. MaxSemanticLineBytes bounds aggregate temporary benchmark
// line assembly across packages. Zero preserves an unlimited byte or entry
// budget. Zero-length output chunks are ignored before output and entry
// accounting, regardless of whether an output limit is configured.
type AnalyzerOptions struct {
	MaxOutputBytes      int64
	MaxTotalOutputBytes int64
	MaxResultEntries    int64
	MaxNormalizedBytes  int64
	// MaxSemanticLineBytes bounds aggregate output temporarily retained to
	// reassemble benchmark result lines split across protocol events. Zero
	// explicitly disables this byte bound.
	MaxSemanticLineBytes int64
	MaxDiagnostics       int
}

// Analyzer incrementally aggregates protocol records. It is safe for
// concurrent callers, although protocol event order remains authoritative.
type Analyzer struct {
	mu sync.Mutex

	maxOutputBytes       int64
	maxTotalOutputBytes  int64
	maxResultEntries     int64
	maxNormalizedBytes   int64
	maxSemanticLineBytes int64
	maxDiagnostics       int
	nextSequence         uint64

	packages map[string]*packageState
	builds   map[string]*buildState
	ordinals map[testKey]uint64
	active   map[testKey]*testState
	latest   map[testKey]*testState

	unattributedOutput        []Output
	unattributedOutputBytes   int64
	unattributedRetainedBytes int64
	unattributedTruncated     bool
	unattributedSignalTail    string

	totalOutputBytes         int64
	totalOutputRetainedBytes int64
	totalOutputTruncated     bool

	normalizedEntries          uint64
	normalizedEntriesRetained  uint64
	normalizedEntriesTruncated bool
	normalizedBytes            int64
	normalizedBytesRetained    int64
	normalizedBytesTruncated   bool
	resultIncomplete           bool
	resourceCapacityDiagnosed  bool
	benchmarkAssemblyDiagnosed bool
	pendingBenchmarkBytes      int64
	pendingBenchmarkFragments  int64

	diagnostics              []Diagnostic
	diagnosticCount          uint64
	integrityDiagnosticCount uint64
	unknownActions           map[unknownActionKey]*UnknownAction
	signals                  Signals

	eventStartedAt  *time.Time
	eventFinishedAt *time.Time
	eventTimestamps uint64

	finalized bool
	metadata  RunMetadata
	timing    Timing
}

type packageState struct {
	value            Package
	tests            []*testState
	pendingBenchmark *pendingBenchmarkOutput
	semanticTail     string
	signalTail       string
}

type buildState struct {
	value      Build
	signalTail string
}

type pendingBenchmarkOutput struct {
	name              string
	test              string
	key               testKey
	occurrence        *testState
	text              []byte
	bytes             int64
	fragments         int64
	invalidateOnAbort bool
}

type testState struct {
	value                   TestOccurrence
	signalTail              string
	phase                   occurrencePhase
	invalid                 bool
	benchmarkResultObserved bool
}

type occurrencePhase uint8

const (
	occurrenceEvidence occurrencePhase = iota
	occurrenceRunning
	occurrencePaused
	occurrenceTerminal
)

func (p occurrencePhase) String() string {
	switch p {
	case occurrenceEvidence:
		return "without a run"
	case occurrenceRunning:
		return "running"
	case occurrencePaused:
		return "paused"
	case occurrenceTerminal:
		return "terminal"
	default:
		return "unknown"
	}
}

type testKey struct {
	pkg  string
	name string
}

type unknownActionKey struct {
	kind   protocol.EventKind
	action string
}

// NewAnalyzer validates options and creates an empty analyzer.
func NewAnalyzer(options AnalyzerOptions) (*Analyzer, error) {
	if options.MaxOutputBytes < 0 {
		return nil, fmt.Errorf("max output bytes must not be negative")
	}
	if options.MaxTotalOutputBytes < 0 {
		return nil, fmt.Errorf("max total output bytes must not be negative")
	}
	if options.MaxResultEntries < 0 {
		return nil, fmt.Errorf("max result entries must not be negative")
	}
	if options.MaxNormalizedBytes < 0 {
		return nil, fmt.Errorf("max normalized bytes must not be negative")
	}
	if options.MaxSemanticLineBytes < 0 {
		return nil, fmt.Errorf("max semantic line bytes must not be negative")
	}
	if options.MaxDiagnostics < 0 {
		return nil, fmt.Errorf("max diagnostics must not be negative")
	}
	maxDiagnostics := options.MaxDiagnostics
	if maxDiagnostics == 0 {
		maxDiagnostics = DefaultMaxDiagnostics
	}
	return &Analyzer{
		maxOutputBytes:       options.MaxOutputBytes,
		maxTotalOutputBytes:  options.MaxTotalOutputBytes,
		maxResultEntries:     options.MaxResultEntries,
		maxNormalizedBytes:   options.MaxNormalizedBytes,
		maxSemanticLineBytes: options.MaxSemanticLineBytes,
		maxDiagnostics:       maxDiagnostics,
		packages:             make(map[string]*packageState),
		builds:               make(map[string]*buildState),
		ordinals:             make(map[testKey]uint64),
		active:               make(map[testKey]*testState),
		latest:               make(map[testKey]*testState),
		unknownActions:       make(map[unknownActionKey]*UnknownAction),
	}, nil
}

// New creates an analyzer with unlimited normalized output and bounded
// diagnostics. Runners should normally use NewAnalyzer with an explicit output
// limit.
func New() *Analyzer {
	analyzer, _ := NewAnalyzer(AnalyzerOptions{})
	return analyzer
}

// Add consumes one decoded event. Events decoded outside Stream receive a
// monotonically increasing sequence number.
func (a *Analyzer) Add(event protocol.Event) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.finalized {
		return ErrFinalized
	}
	a.addEvent(event, false)
	return nil
}

// AddRecord consumes either a decoded event or a recoverable framing
// diagnostic.
func (a *Analyzer) AddRecord(record protocol.Record) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.finalized {
		return ErrFinalized
	}
	a.observeSequence(record.Sequence)
	unknownDiagnosed := false
	if record.Diagnostic != nil {
		if record.Event == nil {
			a.abortAllPendingBenchmarkOutputs(
				"benchmark result interrupted by an invalid protocol record",
			)
		}
		a.addDiagnostic(*record.Diagnostic)
		unknownDiagnosed = record.Diagnostic.Kind == protocol.DiagnosticUnknown
	}
	if record.Event != nil {
		event := *record.Event
		if event.Sequence == 0 {
			event.Sequence = record.Sequence
		}
		if event.Line == 0 {
			event.Line = record.Line
		}
		a.addEvent(event, unknownDiagnosed)
	}
	return nil
}

// Finalize turns every started nonterminal package and test occurrence into an
// explicit incomplete result and records authoritative runner metadata. It is
// idempotent.
func (a *Analyzer) Finalize(metadata RunMetadata) Result {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.finalized {
		a.abortAllPendingBenchmarkOutputs(
			"benchmark result line ended before a complete result was observed",
		)
		for key := range a.active {
			delete(a.active, key)
		}
		for _, pkg := range a.packages {
			for _, test := range pkg.tests {
				if !test.value.Status.Terminal() {
					test.value.Status = StatusIncomplete
					test.value.IncompleteReason = "missing terminal event"
				}
			}
		}
		a.finalizeSignals()
		for _, pkg := range a.packages {
			if !pkg.value.Status.Terminal() {
				pkg.value.Status = StatusIncomplete
				pkg.value.IncompleteReason = "missing terminal package event"
			}
		}
		a.metadata = a.cloneMetadataBounded(metadata)
		a.timing = a.buildTiming(metadata)
		a.finalized = true
	}
	return a.snapshotLocked()
}

// Snapshot returns a deep, deterministically ordered copy of the current
// normalized model.
func (a *Analyzer) Snapshot() Result {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.snapshotLocked()
}

// Package returns a deep, deterministically ordered snapshot of one package.
// It avoids cloning or sorting unrelated packages, builds, diagnostics, and
// run-level output. The boolean is false when name has not been observed.
func (a *Analyzer) Package(name string) (Package, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	state, ok := a.packages[name]
	if !ok {
		return Package{}, false
	}
	return clonePackageState(state), true
}

// Summary returns aggregate counts for the current snapshot.
func (a *Analyzer) Summary() Summary {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.snapshotLocked().Summary
}

func (a *Analyzer) addEvent(
	event protocol.Event,
	unknownDiagnosed bool,
) {
	sequence := a.eventSequence(event.Sequence)
	event.Sequence = sequence
	a.observeSequence(sequence)

	switch event.Kind {
	case protocol.EventKindTest:
		if event.Test == nil {
			a.recordUnknown(event, event.Kind, event.Action(), unknownDiagnosed)
			return
		}
		a.observeEventTime(event.Test)
		if !protocol.IsTestAction(event.Test.Action) {
			a.recordUnknown(
				event,
				event.Kind,
				event.Test.Action,
				unknownDiagnosed,
			)
		}
		a.addTestEvent(event, event.Test)
	case protocol.EventKindBuild:
		if event.Build == nil {
			a.recordUnknown(event, event.Kind, event.Action(), unknownDiagnosed)
			return
		}
		if !protocol.IsBuildAction(event.Build.Action) {
			a.recordUnknown(
				event,
				event.Kind,
				event.Build.Action,
				unknownDiagnosed,
			)
		}
		a.addBuildEvent(event, event.Build)
	default:
		a.recordUnknown(event, event.Kind, event.Action(), unknownDiagnosed)
		a.addUnknownOutput(event)
	}
}

func (a *Analyzer) addTestEvent(event protocol.Event, input *protocol.TestEvent) {
	pkg := a.packageForEvent(input.Package, event)
	if pkg == nil {
		if input.Action == protocol.ActionOutput || input.Output != "" {
			a.observeDroppedOutput(
				input.Output,
				droppedTestEventOutputStringBytes(input),
			)
		}
		return
	}
	pkg.value.LastSequence = event.Sequence

	if input.Action == protocol.ActionOutput {
		a.addOutputEvent(pkg, event, input)
		return
	}
	a.abortPendingBenchmarkOutput(
		pkg,
		"benchmark result interrupted by a non-output event",
	)

	if input.Test == "" {
		a.addPackageEvent(pkg, event, input)
		return
	}
	if pkg.value.Status == StatusUnknown {
		pkg.value.Status = StatusRunning
	}

	key := testKey{pkg: input.Package, name: input.Test}
	switch input.Action {
	case protocol.ActionStart, protocol.ActionRun:
		test := a.startOccurrence(pkg, key, event, input)
		if test != nil && input.Action == protocol.ActionStart {
			test.value.Kind = inferTestKind(input.Test)
		}
	case protocol.ActionPause:
		a.transitionOccurrence(pkg, key, event, input)
	case protocol.ActionCont:
		a.transitionOccurrence(pkg, key, event, input)
	case protocol.ActionPass:
		a.finishOccurrence(pkg, key, event, input, StatusPassed)
	case protocol.ActionFail:
		a.finishOccurrence(pkg, key, event, input, StatusFailed)
	case protocol.ActionSkip:
		a.finishOccurrence(pkg, key, event, input, StatusSkipped)
	case protocol.ActionBench:
		test := a.finishBenchmarkOccurrence(pkg, key, event, input)
		if test != nil && input.Output != "" {
			if _, ok := benchmarkOutputName(input.Output); ok {
				test.benchmarkResultObserved = true
			}
			a.appendTestOutput(pkg, test, event, input)
		} else if test == nil && input.Output != "" {
			a.observeDroppedOutput(
				input.Output,
				testOutputStringBytes(input.Package, input.Test),
			)
		}
	case protocol.ActionAttr, protocol.ActionArtifacts:
		a.addTestMetadata(pkg, key, event, input)
	default:
		if input.Output != "" {
			test := a.outputOccurrence(pkg, key, event, input)
			if test != nil {
				a.appendTestOutput(pkg, test, event, input)
			} else {
				a.observeDroppedOutput(
					input.Output,
					testOutputStringBytes(input.Package, input.Test),
				)
			}
		}
	}
}

func (a *Analyzer) addPackageEvent(
	pkg *packageState,
	event protocol.Event,
	input *protocol.TestEvent,
) {
	next, lifecycle := packageLifecycleStatus(input.Action)
	if input.Action == protocol.ActionFail {
		a.recordFailedBuild(pkg, event, input.FailedBuild)
	}
	if lifecycle && pkg.value.Status.Terminal() {
		a.addDiagnostic(packageTransitionDiagnostic(
			event,
			pkg.value.Name,
			pkg.value.Status,
			input.Action,
		))
		// A failure is monotonic: it cannot be weakened by later lifecycle
		// evidence. Conversely, a later failure is the strongest terminal
		// outcome and must replace an earlier pass or skip.
		if next != StatusFailed || pkg.value.Status == StatusFailed {
			return
		}
	}

	switch input.Action {
	case protocol.ActionStart, protocol.ActionRun, protocol.ActionCont:
		pkg.value.Status = StatusRunning
		if pkg.value.StartedAt == nil && input.TimePresent {
			pkg.value.StartedAt = timePointer(input.Time)
		}
	case protocol.ActionPause:
		pkg.value.Status = StatusPaused
	case protocol.ActionPass:
		a.finishPackage(pkg, input, StatusPassed)
	case protocol.ActionFail:
		a.finishPackage(pkg, input, StatusFailed)
	case protocol.ActionSkip:
		a.finishPackage(pkg, input, StatusSkipped)
	case protocol.ActionAttr:
		if a.reserveEntry(
			event,
			"package attribute",
			attributeStringBytes(input),
		) {
			pkg.value.Attributes = append(
				pkg.value.Attributes,
				eventAttribute(event, input, nil),
			)
		}
	case protocol.ActionArtifacts:
		if a.reserveEntry(
			event,
			"package artifact",
			artifactStringBytes(input),
		) {
			pkg.value.Artifacts = append(
				pkg.value.Artifacts,
				eventArtifact(event, input, nil),
			)
		}
	default:
		if input.Output != "" {
			a.appendPackageOutput(pkg, event, input)
		}
	}
}

func (a *Analyzer) addOutputEvent(
	pkg *packageState,
	event protocol.Event,
	input *protocol.TestEvent,
) {
	if pending := pkg.pendingBenchmark; pending != nil {
		if input.Test != pending.test {
			a.abortPendingBenchmarkOutput(
				pkg,
				"benchmark result scope changed before the line completed",
			)
		} else if _, startsNext := benchmarkOutputPrefixName(
			input.Output,
		); startsNext {
			a.abortPendingBenchmarkOutput(
				pkg,
				"benchmark result was interrupted by another result prefix",
			)
		} else {
			retained := a.retainPendingBenchmarkText(
				pending,
				input.Output,
			)
			if !retained {
				a.markBenchmarkAssemblyIncomplete(event)
				a.abortPendingBenchmarkOutput(
					pkg,
					"benchmark result exceeded semantic assembly capacity",
				)
			} else if !strings.ContainsRune(input.Output, '\n') {
				pending.occurrence.value.LastSequence = event.Sequence
				a.appendTestOutput(
					pkg,
					pending.occurrence,
					event,
					input,
				)
				return
			} else if name, complete := benchmarkOutputName(
				string(pending.text),
			); complete && name == pending.name {
				pkg.pendingBenchmark = nil
				a.releasePendingBenchmark(pending)
				pending.occurrence.value.LastSequence = event.Sequence
				a.appendTestOutput(
					pkg,
					pending.occurrence,
					event,
					input,
				)
				a.finishBenchmarkState(
					pending.key,
					pending.occurrence,
					event,
					input,
				)
				pending.occurrence.benchmarkResultObserved = true
				return
			} else {
				a.abortPendingBenchmarkOutput(
					pkg,
					"benchmark result line was not a valid completion",
				)
			}
			// The current output did not complete the pending line. Process it
			// again as independent evidence instead of consuming a possible
			// benchmark result or changing its source-order accounting.
		}
	}

	if name, ok := benchmarkOutputName(input.Output); ok &&
		(input.Test == "" || benchmarkNamesMatch(input.Test, name)) {
		var preferredKey *testKey
		if input.Test != "" {
			key := testKey{pkg: input.Package, name: input.Test}
			preferredKey = &key
		}
		test := a.benchmarkResultOccurrence(
			pkg,
			name,
			preferredKey,
			event,
			input,
		)
		if test != nil {
			a.appendTestOutput(pkg, test, event, input)
		} else {
			a.observeDroppedOutput(
				input.Output,
				testOutputStringBytes(input.Package, name),
			)
		}
		return
	}

	if name, ok := benchmarkOutputPrefixName(input.Output); ok &&
		(input.Test == "" || benchmarkNamesMatch(input.Test, name)) {
		pending := &pendingBenchmarkOutput{
			name: name,
			test: input.Test,
		}
		if !a.retainPendingBenchmarkText(pending, input.Output) {
			a.markBenchmarkAssemblyIncomplete(event)
			a.appendOrdinaryOutputEvent(pkg, event, input)
			return
		}
		var preferredKey *testKey
		if input.Test != "" {
			key := testKey{pkg: input.Package, name: input.Test}
			preferredKey = &key
		}
		test, key, invalidateOnAbort := a.beginBenchmarkResultOccurrence(
			pkg,
			name,
			preferredKey,
			event,
			input,
		)
		if test == nil {
			a.releasePendingBenchmark(pending)
			a.appendOrdinaryOutputEvent(pkg, event, input)
			return
		}
		pending.key = key
		pending.occurrence = test
		pending.invalidateOnAbort = invalidateOnAbort
		pkg.pendingBenchmark = pending
		test.value.LastSequence = event.Sequence
		a.appendTestOutput(pkg, test, event, input)
		return
	}

	a.appendOrdinaryOutputEvent(pkg, event, input)
}

func (a *Analyzer) abortPendingBenchmarkOutput(
	pkg *packageState,
	reason string,
) {
	if pkg == nil || pkg.pendingBenchmark == nil {
		return
	}
	pending := pkg.pendingBenchmark
	pkg.pendingBenchmark = nil
	a.releasePendingBenchmark(pending)
	if !pending.invalidateOnAbort || pending.occurrence == nil {
		return
	}
	a.invalidateOccurrence(pending.occurrence, reason)
	pending.occurrence.phase = occurrenceTerminal
	if a.active[pending.key] == pending.occurrence {
		delete(a.active, pending.key)
	}
	a.latest[pending.key] = pending.occurrence
}

func (a *Analyzer) abortAllPendingBenchmarkOutputs(reason string) {
	for _, pkg := range a.packages {
		a.abortPendingBenchmarkOutput(pkg, reason)
	}
}

func (a *Analyzer) retainPendingBenchmarkText(
	pending *pendingBenchmarkOutput,
	output string,
) bool {
	size := int64(len(output))
	byteExceeded := a.maxSemanticLineBytes > 0 &&
		size > a.maxSemanticLineBytes-a.pendingBenchmarkBytes
	fragmentExceeded := a.pendingBenchmarkFragments >= maxBenchmarkFragments
	if byteExceeded || fragmentExceeded {
		return false
	}
	a.pendingBenchmarkBytes = saturatingAddInt64(
		a.pendingBenchmarkBytes,
		size,
	)
	a.pendingBenchmarkFragments++
	pending.text = append(pending.text, output...)
	pending.bytes = saturatingAddInt64(pending.bytes, size)
	pending.fragments++
	return true
}

func (a *Analyzer) releasePendingBenchmark(pending *pendingBenchmarkOutput) {
	if pending == nil {
		return
	}
	a.pendingBenchmarkBytes -= pending.bytes
	if a.pendingBenchmarkBytes < 0 {
		a.pendingBenchmarkBytes = 0
	}
	a.pendingBenchmarkFragments -= pending.fragments
	if a.pendingBenchmarkFragments < 0 {
		a.pendingBenchmarkFragments = 0
	}
}

func (a *Analyzer) markBenchmarkAssemblyIncomplete(event protocol.Event) {
	a.resultIncomplete = true
	if a.benchmarkAssemblyDiagnosed {
		return
	}
	a.benchmarkAssemblyDiagnosed = true
	a.addDiagnostic(benchmarkAssemblyDiagnostic(
		event,
		a.maxSemanticLineBytes,
		maxBenchmarkFragments,
	))
}

func (a *Analyzer) beginBenchmarkResultOccurrence(
	pkg *packageState,
	name string,
	preferredKey *testKey,
	event protocol.Event,
	input *protocol.TestEvent,
) (*testState, testKey, bool) {
	key := testKey{pkg: input.Package, name: benchmarkBaseName(name)}
	if preferredKey != nil {
		key = *preferredKey
	}
	if active := a.active[key]; active != nil {
		active.value.Kind = TestKindBenchmark
		active.value.LastSequence = event.Sequence
		return active, key, true
	}
	if latest := a.latest[key]; latest != nil &&
		latest.value.Status == StatusBenchmarked &&
		!latest.benchmarkResultObserved {
		latest.value.Kind = TestKindBenchmark
		latest.value.LastSequence = event.Sequence
		return latest, key, false
	}

	key = testKey{pkg: input.Package, name: name}
	test := a.createOccurrence(pkg, key, event, input)
	if test == nil {
		return nil, testKey{}, false
	}
	test.value.StartedAt = nil
	test.value.Kind = TestKindBenchmark
	return test, key, true
}

func (a *Analyzer) appendOrdinaryOutputEvent(
	pkg *packageState,
	event protocol.Event,
	input *protocol.TestEvent,
) {
	if input.Test == "" {
		a.appendPackageOutput(pkg, event, input)
		return
	}
	key := testKey{pkg: input.Package, name: input.Test}
	test := a.outputOccurrence(pkg, key, event, input)
	if test != nil {
		a.appendTestOutput(pkg, test, event, input)
		return
	}
	a.observeDroppedOutput(
		input.Output,
		testOutputStringBytes(input.Package, input.Test),
	)
}

func packageLifecycleStatus(action string) (Status, bool) {
	switch action {
	case protocol.ActionStart, protocol.ActionRun, protocol.ActionCont:
		return StatusRunning, true
	case protocol.ActionPause:
		return StatusPaused, true
	case protocol.ActionPass:
		return StatusPassed, true
	case protocol.ActionFail:
		return StatusFailed, true
	case protocol.ActionSkip:
		return StatusSkipped, true
	default:
		return StatusUnknown, false
	}
}

func (a *Analyzer) addBuildEvent(event protocol.Event, input *protocol.BuildEvent) {
	build := a.buildForEvent(input.ImportPath, event)
	if build == nil {
		if input.Action == protocol.ActionBuildOutput || input.Output != "" {
			a.observeDroppedOutput(
				input.Output,
				stringBytes(input.ImportPath),
			)
		}
		return
	}
	build.value.LastSequence = event.Sequence

	switch input.Action {
	case protocol.ActionBuildOutput:
		a.appendBuildOutput(build, event, input)
	case protocol.ActionBuildFail:
		build.value.Status = StatusFailed
	default:
		if input.Output != "" {
			a.appendBuildOutput(build, event, input)
		}
	}
}

func (a *Analyzer) packageForEvent(
	name string,
	event protocol.Event,
) *packageState {
	if pkg := a.packages[name]; pkg != nil {
		return pkg
	}
	if !a.reserveEntry(event, "package", stringBytes(name)) {
		return nil
	}
	return a.ensurePackage(name, event.Sequence)
}

func (a *Analyzer) ensurePackage(name string, sequence uint64) *packageState {
	if pkg, exists := a.packages[name]; exists {
		return pkg
	}
	pkg := &packageState{
		value: Package{
			Name:           name,
			Status:         StatusUnknown,
			FirstSequence:  sequence,
			LastSequence:   sequence,
			DurationSource: DurationUnknown,
		},
	}
	a.packages[name] = pkg
	return pkg
}

func (a *Analyzer) buildForEvent(
	importPath string,
	event protocol.Event,
) *buildState {
	if build := a.builds[importPath]; build != nil {
		return build
	}
	if !a.reserveEntry(event, "build", stringBytes(importPath)) {
		return nil
	}
	return a.ensureBuild(importPath, event.Sequence)
}

func (a *Analyzer) ensureBuild(importPath string, sequence uint64) *buildState {
	if build, exists := a.builds[importPath]; exists {
		return build
	}
	build := &buildState{
		value: Build{
			ImportPath:    importPath,
			Status:        StatusUnknown,
			FirstSequence: sequence,
			LastSequence:  sequence,
		},
	}
	a.builds[importPath] = build
	return build
}

func (a *Analyzer) createOccurrence(
	pkg *packageState,
	key testKey,
	event protocol.Event,
	input *protocol.TestEvent,
) *testState {
	identityBytes := saturatingAddInt64(
		stringBytes(key.pkg),
		stringBytes(key.name),
	)
	if !a.reserveEntry(event, "test occurrence", identityBytes) {
		return nil
	}
	a.ordinals[key]++
	id := OccurrenceID{
		Package: key.pkg,
		Name:    key.name,
		Ordinal: a.ordinals[key],
	}
	test := &testState{
		value: TestOccurrence{
			ID:             id,
			Parent:         a.activeParent(key),
			Kind:           inferTestKind(key.name),
			Status:         StatusUnknown,
			DurationSource: DurationUnknown,
			FirstSequence:  event.Sequence,
			LastSequence:   event.Sequence,
		},
	}
	if input.TimePresent {
		test.value.StartedAt = timePointer(input.Time)
	}
	pkg.tests = append(pkg.tests, test)
	a.active[key] = test
	a.latest[key] = test
	return test
}

func (a *Analyzer) startOccurrence(
	pkg *packageState,
	key testKey,
	event protocol.Event,
	input *protocol.TestEvent,
) *testState {
	if existing := a.active[key]; existing != nil {
		if existing.phase == occurrenceEvidence && !existing.invalid {
			existing.phase = occurrenceRunning
			existing.value.Status = StatusRunning
			existing.value.LastSequence = event.Sequence
			if input.TimePresent {
				existing.value.StartedAt = timePointer(input.Time)
			}
			return existing
		}
		existing.value.LastSequence = event.Sequence
		a.invalidateOccurrence(
			existing,
			"repeated run event before a terminal event",
		)
		a.addDiagnostic(occurrenceTransitionDiagnostic(
			event,
			existing.value.ID,
			existing.phase,
			input.Action,
		))
		return existing
	}
	test := a.createOccurrence(pkg, key, event, input)
	if test == nil {
		return nil
	}
	test.phase = occurrenceRunning
	test.value.Status = StatusRunning
	return test
}

func (a *Analyzer) transitionOccurrence(
	pkg *packageState,
	key testKey,
	event protocol.Event,
	input *protocol.TestEvent,
) *testState {
	test := a.active[key]
	if test == nil {
		test = a.latest[key]
		if test == nil {
			test = a.createOccurrence(pkg, key, event, input)
			if test == nil {
				return nil
			}
			test.value.StartedAt = nil
		}
		phase := test.phase
		test.value.LastSequence = event.Sequence
		a.invalidateOccurrence(
			test,
			fmt.Sprintf("orphan %s event without an active run", input.Action),
		)
		test.phase = occurrenceTerminal
		delete(a.active, key)
		a.addDiagnostic(occurrenceTransitionDiagnostic(
			event,
			test.value.ID,
			phase,
			input.Action,
		))
		return test
	}
	test.value.LastSequence = event.Sequence

	valid := false
	switch input.Action {
	case protocol.ActionPause:
		valid = test.phase == occurrenceRunning
		if valid {
			test.phase = occurrencePaused
			test.value.PauseCount++
		}
	case protocol.ActionCont:
		valid = test.phase == occurrencePaused
		if valid {
			test.phase = occurrenceRunning
		}
	}
	if !valid {
		a.invalidateOccurrence(
			test,
			fmt.Sprintf(
				"invalid %s event while occurrence was %s",
				input.Action,
				test.phase,
			),
		)
		a.addDiagnostic(occurrenceTransitionDiagnostic(
			event,
			test.value.ID,
			test.phase,
			input.Action,
		))
		return test
	}

	if !test.invalid {
		switch test.phase {
		case occurrencePaused:
			test.value.Status = StatusPaused
		case occurrenceRunning:
			test.value.Status = StatusRunning
		}
	}
	return test
}

func (a *Analyzer) finishOccurrence(
	pkg *packageState,
	key testKey,
	event protocol.Event,
	input *protocol.TestEvent,
	status Status,
) *testState {
	test := a.active[key]
	if test == nil {
		test = a.latest[key]
		if test == nil {
			test = a.createOccurrence(pkg, key, event, input)
			if test == nil {
				return nil
			}
			test.value.StartedAt = nil
		}
		test.value.LastSequence = event.Sequence
		a.invalidateOccurrence(
			test,
			fmt.Sprintf("orphan %s event without an active run", input.Action),
		)
		a.addDiagnostic(occurrenceTransitionDiagnostic(
			event,
			test.value.ID,
			test.phase,
			input.Action,
		))
		a.recordOccurrenceFinish(test, input)
		test.phase = occurrenceTerminal
		delete(a.active, key)
		a.latest[key] = test
		return test
	}

	phase := test.phase
	test.value.LastSequence = event.Sequence
	if phase != occurrenceRunning {
		a.invalidateOccurrence(
			test,
			fmt.Sprintf(
				"invalid %s event while occurrence was %s",
				input.Action,
				phase,
			),
		)
		a.addDiagnostic(occurrenceTransitionDiagnostic(
			event,
			test.value.ID,
			phase,
			input.Action,
		))
	} else if !test.invalid {
		test.value.Status = status
	}
	a.recordOccurrenceFinish(test, input)
	test.phase = occurrenceTerminal
	delete(a.active, key)
	a.latest[key] = test
	return test
}

func (a *Analyzer) invalidateOccurrence(test *testState, reason string) {
	test.invalid = true
	test.value.Status = StatusIncomplete
	if test.value.IncompleteReason == "" {
		test.value.IncompleteReason = reason
	}
}

func (a *Analyzer) recordOccurrenceFinish(
	test *testState,
	input *protocol.TestEvent,
) {
	if test.value.FinishedAt == nil && input.TimePresent {
		test.value.FinishedAt = timePointer(input.Time)
	}
	if test.value.DurationSource != DurationUnknown {
		return
	}
	applyDuration(
		&test.value.Elapsed,
		&test.value.DurationSource,
		input,
		test.value.StartedAt,
		test.value.FinishedAt,
	)
}

func (a *Analyzer) outputOccurrence(
	pkg *packageState,
	key testKey,
	event protocol.Event,
	input *protocol.TestEvent,
) *testState {
	if test := a.active[key]; test != nil {
		test.value.LastSequence = event.Sequence
		return test
	}
	if test := a.latest[key]; test != nil && test.value.Status.Terminal() {
		test.value.LastSequence = event.Sequence
		return test
	}
	test := a.createOccurrence(pkg, key, event, input)
	if test == nil {
		return nil
	}
	test.value.StartedAt = nil
	return test
}

func (a *Analyzer) finishBenchmarkOccurrence(
	pkg *packageState,
	key testKey,
	event protocol.Event,
	input *protocol.TestEvent,
) *testState {
	test := a.finishOccurrence(
		pkg,
		key,
		event,
		input,
		StatusBenchmarked,
	)
	if test == nil {
		return nil
	}
	test.value.Kind = TestKindBenchmark
	return test
}

func (a *Analyzer) benchmarkResultOccurrence(
	pkg *packageState,
	name string,
	preferredKey *testKey,
	event protocol.Event,
	input *protocol.TestEvent,
) *testState {
	baseKey := testKey{pkg: input.Package, name: benchmarkBaseName(name)}
	candidateKey := baseKey
	if preferredKey != nil {
		candidateKey = *preferredKey
	}
	if active := a.active[candidateKey]; active != nil {
		a.finishBenchmarkState(candidateKey, active, event, input)
		active.benchmarkResultObserved = true
		return active
	}
	if latest := a.latest[candidateKey]; latest != nil &&
		latest.value.Status == StatusBenchmarked &&
		!latest.benchmarkResultObserved {
		a.finishBenchmarkState(candidateKey, latest, event, input)
		latest.benchmarkResultObserved = true
		return latest
	}

	key := testKey{pkg: input.Package, name: name}
	test := a.createOccurrence(pkg, key, event, input)
	if test == nil {
		return nil
	}
	test.value.StartedAt = nil
	a.finishBenchmarkState(key, test, event, input)
	test.benchmarkResultObserved = true
	return test
}

func (a *Analyzer) finishBenchmarkState(
	key testKey,
	test *testState,
	event protocol.Event,
	input *protocol.TestEvent,
) {
	test.value.Kind = TestKindBenchmark
	if !test.invalid {
		test.value.Status = StatusBenchmarked
		test.value.IncompleteReason = ""
	}
	test.value.LastSequence = event.Sequence
	a.recordOccurrenceFinish(test, input)
	test.phase = occurrenceTerminal
	delete(a.active, key)
	a.latest[key] = test
}

func (a *Analyzer) finishPackage(
	pkg *packageState,
	input *protocol.TestEvent,
	status Status,
) {
	pkg.value.Status = status
	if input.TimePresent {
		pkg.value.FinishedAt = timePointer(input.Time)
	}
	applyDuration(
		&pkg.value.Elapsed,
		&pkg.value.DurationSource,
		input,
		pkg.value.StartedAt,
		pkg.value.FinishedAt,
	)
}

func (a *Analyzer) recordFailedBuild(
	pkg *packageState,
	event protocol.Event,
	failedBuild string,
) {
	if failedBuild == "" {
		return
	}
	if pkg.value.FailedBuild == "" {
		if !a.reserveNormalizedBytes(
			event,
			"failed-build identifier",
			stringBytes(failedBuild),
		) {
			return
		}
		pkg.value.FailedBuild = failedBuild
		return
	}
	if pkg.value.FailedBuild == failedBuild {
		return
	}
	a.addDiagnostic(failedBuildDiagnostic(
		event,
		pkg.value.Name,
		pkg.value.FailedBuild,
		failedBuild,
	))
}

func (a *Analyzer) addTestMetadata(
	pkg *packageState,
	key testKey,
	event protocol.Event,
	input *protocol.TestEvent,
) {
	test := a.active[key]
	active := test != nil &&
		(test.phase == occurrenceRunning || test.phase == occurrencePaused)
	if test == nil {
		test = a.latest[key]
	}
	if test == nil {
		test = a.createOccurrence(pkg, key, event, input)
		if test == nil {
			return
		}
		test.value.StartedAt = nil
	}

	test.value.LastSequence = event.Sequence
	entryKind := "test attribute"
	if input.Action == protocol.ActionArtifacts {
		entryKind = "test artifact"
	}
	metadataBytes := attributeStringBytes(input)
	if input.Action == protocol.ActionArtifacts {
		metadataBytes = artifactStringBytes(input)
	}
	if !a.reserveEntry(event, entryKind, metadataBytes) {
		return
	}
	switch input.Action {
	case protocol.ActionAttr:
		test.value.Attributes = append(
			test.value.Attributes,
			eventAttribute(event, input, &test.value.ID),
		)
	case protocol.ActionArtifacts:
		test.value.Artifacts = append(
			test.value.Artifacts,
			eventArtifact(event, input, &test.value.ID),
		)
	}
	if active {
		return
	}

	a.invalidateOccurrence(
		test,
		fmt.Sprintf(
			"orphan %s metadata without an active run",
			input.Action,
		),
	)
	phase := test.phase
	test.phase = occurrenceTerminal
	delete(a.active, key)
	a.latest[key] = test
	a.addDiagnostic(metadataOrphanDiagnostic(
		event,
		test.value.ID,
		phase,
		input.Action,
	))
}

func eventAttribute(
	event protocol.Event,
	input *protocol.TestEvent,
	test *OccurrenceID,
) Attribute {
	attribute := Attribute{
		Sequence: event.Sequence,
		Line:     event.Line,
		Package:  input.Package,
		Test:     occurrencePointerValue(test),
		Key:      input.Key,
		Value:    input.Value,
	}
	if input.TimePresent {
		attribute.Time = timePointer(input.Time)
	}
	return attribute
}

func eventArtifact(
	event protocol.Event,
	input *protocol.TestEvent,
	test *OccurrenceID,
) Artifact {
	artifact := Artifact{
		Sequence: event.Sequence,
		Line:     event.Line,
		Package:  input.Package,
		Test:     occurrencePointerValue(test),
		Path:     input.Path,
	}
	if input.TimePresent {
		artifact.Time = timePointer(input.Time)
	}
	return artifact
}

func attributeStringBytes(input *protocol.TestEvent) int64 {
	if input == nil {
		return 0
	}
	total := stringBytes(input.Package)
	if input.Test != "" {
		// Test metadata retains the package both on the metadata record and
		// inside its occurrence identifier.
		total = saturatingAddInt64(total, stringBytes(input.Package))
		total = saturatingAddInt64(total, stringBytes(input.Test))
	}
	total = saturatingAddInt64(total, stringBytes(input.Key))
	return saturatingAddInt64(total, stringBytes(input.Value))
}

func artifactStringBytes(input *protocol.TestEvent) int64 {
	if input == nil {
		return 0
	}
	total := stringBytes(input.Package)
	if input.Test != "" {
		// Test metadata retains the package both on the metadata record and
		// inside its occurrence identifier.
		total = saturatingAddInt64(total, stringBytes(input.Package))
		total = saturatingAddInt64(total, stringBytes(input.Test))
	}
	return saturatingAddInt64(total, stringBytes(input.Path))
}

func outputStringBytes(output Output) int64 {
	total := stringBytes(output.Package)
	total = saturatingAddInt64(total, stringBytes(output.ImportPath))
	if output.Test != nil {
		total = saturatingAddInt64(
			total,
			stringBytes(output.Test.Package),
		)
		total = saturatingAddInt64(total, stringBytes(output.Test.Name))
	}
	return total
}

func testOutputStringBytes(packageName, testName string) int64 {
	total := stringBytes(packageName)
	total = saturatingAddInt64(total, stringBytes(packageName))
	return saturatingAddInt64(total, stringBytes(testName))
}

func droppedTestEventOutputStringBytes(input *protocol.TestEvent) int64 {
	if input == nil {
		return 0
	}
	if input.Action == protocol.ActionOutput {
		if name, ok := benchmarkOutputName(input.Output); ok &&
			(input.Test == "" || benchmarkNamesMatch(input.Test, name)) {
			return testOutputStringBytes(input.Package, name)
		}
	}
	if input.Test != "" {
		return testOutputStringBytes(input.Package, input.Test)
	}
	return stringBytes(input.Package)
}

func stringBytes(value string) int64 {
	return int64(len(value))
}

func (a *Analyzer) appendTestOutput(
	pkg *packageState,
	test *testState,
	event protocol.Event,
	input *protocol.TestEvent,
) {
	output := eventOutput(event, input, OutputTest)
	output.Test = occurrencePointer(test.value.ID)
	a.appendOutput(
		event,
		&test.value.Output,
		&test.value.OutputBytes,
		&test.value.OutputRetainedBytes,
		&test.value.OutputTruncated,
		output,
		a.maxOutputBytes,
	)
	signals, tail := detectSignals(test.signalTail, input.Output)
	test.signalTail = tail
	mergeSignals(&test.value.Signals, signals)
	mergeSignals(&pkg.value.Signals, signals)
	mergeSignals(&a.signals, signals)
}

func (a *Analyzer) appendPackageOutput(
	pkg *packageState,
	event protocol.Event,
	input *protocol.TestEvent,
) {
	output := eventOutput(event, input, OutputPackage)
	a.appendOutput(
		event,
		&pkg.value.Output,
		&pkg.value.OutputBytes,
		&pkg.value.OutputRetainedBytes,
		&pkg.value.OutputTruncated,
		output,
		a.maxOutputBytes,
	)

	semanticText, semanticTail := joinedTail(pkg.semanticTail, input.Output)
	pkg.semanticTail = semanticTail
	lower := strings.ToLower(semanticText)
	if strings.Contains(lower, "(cached)") {
		pkg.value.Cached = true
	}
	if strings.Contains(lower, "[no test files]") {
		pkg.value.NoTests = true
	}

	signals, tail := detectSignals(pkg.signalTail, input.Output)
	pkg.signalTail = tail
	mergeSignals(&pkg.value.Signals, signals)
	mergeSignals(&a.signals, signals)
	if signals.Timeout {
		for key, test := range a.active {
			if key.pkg == pkg.value.Name {
				test.value.Signals.Timeout = true
			}
		}
	}
}

func (a *Analyzer) appendBuildOutput(
	build *buildState,
	event protocol.Event,
	input *protocol.BuildEvent,
) {
	output := Output{
		Sequence:      event.Sequence,
		Line:          event.Line,
		Scope:         OutputBuild,
		ImportPath:    input.ImportPath,
		Text:          input.Output,
		OriginalBytes: int64(len(input.Output)),
	}
	a.appendOutput(
		event,
		&build.value.Output,
		&build.value.OutputBytes,
		&build.value.OutputRetainedBytes,
		&build.value.OutputTruncated,
		output,
		a.maxOutputBytes,
	)
	signals, tail := detectSignals(build.signalTail, input.Output)
	build.signalTail = tail
	mergeSignals(&build.value.Signals, signals)
	mergeSignals(&a.signals, signals)
}

func (a *Analyzer) addDiagnostic(input protocol.Diagnostic) {
	a.diagnosticCount++
	if input.Kind == protocol.DiagnosticIntegrity {
		a.integrityDiagnosticCount++
	}
	if a.maxDiagnostics >= 0 && len(a.diagnostics) >= a.maxDiagnostics {
		return
	}
	preview, previewTruncated := boundedDiagnosticText(
		input.Preview,
		protocol.DefaultMaxDiagnosticBytes,
	)
	message, messageTruncated := boundedDiagnosticText(
		input.Message,
		diagnosticMessageBytes,
	)
	a.diagnostics = append(a.diagnostics, Diagnostic{
		Kind:      input.Kind,
		Sequence:  input.Sequence,
		Line:      input.Line,
		Bytes:     input.Bytes,
		Preview:   preview,
		Truncated: input.Truncated || previewTruncated || messageTruncated,
		Message:   message,
	})
}

func (a *Analyzer) addUnknownOutput(event protocol.Event) {
	if event.Unknown == nil {
		return
	}
	raw, exists := event.Unknown.Fields["Output"]
	if !exists {
		return
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return
	}
	output := Output{
		Sequence:      event.Sequence,
		Line:          event.Line,
		Scope:         OutputUnattributed,
		Text:          text,
		OriginalBytes: int64(len(text)),
	}
	a.appendOutput(
		event,
		&a.unattributedOutput,
		&a.unattributedOutputBytes,
		&a.unattributedRetainedBytes,
		&a.unattributedTruncated,
		output,
		a.maxOutputBytes,
	)
	signals, tail := detectSignals(a.unattributedSignalTail, text)
	a.unattributedSignalTail = tail
	mergeSignals(&a.signals, signals)
}

func (a *Analyzer) recordUnknown(
	event protocol.Event,
	kind protocol.EventKind,
	action string,
	diagnosed bool,
) {
	action = boundedUnknownAction(action)
	key := unknownActionKey{kind: kind, action: action}
	if existing := a.unknownActions[key]; existing != nil {
		existing.Count++
	} else {
		unknownBytes := saturatingAddInt64(
			stringBytes(string(kind)),
			stringBytes(action),
		)
		if !a.reserveEntry(
			event,
			"unknown-action summary",
			unknownBytes,
		) {
			if !diagnosed {
				a.addDiagnostic(unknownEventDiagnostic(event, kind, action))
			}
			return
		}
		a.unknownActions[key] = &UnknownAction{
			Kind:          kind,
			Action:        action,
			Count:         1,
			FirstSequence: event.Sequence,
		}
	}
	if diagnosed {
		return
	}
	a.addDiagnostic(unknownEventDiagnostic(event, kind, action))
}

func boundedUnknownAction(action string) string {
	if len(action) <= unknownActionBytes {
		return action
	}
	const suffix = "..."
	end := unknownActionBytes - len(suffix)
	for end > 0 && !utf8.ValidString(action[:end]) {
		end--
	}
	return action[:end] + suffix
}

func unknownEventDiagnostic(
	event protocol.Event,
	kind protocol.EventKind,
	action string,
) protocol.Diagnostic {
	return eventDiagnostic(
		event,
		protocol.DiagnosticUnknown,
		fmt.Sprintf("unknown action %q for %s event", action, kind),
	)
}

func packageTransitionDiagnostic(
	event protocol.Event,
	pkg string,
	status Status,
	action string,
) protocol.Diagnostic {
	return eventDiagnostic(
		event,
		protocol.DiagnosticIntegrity,
		fmt.Sprintf(
			"duplicate or contradictory package lifecycle for %q: terminal status %q followed by action %q",
			boundedDiagnosticValue(pkg),
			status,
			action,
		),
	)
}

func occurrenceTransitionDiagnostic(
	event protocol.Event,
	id OccurrenceID,
	phase occurrencePhase,
	action string,
) protocol.Diagnostic {
	return eventDiagnostic(
		event,
		protocol.DiagnosticIntegrity,
		fmt.Sprintf(
			"invalid test lifecycle for %q: occurrence %s was %s when action %q arrived",
			boundedDiagnosticValue(id.Name),
			boundedOccurrenceLabel(id),
			phase,
			action,
		),
	)
}

func benchmarkAssemblyDiagnostic(
	event protocol.Event,
	maxLineBytes int64,
	maxFragments int64,
) protocol.Diagnostic {
	byteLimit := "unlimited"
	if maxLineBytes > 0 {
		byteLimit = strconv.FormatInt(maxLineBytes, 10)
	}
	return eventDiagnostic(
		event,
		protocol.DiagnosticIntegrity,
		fmt.Sprintf(
			"benchmark result line exceeded semantic assembly capacity "+
				"(maximum bytes %s, maximum fragments %d)",
			byteLimit,
			maxFragments,
		),
	)
}

func metadataOrphanDiagnostic(
	event protocol.Event,
	id OccurrenceID,
	phase occurrencePhase,
	action string,
) protocol.Diagnostic {
	return eventDiagnostic(
		event,
		protocol.DiagnosticIntegrity,
		fmt.Sprintf(
			"orphan test metadata for %s: occurrence was %s when action %q arrived",
			boundedOccurrenceLabel(id),
			phase,
			action,
		),
	)
}

func failedBuildDiagnostic(
	event protocol.Event,
	pkg string,
	first string,
	conflicting string,
) protocol.Diagnostic {
	return eventDiagnostic(
		event,
		protocol.DiagnosticIntegrity,
		fmt.Sprintf(
			"conflicting failed build identifiers for package %q: preserving %q and rejecting %q",
			boundedDiagnosticValue(pkg),
			boundedDiagnosticValue(first),
			boundedDiagnosticValue(conflicting),
		),
	)
}

func resourceCapacityDiagnostic(
	event protocol.Event,
	kind string,
	entryLimit int64,
	byteLimit int64,
	entryExceeded bool,
	byteExceeded bool,
) protocol.Diagnostic {
	reason := "configured capacity"
	switch {
	case entryExceeded && byteExceeded:
		reason = "entry and normalized-string byte capacities"
	case entryExceeded:
		reason = "entry capacity"
	case byteExceeded:
		reason = "normalized-string byte capacity"
	}
	return eventDiagnostic(
		event,
		protocol.DiagnosticIntegrity,
		fmt.Sprintf(
			"normalized result %s exceeded while retaining %s (entry limit %d, normalized byte limit %d); later semantic data is omitted",
			reason,
			boundedDiagnosticValue(kind),
			entryLimit,
			byteLimit,
		),
	)
}

func boundedOccurrenceLabel(id OccurrenceID) string {
	return fmt.Sprintf(
		"%s::%s#%d",
		boundedDiagnosticValue(id.Package),
		boundedDiagnosticValue(id.Name),
		id.Ordinal,
	)
}

func boundedDiagnosticValue(value string) string {
	bounded, _ := boundedDiagnosticText(value, diagnosticValueBytes)
	return bounded
}

func boundedDiagnosticText(value string, limit int) (string, bool) {
	if limit <= 0 {
		return "", value != ""
	}
	if len(value) <= limit {
		return value, false
	}
	const suffix = "..."
	end := limit - len(suffix)
	if end < 0 {
		end = 0
	}
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end] + suffix, true
}

func eventDiagnostic(
	event protocol.Event,
	kind protocol.DiagnosticKind,
	message string,
) protocol.Diagnostic {
	const previewLimit = protocol.DefaultMaxDiagnosticBytes

	previewBytes := event.Raw
	if len(previewBytes) > previewLimit {
		previewBytes = previewBytes[:previewLimit]
	}
	preview := string(previewBytes)
	for len(preview) > 0 && !utf8.ValidString(preview) {
		preview = preview[:len(preview)-1]
	}
	return protocol.Diagnostic{
		Kind:      kind,
		Sequence:  event.Sequence,
		Line:      event.Line,
		Bytes:     int64(len(event.Raw)),
		Preview:   preview,
		Truncated: len(preview) < len(event.Raw),
		Message:   message,
	}
}

func (a *Analyzer) finalizeSignals() {
	for _, pkg := range a.packages {
		signals, _ := detectSignals(pkg.signalTail, "\n")
		mergeSignals(&pkg.value.Signals, signals)
		mergeSignals(&a.signals, signals)
		for _, test := range pkg.tests {
			signals, _ := detectSignals(test.signalTail, "\n")
			mergeSignals(&test.value.Signals, signals)
			mergeSignals(&pkg.value.Signals, signals)
			mergeSignals(&a.signals, signals)
		}
	}
	for _, build := range a.builds {
		signals, _ := detectSignals(build.signalTail, "\n")
		mergeSignals(&build.value.Signals, signals)
		mergeSignals(&a.signals, signals)
	}
	signals, _ := detectSignals(a.unattributedSignalTail, "\n")
	mergeSignals(&a.signals, signals)
}

func (a *Analyzer) observeEventTime(event *protocol.TestEvent) {
	if !event.TimePresent {
		return
	}
	a.eventTimestamps++
	if a.eventStartedAt == nil || event.Time.Before(*a.eventStartedAt) {
		a.eventStartedAt = timePointer(event.Time)
	}
	if a.eventFinishedAt == nil || event.Time.After(*a.eventFinishedAt) {
		a.eventFinishedAt = timePointer(event.Time)
	}
}

func (a *Analyzer) eventSequence(sequence uint64) uint64 {
	if sequence != 0 {
		return sequence
	}
	a.nextSequence++
	return a.nextSequence
}

func (a *Analyzer) observeSequence(sequence uint64) {
	if sequence > a.nextSequence {
		a.nextSequence = sequence
	}
}

func (a *Analyzer) activeParent(key testKey) *OccurrenceID {
	name := key.name
	for {
		offset := strings.LastIndexByte(name, '/')
		if offset < 0 {
			return nil
		}
		name = name[:offset]
		if parent := a.active[testKey{pkg: key.pkg, name: name}]; parent != nil {
			return occurrencePointer(parent.value.ID)
		}
	}
}

func (a *Analyzer) buildTiming(metadata RunMetadata) Timing {
	timing := Timing{
		EventStartedAt:  cloneTime(a.eventStartedAt),
		EventFinishedAt: cloneTime(a.eventFinishedAt),
		EventTimestamps: a.eventTimestamps,
	}
	if a.eventStartedAt != nil && a.eventFinishedAt != nil {
		timing.EventDuration = a.eventFinishedAt.Sub(*a.eventStartedAt)
		if timing.EventDuration < 0 {
			timing.EventDuration = 0
		}
		timing.EventEstimated = true
	}
	if !metadata.StartedAt.IsZero() {
		timing.WallStartedAt = timePointer(metadata.StartedAt)
	}
	if !metadata.FinishedAt.IsZero() {
		timing.WallFinishedAt = timePointer(metadata.FinishedAt)
	}
	if timing.WallStartedAt != nil && timing.WallFinishedAt != nil {
		timing.WallDuration = metadata.Duration
		if timing.WallDuration <= 0 {
			timing.WallDuration = timing.WallFinishedAt.Sub(*timing.WallStartedAt)
		}
		if timing.WallDuration < 0 {
			timing.WallDuration = 0
		}
		timing.WallMeasured = true
	}
	return timing
}

func (a *Analyzer) snapshotLocked() Result {
	result := Result{
		Finalized:                  a.finalized,
		Incomplete:                 a.resultIncomplete,
		Metadata:                   cloneMetadata(a.metadata),
		Timing:                     cloneTiming(a.currentTiming()),
		UnattributedOutput:         cloneOutputs(a.unattributedOutput),
		OutputBytes:                a.unattributedOutputBytes,
		OutputRetainedBytes:        a.unattributedRetainedBytes,
		OutputTruncated:            a.unattributedTruncated,
		TotalOutputBytes:           a.totalOutputBytes,
		TotalOutputRetainedBytes:   a.totalOutputRetainedBytes,
		TotalOutputTruncated:       a.totalOutputTruncated,
		NormalizedEntries:          a.normalizedEntries,
		NormalizedEntriesRetained:  a.normalizedEntriesRetained,
		NormalizedEntriesTruncated: a.normalizedEntriesTruncated,
		NormalizedBytes:            a.normalizedBytes,
		NormalizedBytesRetained:    a.normalizedBytesRetained,
		NormalizedBytesTruncated:   a.normalizedBytesTruncated,
		DiagnosticCount:            a.diagnosticCount,
		IntegrityDiagnosticCount:   a.integrityDiagnosticCount,
		DiagnosticsTruncated:       a.diagnosticCount > uint64(len(a.diagnostics)),
		Signals:                    a.signals,
	}

	for _, state := range a.packages {
		result.Packages = append(result.Packages, clonePackageState(state))
	}
	sort.SliceStable(result.Packages, func(i, j int) bool {
		return result.Packages[i].Name < result.Packages[j].Name
	})

	for _, state := range a.builds {
		result.Builds = append(result.Builds, cloneBuild(state.value))
	}
	sort.SliceStable(result.Builds, func(i, j int) bool {
		return result.Builds[i].ImportPath < result.Builds[j].ImportPath
	})

	result.Diagnostics = append([]Diagnostic(nil), a.diagnostics...)
	sort.SliceStable(result.Diagnostics, func(i, j int) bool {
		if result.Diagnostics[i].Sequence != result.Diagnostics[j].Sequence {
			return result.Diagnostics[i].Sequence < result.Diagnostics[j].Sequence
		}
		return result.Diagnostics[i].Line < result.Diagnostics[j].Line
	})

	for _, unknown := range a.unknownActions {
		result.UnknownActions = append(result.UnknownActions, *unknown)
	}
	sort.SliceStable(result.UnknownActions, func(i, j int) bool {
		if result.UnknownActions[i].Kind != result.UnknownActions[j].Kind {
			return result.UnknownActions[i].Kind < result.UnknownActions[j].Kind
		}
		return result.UnknownActions[i].Action < result.UnknownActions[j].Action
	})

	result.Summary = summarize(result)
	return result
}

func clonePackageState(state *packageState) Package {
	pkg := clonePackage(state.value)
	for _, test := range state.tests {
		pkg.Tests = append(pkg.Tests, cloneTest(test.value))
	}
	sort.SliceStable(pkg.Tests, func(i, j int) bool {
		if pkg.Tests[i].ID.Name != pkg.Tests[j].ID.Name {
			return pkg.Tests[i].ID.Name < pkg.Tests[j].ID.Name
		}
		return pkg.Tests[i].ID.Ordinal < pkg.Tests[j].ID.Ordinal
	})
	return pkg
}

func (a *Analyzer) currentTiming() Timing {
	if a.finalized {
		return a.timing
	}
	return Timing{
		EventStartedAt:  cloneTime(a.eventStartedAt),
		EventFinishedAt: cloneTime(a.eventFinishedAt),
		EventDuration:   eventDuration(a.eventStartedAt, a.eventFinishedAt),
		EventEstimated:  a.eventStartedAt != nil && a.eventFinishedAt != nil,
		EventTimestamps: a.eventTimestamps,
	}
}

func eventOutput(
	event protocol.Event,
	input *protocol.TestEvent,
	scope OutputScope,
) Output {
	output := Output{
		Sequence:      event.Sequence,
		Line:          event.Line,
		Scope:         scope,
		Package:       input.Package,
		Text:          input.Output,
		OriginalBytes: int64(len(input.Output)),
	}
	if input.TimePresent {
		output.Time = timePointer(input.Time)
	}
	return output
}

func (a *Analyzer) appendOutput(
	event protocol.Event,
	outputs *[]Output,
	totalBytes *int64,
	retainedBytes *int64,
	truncated *bool,
	output Output,
	maxBytes int64,
) {
	originalBytes := int64(len(output.Text))
	if originalBytes == 0 {
		return
	}
	identityBytes := outputStringBytes(output)
	output.OriginalBytes = originalBytes
	*totalBytes = saturatingAddInt64(*totalBytes, originalBytes)
	a.totalOutputBytes = saturatingAddInt64(
		a.totalOutputBytes,
		originalBytes,
	)
	a.observeEntry()

	retainBytes := originalBytes
	if maxBytes > 0 {
		remaining := maxBytes - *retainedBytes
		if remaining < 0 {
			remaining = 0
		}
		if retainBytes > remaining {
			retainBytes = remaining
		}
	}
	if a.maxTotalOutputBytes > 0 {
		remaining := a.maxTotalOutputBytes - a.totalOutputRetainedBytes
		if remaining < 0 {
			remaining = 0
		}
		if retainBytes > remaining {
			retainBytes = remaining
		}
	}
	if retainBytes < originalBytes {
		*truncated = true
		a.totalOutputTruncated = true
	}
	if retainBytes == 0 {
		a.normalizedEntriesTruncated = true
		a.observeNormalizedBytes(identityBytes)
		if identityBytes != 0 {
			a.normalizedBytesTruncated = true
		}
		return
	}

	retain := validUTF8PrefixBytes(output.Text, int(retainBytes))
	if int64(retain) < originalBytes {
		*truncated = true
		a.totalOutputTruncated = true
	}
	if retain == 0 && originalBytes != 0 {
		a.normalizedEntriesTruncated = true
		a.observeNormalizedBytes(identityBytes)
		if identityBytes != 0 {
			a.normalizedBytesTruncated = true
		}
		return
	}
	if !a.retainObservedEntry(
		event,
		"output chunk",
		identityBytes,
	) {
		*truncated = true
		a.normalizedEntriesTruncated = true
		if originalBytes != 0 {
			a.totalOutputTruncated = true
		}
		return
	}
	output.Text = output.Text[:retain]
	*retainedBytes = saturatingAddInt64(*retainedBytes, int64(retain))
	a.totalOutputRetainedBytes = saturatingAddInt64(
		a.totalOutputRetainedBytes,
		int64(retain),
	)
	*outputs = append(*outputs, output)
}

func (a *Analyzer) observeDroppedOutput(
	output string,
	normalizedBytes int64,
) {
	originalBytes := int64(len(output))
	if originalBytes == 0 {
		return
	}
	a.totalOutputBytes = saturatingAddInt64(
		a.totalOutputBytes,
		originalBytes,
	)
	a.observeEntry()
	a.observeNormalizedBytes(normalizedBytes)
	a.normalizedEntriesTruncated = true
	if normalizedBytes != 0 {
		a.normalizedBytesTruncated = true
	}
	a.totalOutputTruncated = true
}

func validUTF8PrefixBytes(value string, limit int) int {
	if limit <= 0 {
		return 0
	}
	if limit > len(value) {
		limit = len(value)
	}
	offset := 0
	for offset < limit {
		runeValue, size := utf8.DecodeRuneInString(value[offset:limit])
		if runeValue == utf8.RuneError && size == 1 {
			return offset
		}
		offset += size
	}
	return offset
}

func (a *Analyzer) reserveEntry(
	event protocol.Event,
	kind string,
	normalizedBytes int64,
) bool {
	a.observeEntry()
	return a.retainObservedEntry(event, kind, normalizedBytes)
}

func (a *Analyzer) observeOmittedSemanticEntry(normalizedBytes int64) {
	a.observeEntry()
	a.observeNormalizedBytes(normalizedBytes)
	a.normalizedEntriesTruncated = true
	if normalizedBytes != 0 {
		a.normalizedBytesTruncated = true
	}
	a.resultIncomplete = true
}

func (a *Analyzer) observeEntry() {
	if a.normalizedEntries != math.MaxUint64 {
		a.normalizedEntries++
	}
}

func (a *Analyzer) retainObservedEntry(
	event protocol.Event,
	kind string,
	normalizedBytes int64,
) bool {
	a.observeNormalizedBytes(normalizedBytes)
	entryExceeded := a.maxResultEntries > 0 &&
		a.normalizedEntriesRetained >= uint64(a.maxResultEntries)
	normalizedExceeded := a.maxNormalizedBytes > 0 &&
		normalizedBytes > a.maxNormalizedBytes-a.normalizedBytesRetained
	if entryExceeded || normalizedExceeded {
		a.normalizedEntriesTruncated = true
		if normalizedBytes != 0 {
			a.normalizedBytesTruncated = true
		}
		a.resultIncomplete = true
		if !a.resourceCapacityDiagnosed {
			a.resourceCapacityDiagnosed = true
			a.addDiagnostic(resourceCapacityDiagnostic(
				event,
				kind,
				a.maxResultEntries,
				a.maxNormalizedBytes,
				entryExceeded,
				normalizedExceeded,
			))
		}
		return false
	}
	if a.normalizedEntriesRetained != math.MaxUint64 {
		a.normalizedEntriesRetained++
	}
	a.normalizedBytesRetained = saturatingAddInt64(
		a.normalizedBytesRetained,
		normalizedBytes,
	)
	return true
}

func (a *Analyzer) reserveNormalizedBytes(
	event protocol.Event,
	kind string,
	normalizedBytes int64,
) bool {
	a.observeNormalizedBytes(normalizedBytes)
	if a.maxNormalizedBytes > 0 &&
		normalizedBytes > a.maxNormalizedBytes-a.normalizedBytesRetained {
		a.normalizedBytesTruncated = true
		a.resultIncomplete = true
		if !a.resourceCapacityDiagnosed {
			a.resourceCapacityDiagnosed = true
			a.addDiagnostic(resourceCapacityDiagnostic(
				event,
				kind,
				a.maxResultEntries,
				a.maxNormalizedBytes,
				false,
				true,
			))
		}
		return false
	}
	a.normalizedBytesRetained = saturatingAddInt64(
		a.normalizedBytesRetained,
		normalizedBytes,
	)
	return true
}

func (a *Analyzer) observeNormalizedBytes(count int64) {
	a.normalizedBytes = saturatingAddInt64(a.normalizedBytes, count)
}

func saturatingAddInt64(value int64, increment int64) int64 {
	if increment <= 0 {
		return value
	}
	if value > math.MaxInt64-increment {
		return math.MaxInt64
	}
	return value + increment
}

func applyDuration(
	elapsed *time.Duration,
	source *DurationSource,
	input *protocol.TestEvent,
	startedAt *time.Time,
	finishedAt *time.Time,
) {
	if input.ElapsedPresent && input.Elapsed >= 0 {
		*elapsed = secondsDuration(input.Elapsed)
		*source = DurationGoElapsed
		return
	}
	if startedAt != nil && finishedAt != nil {
		value := finishedAt.Sub(*startedAt)
		if value < 0 {
			value = 0
		}
		*elapsed = value
		*source = DurationEventEstimate
	}
}

func secondsDuration(seconds float64) time.Duration {
	if math.IsNaN(seconds) || seconds <= 0 {
		return 0
	}
	maxSeconds := float64(math.MaxInt64) / float64(time.Second)
	if math.IsInf(seconds, 1) || seconds >= maxSeconds {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(seconds * float64(time.Second))
}

func inferTestKind(name string) TestKind {
	root := name
	if offset := strings.IndexByte(root, '/'); offset >= 0 {
		root = root[:offset]
	}
	switch {
	case strings.HasPrefix(root, "Benchmark"):
		return TestKindBenchmark
	case strings.HasPrefix(root, "Example"):
		return TestKindExample
	case strings.HasPrefix(root, "Fuzz"):
		return TestKindFuzz
	case strings.HasPrefix(root, "Test"):
		return TestKindTest
	default:
		return TestKindUnknown
	}
}

func benchmarkOutputName(output string) (string, bool) {
	if output == "" || output[len(output)-1] != '\n' {
		return "", false
	}
	line := output[:len(output)-1]
	if strings.HasSuffix(line, "\r") {
		line = line[:len(line)-1]
	}
	if line == "" || strings.ContainsAny(line, "\r\n") {
		return "", false
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || (len(fields)-2)%2 != 0 ||
		!isBenchmarkName(fields[0]) {
		return "", false
	}
	count := strings.ReplaceAll(fields[1], ",", "")
	iterations, err := strconv.ParseUint(count, 10, 64)
	if err != nil || iterations == 0 {
		return "", false
	}
	for i := 2; i < len(fields); i += 2 {
		value := strings.ReplaceAll(fields[i], ",", "")
		if _, err := strconv.ParseFloat(value, 64); err != nil ||
			fields[i+1] == "" {
			return "", false
		}
	}
	return fields[0], true
}

func benchmarkOutputPrefixName(output string) (string, bool) {
	if output == "" || strings.ContainsAny(output, "\r\n") ||
		output[len(output)-1] != '\t' ||
		strings.IndexByte(output, '\t') != len(output)-1 {
		return "", false
	}
	name := strings.TrimRight(output[:len(output)-1], " ")
	if name == "" || strings.IndexFunc(name, unicode.IsSpace) >= 0 ||
		!isBenchmarkName(name) {
		return "", false
	}
	return name, true
}

func isBenchmarkName(name string) bool {
	if !strings.HasPrefix(name, "Benchmark") {
		return false
	}
	if len(name) == len("Benchmark") {
		return true
	}
	value, _ := utf8.DecodeRuneInString(name[len("Benchmark"):])
	return !unicode.IsLower(value)
}

func benchmarkBaseName(name string) string {
	offset := strings.LastIndexByte(name, '-')
	if offset < 0 || offset == len(name)-1 {
		return name
	}
	if _, err := strconv.ParseUint(name[offset+1:], 10, 64); err == nil {
		return name[:offset]
	}
	return name
}

func benchmarkNamesMatch(testName, resultName string) bool {
	return testName == resultName || testName == benchmarkBaseName(resultName)
}

func detectSignals(tail, output string) (Signals, string) {
	text, nextTail := joinedTail(tail, output)
	lower := strings.ToLower(text)
	timeout := strings.Contains(lower, "test timed out after") ||
		strings.Contains(lower, "test timeout")
	race := strings.Contains(lower, "warning: data race") ||
		strings.Contains(lower, "race detected during execution of test")
	panicSignal := hasCompleteSignalLine(lower, "panic:") ||
		hasCompleteSignalLine(lower, "fatal error:")
	if timeout {
		panicSignal = false
	}
	return Signals{
		Race:    race,
		Panic:   panicSignal,
		Timeout: timeout,
	}, nextTail
}

func hasCompleteSignalLine(text, marker string) bool {
	searchAt := 0
	for searchAt < len(text) {
		offset := strings.Index(text[searchAt:], marker)
		if offset < 0 {
			return false
		}
		offset += searchAt
		atLineStart := offset == 0 || text[offset-1] == '\n'
		if atLineStart && strings.ContainsRune(text[offset+len(marker):], '\n') {
			return true
		}
		searchAt = offset + len(marker)
	}
	return false
}

func joinedTail(tail, output string) (string, string) {
	text := tail + output
	if len(text) <= signalTailBytes {
		return text, text
	}
	return text, text[len(text)-signalTailBytes:]
}

func mergeSignals(destination *Signals, source Signals) {
	destination.Race = destination.Race || source.Race
	destination.Panic = destination.Panic || source.Panic
	destination.Timeout = destination.Timeout || source.Timeout
}

func summarize(result Result) Summary {
	summary := Summary{
		TotalOutputBytes:           result.TotalOutputBytes,
		TotalOutputRetainedBytes:   result.TotalOutputRetainedBytes,
		TotalOutputTruncated:       result.TotalOutputTruncated,
		NormalizedEntries:          result.NormalizedEntries,
		NormalizedEntriesRetained:  result.NormalizedEntriesRetained,
		NormalizedEntriesTruncated: result.NormalizedEntriesTruncated,
		NormalizedBytes:            result.NormalizedBytes,
		NormalizedBytesRetained:    result.NormalizedBytesRetained,
		NormalizedBytesTruncated:   result.NormalizedBytesTruncated,
		Incomplete:                 result.Incomplete,
		Diagnostics:                result.DiagnosticCount,
		DiagnosticsRetained:        uint64(len(result.Diagnostics)),
		IntegrityDiagnostics:       result.IntegrityDiagnosticCount,
		Race:                       result.Signals.Race,
		Panic:                      result.Signals.Panic,
		Timeout:                    result.Signals.Timeout,
	}
	failedBuilds := make(map[string]struct{})
	for _, pkg := range result.Packages {
		countStatus(&summary.Packages, pkg.Status)
		if pkg.Cached {
			summary.CachedPackages++
		}
		if pkg.NoTests {
			summary.PackagesWithoutTests++
		}
		if pkg.FailedBuild != "" {
			failedBuilds[pkg.FailedBuild] = struct{}{}
		}
		for _, test := range pkg.Tests {
			countStatus(&summary.Tests, test.Status)
		}
	}
	for _, build := range result.Builds {
		countStatus(&summary.Builds, build.Status)
		if build.Status == StatusFailed {
			failedBuilds[build.ImportPath] = struct{}{}
		}
	}
	for _, unknown := range result.UnknownActions {
		summary.UnknownActions += unknown.Count
	}
	summary.BuildFailures = uint64(len(failedBuilds))
	return summary
}

func countStatus(counts *OutcomeCounts, status Status) {
	counts.Total++
	switch status {
	case StatusPassed:
		counts.Passed++
	case StatusFailed:
		counts.Failed++
	case StatusSkipped:
		counts.Skipped++
	case StatusBenchmarked:
		counts.Benchmarked++
	case StatusIncomplete:
		counts.Incomplete++
	case StatusRunning:
		counts.Running++
	case StatusPaused:
		counts.Paused++
	default:
		counts.Unknown++
	}
}

func clonePackage(input Package) Package {
	output := input
	output.StartedAt = cloneTime(input.StartedAt)
	output.FinishedAt = cloneTime(input.FinishedAt)
	output.Tests = nil
	output.Output = cloneOutputs(input.Output)
	output.Attributes = cloneAttributes(input.Attributes)
	output.Artifacts = cloneArtifacts(input.Artifacts)
	return output
}

func cloneTest(input TestOccurrence) TestOccurrence {
	output := input
	output.Parent = occurrencePointerValue(input.Parent)
	output.StartedAt = cloneTime(input.StartedAt)
	output.FinishedAt = cloneTime(input.FinishedAt)
	output.Output = cloneOutputs(input.Output)
	output.Attributes = cloneAttributes(input.Attributes)
	output.Artifacts = cloneArtifacts(input.Artifacts)
	return output
}

func cloneBuild(input Build) Build {
	output := input
	output.Output = cloneOutputs(input.Output)
	return output
}

func cloneOutputs(input []Output) []Output {
	if input == nil {
		return nil
	}
	output := make([]Output, len(input))
	for i := range input {
		output[i] = input[i]
		output[i].Time = cloneTime(input[i].Time)
		output[i].Test = occurrencePointerValue(input[i].Test)
	}
	sort.SliceStable(output, func(i, j int) bool {
		if output[i].Sequence != output[j].Sequence {
			return output[i].Sequence < output[j].Sequence
		}
		return output[i].Line < output[j].Line
	})
	return output
}

func cloneAttributes(input []Attribute) []Attribute {
	if input == nil {
		return nil
	}
	output := make([]Attribute, len(input))
	for i := range input {
		output[i] = input[i]
		output[i].Time = cloneTime(input[i].Time)
		output[i].Test = occurrencePointerValue(input[i].Test)
	}
	sort.SliceStable(output, func(i, j int) bool {
		if output[i].Sequence != output[j].Sequence {
			return output[i].Sequence < output[j].Sequence
		}
		if output[i].Line != output[j].Line {
			return output[i].Line < output[j].Line
		}
		if output[i].Key != output[j].Key {
			return output[i].Key < output[j].Key
		}
		return output[i].Value < output[j].Value
	})
	return output
}

func cloneArtifacts(input []Artifact) []Artifact {
	if input == nil {
		return nil
	}
	output := make([]Artifact, len(input))
	for i := range input {
		output[i] = input[i]
		output[i].Time = cloneTime(input[i].Time)
		output[i].Test = occurrencePointerValue(input[i].Test)
	}
	sort.SliceStable(output, func(i, j int) bool {
		if output[i].Sequence != output[j].Sequence {
			return output[i].Sequence < output[j].Sequence
		}
		if output[i].Line != output[j].Line {
			return output[i].Line < output[j].Line
		}
		return output[i].Path < output[j].Path
	})
	return output
}

func (a *Analyzer) cloneMetadataBounded(input RunMetadata) RunMetadata {
	output := input
	output.Command = nil
	output.WorkDir = ""
	output.Signal = ""
	output.Cancellation = ""
	output.CancellationSignal = ""

	sequence := a.nextSequence
	if sequence != math.MaxUint64 {
		sequence++
	}
	event := protocol.Event{Sequence: sequence, Line: sequence}
	commandTruncated := false
	for _, argument := range input.Command {
		argumentBytes := stringBytes(argument)
		if commandTruncated {
			a.observeOmittedSemanticEntry(argumentBytes)
			continue
		}
		if !a.reserveEntry(
			event,
			"run command argument",
			argumentBytes,
		) {
			commandTruncated = true
			continue
		}
		output.Command = append(output.Command, argument)
	}
	retainMetadataString := func(
		kind string,
		value string,
		destination *string,
	) {
		if value == "" {
			return
		}
		if a.reserveNormalizedBytes(
			event,
			kind,
			stringBytes(value),
		) {
			*destination = value
		}
	}
	retainMetadataString("run working directory", input.WorkDir, &output.WorkDir)
	retainMetadataString("run signal", input.Signal, &output.Signal)
	retainMetadataString(
		"run cancellation",
		input.Cancellation,
		&output.Cancellation,
	)
	retainMetadataString(
		"run cancellation signal",
		input.CancellationSignal,
		&output.CancellationSignal,
	)
	return output
}

func cloneMetadata(input RunMetadata) RunMetadata {
	output := input
	output.Command = append([]string(nil), input.Command...)
	return output
}

func cloneTiming(input Timing) Timing {
	output := input
	output.WallStartedAt = cloneTime(input.WallStartedAt)
	output.WallFinishedAt = cloneTime(input.WallFinishedAt)
	output.EventStartedAt = cloneTime(input.EventStartedAt)
	output.EventFinishedAt = cloneTime(input.EventFinishedAt)
	return output
}

func eventDuration(start, finish *time.Time) time.Duration {
	if start == nil || finish == nil {
		return 0
	}
	duration := finish.Sub(*start)
	if duration < 0 {
		return 0
	}
	return duration
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	return timePointer(*value)
}

func occurrencePointer(value OccurrenceID) *OccurrenceID {
	return &value
}

func occurrencePointerValue(value *OccurrenceID) *OccurrenceID {
	if value == nil {
		return nil
	}
	return occurrencePointer(*value)
}
