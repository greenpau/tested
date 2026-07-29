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
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/greenpau/tested/pkg/artifact"
	"github.com/greenpau/tested/pkg/coverage"
	"github.com/greenpau/tested/pkg/report"
	"github.com/greenpau/tested/pkg/result"
	"github.com/greenpau/tested/pkg/runner"
	"github.com/greenpau/tested/pkg/runstatus"
)

func persistRunStatus(
	layout *artifact.Layout,
	runResult runner.Result,
	captureComplete bool,
	runErrors errorSet,
	warnings errorSet,
	policy *report.CoveragePolicy,
) error {
	files, err := bindRunEvidence(layout)
	if err != nil {
		return err
	}
	evidence := runstatus.Evidence{
		Schema:              runstatus.Schema,
		Command:             append([]string(nil), runResult.Command...),
		WorkDir:             layout.WorkDir,
		ChildStarted:        !runResult.StartedAt.IsZero(),
		Signal:              runResult.Signal,
		Interrupted:         runResult.Interrupted,
		Cancellation:        string(runResult.Cancellation),
		CancellationSignal:  runResult.CancellationSignal,
		RecommendedExitCode: runResult.RecommendedExitCode,
		CaptureComplete:     captureComplete,
		Files:               files,
	}
	if !runResult.StartedAt.IsZero() && runResult.Duration > 0 {
		evidence.DurationNS = runResult.Duration.Nanoseconds()
	}
	if policy != nil {
		evidence.CoveragePolicy = &runstatus.CoveragePolicy{
			Minimum:    policy.Minimum,
			Actual:     policy.Actual,
			Covered:    policy.Covered,
			Statements: policy.Statements,
			Available:  policy.Available,
			Satisfied:  policy.Satisfied,
		}
	}
	if !runResult.StartedAt.IsZero() {
		started := runResult.StartedAt
		evidence.StartedAt = &started
	}
	if !runResult.FinishedAt.IsZero() {
		finished := runResult.FinishedAt
		evidence.FinishedAt = &finished
	}
	if runResult.ExitCode >= 0 {
		exitCode := runResult.ExitCode
		evidence.ExitCode = &exitCode
	}
	for _, runErr := range runErrors.values {
		if runErr == nil {
			continue
		}
		evidence.Issues = append(evidence.Issues, runstatus.Issue{
			Kind:    "run",
			Message: runErr.Error(),
			Fatal:   true,
		})
	}
	for _, warning := range warnings.values {
		if warning == nil {
			continue
		}
		evidence.Issues = append(evidence.Issues, runstatus.Issue{
			Kind:    "warning",
			Message: warning.Error(),
		})
	}
	return layout.WriteAtomic(artifact.RunJSON, func(writer io.Writer) error {
		return runstatus.Encode(writer, evidence)
	})
}

func bindRunEvidence(layout *artifact.Layout) ([]runstatus.File, error) {
	if layout == nil {
		return nil, errors.New("bind run evidence: artifact layout is nil")
	}
	names := []artifact.Name{
		artifact.TestOutputJSONL,
		artifact.StderrLog,
		artifact.CoverageProfile,
	}
	files := make([]runstatus.File, 0, len(names))
	for _, name := range names {
		path, err := layout.Path(name)
		if err != nil {
			return nil, err
		}
		_, statErr := os.Lstat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			if name == artifact.TestOutputJSONL {
				return nil, fmt.Errorf(
					"bind run evidence: required %s is missing",
					name,
				)
			}
			continue
		}
		if statErr != nil {
			return nil, fmt.Errorf(
				"bind run evidence: inspect %s: %w",
				name,
				statErr,
			)
		}
		digest, size, err := artifact.SHA256File(path)
		if err != nil {
			return nil, fmt.Errorf(
				"bind run evidence: hash %s: %w",
				name,
				err,
			)
		}
		files = append(files, runstatus.File{
			Name:   string(name),
			Size:   size,
			SHA256: digest,
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})
	return files, nil
}

func loadRunStatus(path string) (runstatus.Evidence, error) {
	file, err := openRegularEvidence(path)
	if err != nil {
		return runstatus.Evidence{}, err
	}
	evidence, decodeErr := runstatus.Decode(file)
	closeErr := file.Close()
	if decodeErr != nil || closeErr != nil {
		return runstatus.Evidence{}, errors.Join(
			decodeErr,
			wrapError(closeErr, "close run metadata %q", path),
		)
	}
	return evidence, nil
}

func verifyRunStatus(
	layout *artifact.Layout,
	evidence runstatus.Evidence,
	verifyCompleteBundle bool,
) error {
	bindings := make(map[artifact.Name]runstatus.File, len(evidence.Files))
	for _, binding := range evidence.Files {
		bindings[artifact.Name(binding.Name)] = binding
	}
	names := []artifact.Name{
		artifact.TestOutputJSONL,
		artifact.StderrLog,
		artifact.CoverageProfile,
	}
	for _, name := range names {
		path, err := layout.Path(name)
		if err != nil {
			return fmt.Errorf(
				"verify run metadata binding %q: %w",
				name,
				err,
			)
		}
		_, statErr := os.Lstat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			if verifyCompleteBundle {
				if _, bound := bindings[name]; bound {
					return fmt.Errorf(
						"verify run metadata binding %q: bound artifact is missing",
						name,
					)
				}
			}
			continue
		}
		if statErr != nil {
			return fmt.Errorf(
				"verify run metadata binding %q: inspect: %w",
				name,
				statErr,
			)
		}
		binding, bound := bindings[name]
		if !bound {
			return fmt.Errorf(
				"verify run metadata: existing evidence %q is not bound",
				name,
			)
		}
		digest, size, err := artifact.SHA256File(path)
		if err != nil {
			return fmt.Errorf(
				"verify run metadata binding %q: %w",
				name,
				err,
			)
		}
		if size != binding.Size || digest != binding.SHA256 {
			return fmt.Errorf(
				"verify run metadata binding %q: got size %d SHA-256 %s, "+
					"want size %d SHA-256 %s",
				name,
				size,
				digest,
				binding.Size,
				binding.SHA256,
			)
		}
	}
	if _, ok := bindings[artifact.TestOutputJSONL]; !ok {
		return errors.New(
			"verify run metadata: test_output.jsonl binding is missing",
		)
	}
	return nil
}

func writeBoundManifest(layout *artifact.Layout) error {
	if layout == nil {
		return errors.New("write bound manifest: artifact layout is nil")
	}
	_, err := layout.WriteManifestValidated(
		func(entries []artifact.ManifestEntry) error {
			evidence, err := loadRunStatus(layout.RunJSON)
			if err != nil {
				return fmt.Errorf(
					"load run metadata for manifest: %w",
					err,
				)
			}
			return verifyManifestRunBindings(evidence, entries)
		},
	)
	return err
}

func verifyManifestRunBindings(
	evidence runstatus.Evidence,
	entries []artifact.ManifestEntry,
) error {
	inventory := make(
		map[artifact.Name]artifact.ManifestEntry,
		len(entries),
	)
	for _, entry := range entries {
		name := artifact.Name(entry.Name)
		if _, exists := inventory[name]; exists {
			return fmt.Errorf(
				"manifest inventory contains duplicate artifact %q",
				entry.Name,
			)
		}
		inventory[name] = entry
	}
	if _, ok := inventory[artifact.RunJSON]; !ok {
		return errors.New(
			"manifest inventory does not contain authoritative run.json",
		)
	}

	bindings := make(map[artifact.Name]runstatus.File, len(evidence.Files))
	for _, binding := range evidence.Files {
		bindings[artifact.Name(binding.Name)] = binding
	}
	for _, name := range []artifact.Name{
		artifact.TestOutputJSONL,
		artifact.StderrLog,
		artifact.CoverageProfile,
	} {
		entry, exists := inventory[name]
		binding, bound := bindings[name]
		switch {
		case exists && !bound:
			return fmt.Errorf(
				"manifest evidence %q is not bound by run.json",
				name,
			)
		case !exists && bound:
			return fmt.Errorf(
				"run.json binds missing manifest evidence %q",
				name,
			)
		case exists &&
			(entry.Size != binding.Size ||
				entry.SHA256 != binding.SHA256):
			return fmt.Errorf(
				"manifest evidence %q has size %d SHA-256 %s, "+
					"but run.json binds size %d SHA-256 %s",
				name,
				entry.Size,
				entry.SHA256,
				binding.Size,
				binding.SHA256,
			)
		}
	}
	return nil
}

func resultMetadataFromRunStatus(
	workDir string,
	evidence runstatus.Evidence,
) result.RunMetadata {
	metadata := result.RunMetadata{
		Command:             append([]string(nil), evidence.Command...),
		WorkDir:             workDir,
		ExitCode:            -1,
		Duration:            time.Duration(evidence.DurationNS),
		Signal:              evidence.Signal,
		Interrupted:         evidence.Interrupted,
		Cancellation:        evidence.Cancellation,
		CancellationSignal:  evidence.CancellationSignal,
		RecommendedExitCode: evidence.RecommendedExitCode,
	}
	if evidence.StartedAt != nil {
		metadata.StartedAt = *evidence.StartedAt
	}
	if evidence.FinishedAt != nil {
		metadata.FinishedAt = *evidence.FinishedAt
	}
	if evidence.ExitCode != nil {
		metadata.ExitCode = *evidence.ExitCode
	}
	return metadata
}

func reportPolicyFromRunStatus(
	evidence runstatus.Evidence,
) *report.CoveragePolicy {
	if evidence.CoveragePolicy == nil {
		return nil
	}
	return &report.CoveragePolicy{
		Minimum:    evidence.CoveragePolicy.Minimum,
		Actual:     evidence.CoveragePolicy.Actual,
		Covered:    evidence.CoveragePolicy.Covered,
		Statements: evidence.CoveragePolicy.Statements,
		Available:  evidence.CoveragePolicy.Available,
		Satisfied:  evidence.CoveragePolicy.Satisfied,
	}
}

func runStatusBinds(
	evidence runstatus.Evidence,
	name artifact.Name,
) bool {
	for _, binding := range evidence.Files {
		if binding.Name == string(name) {
			return true
		}
	}
	return false
}

func withoutCoverageBinding(
	evidence runstatus.Evidence,
) (runstatus.Evidence, bool) {
	files := make([]runstatus.File, 0, len(evidence.Files))
	removed := false
	for _, binding := range evidence.Files {
		if binding.Name == artifact.CoverageProfileName {
			removed = true
			continue
		}
		files = append(files, binding)
	}
	if !removed {
		return evidence, false
	}
	projected := evidence
	projected.Files = files
	return projected, true
}

func validateRunStatusCoveragePolicy(
	evidence runstatus.Evidence,
	profile *coverage.Profile,
	requireProfile bool,
) (*report.CoveragePolicy, error) {
	policy := reportPolicyFromRunStatus(evidence)
	if policy == nil {
		return nil, nil
	}
	threshold, err := coverage.ParseThreshold(policy.Minimum)
	if err != nil {
		return nil, fmt.Errorf(
			"validate run metadata coverage policy: %w",
			err,
		)
	}
	if !policy.Available {
		if policy.Actual != "" || policy.Covered != 0 ||
			policy.Statements != 0 || policy.Satisfied {
			return nil, errors.New(
				"validate run metadata coverage policy: unavailable policy " +
					"contains an outcome",
			)
		}
		if profile != nil && profile.Total.Statements != 0 {
			return nil, fmt.Errorf(
				"validate run metadata coverage policy: policy records "+
					"unavailable coverage, but the profile contains %d statements",
				profile.Total.Statements,
			)
		}
		return policy, nil
	}
	if profile == nil {
		if requireProfile {
			return nil, errors.New(
				"validate run metadata coverage policy: bound profile is unavailable",
			)
		}
		totals := coverage.Totals{
			Covered:    policy.Covered,
			Statements: policy.Statements,
		}
		satisfied, err := threshold.SatisfiedBy(totals)
		if err != nil {
			return nil, fmt.Errorf(
				"validate run metadata coverage policy: %w",
				err,
			)
		}
		actual := formatPolicyPercentage(totals, policy.Minimum)
		if policy.Actual != actual || policy.Satisfied != satisfied {
			return nil, fmt.Errorf(
				"validate run metadata coverage policy: recorded actual/satisfied "+
					"%q/%t, recomputed %q/%t",
				policy.Actual,
				policy.Satisfied,
				actual,
				satisfied,
			)
		}
		return policy, nil
	}
	if policy.Covered != profile.Total.Covered ||
		policy.Statements != profile.Total.Statements {
		return nil, fmt.Errorf(
			"validate run metadata coverage policy: counts %d/%d do not "+
				"match profile %d/%d",
			policy.Covered,
			policy.Statements,
			profile.Total.Covered,
			profile.Total.Statements,
		)
	}
	satisfied, err := threshold.SatisfiedBy(profile.Total)
	if err != nil {
		return nil, fmt.Errorf(
			"validate run metadata coverage policy: %w",
			err,
		)
	}
	actual := formatPolicyPercentage(profile.Total, policy.Minimum)
	if policy.Actual != actual || policy.Satisfied != satisfied {
		return nil, fmt.Errorf(
			"validate run metadata coverage policy: recorded actual/satisfied "+
				"%q/%t, recomputed %q/%t",
			policy.Actual,
			policy.Satisfied,
			actual,
			satisfied,
		)
	}
	return policy, nil
}

func buildReportAssessment(
	snapshot result.Result,
	state ExitState,
	runErrors errorSet,
	warnings errorSet,
	policy *report.CoveragePolicy,
) *report.Assessment {
	assessment := &report.Assessment{
		ChildStarted:         state.ChildStarted,
		ChildExitKnown:       state.ChildExitKnown,
		Cancellation:         snapshot.Metadata.Cancellation,
		CancellationSignal:   snapshot.Metadata.CancellationSignal,
		RecommendedExitCode:  snapshot.Metadata.RecommendedExitCode,
		InfrastructureFailed: state.InfrastructureErr,
		ReportFailed:         state.ReportErr,
		EvidenceIncomplete:   state.EvidenceIncomplete,
		StreamCorrupt:        state.StreamCorrupt,
		CoveragePolicy:       policy,
	}
	for _, runErr := range runErrors.values {
		if runErr == nil {
			continue
		}
		assessment.Issues = append(assessment.Issues, report.Issue{
			Kind:    "run",
			Message: runErr.Error(),
			Fatal:   true,
		})
	}
	for _, warning := range warnings.values {
		if warning == nil {
			continue
		}
		assessment.Issues = append(assessment.Issues, report.Issue{
			Kind:    "warning",
			Message: warning.Error(),
		})
	}
	if resultFailed(snapshot) && state.ChildExitKnown &&
		state.ChildExitCode == 0 {
		assessment.Issues = append(assessment.Issues, report.Issue{
			Kind: "integrity",
			Message: "failing Go test events conflict with the successful " +
				"child process exit status",
			Fatal: true,
		})
		assessment.EvidenceIncomplete = true
	}
	return assessment
}
