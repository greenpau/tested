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

// Package protocol frames and decodes the newline-delimited JSON event stream
// emitted by the Go test and build commands.
//
// Decode rejects duplicate event-object members, including names that become
// equal after JSON escape decoding. Forward-compatible fields are retained as
// exact, independently owned JSON values up to MaxUnknownFieldsPerEvent and
// MaxUnknownFieldBytesPerEvent. Duplicate members in retained extension values
// and extension-capacity exhaustion are malformed protocol evidence. Stream
// reports those failures as diagnostics while its RawWriter remains the
// authoritative byte-for-byte capture.
package protocol
