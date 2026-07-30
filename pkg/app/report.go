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
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/greenpau/tested/pkg/artifact"
	"github.com/greenpau/tested/pkg/cli"
	"github.com/greenpau/tested/pkg/coverage"
	"github.com/greenpau/tested/pkg/protocol"
	"github.com/greenpau/tested/pkg/report"
	"github.com/greenpau/tested/pkg/result"
	"github.com/greenpau/tested/pkg/runstatus"
)

func executeReport(
	ctx context.Context,
	options cli.Options,
	layout *artifact.Layout,
	renderer *report.Renderer,
	console *report.Console,
) executionOutcome {
	var outcome executionOutcome
	reportCommandCancelled := false
	if err := ctx.Err(); err != nil {
		outcome.errors.add(fmt.Errorf("report cancelled before artifact preparation: %w", err))
		applyContextCancellation(ctx, &outcome.state)
		return outcome
	}

	eventPath := resolveInputPath(layout.WorkDir, options.EventsFile)
	if eventPath == "" {
		eventPath = layout.TestOutputJSONL
	}
	eventIsDefault := sameFilePath(eventPath, layout.TestOutputJSONL)
	profilePath := resolveInputPath(layout.WorkDir, options.CoverageProfileFile)
	if profilePath == "" {
		profilePath = layout.CoverageProfile
	}
	profileSelected := options.CoverageProfileFile != ""
	stderrPath := resolveInputPath(layout.WorkDir, options.StderrFile)
	stderrSelected := stderrPath != ""
	statusPath := resolveInputPath(layout.WorkDir, options.RunMetadataFile)
	statusExplicit := statusPath != ""
	if statusPath == "" && eventIsDefault {
		statusPath = layout.RunJSON
	}

	if err := validateOfflineSource(layout, eventPath, artifact.TestOutputJSONL); err != nil {
		outcome.errors.add(err)
		outcome.state.InfrastructureErr = true
		return outcome
	}
	if statusPath != "" {
		if err := validateOfflineSource(layout, statusPath, artifact.RunJSON); err != nil {
			outcome.errors.add(err)
			outcome.state.InfrastructureErr = true
			return outcome
		}
	}

	var statusEvidence runstatus.Evidence
	var statusReadErr error
	if statusPath != "" {
		statusEvidence, statusReadErr = loadRunStatus(statusPath)
	}
	statusProjectedWithoutCoverage := false
	if options.NoCoverage && statusReadErr == nil {
		if statusEvidence.CoveragePolicy != nil {
			outcome.errors.add(errors.New(
				"report --no-coverage cannot omit coverage from run metadata " +
					"with a recorded coverage policy; rerun without " +
					"--no-coverage or select a policy-free bundle",
			))
			outcome.state.InfrastructureErr = true
			return outcome
		}
		statusEvidence, statusProjectedWithoutCoverage =
			withoutCoverageBinding(statusEvidence)
	}
	statusIsDefault := statusPath != "" &&
		sameFilePath(statusPath, layout.RunJSON)
	if statusExplicit && !statusIsDefault && statusReadErr == nil {
		sourceDir := filepath.Dir(statusPath)
		if !stderrSelected &&
			runStatusBinds(statusEvidence, artifact.StderrLog) {
			stderrPath = filepath.Join(
				sourceDir,
				artifact.StderrLogName,
			)
			stderrSelected = true
		}
		if !profileSelected &&
			runStatusBinds(statusEvidence, artifact.CoverageProfile) {
			profilePath = filepath.Join(
				sourceDir,
				artifact.CoverageProfileName,
			)
			profileSelected = true
		}
	}
	if profileSelected {
		if err := validateOfflineSource(
			layout,
			profilePath,
			artifact.CoverageProfile,
		); err != nil {
			outcome.errors.add(err)
			outcome.state.InfrastructureErr = true
			return outcome
		}
	}
	if stderrSelected {
		if err := validateOfflineSource(
			layout,
			stderrPath,
			artifact.StderrLog,
		); err != nil {
			outcome.errors.add(err)
			outcome.state.InfrastructureErr = true
			return outcome
		}
	}

	var prepareErr error
	if options.NoCoverage {
		prepareErr = layout.PrepareReportWithoutCoverage()
	} else {
		prepareErr = layout.PrepareReport()
	}
	if prepareErr != nil {
		outcome.errors.add(prepareErr)
		outcome.state.InfrastructureErr = true
		return outcome
	}
	if !sameFilePath(eventPath, layout.TestOutputJSONL) {
		if err := copyEvidence(
			ctx,
			layout,
			artifact.TestOutputJSONL,
			eventPath,
		); err != nil {
			outcome.errors.add(err)
			outcome.state.InfrastructureErr = true
			applyContextCancellation(ctx, &outcome.state)
			return outcome
		}
		eventPath = layout.TestOutputJSONL
	}
	if stderrSelected {
		if !sameFilePath(stderrPath, layout.StderrLog) {
			if err := copyEvidence(
				ctx,
				layout,
				artifact.StderrLog,
				stderrPath,
			); err != nil {
				outcome.errors.add(err)
				outcome.state.InfrastructureErr = true
				applyContextCancellation(ctx, &outcome.state)
				return outcome
			}
		}
	} else if !eventIsDefault ||
		(statusExplicit && statusReadErr == nil &&
			!runStatusBinds(statusEvidence, artifact.StderrLog)) {
		if err := layout.RemoveEvidence(artifact.StderrLog); err != nil {
			outcome.errors.add(fmt.Errorf(
				"remove stale standard-error evidence: %w",
				err,
			))
			outcome.state.InfrastructureErr = true
			return outcome
		}
	}
	if profileSelected &&
		!sameFilePath(profilePath, layout.CoverageProfile) {
		if err := copyEvidence(
			ctx,
			layout,
			artifact.CoverageProfile,
			profilePath,
		); err != nil {
			outcome.errors.add(err)
			outcome.state.InfrastructureErr = true
			applyContextCancellation(ctx, &outcome.state)
			return outcome
		}
		profilePath = layout.CoverageProfile
	} else if options.NoCoverage ||
		(!profileSelected && (!eventIsDefault ||
			(statusExplicit && statusReadErr == nil &&
				!runStatusBinds(statusEvidence, artifact.CoverageProfile)))) {
		if err := layout.RemoveEvidence(artifact.CoverageProfile); err != nil {
			outcome.errors.add(fmt.Errorf(
				"remove unbound coverage evidence: %w",
				err,
			))
			outcome.state.InfrastructureErr = true
			return outcome
		}
	}
	switch {
	case statusPath == "":
		if err := layout.RemoveEvidence(artifact.RunJSON); err != nil {
			outcome.errors.add(fmt.Errorf("remove unrelated run metadata: %w", err))
			outcome.state.InfrastructureErr = true
			return outcome
		}
	case statusReadErr == nil &&
		(!statusIsDefault || statusProjectedWithoutCoverage):
		if err := layout.WriteAtomic(
			artifact.RunJSON,
			func(writer io.Writer) error {
				return runstatus.Encode(writer, statusEvidence)
			},
		); err != nil {
			statusReadErr = fmt.Errorf("import run metadata: %w", err)
		}
	case statusReadErr != nil && statusExplicit && !statusIsDefault:
		if err := layout.RemoveEvidence(artifact.RunJSON); err != nil {
			outcome.errors.add(fmt.Errorf("remove unrelated run metadata: %w", err))
			outcome.state.InfrastructureErr = true
			return outcome
		}
	}

	analyzer, err := result.NewAnalyzer(result.AnalyzerOptions{
		MaxOutputBytes:       options.MaximumOutputBytes,
		MaxTotalOutputBytes:  options.MaximumTotalOutputBytes,
		MaxResultEntries:     options.MaximumResultEntries,
		MaxNormalizedBytes:   options.MaximumNormalizedBytes,
		MaxSemanticLineBytes: options.MaximumEventBytes,
	})
	if err != nil {
		outcome.errors.add(err)
		outcome.state.InfrastructureErr = true
		return outcome
	}
	eventFile, err := openRegularEvidence(eventPath)
	if err != nil {
		outcome.errors.add(err)
		outcome.state.InfrastructureErr = true
		return outcome
	}
	var eventReader io.Reader = eventFile
	var eventVerifier *boundEvidenceReader
	if statusReadErr == nil {
		if binding, ok := statusFileBinding(
			statusEvidence,
			artifact.TestOutputJSONL,
		); ok {
			eventVerifier, err = newBoundEvidenceReader(
				eventFile,
				eventPath,
				binding,
			)
			if err != nil {
				_ = eventFile.Close()
				outcome.errors.add(err)
				outcome.state.InfrastructureErr = true
				return outcome
			}
			eventReader = eventVerifier
		}
	}
	var analysisErrors errorSet
	streamSummary, readErr := protocol.Read(&contextReader{
		ctx:    ctx,
		reader: eventReader,
	}, protocol.StreamOptions{
		MaxRecordBytes:       int(options.MaximumEventBytes),
		UnlimitedRecordBytes: options.MaximumEventBytes == 0,
		Handler: func(record protocol.Record) {
			analysisErrors.add(analyzer.AddRecord(record))
		},
	})
	var eventVerifyErr error
	if eventVerifier != nil {
		eventVerifyErr = eventVerifier.Verify()
	}
	closeErr := eventFile.Close()
	if readErr != nil {
		outcome.errors.add(fmt.Errorf("read test event log: %w", readErr))
		outcome.state.InfrastructureErr = true
	}
	if closeErr != nil {
		outcome.errors.add(fmt.Errorf("close test event log: %w", closeErr))
		outcome.state.InfrastructureErr = true
	}
	if analysisErrors.count() > 0 {
		appendErrors(&outcome.errors, &analysisErrors)
		outcome.state.InfrastructureErr = true
	}

	metadata := result.RunMetadata{
		WorkDir:  layout.WorkDir,
		ExitCode: -1,
	}
	statusTrusted := false
	var statusVerifyErr error
	if statusPath != "" && statusReadErr == nil {
		statusVerifyErr = errors.Join(
			verifyRunStatus(
				layout,
				statusEvidence,
				true,
			),
			eventVerifyErr,
		)
	}
	switch {
	case statusPath == "":
		outcome.errors.add(errors.New(
			"authoritative run metadata is unavailable; provide --run-metadata " +
				"for an imported event stream",
		))
		outcome.state.InfrastructureErr = true
		outcome.state.EvidenceIncomplete = true
	case statusReadErr != nil:
		outcome.errors.add(fmt.Errorf("load run metadata: %w", statusReadErr))
		outcome.state.InfrastructureErr = true
		outcome.state.EvidenceIncomplete = true
	case statusVerifyErr != nil:
		outcome.errors.add(statusVerifyErr)
		outcome.state.InfrastructureErr = true
		outcome.state.EvidenceIncomplete = true
	default:
		statusTrusted = true
		metadata = resultMetadataFromRunStatus(layout.WorkDir, statusEvidence)
		outcome.state.ChildStarted = statusEvidence.ChildStarted
		outcome.state.ChildExitKnown = statusEvidence.ExitCode != nil
		outcome.state.ChildExitCode = metadata.ExitCode
		outcome.state.Interrupted = statusEvidence.Interrupted
		outcome.state.InterruptExitCode = statusEvidence.RecommendedExitCode
		if !statusEvidence.CaptureComplete {
			outcome.errors.add(errors.New(
				"run metadata records incomplete raw stream capture",
			))
			outcome.state.EvidenceIncomplete = true
		}
		if statusEvidence.ExitCode == nil {
			outcome.errors.add(errors.New(
				"run metadata does not contain an authoritative child exit status",
			))
			outcome.state.EvidenceIncomplete = true
		}
		for _, issue := range statusEvidence.Issues {
			issueErr := fmt.Errorf("%s: %s", issue.Kind, issue.Message)
			if issue.Fatal {
				outcome.errors.add(issueErr)
				outcome.state.InfrastructureErr = true
			} else {
				outcome.warnings.add(issueErr)
			}
		}
	}

	snapshot := analyzer.Finalize(metadata)
	outcome.state.StreamCorrupt = streamSummary.DiagnosticCount > 0
	outcome.state.EvidenceFailed = resultFailed(snapshot)
	outcome.state.EvidenceIncomplete =
		outcome.state.EvidenceIncomplete || resultIncomplete(snapshot)
	outcome.state.AllowFailures = options.AllowFailures

	var profile *coverage.Profile
	var coverageSnapshotPath string
	manifestEligible := readErr == nil && closeErr == nil &&
		analysisErrors.count() == 0 && statusTrusted &&
		statusEvidence.CaptureComplete &&
		!outcome.state.StreamCorrupt &&
		!resultIncomplete(snapshot)
	useCoverage := !options.NoCoverage
	if statusTrusted &&
		!runStatusBinds(statusEvidence, artifact.CoverageProfile) &&
		options.CoverageProfileFile == "" {
		useCoverage = false
	}
	if useCoverage {
		profile, coverageSnapshotPath, err = parseCoverageSnapshot(
			layout,
			profilePath,
			statusEvidence,
			statusTrusted,
		)
		if err != nil {
			outcome.errors.add(err)
			outcome.state.InfrastructureErr = true
			outcome.state.EvidenceIncomplete = true
			manifestEligible = false
		} else {
			defer func() {
				_ = os.Remove(coverageSnapshotPath)
			}()
		}
	}
	var coveragePolicy *report.CoveragePolicy
	if statusTrusted {
		var policyErr error
		coveragePolicy, policyErr = validateRunStatusCoveragePolicy(
			statusEvidence,
			profile,
			useCoverage,
		)
		if policyErr != nil {
			outcome.errors.add(policyErr)
			outcome.state.InfrastructureErr = true
			outcome.state.EvidenceIncomplete = true
			manifestEligible = false
		} else if coveragePolicy != nil {
			outcome.state.CoverageBelow =
				coveragePolicy.Available && !coveragePolicy.Satisfied
		}
	}
	evidenceContradictsChild := outcome.state.EvidenceFailed &&
		outcome.state.ChildExitKnown &&
		outcome.state.ChildExitCode == 0
	manifestEligible = manifestEligible &&
		!outcome.state.InfrastructureErr &&
		!outcome.state.EvidenceIncomplete &&
		!evidenceContradictsChild
	if useCoverage && profile != nil {
		if coverageReportErr := publishCoverageReport(
			ctx,
			options.GoBinary,
			layout,
			renderer,
			coverageSnapshotPath,
		); coverageReportErr != nil {
			outcome.errors.add(coverageReportErr)
			outcome.state.ReportErr = true
			manifestEligible = false
		}
	}
	assessment := buildReportAssessment(
		snapshot,
		outcome.state,
		outcome.errors,
		outcome.warnings,
		coveragePolicy,
	)
	input := report.Input{
		Result:     snapshot,
		Coverage:   profile,
		Assessment: assessment,
	}
	refreshReports := func() {
		assessment = buildReportAssessment(
			snapshot,
			outcome.state,
			outcome.errors,
			outcome.warnings,
			coveragePolicy,
		)
		input.Assessment = assessment
		if repairErrors := repairReports(
			layout,
			renderer,
			input,
		); repairErrors.count() > 0 {
			appendErrors(&outcome.errors, &repairErrors)
			outcome.state.ReportErr = true
			assessment = buildReportAssessment(
				snapshot,
				outcome.state,
				outcome.errors,
				outcome.warnings,
				coveragePolicy,
			)
			input.Assessment = assessment
		}
	}
	if reportErrors := publishReports(
		ctx,
		layout,
		renderer,
		input,
	); reportErrors.count() > 0 {
		appendErrors(&outcome.errors, &reportErrors)
		outcome.state.ReportErr = true
		manifestEligible = false
	}
	if applyContextCancellation(ctx, &outcome.state) &&
		!reportCommandCancelled {
		reportCommandCancelled = true
		outcome.errors.add(fmt.Errorf(
			"report generation cancelled: %w",
			context.Cause(ctx),
		))
		outcome.state.ReportErr = true
		manifestEligible = false
	}
	if reportCommandCancelled ||
		(outcome.state.ReportErr && !assessment.ReportFailed) {
		refreshReports()
	}
	if err := console.Final(input); err != nil {
		outcome.errors.add(err)
		outcome.state.ReportErr = true
		manifestEligible = false
		refreshReports()
	}
	if applyContextCancellation(ctx, &outcome.state) &&
		!reportCommandCancelled {
		reportCommandCancelled = true
		outcome.errors.add(fmt.Errorf(
			"report generation cancelled: %w",
			context.Cause(ctx),
		))
		outcome.state.ReportErr = true
		manifestEligible = false
		refreshReports()
	}

	manifestEligible = manifestEligible &&
		!outcome.state.InfrastructureErr &&
		!outcome.state.EvidenceIncomplete &&
		!outcome.state.ReportErr &&
		!outcome.state.Interrupted &&
		ctx.Err() == nil
	if manifestEligible {
		if err := writeBoundManifest(layout); err != nil {
			outcome.errors.add(err)
			outcome.state.ReportErr = true
			refreshReports()
		}
	}
	if applyContextCancellation(ctx, &outcome.state) &&
		!reportCommandCancelled {
		reportCommandCancelled = true
		outcome.errors.add(fmt.Errorf(
			"report generation cancelled: %w",
			context.Cause(ctx),
		))
		outcome.state.ReportErr = true
		refreshReports()
	}
	if reportCommandCancelled {
		if err := layout.Remove(artifact.ManifestJSON); err != nil {
			outcome.errors.add(fmt.Errorf(
				"remove manifest after report cancellation: %w",
				err,
			))
			outcome.state.InfrastructureErr = true
		}
	}
	return outcome
}

func loadLiveCoverage(
	options cli.Options,
	layout *artifact.Layout,
	childExitCode int,
) (*coverage.Profile, string, error) {
	if options.NoCoverage {
		return nil, "", nil
	}
	profile, snapshotPath, err := parseCoverageSnapshot(
		layout,
		layout.CoverageProfile,
		runstatus.Evidence{},
		false,
	)
	if err != nil {
		return nil, "", fmt.Errorf(
			"load coverage after child exit %d: %w",
			childExitCode,
			err,
		)
	}
	return profile, snapshotPath, nil
}

func publishReports(
	ctx context.Context,
	layout *artifact.Layout,
	renderer *report.Renderer,
	input report.Input,
) errorSet {
	var reportErrors errorSet
	if err := ctx.Err(); err != nil {
		reportErrors.add(fmt.Errorf("publish test reports: %w", err))
		return reportErrors
	}
	reportErrors.add(renderer.PublishTestOutputHTML(layout, input))
	if err := ctx.Err(); err != nil {
		reportErrors.add(fmt.Errorf("publish test reports: %w", err))
		return reportErrors
	}
	reportErrors.add(renderer.PublishSummaryJSON(layout, input))
	if err := ctx.Err(); err != nil {
		reportErrors.add(fmt.Errorf("publish test reports: %w", err))
		return reportErrors
	}
	reportErrors.add(renderer.PublishJUnitXML(layout, input))
	if err := ctx.Err(); err != nil {
		reportErrors.add(fmt.Errorf("publish test reports: %w", err))
		return reportErrors
	}
	reportErrors.add(renderer.PublishIndexHTML(layout, input))
	return reportErrors
}

func repairReports(
	layout *artifact.Layout,
	renderer *report.Renderer,
	input report.Input,
) errorSet {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reportErrors := publishReports(ctx, layout, renderer, input)
	if reportErrors.count() == 0 {
		return reportErrors
	}
	for _, name := range []artifact.Name{
		artifact.TestOutputHTML,
		artifact.SummaryJSON,
		artifact.JUnitXML,
		artifact.IndexHTML,
	} {
		if err := layout.Remove(name); err != nil {
			reportErrors.add(fmt.Errorf(
				"remove stale report projection %q: %w",
				name,
				err,
			))
		}
	}
	return reportErrors
}

func publishCoverageReport(
	ctx context.Context,
	goCommand string,
	layout *artifact.Layout,
	renderer *report.Renderer,
	profilePath string,
) error {
	if renderer == nil {
		return errors.New("publish coverage report: renderer is nil")
	}
	if profilePath == "" {
		profilePath = layout.CoverageProfile
	}
	return coverage.GenerateHTML(ctx, coverage.HTMLOptions{
		GoCommand:   goCommand,
		ProjectDir:  layout.WorkDir,
		ProfilePath: profilePath,
		OutputPath:  layout.CoverageHTML,
		Decorate:    renderer.DecorateCoverageHTML,
	})
}

func resultFailed(snapshot result.Result) bool {
	return snapshot.Summary.Tests.Failed > 0 ||
		snapshot.Summary.Packages.Failed > 0 ||
		snapshot.Summary.BuildFailures > 0
}

func resultIncomplete(snapshot result.Result) bool {
	return !snapshot.Finalized ||
		snapshot.Incomplete ||
		snapshot.Summary.Incomplete ||
		snapshot.IntegrityDiagnosticCount > 0 ||
		snapshot.Summary.Tests.Incomplete > 0 ||
		snapshot.Summary.Packages.Incomplete > 0
}

func resolveInputPath(workDir, path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(workDir, path)
}

func sameFilePath(left, right string) bool {
	if filepath.Clean(left) == filepath.Clean(right) {
		return true
	}
	leftInfo, leftErr := os.Lstat(left)
	rightInfo, rightErr := os.Lstat(right)
	return leftErr == nil && rightErr == nil &&
		os.SameFile(leftInfo, rightInfo)
}

func copyEvidence(
	ctx context.Context,
	layout *artifact.Layout,
	name artifact.Name,
	source string,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("copy evidence %q: %w", source, err)
	}
	sourceFile, err := openRegularEvidence(source)
	if err != nil {
		return err
	}
	writeErr := layout.WriteAtomic(name, func(writer io.Writer) error {
		_, err := io.Copy(writer, &contextReader{
			ctx:    ctx,
			reader: sourceFile,
		})
		return err
	})
	closeErr := sourceFile.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(
			writeErr,
			wrapError(closeErr, "close imported evidence %q", source),
		)
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if r == nil || r.reader == nil {
		return 0, errors.New("context reader is nil")
	}
	if r.ctx == nil {
		return 0, errors.New("context reader has nil context")
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

func validateOfflineSource(
	layout *artifact.Layout,
	source string,
	expected artifact.Name,
) error {
	if layout == nil {
		return errors.New("validate offline source: artifact layout is nil")
	}
	cleanSource := filepath.Clean(source)
	for _, name := range artifact.ManagedNames() {
		path, err := layout.Path(name)
		if err != nil {
			return err
		}
		if cleanSource != filepath.Clean(path) {
			continue
		}
		if name == expected {
			return nil
		}
		return fmt.Errorf(
			"offline %s source %q is managed as %s and cannot be imported",
			expected,
			source,
			name,
		)
	}
	return nil
}

func openRegularEvidence(path string) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect evidence %q: %w", path, err)
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("inspect evidence %q: symbolic links are not accepted", path)
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("inspect evidence %q: not a regular file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open evidence %q: %w", path, err)
	}
	opened, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect opened evidence %q: %w", path, statErr)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		_ = file.Close()
		return nil, fmt.Errorf("open evidence %q: file changed while opening", path)
	}
	return file, nil
}

func appendErrors(destination, source *errorSet) {
	values := append([]error(nil), source.values...)
	for _, err := range values {
		destination.add(err)
	}
}

func wrapError(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf(format+": %w", append(args, err)...)
}
