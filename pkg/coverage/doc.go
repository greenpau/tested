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

// Package coverage parses Go coverage profiles with bounded records, aggregate
// bytes, unique files, and normalized blocks; performs cardinality-bounded
// deterministic merges; evaluates exact weighted minimums; generates the
// source-annotated HTML report produced by "go tool cover"; constructs an
// explicitly requested, bounded source comparison against one immutable local
// Git commit; and stages an optional presentation decorator securely before
// atomic publication.
package coverage
