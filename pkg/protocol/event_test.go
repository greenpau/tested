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

package protocol

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDecode(t *testing.T) {
	timestamp := time.Date(2026, time.July, 29, 12, 34, 56, 123, time.UTC)
	tests := []struct {
		name        string
		input       string
		kind        EventKind
		action      string
		knownAction bool
		validate    func(*testing.T, Event)
	}{
		{
			name: "test event with presence and extension",
			input: `{"Time":"2026-07-29T12:34:56.000000123Z","Action":"fail",` +
				`"Package":"example.com/p","Test":"TestOne/sub","Elapsed":0,` +
				`"Output":"nope\n","FailedBuild":"example.com/dependency",` +
				`"Future":{"enabled":true}}`,
			kind:        EventKindTest,
			action:      ActionFail,
			knownAction: true,
			validate: func(t *testing.T, event Event) {
				t.Helper()
				got := event.Test
				if got == nil {
					t.Fatal("missing TestEvent")
				}
				if !got.TimePresent || !got.Time.Equal(timestamp) {
					t.Fatalf("Time = (%v, %v), want %v", got.Time, got.TimePresent, timestamp)
				}
				if !got.ElapsedPresent || got.Elapsed != 0 {
					t.Fatalf("Elapsed = (%v, %v), want explicit zero", got.Elapsed, got.ElapsedPresent)
				}
				if got.Package != "example.com/p" || got.Test != "TestOne/sub" {
					t.Fatalf("test identity = %q/%q", got.Package, got.Test)
				}
				if got.FailedBuild != "example.com/dependency" {
					t.Fatalf("FailedBuild = %q", got.FailedBuild)
				}
				if string(got.UnknownFields["Future"]) != `{"enabled":true}` {
					t.Fatalf("unknown fields = %#v", got.UnknownFields)
				}
			},
		},
		{
			name:        "Go 1.26 build output",
			input:       `{"ImportPath":"example.com/p [example.com/p.test]","Action":"build-output","Output":"compile error\n","Future":7}`,
			kind:        EventKindBuild,
			action:      ActionBuildOutput,
			knownAction: true,
			validate: func(t *testing.T, event Event) {
				t.Helper()
				got := event.Build
				if got == nil || got.ImportPath != "example.com/p [example.com/p.test]" {
					t.Fatalf("BuildEvent = %#v", got)
				}
				if string(got.UnknownFields["Future"]) != "7" {
					t.Fatalf("unknown fields = %#v", got.UnknownFields)
				}
			},
		},
		{
			name:        "Go 1.25 test attribute",
			input:       `{"Action":"attr","Package":"example.com/p","Test":"TestOne","Key":"region","Value":"east"}`,
			kind:        EventKindTest,
			action:      ActionAttr,
			knownAction: true,
			validate: func(t *testing.T, event Event) {
				t.Helper()
				got := event.Test
				if got == nil || got.Key != "region" || got.Value != "east" {
					t.Fatalf("attribute event = %#v", got)
				}
				if len(got.UnknownFields) != 0 {
					t.Fatalf("attribute unknown fields = %#v", got.UnknownFields)
				}
			},
		},
		{
			name:        "Go 1.26 test artifacts",
			input:       `{"Action":"artifacts","Package":"example.com/p","Test":"TestOne","Path":"/tmp/artifacts"}`,
			kind:        EventKindTest,
			action:      ActionArtifacts,
			knownAction: true,
			validate: func(t *testing.T, event Event) {
				t.Helper()
				got := event.Test
				if got == nil || got.Path != "/tmp/artifacts" {
					t.Fatalf("artifacts event = %#v", got)
				}
				if len(got.UnknownFields) != 0 {
					t.Fatalf("artifacts unknown fields = %#v", got.UnknownFields)
				}
			},
		},
		{
			name:        "future test action inferred by Package",
			input:       `{"Action":"retry","Package":"example.com/p","Attempt":2}`,
			kind:        EventKindTest,
			action:      "retry",
			knownAction: false,
		},
		{
			name: "bounded extension preserves arbitrary JSON number",
			input: `{"Action":"start","Package":"example.com/p",` +
				`"Future":{"Huge":1e10000}}`,
			kind:        EventKindTest,
			action:      ActionStart,
			knownAction: true,
			validate: func(t *testing.T, event Event) {
				t.Helper()
				if got := string(
					event.Test.UnknownFields["Future"],
				); got != `{"Huge":1e10000}` {
					t.Fatalf("future number = %q", got)
				}
			},
		},
		{
			name:        "future build action inferred by ImportPath",
			input:       `{"Action":"build-cache-hit","ImportPath":"example.com/p","Digest":"abc"}`,
			kind:        EventKindBuild,
			action:      "build-cache-hit",
			knownAction: false,
		},
		{
			name:        "ambiguous future event stays unknown",
			input:       `{"Action":"notice","Output":"hello","Future":true}`,
			kind:        EventKindUnknown,
			action:      "notice",
			knownAction: false,
			validate: func(t *testing.T, event Event) {
				t.Helper()
				if event.Unknown == nil ||
					string(event.Unknown.Fields["Future"]) != "true" {
					t.Fatalf("Unknown = %#v", event.Unknown)
				}
			},
		},
		{
			name:        "known action wins over stray opposing identity",
			input:       `{"Action":"build-fail","ImportPath":"example.com/p","Package":"display-name"}`,
			kind:        EventKindBuild,
			action:      ActionBuildFail,
			knownAction: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event, err := Decode([]byte(test.input))
			if err != nil {
				t.Fatalf("Decode() unexpected error: %v", err)
			}
			if event.Kind != test.kind {
				t.Fatalf("Kind = %q, want %q", event.Kind, test.kind)
			}
			if event.Action() != test.action {
				t.Fatalf("Action() = %q, want %q", event.Action(), test.action)
			}
			if event.IsKnownAction() != test.knownAction {
				t.Fatalf("IsKnownAction() = %v, want %v", event.IsKnownAction(), test.knownAction)
			}
			if string(event.Raw) != test.input {
				t.Fatalf("Raw = %q, want exact input", event.Raw)
			}
			if test.validate != nil {
				test.validate(t, event)
			}
		})
	}
}

func TestDecodeRejectsMalformedRecords(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "empty", input: "", wantErr: "empty record"},
		{name: "whitespace", input: " \t ", wantErr: "empty record"},
		{name: "invalid JSON", input: "{", wantErr: "unexpected end"},
		{name: "array", input: `["start"]`, wantErr: "cannot unmarshal"},
		{name: "null", input: "null", wantErr: "expected JSON object"},
		{name: "missing action", input: `{"Package":"p"}`, wantErr: "missing required field"},
		{name: "empty action", input: `{"Action":""}`, wantErr: "must not be empty"},
		{name: "null action", input: `{"Action":null}`, wantErr: `field "Action" must not be null`},
		{name: "action type", input: `{"Action":7}`, wantErr: "cannot unmarshal"},
		{name: "known field type", input: `{"Action":"run","Test":false}`, wantErr: `field "Test"`},
		{name: "time type", input: `{"Action":"start","Time":"yesterday"}`, wantErr: `field "Time"`},
		{name: "elapsed type", input: `{"Action":"pass","Elapsed":"fast"}`, wantErr: `field "Elapsed"`},
		{name: "null package", input: `{"Action":"start","Package":null}`, wantErr: `field "Package" must not be null`},
		{name: "null test", input: `{"Action":"run","Test":null}`, wantErr: `field "Test" must not be null`},
		{name: "null output", input: `{"Action":"output","Output":null}`, wantErr: `field "Output" must not be null`},
		{name: "null failed build", input: `{"Action":"fail","FailedBuild":null}`, wantErr: `field "FailedBuild" must not be null`},
		{name: "null time", input: `{"Action":"start","Time":null}`, wantErr: `field "Time" must not be null`},
		{name: "null elapsed", input: `{"Action":"pass","Elapsed":null}`, wantErr: `field "Elapsed" must not be null`},
		{name: "null attribute key", input: `{"Action":"attr","Key":null}`, wantErr: `field "Key" must not be null`},
		{name: "null attribute value", input: `{"Action":"attr","Value":null}`, wantErr: `field "Value" must not be null`},
		{name: "null artifact path", input: `{"Action":"artifacts","Path":null}`, wantErr: `field "Path" must not be null`},
		{name: "null import path", input: `{"Action":"build-fail","ImportPath":null}`, wantErr: `field "ImportPath" must not be null`},
		{name: "null build output", input: `{"Action":"build-output","Output":null}`, wantErr: `field "Output" must not be null`},
		{
			name:    "duplicate action",
			input:   `{"Action":"start","Action":"pass"}`,
			wantErr: `duplicate JSON object member "Action"`,
		},
		{
			name:    "duplicate known field",
			input:   `{"Action":"start","Package":"one","Package":"two"}`,
			wantErr: `duplicate JSON object member "Package"`,
		},
		{
			name:    "duplicate extension field",
			input:   `{"Action":"start","Future":1,"Future":2}`,
			wantErr: `duplicate JSON object member "Future"`,
		},
		{
			name:    "escaped duplicate action",
			input:   `{"\u0041ction":"start","Action":"pass"}`,
			wantErr: `duplicate JSON object member "Action"`,
		},
		{
			name: "nested duplicate extension field",
			input: `{"Action":"start",` +
				`"Future":{"Enabled":true,"Enabled":false}}`,
			wantErr: `duplicate JSON object member "Enabled"`,
		},
		{
			name: "nested escaped duplicate extension field",
			input: `{"Action":"start",` +
				`"Future":[{"Key":1,"\u004bey":2}]}`,
			wantErr: `duplicate JSON object member "Key"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode([]byte(test.input))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Decode() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestDecodeBoundsUnknownFieldCardinality(t *testing.T) {
	atLimit := eventWithUnknownFields(MaxUnknownFieldsPerEvent)
	event, err := Decode(atLimit)
	if err != nil {
		t.Fatalf("Decode(at limit) unexpected error: %v", err)
	}
	if event.Test == nil ||
		len(event.Test.UnknownFields) != MaxUnknownFieldsPerEvent ||
		string(event.Raw) != string(atLimit) {
		t.Fatalf("bounded extension event = %#v", event)
	}

	overLimit := eventWithUnknownFields(MaxUnknownFieldsPerEvent + 1)
	if _, err := Decode(overLimit); err == nil ||
		!strings.Contains(
			err.Error(),
			"unknown field count exceeds maximum of 64",
		) {
		t.Fatalf("Decode(over limit) error = %v", err)
	}
}

func TestDecodeBoundsUnknownFieldRetentionBytes(t *testing.T) {
	const name = "Future"
	atLimitPayload := strings.Repeat(
		"x",
		MaxUnknownFieldBytesPerEvent-len(name)-2,
	)
	atLimit := []byte(
		`{"Action":"start","Future":"` + atLimitPayload + `"}`,
	)
	event, err := Decode(atLimit)
	if err != nil {
		t.Fatalf("Decode(at byte limit) unexpected error: %v", err)
	}
	if event.Test == nil ||
		len(event.Test.UnknownFields) != 1 ||
		len(event.Test.UnknownFields[name])+len(name) !=
			MaxUnknownFieldBytesPerEvent {
		t.Fatalf("byte-bounded extension event = %#v", event.Test)
	}

	overLimit := []byte(
		`{"Action":"start","Future":"` + atLimitPayload + `x"}`,
	)
	if _, err := Decode(overLimit); err == nil ||
		!strings.Contains(
			err.Error(),
			"unknown field retention exceeds maximum of 65536 bytes",
		) {
		t.Fatalf("Decode(over byte limit) error = %v", err)
	}

	const opposingName = "ImportPath"
	opposingPayload := strings.Repeat(
		"x",
		MaxUnknownFieldBytesPerEvent-len(opposingName)-1,
	)
	opposingSchema := []byte(
		`{"Action":"start","ImportPath":"` + opposingPayload + `"}`,
	)
	if _, err := Decode(opposingSchema); err == nil ||
		!strings.Contains(
			err.Error(),
			"unknown field retention exceeds maximum of 65536 bytes",
		) {
		t.Fatalf("Decode(opposing schema field) error = %v", err)
	}
}

func TestDecodeUnknownFieldCardinalityBoundsAllocations(t *testing.T) {
	input := eventWithUnknownFields(10_000)
	var decodeErr error
	allocations := testing.AllocsPerRun(10, func() {
		_, decodeErr = Decode(input)
	})
	if decodeErr == nil ||
		!strings.Contains(decodeErr.Error(), "unknown field count") {
		t.Fatalf("Decode(adversarial cardinality) error = %v", decodeErr)
	}
	// Decode stops after the first field beyond the fixed allowance. This
	// generous ceiling distinguishes bounded prefix work from allocating a
	// RawMessage and map entry for every field in the complete record.
	if allocations > 1_000 {
		t.Fatalf(
			"Decode(adversarial cardinality) allocations = %.0f, want <= 1000",
			allocations,
		)
	}
}

func TestDecodeOwnsRawAndUnknownFields(t *testing.T) {
	input := []byte(`{"Action":"retry","Package":"p","Future":{"value":1}}`)
	event, err := Decode(input)
	if err != nil {
		t.Fatalf("Decode() unexpected error: %v", err)
	}
	input[0] = '['
	if event.Raw[0] != '{' {
		t.Fatalf("Raw aliases input: %q", event.Raw)
	}
	event.Test.UnknownFields["Future"][0] = '['
	var value map[string]json.RawMessage
	if err := json.Unmarshal(event.Raw, &value); err != nil {
		t.Fatalf("Raw was mutated with unknown field: %v", err)
	}
}

func FuzzDecode(f *testing.F) {
	for _, seed := range []string{
		`{"Action":"start","Package":"p"}`,
		`{"Action":"build-output","ImportPath":"p","Output":"x\n"}`,
		`{"Action":"future","Extension":{"nested":[1,2,3]}}`,
		`{"Action":"start","Action":"pass"}`,
		string(eventWithUnknownFields(MaxUnknownFieldsPerEvent + 1)),
		"",
		"{",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		event, err := Decode(input)
		if err != nil {
			return
		}
		if event.Action() == "" {
			t.Fatal("successful decode produced an empty action")
		}
		if string(event.Raw) != string(input) {
			t.Fatal("successful decode did not preserve raw bytes")
		}
	})
}

func FuzzDecodeRejectsDuplicateMembers(f *testing.F) {
	for _, seed := range []struct {
		name   string
		first  string
		second string
	}{
		{name: "Future", first: "one", second: "two"},
		{name: "Action", first: "run", second: "pass"},
		{name: "\u2603", first: "", second: "value"},
	} {
		f.Add(seed.name, seed.first, seed.second)
	}
	f.Fuzz(func(t *testing.T, name, first, second string) {
		if len(name) > 256 || len(first) > 256 || len(second) > 256 {
			t.Skip()
		}
		nameJSON, err := json.Marshal(name)
		if err != nil {
			t.Fatalf("marshal member name: %v", err)
		}
		firstJSON, err := json.Marshal(first)
		if err != nil {
			t.Fatalf("marshal first value: %v", err)
		}
		secondJSON, err := json.Marshal(second)
		if err != nil {
			t.Fatalf("marshal second value: %v", err)
		}
		record := make([]byte, 0, 64+len(nameJSON)*2+
			len(firstJSON)+len(secondJSON))
		record = append(record, `{"Action":"start",`...)
		record = append(record, nameJSON...)
		record = append(record, ':')
		record = append(record, firstJSON...)
		record = append(record, ',')
		record = append(record, nameJSON...)
		record = append(record, ':')
		record = append(record, secondJSON...)
		record = append(record, '}')

		if _, err := Decode(record); err == nil ||
			!strings.Contains(err.Error(), "duplicate JSON object member") {
			t.Fatalf("Decode(%q) duplicate error = %v", record, err)
		}
	})
}

func eventWithUnknownFields(count int) []byte {
	var builder strings.Builder
	builder.Grow(32 + count*12)
	builder.WriteString(`{"Action":"start"`)
	for i := 0; i < count; i++ {
		builder.WriteString(`,"Future`)
		builder.WriteString(strconv.Itoa(i))
		builder.WriteString(`":0`)
	}
	builder.WriteByte('}')
	return []byte(builder.String())
}
