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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"
)

// EventKind identifies which Go event schema a record uses.
type EventKind string

const (
	// EventKindUnknown is a valid JSON object whose action cannot be
	// unambiguously assigned to the test or build schema.
	EventKindUnknown EventKind = "unknown"
	// EventKindTest identifies a TestEvent record.
	EventKindTest EventKind = "test"
	// EventKindBuild identifies a Go 1.26 BuildEvent record.
	EventKindBuild EventKind = "build"
)

const (
	// MaxUnknownFieldsPerEvent bounds fields unknown to the selected event
	// schema. Known test/build fields do not consume this allowance.
	MaxUnknownFieldsPerEvent = 64
	// MaxUnknownFieldBytesPerEvent bounds the decoded field-name bytes plus
	// exact raw JSON value bytes retained for unknown fields.
	MaxUnknownFieldBytesPerEvent = 64 << 10
)

const (
	// Test event actions documented by cmd/test2json.
	ActionStart     = "start"
	ActionRun       = "run"
	ActionPause     = "pause"
	ActionCont      = "cont"
	ActionPass      = "pass"
	ActionBench     = "bench"
	ActionFail      = "fail"
	ActionOutput    = "output"
	ActionSkip      = "skip"
	ActionAttr      = "attr"
	ActionArtifacts = "artifacts"

	// Build event actions documented by go help buildjson.
	ActionBuildOutput = "build-output"
	ActionBuildFail   = "build-fail"
)

var (
	testFieldNames = map[string]struct{}{
		"Time":        {},
		"Action":      {},
		"Package":     {},
		"Test":        {},
		"Elapsed":     {},
		"Output":      {},
		"FailedBuild": {},
		"Key":         {},
		"Value":       {},
		"Path":        {},
	}
	buildFieldNames = map[string]struct{}{
		"ImportPath": {},
		"Action":     {},
		"Output":     {},
	}
	unknownEventFieldNames = map[string]struct{}{
		"Action": {},
	}
)

// TestEvent is the lossless typed projection of a Go TestEvent. Presence
// flags distinguish omitted fields from explicit zero values.
type TestEvent struct {
	Time           time.Time
	TimePresent    bool
	Action         string
	Package        string
	Test           string
	Elapsed        float64
	ElapsedPresent bool
	Output         string
	FailedBuild    string
	Key            string
	Value          string
	Path           string
	UnknownFields  map[string]json.RawMessage
}

// BuildEvent is the typed projection of the BuildEvent introduced for
// go test -json in Go 1.26. ImportPath is a package build ID and must not be
// treated as equivalent to TestEvent.Package.
type BuildEvent struct {
	ImportPath    string
	Action        string
	Output        string
	UnknownFields map[string]json.RawMessage
}

// UnknownEvent preserves a forward-compatible JSON object that cannot yet be
// assigned to a known event schema.
type UnknownEvent struct {
	Action string
	Fields map[string]json.RawMessage
}

// Event is the discriminated union of TestEvent and BuildEvent. Raw contains
// the exact JSON record without its line ending. Sequence and Line are filled
// by Stream and remain zero when Decode is called directly.
type Event struct {
	Sequence uint64
	Line     uint64
	Kind     EventKind
	Raw      json.RawMessage
	Test     *TestEvent
	Build    *BuildEvent
	Unknown  *UnknownEvent
}

// Decode parses one JSON record from the Go event stream. It rejects duplicate
// object members and records whose schema extensions exceed
// MaxUnknownFieldsPerEvent or MaxUnknownFieldBytesPerEvent. Bounded unknown
// fields and unknown action values are retained exactly; Stream additionally
// marks unknown actions with integrity diagnostics and turns decode errors into
// malformed-record diagnostics.
func Decode(data []byte) (Event, error) {
	return decode(data, true)
}

func decodeOwned(data []byte) (Event, error) {
	return decode(data, false)
}

func decode(data []byte, cloneRaw bool) (Event, error) {
	raw := bytes.TrimSpace(data)
	if len(raw) == 0 {
		return Event{}, fmt.Errorf("decode Go event: empty record")
	}

	fields, err := decodeObjectFields(raw)
	if err != nil {
		return Event{}, fmt.Errorf("decode Go event: %w", err)
	}

	action, err := requiredString(fields, "Action")
	if err != nil {
		return Event{}, fmt.Errorf("decode Go event: %w", err)
	}

	event := Event{Kind: EventKindUnknown}
	switch classify(fields, action) {
	case EventKindTest:
		testEvent, err := decodeTestEvent(fields)
		if err != nil {
			return Event{}, fmt.Errorf("decode Go test event: %w", err)
		}
		event.Kind = EventKindTest
		event.Test = testEvent
	case EventKindBuild:
		buildEvent, err := decodeBuildEvent(fields)
		if err != nil {
			return Event{}, fmt.Errorf("decode Go build event: %w", err)
		}
		event.Kind = EventKindBuild
		event.Build = buildEvent
	default:
		retained, err := retainBoundedFields(
			fields,
			unknownEventFieldNames,
			true,
		)
		if err != nil {
			return Event{}, fmt.Errorf("decode unknown Go event: %w", err)
		}
		event.Unknown = &UnknownEvent{
			Action: action,
			Fields: retained,
		}
	}
	event.Raw = data
	if cloneRaw {
		event.Raw = cloneBytes(data)
	}
	return event, nil
}

// Action returns the action discriminator regardless of the event schema.
func (e Event) Action() string {
	switch e.Kind {
	case EventKindTest:
		if e.Test != nil {
			return e.Test.Action
		}
	case EventKindBuild:
		if e.Build != nil {
			return e.Build.Action
		}
	default:
		if e.Unknown != nil {
			return e.Unknown.Action
		}
	}
	return ""
}

// IsKnownAction reports whether the action is documented for the event's
// schema. A false result does not make the event malformed.
func (e Event) IsKnownAction() bool {
	switch e.Kind {
	case EventKindTest:
		return IsTestAction(e.Action())
	case EventKindBuild:
		return IsBuildAction(e.Action())
	default:
		return false
	}
}

// IsTestAction reports whether action is a documented TestEvent action.
func IsTestAction(action string) bool {
	switch action {
	case ActionStart, ActionRun, ActionPause, ActionCont, ActionPass,
		ActionBench, ActionFail, ActionOutput, ActionSkip, ActionAttr,
		ActionArtifacts:
		return true
	default:
		return false
	}
}

// IsBuildAction reports whether action is a documented BuildEvent action.
func IsBuildAction(action string) bool {
	switch action {
	case ActionBuildOutput, ActionBuildFail:
		return true
	default:
		return false
	}
}

func classify(fields map[string]json.RawMessage, action string) EventKind {
	if IsBuildAction(action) {
		return EventKindBuild
	}
	if IsTestAction(action) {
		return EventKindTest
	}

	_, hasImportPath := fields["ImportPath"]
	_, hasPackage := fields["Package"]
	if hasImportPath && !hasPackage {
		return EventKindBuild
	}
	if hasPackage && !hasImportPath {
		return EventKindTest
	}
	for _, name := range []string{"Time", "Test", "Elapsed", "FailedBuild"} {
		if _, exists := fields[name]; exists && !hasImportPath {
			return EventKindTest
		}
	}
	return EventKindUnknown
}

func decodeTestEvent(fields map[string]json.RawMessage) (*TestEvent, error) {
	unknownFields, err := retainBoundedFields(
		fields,
		testFieldNames,
		false,
	)
	if err != nil {
		return nil, err
	}
	event := &TestEvent{
		UnknownFields: unknownFields,
	}
	if event.Action, err = requiredString(fields, "Action"); err != nil {
		return nil, err
	}
	if event.Package, err = optionalString(fields, "Package"); err != nil {
		return nil, err
	}
	if event.Test, err = optionalString(fields, "Test"); err != nil {
		return nil, err
	}
	if event.Output, err = optionalString(fields, "Output"); err != nil {
		return nil, err
	}
	if event.FailedBuild, err = optionalString(fields, "FailedBuild"); err != nil {
		return nil, err
	}
	if event.Key, err = optionalString(fields, "Key"); err != nil {
		return nil, err
	}
	if event.Value, err = optionalString(fields, "Value"); err != nil {
		return nil, err
	}
	if event.Path, err = optionalString(fields, "Path"); err != nil {
		return nil, err
	}
	if value, exists := fields["Elapsed"]; exists {
		if isJSONNull(value) {
			return nil, fmt.Errorf("field %q must not be null", "Elapsed")
		}
		if err := json.Unmarshal(value, &event.Elapsed); err != nil {
			return nil, fmt.Errorf("field %q: %w", "Elapsed", err)
		}
		event.ElapsedPresent = true
	}
	if value, exists := fields["Time"]; exists {
		if isJSONNull(value) {
			return nil, fmt.Errorf("field %q must not be null", "Time")
		}
		if err := json.Unmarshal(value, &event.Time); err != nil {
			return nil, fmt.Errorf("field %q: %w", "Time", err)
		}
		event.TimePresent = true
	}
	return event, nil
}

func decodeBuildEvent(fields map[string]json.RawMessage) (*BuildEvent, error) {
	unknownFields, err := retainBoundedFields(
		fields,
		buildFieldNames,
		false,
	)
	if err != nil {
		return nil, err
	}
	event := &BuildEvent{
		UnknownFields: unknownFields,
	}
	if event.Action, err = requiredString(fields, "Action"); err != nil {
		return nil, err
	}
	if event.ImportPath, err = optionalString(fields, "ImportPath"); err != nil {
		return nil, err
	}
	if event.Output, err = optionalString(fields, "Output"); err != nil {
		return nil, err
	}
	return event, nil
}

func requiredString(fields map[string]json.RawMessage, name string) (string, error) {
	value, exists := fields[name]
	if !exists {
		return "", fmt.Errorf("missing required field %q", name)
	}
	if isJSONNull(value) {
		return "", fmt.Errorf("field %q must not be null", name)
	}
	var result string
	if err := json.Unmarshal(value, &result); err != nil {
		return "", fmt.Errorf("field %q: %w", name, err)
	}
	if result == "" {
		return "", fmt.Errorf("field %q must not be empty", name)
	}
	return result, nil
}

func optionalString(fields map[string]json.RawMessage, name string) (string, error) {
	value, exists := fields[name]
	if !exists {
		return "", nil
	}
	if isJSONNull(value) {
		return "", fmt.Errorf("field %q must not be null", name)
	}
	var result string
	if err := json.Unmarshal(value, &result); err != nil {
		return "", fmt.Errorf("field %q: %w", name, err)
	}
	return result, nil
}

func isJSONNull(value json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(value), []byte("null"))
}

func decodeObjectFields(
	raw []byte,
) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return nil, normalizeJSONDecodeError(err)
	}
	opening, ok := token.(json.Delim)
	if !ok || opening != '{' {
		return nil, fmt.Errorf(
			"cannot unmarshal JSON value: expected JSON object",
		)
	}

	fields := make(map[string]json.RawMessage)
	unknownFields := 0
	unknownBytes := 0
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, normalizeJSONDecodeError(err)
		}
		name, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("expected JSON object member name")
		}
		if _, exists := fields[name]; exists {
			return nil, fmt.Errorf(
				"duplicate JSON object member %q",
				name,
			)
		}

		potentialUnknown := !isPotentialKnownField(name)
		if potentialUnknown {
			unknownFields++
			if unknownFields > MaxUnknownFieldsPerEvent {
				return nil, fmt.Errorf(
					"unknown field count exceeds maximum of %d",
					MaxUnknownFieldsPerEvent,
				)
			}
			if !retainUnknownBytes(&unknownBytes, len(name)) {
				return nil, fmt.Errorf(
					"unknown field retention exceeds maximum of %d bytes",
					MaxUnknownFieldBytesPerEvent,
				)
			}
		}

		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, normalizeJSONDecodeError(err)
		}
		if potentialUnknown {
			if !retainUnknownBytes(&unknownBytes, len(value)) {
				return nil, fmt.Errorf(
					"unknown field retention exceeds maximum of %d bytes",
					MaxUnknownFieldBytesPerEvent,
				)
			}
		}
		fields[name] = value
	}

	token, err = decoder.Token()
	if err != nil {
		return nil, normalizeJSONDecodeError(err)
	}
	closing, ok := token.(json.Delim)
	if !ok || closing != '}' {
		return nil, fmt.Errorf("expected end of JSON object")
	}
	if token, err = decoder.Token(); err == nil {
		return nil, fmt.Errorf("unexpected trailing JSON value %v", token)
	} else if !errorsIsEOF(err) {
		return nil, normalizeJSONDecodeError(err)
	}
	return fields, nil
}

func normalizeJSONDecodeError(err error) error {
	if errorsIsEOF(err) {
		return fmt.Errorf("unexpected end of JSON input")
	}
	return err
}

func errorsIsEOF(err error) bool {
	return err == io.EOF || err == io.ErrUnexpectedEOF
}

func isPotentialKnownField(name string) bool {
	switch name {
	case "Time", "Action", "Package", "Test", "Elapsed", "Output",
		"FailedBuild", "Key", "Value", "Path", "ImportPath":
		return true
	default:
		return false
	}
}

func retainBoundedFields(
	fields map[string]json.RawMessage,
	known map[string]struct{},
	includeKnown bool,
) (map[string]json.RawMessage, error) {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)

	unknownFields := 0
	unknownBytes := 0
	for _, name := range names {
		if _, exists := known[name]; exists {
			continue
		}
		value := fields[name]
		unknownFields++
		if unknownFields > MaxUnknownFieldsPerEvent {
			return nil, fmt.Errorf(
				"unknown field count exceeds maximum of %d",
				MaxUnknownFieldsPerEvent,
			)
		}
		if !retainUnknownBytes(&unknownBytes, len(name)) ||
			!retainUnknownBytes(&unknownBytes, len(value)) {
			return nil, fmt.Errorf(
				"unknown field retention exceeds maximum of %d bytes",
				MaxUnknownFieldBytesPerEvent,
			)
		}
	}

	retainedCount := unknownFields
	if includeKnown {
		retainedCount = len(fields)
	}
	if retainedCount == 0 {
		return nil, nil
	}
	retained := make(map[string]json.RawMessage, retainedCount)
	for _, name := range names {
		if _, exists := known[name]; exists && !includeKnown {
			continue
		}
		value := fields[name]
		if _, exists := known[name]; !exists {
			if err := rejectDuplicateNestedMembers(value); err != nil {
				return nil, fmt.Errorf("field %q: %w", name, err)
			}
		}
		retained[name] = value
	}
	return retained, nil
}

func retainUnknownBytes(total *int, value int) bool {
	if value < 0 ||
		*total > MaxUnknownFieldBytesPerEvent ||
		value > MaxUnknownFieldBytesPerEvent-*total {
		return false
	}
	*total += value
	return true
}

type jsonContainer struct {
	kind      json.Delim
	expectKey bool
	members   map[string]struct{}
}

func rejectDuplicateNestedMembers(raw json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var stack []jsonContainer
	rootValues := 0
	for {
		token, err := decoder.Token()
		if errorsIsEOF(err) {
			break
		}
		if err != nil {
			return normalizeJSONDecodeError(err)
		}

		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{', '[':
				if len(stack) == 0 {
					rootValues++
				} else if stack[len(stack)-1].kind == '{' {
					parent := &stack[len(stack)-1]
					if parent.expectKey {
						return fmt.Errorf(
							"expected JSON object member name",
						)
					}
					parent.expectKey = true
				}
				container := jsonContainer{kind: delimiter}
				if delimiter == '{' {
					container.expectKey = true
					container.members = make(map[string]struct{})
				}
				stack = append(stack, container)
			case '}', ']':
				if len(stack) == 0 {
					return fmt.Errorf("unexpected JSON closing delimiter")
				}
				parent := stack[len(stack)-1]
				if delimiter == '}' && parent.kind != '{' ||
					delimiter == ']' && parent.kind != '[' {
					return fmt.Errorf("mismatched JSON closing delimiter")
				}
				if parent.kind == '{' && !parent.expectKey {
					return fmt.Errorf("missing JSON object member value")
				}
				stack = stack[:len(stack)-1]
			}
			continue
		}

		if len(stack) == 0 {
			rootValues++
			continue
		}
		parent := &stack[len(stack)-1]
		if parent.kind != '{' {
			continue
		}
		if !parent.expectKey {
			parent.expectKey = true
			continue
		}
		name, ok := token.(string)
		if !ok {
			return fmt.Errorf("expected JSON object member name")
		}
		if _, exists := parent.members[name]; exists {
			return fmt.Errorf("duplicate JSON object member %q", name)
		}
		parent.members[name] = struct{}{}
		parent.expectKey = false
	}
	if len(stack) != 0 {
		return fmt.Errorf("unexpected end of JSON input")
	}
	if rootValues != 1 {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}

func cloneBytes(data []byte) []byte {
	if data == nil {
		return nil
	}
	return append([]byte(nil), data...)
}
