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
	"strings"
	"testing"
)

func TestStreamArbitraryChunksAndRecovery(t *testing.T) {
	first := `{"Action":"start","Package":"p"}`
	last := `{"Action":"pass","Package":"p","Elapsed":0}`
	oversized := strings.Repeat("x", 100)
	input := first + "\r\n" + "{broken}\n" + oversized + "\n" + last

	var raw bytes.Buffer
	var records []Record
	stream := NewStream(StreamOptions{
		RawWriter:          &raw,
		MaxRecordBytes:     64,
		MaxDiagnosticBytes: 5,
		MaxDiagnostics:     1,
		Handler: func(record Record) {
			records = append(records, record)
			if raw.Len() == 0 {
				t.Fatal("handler ran before raw capture")
			}
		},
	})
	for _, value := range []byte(input) {
		if _, err := stream.Write([]byte{value}); err != nil {
			t.Fatalf("Write() unexpected error: %v", err)
		}
	}
	summary, err := stream.Finish()
	if err != nil {
		t.Fatalf("Finish() unexpected error: %v", err)
	}

	if raw.String() != input {
		t.Fatalf("raw capture differs:\ngot:  %q\nwant: %q", raw.String(), input)
	}
	if len(records) != 4 {
		t.Fatalf("records = %d, want 4", len(records))
	}
	if records[0].Event == nil || records[0].Event.Action() != ActionStart ||
		string(records[0].Raw) != first {
		t.Fatalf("first record = %#v", records[0])
	}
	if got := records[1].Diagnostic; got == nil ||
		got.Kind != DiagnosticMalformed || got.Line != 2 ||
		got.Sequence != 2 || got.Preview != "{brok" || !got.Truncated {
		t.Fatalf("malformed diagnostic = %#v", got)
	}
	if got := records[2].Diagnostic; got == nil ||
		got.Kind != DiagnosticOversized || got.Line != 3 ||
		got.Sequence != 3 || got.Bytes != 100 || got.Preview != "xxxxx" {
		t.Fatalf("oversized diagnostic = %#v", got)
	}
	if records[3].Event == nil || records[3].Event.Action() != ActionPass ||
		records[3].Line != 4 {
		t.Fatalf("trailing record = %#v", records[3])
	}
	if summary.RawBytes != int64(len(input)) || summary.Records != 4 ||
		summary.Events != 2 || summary.DiagnosticCount != 2 ||
		summary.MalformedRecords != 1 || summary.OversizedRecords != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	if len(summary.Diagnostics) != 1 || summary.SuppressedDiagnostics != 1 {
		t.Fatalf("bounded diagnostics = %#v", summary)
	}

	again, err := stream.Finish()
	if err != nil || again.Records != summary.Records {
		t.Fatalf("second Finish() = (%#v, %v)", again, err)
	}
	if _, err := stream.Write([]byte("later")); !errors.Is(err, ErrStreamClosed) {
		t.Fatalf("Write() after Finish error = %v", err)
	}
}

func TestStreamAcceptsMaxRecordWithCRLF(t *testing.T) {
	record := `{"Action":"start","Package":"p"}`
	var records []Record
	stream := NewStream(StreamOptions{
		MaxRecordBytes: len(record),
		Handler: func(record Record) {
			records = append(records, record)
		},
	})
	if _, err := stream.Write([]byte(record + "\r\n")); err != nil {
		t.Fatalf("Write() unexpected error: %v", err)
	}
	summary, err := stream.Finish()
	if err != nil {
		t.Fatalf("Finish() unexpected error: %v", err)
	}
	if summary.Events != 1 || summary.DiagnosticCount != 0 ||
		len(records) != 1 || string(records[0].Raw) != record {
		t.Fatalf("stream result = (%#v, %#v)", summary, records)
	}
}

func TestStreamIgnoresBlankLines(t *testing.T) {
	var records []Record
	summary, err := Read(strings.NewReader(
		"\n\r\n"+`{"Action":"start","Package":"p"}`+"\n",
	), StreamOptions{
		Handler: func(record Record) {
			records = append(records, record)
		},
	})
	if err != nil {
		t.Fatalf("Read() unexpected error: %v", err)
	}
	if summary.BlankRecords != 2 || summary.Events != 1 || len(records) != 1 {
		t.Fatalf("blank-line result = (%#v, %#v)", summary, records)
	}
	if records[0].Sequence != 1 || records[0].Line != 3 {
		t.Fatalf("event position = %#v", records[0])
	}
}

func TestStreamRetainsUnknownActionsAsIntegrityDiagnostics(t *testing.T) {
	lines := []string{
		`{"Action":"attr","Package":"p","Test":"TestOne","Key":"region","Value":"east"}`,
		`{"Action":"retry","Package":"p","Test":"TestOne","Output":"retrying\n"}`,
		`{"Action":"build-cache-hit","ImportPath":"p","Output":"cached\n"}`,
		`{"Action":"notice","Output":"future\n","Future":true}`,
	}
	var records []Record
	summary, err := Read(strings.NewReader(strings.Join(lines, "\n")), StreamOptions{
		MaxDiagnostics: 2,
		Handler: func(record Record) {
			records = append(records, record)
		},
	})
	if err != nil {
		t.Fatalf("Read() unexpected error: %v", err)
	}
	if summary.Records != 4 || summary.Events != 4 ||
		summary.DiagnosticCount != 3 || summary.UnknownRecords != 3 ||
		len(summary.Diagnostics) != 2 || summary.SuppressedDiagnostics != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	if len(records) != 4 || records[0].Event == nil ||
		records[0].Diagnostic != nil ||
		records[0].Event.Action() != ActionAttr {
		t.Fatalf("known record = %#v", records)
	}
	for i, record := range records[1:] {
		if record.Event == nil || record.Diagnostic == nil ||
			record.Diagnostic.Kind != DiagnosticUnknown ||
			string(record.Raw) != lines[i+1] {
			t.Fatalf("unknown record %d = %#v", i+1, record)
		}
	}
}

func TestStreamRejectsUnsafeJSONObjectsWithoutLosingRawEvidence(
	t *testing.T,
) {
	tests := []struct {
		name       string
		record     []byte
		wantDetail string
	}{
		{
			name:       "unknown field capacity",
			record:     eventWithUnknownFields(MaxUnknownFieldsPerEvent + 1),
			wantDetail: "unknown field count exceeds maximum",
		},
		{
			name: "duplicate action",
			record: []byte(
				`{"Action":"start","Action":"pass","Package":"p"}`,
			),
			wantDetail: `duplicate JSON object member "Action"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var raw bytes.Buffer
			var records []Record
			stream := NewStream(StreamOptions{
				RawWriter:          &raw,
				MaxRecordBytes:     len(test.record),
				MaxDiagnosticBytes: 1024,
				Handler: func(record Record) {
					records = append(records, record)
				},
			})
			if _, err := stream.Write(test.record); err != nil {
				t.Fatalf("Write() unexpected error: %v", err)
			}
			summary, err := stream.Finish()
			if err != nil {
				t.Fatalf("Finish() unexpected error: %v", err)
			}
			if !bytes.Equal(raw.Bytes(), test.record) {
				t.Fatalf(
					"raw capture = %q, want %q",
					raw.Bytes(),
					test.record,
				)
			}
			if summary.Records != 1 ||
				summary.Events != 0 ||
				summary.DiagnosticCount != 1 ||
				summary.MalformedRecords != 1 ||
				len(records) != 1 ||
				records[0].Event != nil ||
				records[0].Diagnostic == nil ||
				records[0].Diagnostic.Kind != DiagnosticMalformed ||
				!strings.Contains(
					records[0].Diagnostic.Message,
					test.wantDetail,
				) ||
				!bytes.Equal(records[0].Raw, test.record) {
				t.Fatalf(
					"unsafe-object stream result = (%#v, %#v)",
					summary,
					records,
				)
			}
		})
	}
}

func TestStreamExplicitUnlimitedRecordSize(t *testing.T) {
	output := strings.Repeat("x", DefaultMaxRecordBytes+1)
	record := `{"Action":"output","Package":"p","Output":"` + output + `"}`
	var got []Record
	stream := NewStream(StreamOptions{
		UnlimitedRecordBytes: true,
		Handler: func(record Record) {
			got = append(got, record)
		},
	})
	if _, err := stream.Write([]byte(record)); err != nil {
		t.Fatalf("Write() unexpected error: %v", err)
	}
	summary, err := stream.Finish()
	if err != nil {
		t.Fatalf("Finish() unexpected error: %v", err)
	}
	if summary.Events != 1 || summary.OversizedRecords != 0 ||
		len(got) != 1 || got[0].Event == nil {
		t.Fatalf("unlimited result = (%#v, %d records)", summary, len(got))
	}
}

func TestStreamPropagatesRawWriterFailure(t *testing.T) {
	writer := &shortWriter{limit: 5}
	var records []Record
	stream := NewStream(StreamOptions{
		RawWriter: writer,
		Handler: func(record Record) {
			records = append(records, record)
		},
	})
	input := []byte(`{"Action":"start"}` + "\n")
	n, err := stream.Write(input)
	if n != 5 || !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Write() = (%d, %v), want (5, io.ErrShortWrite)", n, err)
	}
	summary, finishErr := stream.Finish()
	if !errors.Is(finishErr, io.ErrShortWrite) {
		t.Fatalf("Finish() error = %v, want io.ErrShortWrite", finishErr)
	}
	if summary.RawBytes != 5 || writer.String() != string(input[:5]) {
		t.Fatalf("raw result = (%#v, %q)", summary, writer.String())
	}
	if len(records) != 1 || records[0].Diagnostic == nil {
		t.Fatalf("partial trailing record = %#v", records)
	}
}

func TestStreamBoundsDiagnosticMessage(t *testing.T) {
	record := `{"Action":"start","Time":"` + strings.Repeat("not-a-time", 100) + `"}`
	var got Record
	stream := NewStream(StreamOptions{
		MaxRecordBytes:     len(record),
		MaxDiagnosticBytes: 32,
		Handler: func(record Record) {
			got = record
		},
	})
	if _, err := stream.Write([]byte(record)); err != nil {
		t.Fatalf("Write() unexpected error: %v", err)
	}
	if _, err := stream.Finish(); err != nil {
		t.Fatalf("Finish() unexpected error: %v", err)
	}
	if got.Diagnostic == nil {
		t.Fatalf("record = %#v, want diagnostic", got)
	}
	if len(got.Diagnostic.Message) > 32 || len(got.Diagnostic.Preview) > 32 {
		t.Fatalf("unbounded diagnostic = %#v", got.Diagnostic)
	}
}

func TestStreamNonpositiveDiagnosticBoundsUseDefaults(t *testing.T) {
	for _, maxDiagnostics := range []int{-1, 0} {
		t.Run(fmt.Sprintf("diagnostics_%d", maxDiagnostics), func(t *testing.T) {
			input := strings.Repeat("{broken}\n", DefaultMaxDiagnostics+1)
			summary, err := Read(strings.NewReader(input), StreamOptions{
				MaxDiagnosticBytes: -1,
				MaxDiagnostics:     maxDiagnostics,
			})
			if err != nil {
				t.Fatalf("Read() unexpected error: %v", err)
			}
			if summary.DiagnosticCount != DefaultMaxDiagnostics+1 ||
				len(summary.Diagnostics) != DefaultMaxDiagnostics ||
				summary.SuppressedDiagnostics != 1 {
				t.Fatalf("bounded diagnostics = %#v", summary)
			}
			if got := summary.Diagnostics[0].Preview; got != "{broken}" {
				t.Fatalf("diagnostic preview = %q", got)
			}
		})
	}
}

func TestReadRejectsNilReader(t *testing.T) {
	if _, err := Read(nil, StreamOptions{}); err == nil {
		t.Fatal("Read() accepted nil reader")
	}
}

func FuzzStreamChunking(f *testing.F) {
	f.Add(
		[]byte(`{"Action":"start","Package":"p"}`+"\r\n"+
			`{"Action":"pass","Package":"p"}`),
		uint8(3),
	)
	f.Add([]byte("bad\n"+strings.Repeat("x", 40)+"\n"), uint8(1))
	f.Fuzz(func(t *testing.T, input []byte, chunkByte uint8) {
		if len(input) > 1<<20 {
			t.Skip()
		}
		original := append([]byte(nil), input...)
		chunk := int(chunkByte%31) + 1
		var raw bytes.Buffer
		stream := NewStream(StreamOptions{
			RawWriter:      &raw,
			MaxRecordBytes: 128,
		})
		for len(input) > 0 {
			size := chunk
			if size > len(input) {
				size = len(input)
			}
			if _, err := stream.Write(input[:size]); err != nil {
				t.Fatalf("Write() unexpected error: %v", err)
			}
			input = input[size:]
		}
		if _, err := stream.Finish(); err != nil {
			t.Fatalf("Finish() unexpected error: %v", err)
		}
		if !bytes.Equal(raw.Bytes(), original) {
			t.Fatal("raw capture differs from input")
		}
	})
}

type shortWriter struct {
	bytes.Buffer
	limit int
}

func (w *shortWriter) Write(data []byte) (int, error) {
	if len(data) > w.limit {
		data = data[:w.limit]
	}
	return w.Buffer.Write(data)
}
