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
	"time"

	"github.com/greenpau/tested/pkg/protocol"
)

// Progress is a transient normalized update, without a copy of the accumulated
// transcript. Output contains only bytes retained for this record. It never
// establishes an authoritative child outcome.
type Progress struct {
	Action          string
	Scope           OutputScope
	Package         string
	Test            *OccurrenceID
	Status          Status
	Elapsed         time.Duration
	DurationSource  DurationSource
	Output          string
	OutputBytes     int64
	OutputTruncated bool
	Diagnostics     uint64
	// Tests is populated only for package terminal events.
	Tests *OutcomeCounts
}

// AddRecordWithProgress consumes a record and returns its normalized progress
// atomically. Unlike Snapshot, it does not copy or sort accumulated evidence.
func (a *Analyzer) AddRecordWithProgress(record protocol.Record) (Progress, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finalized {
		return Progress{}, ErrFinalized
	}
	before := a.diagnosticCount
	a.collectProgress = true
	defer func() {
		a.collectProgress = false
		a.progressOutput = Output{}
	}()
	a.addRecordLocked(record)
	update := a.progressLocked(record)
	update.Diagnostics = a.diagnosticCount - before
	return update, nil
}

func (a *Analyzer) progressLocked(record protocol.Record) Progress {
	update := Progress{
		Output:          a.progressOutput.Text,
		OutputBytes:     a.progressOutput.OriginalBytes,
		OutputTruncated: int64(len(a.progressOutput.Text)) < a.progressOutput.OriginalBytes,
	}
	event := record.Event
	if event == nil {
		return update
	}
	update.Action = event.Action()
	switch {
	case event.Kind == protocol.EventKindTest && event.Test != nil:
		input := event.Test
		pkg := a.packages[input.Package]
		if pkg == nil {
			return Progress{}
		}
		update.Scope, update.Package = OutputPackage, pkg.value.Name
		update.Status, update.Elapsed, update.DurationSource = pkg.value.Status, pkg.value.Elapsed, pkg.value.DurationSource
		if input.Test == "" && (input.Action == protocol.ActionPass || input.Action == protocol.ActionFail || input.Action == protocol.ActionSkip) {
			counts := &OutcomeCounts{}
			for _, test := range pkg.tests {
				countStatus(counts, test.value.Status)
			}
			update.Tests = counts
		}
		key := testKey{pkg: input.Package, name: input.Test}
		if a.progressOutput.Test != nil {
			key = testKey{pkg: a.progressOutput.Test.Package, name: a.progressOutput.Test.Name}
		}
		if key.name != "" {
			state := a.latest[key]
			if state == nil {
				return Progress{}
			}
			update.Scope = OutputTest
			update.Test = occurrencePointer(state.value.ID)
			update.Status, update.Elapsed, update.DurationSource = state.value.Status, state.value.Elapsed, state.value.DurationSource
		}
	case event.Kind == protocol.EventKindBuild && event.Build != nil:
		state := a.builds[event.Build.ImportPath]
		if state == nil {
			return Progress{}
		}
		update.Scope, update.Package, update.Status = OutputBuild, state.value.ImportPath, state.value.Status
	default:
		if a.progressOutput.Scope != OutputUnattributed {
			return Progress{}
		}
		update.Scope = OutputUnattributed
	}
	return update
}
