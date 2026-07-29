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
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"unicode/utf8"
)

const (
	// DefaultMaxRecordBytes is the protocol package default. Applications may
	// impose a different default before constructing a Stream.
	DefaultMaxRecordBytes = 8 << 20
	// DefaultMaxDiagnosticBytes bounds the input preview attached to a
	// malformed or oversized record.
	DefaultMaxDiagnosticBytes = 512
	// DefaultMaxDiagnostics bounds the diagnostic sample retained in a stream
	// summary. Every diagnostic is still delivered to the handler.
	DefaultMaxDiagnostics = 100
)

// ErrStreamClosed is returned by Write after Finish or Close.
var ErrStreamClosed = errors.New("protocol stream is closed")

// DiagnosticKind classifies a framing or protocol-integrity issue.
type DiagnosticKind string

const (
	// DiagnosticMalformed marks a record that is not a valid supported JSON
	// event object.
	DiagnosticMalformed DiagnosticKind = "malformed"
	// DiagnosticOversized marks a record larger than the configured bound.
	DiagnosticOversized DiagnosticKind = "oversized"
	// DiagnosticUnknown marks a valid, retained JSON event whose action is not
	// recognized by the decoded event schema.
	DiagnosticUnknown DiagnosticKind = "unknown"
	// DiagnosticIntegrity marks contradictory lifecycle evidence detected
	// while normalizing otherwise valid protocol events.
	DiagnosticIntegrity DiagnosticKind = "integrity"
)

// Diagnostic describes recoverable input damage, an unknown protocol action,
// or contradictory lifecycle evidence. Preview is bounded by
// StreamOptions.MaxDiagnosticBytes and may contain arbitrary input bytes.
type Diagnostic struct {
	Kind      DiagnosticKind
	Sequence  uint64
	Line      uint64
	Bytes     int64
	Preview   string
	Truncated bool
	Message   string
}

// Record is one framed input record. Event and Diagnostic are mutually
// exclusive except for a valid event with an unknown action, which retains the
// decoded Event while also carrying its unknown-action Diagnostic. Raw
// excludes a trailing LF and its optional preceding CR. For an oversized
// record Raw is the bounded preview rather than the complete line; the
// RawWriter remains the authoritative byte-for-byte evidence. Raw and
// Event.Raw may share owned backing storage and must be treated as immutable.
type Record struct {
	Sequence   uint64
	Line       uint64
	Bytes      int64
	Raw        []byte
	Event      *Event
	Diagnostic *Diagnostic
}

// Handler receives records synchronously and in input order.
type Handler func(Record)

// StreamOptions configures raw capture, framing, and diagnostic retention.
// Nonpositive bounds select their conservative package defaults unless
// UnlimitedRecordBytes is true for MaxRecordBytes.
type StreamOptions struct {
	RawWriter            io.Writer
	Handler              Handler
	MaxRecordBytes       int
	UnlimitedRecordBytes bool
	MaxDiagnosticBytes   int
	MaxDiagnostics       int
}

// StreamSummary reports framing results. Diagnostics contains a bounded sample
// while DiagnosticCount includes every malformed, oversized, and unknown
// record.
type StreamSummary struct {
	RawBytes              int64
	BlankRecords          uint64
	Records               uint64
	Events                uint64
	DiagnosticCount       uint64
	MalformedRecords      uint64
	OversizedRecords      uint64
	UnknownRecords        uint64
	Diagnostics           []Diagnostic
	SuppressedDiagnostics uint64
}

// Stream implements io.WriteCloser for direct use as exec.Cmd.Stdout. Each
// Write persists bytes to RawWriter before those same bytes are interpreted.
// Close does not close RawWriter because the caller owns that resource.
type Stream struct {
	mu sync.Mutex

	rawWriter          io.Writer
	handler            Handler
	maxRecordBytes     int
	maxDiagnosticBytes int
	maxDiagnostics     int

	line      uint64
	sequence  uint64
	current   partialRecord
	summary   StreamSummary
	finished  bool
	finishErr error
}

type partialRecord struct {
	data      []byte
	preview   []byte
	bytes     int64
	lastByte  byte
	hasByte   bool
	oversized bool
}

// NewStream creates a raw-first, chunk-oriented event stream.
func NewStream(options StreamOptions) *Stream {
	maxRecordBytes := options.MaxRecordBytes
	if options.UnlimitedRecordBytes {
		maxRecordBytes = 0
	} else if maxRecordBytes <= 0 {
		maxRecordBytes = DefaultMaxRecordBytes
	}
	maxDiagnosticBytes := options.MaxDiagnosticBytes
	if maxDiagnosticBytes <= 0 {
		maxDiagnosticBytes = DefaultMaxDiagnosticBytes
	}
	maxDiagnostics := options.MaxDiagnostics
	if maxDiagnostics <= 0 {
		maxDiagnostics = DefaultMaxDiagnostics
	}
	rawWriter := options.RawWriter
	if rawWriter == nil {
		rawWriter = io.Discard
	}
	return &Stream{
		rawWriter:          rawWriter,
		handler:            options.Handler,
		maxRecordBytes:     maxRecordBytes,
		maxDiagnosticBytes: maxDiagnosticBytes,
		maxDiagnostics:     maxDiagnostics,
		line:               1,
	}
}

// Write records and then frames p. Calls are serialized so the raw capture,
// sequence numbers, and handler observations share one order.
func (s *Stream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.finished {
		return 0, ErrStreamClosed
	}
	if len(p) == 0 {
		return 0, nil
	}

	n, err := s.rawWriter.Write(p)
	if n < 0 {
		n = 0
	}
	if n > len(p) {
		n = len(p)
	}
	if n > 0 {
		s.summary.RawBytes += int64(n)
		s.consume(p[:n])
	}
	if err != nil {
		s.finishErr = err
		return n, err
	}
	if n != len(p) {
		s.finishErr = io.ErrShortWrite
		return n, io.ErrShortWrite
	}
	return n, nil
}

// Finish emits a final record without a newline and returns an idempotent
// summary. Malformed input is represented by diagnostics, not as a Finish
// error. The returned error is reserved for raw-capture failures.
func (s *Stream) Finish() (StreamSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.finished {
		if s.current.hasByte || s.current.oversized {
			s.emitCurrent()
		}
		s.finished = true
		s.summary.SuppressedDiagnostics =
			s.summary.DiagnosticCount - uint64(len(s.summary.Diagnostics))
	}
	return cloneSummary(s.summary), s.finishErr
}

// Close finishes the stream. Use Finish when the framing summary is needed.
func (s *Stream) Close() error {
	_, err := s.Finish()
	return err
}

// Read copies a complete source through a Stream and returns its framing
// summary. It is suitable for offline report generation and deliberately does
// not use bufio.Scanner.
func Read(reader io.Reader, options StreamOptions) (StreamSummary, error) {
	if reader == nil {
		return StreamSummary{}, fmt.Errorf("read Go event stream: reader must not be nil")
	}
	stream := NewStream(options)
	_, copyErr := io.Copy(stream, reader)
	summary, finishErr := stream.Finish()
	return summary, errors.Join(copyErr, finishErr)
}

func (s *Stream) consume(data []byte) {
	for len(data) > 0 {
		offset := bytes.IndexByte(data, '\n')
		if offset < 0 {
			s.appendCurrent(data)
			return
		}
		s.appendCurrent(data[:offset])
		s.emitCurrent()
		s.line++
		data = data[offset+1:]
	}
}

func (s *Stream) appendCurrent(data []byte) {
	if len(data) == 0 {
		return
	}
	s.current.hasByte = true
	s.current.lastByte = data[len(data)-1]
	s.current.bytes += int64(len(data))

	if len(s.current.preview) < s.maxDiagnosticBytes {
		remaining := s.maxDiagnosticBytes - len(s.current.preview)
		if remaining > len(data) {
			remaining = len(data)
		}
		s.current.preview = append(s.current.preview, data[:remaining]...)
	}
	if s.current.oversized {
		return
	}
	if s.maxRecordBytes == 0 {
		s.current.data = append(s.current.data, data...)
		return
	}

	// Retain one extra byte until the delimiter arrives so a record containing
	// exactly MaxRecordBytes plus the CR from CRLF is accepted.
	limit := s.maxRecordBytes
	if limit < math.MaxInt {
		limit++
	}
	remaining := limit - len(s.current.data)
	if remaining > len(data) {
		remaining = len(data)
	}
	if remaining > 0 {
		s.current.data = append(s.current.data, data[:remaining]...)
	}
	if remaining != len(data) {
		s.current.oversized = true
		s.current.data = nil
	}
}

func (s *Stream) emitCurrent() {
	recordBytes := s.current.bytes
	hasTrailingCR := s.current.hasByte && s.current.lastByte == '\r'
	if hasTrailingCR {
		recordBytes--
	}
	if recordBytes == 0 && !s.current.oversized {
		s.summary.BlankRecords++
		s.current = partialRecord{}
		return
	}

	s.sequence++
	s.summary.Records++
	oversized := s.current.oversized ||
		(s.maxRecordBytes > 0 && recordBytes > int64(s.maxRecordBytes))
	if oversized {
		preview := s.current.preview
		if hasTrailingCR && int64(len(preview)) == s.current.bytes {
			preview = preview[:len(preview)-1]
		}
		diagnostic := Diagnostic{
			Kind:      DiagnosticOversized,
			Sequence:  s.sequence,
			Line:      s.line,
			Bytes:     recordBytes,
			Preview:   string(preview),
			Truncated: true,
			Message:   fmt.Sprintf("record exceeds maximum of %d bytes", s.maxRecordBytes),
		}
		s.emitDiagnostic(diagnostic, preview)
		s.current = partialRecord{}
		return
	}

	raw := s.current.data
	if hasTrailingCR {
		raw = raw[:len(raw)-1]
	}
	event, err := decodeOwned(raw)
	if err != nil {
		preview := raw
		if len(preview) > s.maxDiagnosticBytes {
			preview = preview[:s.maxDiagnosticBytes]
		}
		diagnostic := Diagnostic{
			Kind:      DiagnosticMalformed,
			Sequence:  s.sequence,
			Line:      s.line,
			Bytes:     recordBytes,
			Preview:   string(preview),
			Truncated: len(preview) < len(raw),
			Message:   clipUTF8(err.Error(), s.maxDiagnosticBytes),
		}
		s.emitDiagnostic(diagnostic, raw)
		s.current = partialRecord{}
		return
	}

	event.Sequence = s.sequence
	event.Line = s.line
	s.summary.Events++
	record := Record{
		Sequence: s.sequence,
		Line:     s.line,
		Bytes:    recordBytes,
		Raw:      event.Raw,
		Event:    &event,
	}
	if !event.IsKnownAction() {
		preview := raw
		if len(preview) > s.maxDiagnosticBytes {
			preview = preview[:s.maxDiagnosticBytes]
		}
		diagnostic := Diagnostic{
			Kind:      DiagnosticUnknown,
			Sequence:  s.sequence,
			Line:      s.line,
			Bytes:     recordBytes,
			Preview:   string(preview),
			Truncated: len(preview) < len(raw),
			Message: clipUTF8(
				fmt.Sprintf(
					"unknown action %q for %s event",
					clipUTF8(event.Action(), 256),
					event.Kind,
				),
				s.maxDiagnosticBytes,
			),
		}
		s.observeDiagnostic(diagnostic)
		record.Diagnostic = &diagnostic
	}
	s.emit(record)
	s.current = partialRecord{}
}

func (s *Stream) emitDiagnostic(diagnostic Diagnostic, raw []byte) {
	s.observeDiagnostic(diagnostic)
	s.emit(Record{
		Sequence:   diagnostic.Sequence,
		Line:       diagnostic.Line,
		Bytes:      diagnostic.Bytes,
		Raw:        raw,
		Diagnostic: &diagnostic,
	})
}

func (s *Stream) observeDiagnostic(diagnostic Diagnostic) {
	s.summary.DiagnosticCount++
	switch diagnostic.Kind {
	case DiagnosticMalformed:
		s.summary.MalformedRecords++
	case DiagnosticOversized:
		s.summary.OversizedRecords++
	case DiagnosticUnknown:
		s.summary.UnknownRecords++
	}
	if len(s.summary.Diagnostics) < s.maxDiagnostics {
		s.summary.Diagnostics = append(s.summary.Diagnostics, diagnostic)
	}
}

func (s *Stream) emit(record Record) {
	if s.handler != nil {
		s.handler(record)
	}
}

func cloneSummary(summary StreamSummary) StreamSummary {
	cloned := summary
	cloned.Diagnostics = append([]Diagnostic(nil), summary.Diagnostics...)
	return cloned
}

func clipUTF8(value string, maxBytes int) string {
	if maxBytes < 0 || len(value) <= maxBytes {
		return value
	}
	end := maxBytes
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end]
}
