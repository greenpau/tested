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

package app

import (
	"fmt"
	"io"
)

type executionOutcome struct {
	state    ExitState
	errors   errorSet
	warnings errorSet
}

type errorSet struct {
	values []error
}

func (s *errorSet) add(err error) {
	if err == nil {
		return
	}
	s.values = append(s.values, err)
}

func (s *errorSet) count() int {
	return len(s.values)
}

func (s errorSet) clone() errorSet {
	return errorSet{
		values: append([]error(nil), s.values...),
	}
}

func (s *errorSet) write(writer io.Writer, sanitize func(string) string) {
	values := append([]error(nil), s.values...)
	for _, err := range values {
		message := err.Error()
		if sanitize != nil {
			message = sanitize(message)
		}
		writeDiagnostic(writer, message)
	}
}

func (s *errorSet) writeWarnings(
	writer io.Writer,
	sanitize func(string) string,
) {
	values := append([]error(nil), s.values...)
	for _, err := range values {
		message := err.Error()
		if sanitize != nil {
			message = sanitize(message)
		}
		_, _ = fmt.Fprintf(writer, "tested: warning: %s\n", message)
	}
}
