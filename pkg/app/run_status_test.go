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
	"bytes"
	"strings"
	"testing"

	"github.com/greenpau/tested/pkg/artifact"
	"github.com/greenpau/tested/pkg/coverage"
	"github.com/greenpau/tested/pkg/runstatus"
)

func TestVerifyManifestRunBindings(t *testing.T) {
	const (
		eventDigest  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		statusDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	evidence := runstatus.Evidence{
		Schema:          runstatus.Schema,
		CaptureComplete: true,
		Files: []runstatus.File{{
			Name:   artifact.TestOutputJSONLName,
			Size:   17,
			SHA256: eventDigest,
		}},
	}
	entries := []artifact.ManifestEntry{{
		Name:   artifact.RunJSONName,
		Size:   50,
		SHA256: statusDigest,
	}, {
		Name:   artifact.TestOutputJSONLName,
		Size:   17,
		SHA256: eventDigest,
	}}
	if err := verifyManifestRunBindings(evidence, entries); err != nil {
		t.Fatalf("verifyManifestRunBindings() error = %v", err)
	}

	tests := []struct {
		name     string
		evidence runstatus.Evidence
		entries  []artifact.ManifestEntry
		want     string
	}{
		{
			name:     "missing run status inventory",
			evidence: evidence,
			entries:  entries[1:],
			want:     "authoritative run.json",
		},
		{
			name:     "unbound inventory evidence",
			evidence: runstatus.Evidence{},
			entries:  entries,
			want:     "is not bound",
		},
		{
			name:     "bound evidence missing from inventory",
			evidence: evidence,
			entries:  entries[:1],
			want:     "binds missing",
		},
		{
			name:     "binding digest mismatch",
			evidence: evidence,
			entries: []artifact.ManifestEntry{
				entries[0],
				{
					Name:   artifact.TestOutputJSONLName,
					Size:   17,
					SHA256: strings.Repeat("c", 64),
				},
			},
			want: "but run.json binds",
		},
		{
			name:     "duplicate inventory entry",
			evidence: evidence,
			entries:  append(append([]artifact.ManifestEntry(nil), entries...), entries[1]),
			want:     "duplicate artifact",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := verifyManifestRunBindings(test.evidence, test.entries)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf(
					"verifyManifestRunBindings() error = %v, want containing %q",
					err,
					test.want,
				)
			}
		})
	}
}

func TestValidateRunStatusCoveragePolicyExactUint64RoundTrip(t *testing.T) {
	const maxUint64 = ^uint64(0)
	totals := coverage.Totals{
		Covered:    maxUint64 - 1,
		Statements: maxUint64,
	}
	if got, want := formatPolicyPercentage(totals, "100"),
		"99.999999999999999994578989"; got != want {
		t.Fatalf("formatPolicyPercentage() = %q, want %q", got, want)
	}

	tests := []struct {
		name      string
		minimum   string
		satisfied bool
	}{
		{
			name:      "ratio is above nearby finite decimal",
			minimum:   "99.99999999999999999",
			satisfied: true,
		},
		{
			name:      "ratio is below nearby finite decimal",
			minimum:   "99.999999999999999999",
			satisfied: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := runstatus.Evidence{
				Schema:          runstatus.Schema,
				CaptureComplete: true,
				Files: []runstatus.File{{
					Name:   "test_output.jsonl",
					SHA256: strings.Repeat("0", 64),
				}, {
					Name:   "coverage.out",
					SHA256: strings.Repeat("1", 64),
				}},
				CoveragePolicy: &runstatus.CoveragePolicy{
					Minimum:    test.minimum,
					Actual:     formatPolicyPercentage(totals, test.minimum),
					Covered:    totals.Covered,
					Statements: totals.Statements,
					Available:  true,
					Satisfied:  test.satisfied,
				},
			}
			var encoded bytes.Buffer
			if err := runstatus.Encode(&encoded, evidence); err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			decoded, err := runstatus.Decode(&encoded)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}

			profile := &coverage.Profile{Total: totals}
			withProfile, err := validateRunStatusCoveragePolicy(
				decoded,
				profile,
				true,
			)
			if err != nil {
				t.Fatalf("validate with profile: %v", err)
			}
			withoutProfile, err := validateRunStatusCoveragePolicy(
				decoded,
				nil,
				false,
			)
			if err != nil {
				t.Fatalf("validate recorded counts: %v", err)
			}
			if withProfile.Satisfied != test.satisfied ||
				withoutProfile.Satisfied != test.satisfied ||
				withProfile.Actual != withoutProfile.Actual {
				t.Fatalf(
					"round-trip policies = %#v and %#v, want satisfied %t",
					withProfile,
					withoutProfile,
					test.satisfied,
				)
			}
		})
	}
}

func TestWithoutCoverageBindingPreservesAuthoritativeStatus(t *testing.T) {
	exitCode := 1
	policy := &runstatus.CoveragePolicy{
		Minimum:    "80",
		Actual:     "50.0",
		Covered:    1,
		Statements: 2,
		Available:  true,
	}
	evidence := runstatus.Evidence{
		Schema:          runstatus.Schema,
		Command:         []string{"go", "test", "./..."},
		ChildStarted:    true,
		ExitCode:        &exitCode,
		CaptureComplete: true,
		Files: []runstatus.File{
			{Name: artifact.TestOutputJSONLName},
			{Name: artifact.CoverageProfileName},
			{Name: artifact.StderrLogName},
		},
		Issues: []runstatus.Issue{{
			Kind:    "run",
			Message: "authoritative child failure",
			Fatal:   true,
		}},
		CoveragePolicy: policy,
	}

	projected, changed := withoutCoverageBinding(evidence)
	if !changed {
		t.Fatal("withoutCoverageBinding() changed = false, want true")
	}
	if len(projected.Files) != 2 ||
		projected.Files[0].Name != artifact.TestOutputJSONLName ||
		projected.Files[1].Name != artifact.StderrLogName {
		t.Fatalf("projected bindings = %#v", projected.Files)
	}
	if projected.ExitCode != evidence.ExitCode ||
		projected.CoveragePolicy != policy ||
		len(projected.Issues) != 1 ||
		projected.Issues[0] != evidence.Issues[0] {
		t.Fatalf("projected authoritative status changed: %#v", projected)
	}
	if len(evidence.Files) != 3 ||
		evidence.Files[1].Name != artifact.CoverageProfileName {
		t.Fatalf("source status was mutated: %#v", evidence.Files)
	}

	again, changed := withoutCoverageBinding(projected)
	if changed {
		t.Fatal("withoutCoverageBinding(projected) changed = true, want false")
	}
	if len(again.Files) != len(projected.Files) {
		t.Fatalf("idempotent projection files = %#v", again.Files)
	}
}

func TestValidateRunStatusCoveragePolicyRejectsUnavailableProfileConflict(
	t *testing.T,
) {
	evidence := runstatus.Evidence{
		CoveragePolicy: &runstatus.CoveragePolicy{
			Minimum: "80",
		},
	}
	if _, err := validateRunStatusCoveragePolicy(
		evidence,
		&coverage.Profile{
			Total: coverage.Totals{Covered: 1, Statements: 1},
		},
		true,
	); err == nil {
		t.Fatal("unavailable policy with populated profile error = nil")
	}
	if _, err := validateRunStatusCoveragePolicy(
		evidence,
		nil,
		false,
	); err != nil {
		t.Fatalf("unavailable policy without required profile: %v", err)
	}
}
