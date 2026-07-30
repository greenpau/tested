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
	"fmt"
	"os"
	"strings"

	"github.com/greenpau/tested/pkg/artifact"
	"github.com/greenpau/tested/pkg/cli"
	"github.com/greenpau/tested/pkg/coverage"
	"github.com/greenpau/tested/pkg/protocol"
	"github.com/greenpau/tested/pkg/report"
	"github.com/greenpau/tested/pkg/result"
	"github.com/greenpau/tested/pkg/runner"
)

func executeRun(
	ctx context.Context,
	options cli.Options,
	layout *artifact.Layout,
	renderer *report.Renderer,
	console *report.Console,
) executionOutcome {
	var outcome executionOutcome
	var analysisErrors errorSet
	var liveConsoleErrors errorSet
	captureComplete := true
	if err := layout.PrepareRun(); err != nil {
		outcome.errors.add(err)
		outcome.state.InfrastructureErr = true
		return outcome
	}

	eventFile, err := layout.CreateTestOutput()
	if err != nil {
		outcome.errors.add(err)
		outcome.state.InfrastructureErr = true
		return outcome
	}
	stderrFile, err := layout.CreateStderrLog()
	if err != nil {
		outcome.errors.add(err)
		outcome.errors.add(syncCloseFile("test event log", eventFile))
		outcome.state.InfrastructureErr = true
		return outcome
	}
	if !options.NoCoverage {
		profileFile, createErr := layout.CreateCoverageProfile()
		if createErr != nil {
			outcome.errors.add(createErr)
			outcome.errors.add(syncCloseFile("test event log", eventFile))
			outcome.errors.add(syncCloseFile("standard error log", stderrFile))
			outcome.state.InfrastructureErr = true
			return outcome
		}
		if closeErr := syncCloseFile("coverage profile placeholder", profileFile); closeErr != nil {
			outcome.errors.add(closeErr)
			outcome.errors.add(syncCloseFile("test event log", eventFile))
			outcome.errors.add(syncCloseFile("standard error log", stderrFile))
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
		outcome.errors.add(syncCloseFile("test event log", eventFile))
		outcome.errors.add(syncCloseFile("standard error log", stderrFile))
		outcome.state.InfrastructureErr = true
		return outcome
	}

	stream := protocol.NewStream(protocol.StreamOptions{
		RawWriter:            eventFile,
		MaxRecordBytes:       int(options.MaximumEventBytes),
		UnlimitedRecordBytes: options.MaximumEventBytes == 0,
		Handler: func(record protocol.Record) {
			if addErr := analyzer.AddRecord(record); addErr != nil {
				analysisErrors.add(fmt.Errorf("analyze event record: %w", addErr))
				return
			}
			if !options.Quiet && options.Format != cli.FormatJSON {
				if pkg, ok := terminalPackage(analyzer, record); ok {
					liveConsoleErrors.add(console.Package(pkg))
				}
			}
		},
	})

	coveragePath := ""
	if !options.NoCoverage {
		coveragePath = layout.CoverageProfile
	}
	runResult, runErr := runner.New().Run(ctx, runner.Options{
		GoCommand:       options.GoBinary,
		WorkDir:         layout.WorkDir,
		TestArguments:   options.GoTestArgs,
		CoverageProfile: coveragePath,
		StandardOutput:  stream,
		StandardError:   stderrFile,
	})
	if runErr != nil {
		outcome.errors.add(runErr)
		outcome.state.InfrastructureErr = true
		captureComplete = false
	}
	streamSummary, streamErr := stream.Finish()
	if streamErr != nil {
		outcome.errors.add(fmt.Errorf("finish test event stream: %w", streamErr))
		outcome.state.InfrastructureErr = true
		captureComplete = false
	}
	if err := syncCloseFile("test event log", eventFile); err != nil {
		outcome.errors.add(err)
		outcome.state.InfrastructureErr = true
		captureComplete = false
	}
	if err := syncCloseFile("standard error log", stderrFile); err != nil {
		outcome.errors.add(err)
		outcome.state.InfrastructureErr = true
		captureComplete = false
	}
	if analysisErrors.count() > 0 {
		appendErrors(&outcome.errors, &analysisErrors)
		outcome.state.InfrastructureErr = true
		captureComplete = false
	}
	if info, statErr := os.Stat(layout.StderrLog); statErr != nil {
		outcome.errors.add(fmt.Errorf("inspect standard error log: %w", statErr))
		outcome.state.InfrastructureErr = true
		captureComplete = false
	} else if info.Size() > 0 {
		outcome.warnings.add(fmt.Errorf(
			"child standard error captured in %q",
			layout.StderrLog,
		))
	}
	mergeContextCancellation(ctx, &runResult)

	snapshot := analyzer.Finalize(result.RunMetadata{
		Command:             runResult.Command,
		WorkDir:             layout.WorkDir,
		StartedAt:           runResult.StartedAt,
		FinishedAt:          runResult.FinishedAt,
		Duration:            runResult.Duration,
		ExitCode:            runResult.ExitCode,
		Signal:              runResult.Signal,
		Interrupted:         runResult.Interrupted,
		Cancellation:        string(runResult.Cancellation),
		CancellationSignal:  runResult.CancellationSignal,
		RecommendedExitCode: runResult.RecommendedExitCode,
	})
	outcome.state.ChildStarted = !runResult.StartedAt.IsZero()
	outcome.state.ChildExitKnown = runResult.ExitCode >= 0
	outcome.state.ChildExitCode = runResult.ExitCode
	outcome.state.Interrupted = runResult.Interrupted || ctx.Err() != nil
	outcome.state.InterruptExitCode = runResult.RecommendedExitCode
	outcome.state.StreamCorrupt = streamSummary.DiagnosticCount > 0
	outcome.state.EvidenceFailed = resultFailed(snapshot)
	outcome.state.EvidenceIncomplete = resultIncomplete(snapshot)

	profile, coverageSnapshotPath, profileErr := loadLiveCoverage(
		options,
		layout,
		runResult.ExitCode,
	)
	if coverageSnapshotPath != "" {
		defer func() {
			_ = os.Remove(coverageSnapshotPath)
		}()
	}
	var coveragePolicy *report.CoveragePolicy
	if profileErr != nil {
		removed, removeErr := removeEmptyCoverage(layout)
		if removeErr != nil {
			outcome.errors.add(removeErr)
			outcome.state.InfrastructureErr = true
		}
		if runResult.ExitCode != 0 {
			outcome.warnings.add(profileErr)
		} else {
			outcome.errors.add(profileErr)
			outcome.state.ReportErr = true
		}
		if removed {
			outcome.warnings.add(fmt.Errorf(
				"removed empty coverage placeholder %q",
				layout.CoverageProfile,
			))
		}
	}
	if options.MinimumCoverage != "" {
		coveragePolicy = &report.CoveragePolicy{
			Minimum: options.MinimumCoverage,
		}
		if profile == nil {
			outcome.errors.add(fmt.Errorf(
				"minimum coverage %s%% requested, but coverage is unavailable",
				options.MinimumCoverage,
			))
			outcome.state.ReportErr = true
		} else {
			threshold, thresholdErr := coverage.ParseThreshold(
				options.MinimumCoverage,
			)
			if thresholdErr != nil {
				outcome.errors.add(fmt.Errorf(
					"evaluate minimum coverage: %w",
					thresholdErr,
				))
				outcome.state.InfrastructureErr = true
			} else {
				satisfied, policyErr := threshold.SatisfiedBy(profile.Total)
				if policyErr != nil {
					outcome.errors.add(fmt.Errorf(
						"evaluate minimum coverage %s: %w",
						threshold.Display(),
						policyErr,
					))
					outcome.state.ReportErr = true
				} else {
					coveragePolicy.Actual = formatPolicyPercentage(
						profile.Total,
						options.MinimumCoverage,
					)
					coveragePolicy.Covered = profile.Total.Covered
					coveragePolicy.Statements = profile.Total.Statements
					coveragePolicy.Available = true
					coveragePolicy.Satisfied = satisfied
					outcome.state.CoverageBelow = !satisfied
				}
			}
		}
	}
	outcome.state.EvidenceIncomplete =
		outcome.state.EvidenceIncomplete || !captureComplete

	mergeContextCancellation(ctx, &runResult)
	applyRunResultOutcome(&snapshot, &outcome.state, runResult)
	durableRunErrors := outcome.errors.clone()

	statusWritten := false
	if statusErr := persistRunStatus(
		layout,
		runResult,
		captureComplete,
		durableRunErrors,
		outcome.warnings,
		coveragePolicy,
	); statusErr != nil {
		outcome.errors.add(fmt.Errorf("persist run status: %w", statusErr))
		outcome.state.InfrastructureErr = true
		outcome.state.EvidenceIncomplete = true
	} else {
		statusWritten = true
	}

	if liveConsoleErrors.count() > 0 {
		appendErrors(&outcome.errors, &liveConsoleErrors)
		outcome.state.ReportErr = true
	}
	if !options.NoCoverage && profile != nil {
		if coverageReportErr := publishCoverageReport(
			ctx,
			options.GoBinary,
			layout,
			renderer,
			coverageSnapshotPath,
		); coverageReportErr != nil {
			outcome.errors.add(coverageReportErr)
			outcome.state.ReportErr = true
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
	evidenceContradictsChild := outcome.state.EvidenceFailed &&
		outcome.state.ChildExitKnown &&
		outcome.state.ChildExitCode == 0
	manifestEligible := statusWritten && captureComplete &&
		!outcome.state.StreamCorrupt &&
		!outcome.state.InfrastructureErr &&
		!outcome.state.EvidenceIncomplete &&
		!outcome.state.Interrupted &&
		!resultIncomplete(snapshot) &&
		analysisErrors.count() == 0 &&
		profileErr == nil &&
		!evidenceContradictsChild &&
		!outcome.state.ReportErr
	refreshReports := func() {
		assessment = buildReportAssessment(
			snapshot,
			outcome.state,
			outcome.errors,
			outcome.warnings,
			coveragePolicy,
		)
		input.Result = snapshot
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
	cancelledAfterPublish := mergeContextCancellation(ctx, &runResult)
	if cancelledAfterPublish {
		applyRunResultOutcome(&snapshot, &outcome.state, runResult)
		manifestEligible = false
		if statusErr := persistRunStatus(
			layout,
			runResult,
			captureComplete,
			durableRunErrors,
			outcome.warnings,
			coveragePolicy,
		); statusErr != nil {
			outcome.errors.add(fmt.Errorf(
				"update cancelled run status: %w",
				statusErr,
			))
			outcome.state.InfrastructureErr = true
			outcome.state.EvidenceIncomplete = true
		}
	}
	if cancelledAfterPublish ||
		(outcome.state.ReportErr && !assessment.ReportFailed) {
		refreshReports()
	}
	if err := console.Final(input); err != nil {
		outcome.errors.add(err)
		outcome.state.ReportErr = true
		manifestEligible = false
		refreshReports()
	}

	cancelledAfterConsole := mergeContextCancellation(ctx, &runResult)
	if cancelledAfterConsole {
		applyRunResultOutcome(&snapshot, &outcome.state, runResult)
		manifestEligible = false
		if statusErr := persistRunStatus(
			layout,
			runResult,
			captureComplete,
			durableRunErrors,
			outcome.warnings,
			coveragePolicy,
		); statusErr != nil {
			outcome.errors.add(fmt.Errorf(
				"update cancelled run status: %w",
				statusErr,
			))
			outcome.state.InfrastructureErr = true
			outcome.state.EvidenceIncomplete = true
		}
		refreshReports()
	}

	manifestEligible = manifestEligible &&
		statusWritten &&
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
	if mergeContextCancellation(ctx, &runResult) {
		applyRunResultOutcome(&snapshot, &outcome.state, runResult)
		if err := layout.Remove(artifact.ManifestJSON); err != nil {
			outcome.errors.add(fmt.Errorf(
				"remove manifest after cancellation: %w",
				err,
			))
			outcome.state.InfrastructureErr = true
		}
		if statusErr := persistRunStatus(
			layout,
			runResult,
			captureComplete,
			durableRunErrors,
			outcome.warnings,
			coveragePolicy,
		); statusErr != nil {
			outcome.errors.add(fmt.Errorf(
				"update cancelled run status: %w",
				statusErr,
			))
			outcome.state.InfrastructureErr = true
			outcome.state.EvidenceIncomplete = true
		}
		refreshReports()
	}
	if outcome.errors.count() > 0 && runErr == nil && streamErr == nil &&
		!outcome.state.ReportErr {
		outcome.state.InfrastructureErr = true
	}
	return outcome
}

func applyRunResultOutcome(
	snapshot *result.Result,
	state *ExitState,
	runResult runner.Result,
) {
	if snapshot != nil {
		snapshot.Metadata.Interrupted = runResult.Interrupted
		snapshot.Metadata.Cancellation = string(runResult.Cancellation)
		snapshot.Metadata.CancellationSignal =
			runResult.CancellationSignal
		snapshot.Metadata.RecommendedExitCode =
			runResult.RecommendedExitCode
	}
	if state != nil {
		state.Interrupted = runResult.Interrupted
		state.InterruptExitCode = runResult.RecommendedExitCode
	}
}

func removeEmptyCoverage(layout *artifact.Layout) (bool, error) {
	info, err := os.Lstat(layout.CoverageProfile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect coverage profile: %w", err)
	}
	if info.Size() != 0 {
		return false, nil
	}
	if err := layout.RemoveEvidence(artifact.CoverageProfile); err != nil {
		return false, fmt.Errorf("remove empty coverage profile: %w", err)
	}
	return true, nil
}

func formatPolicyPercentage(
	totals coverage.Totals,
	minimum string,
) string {
	precision := 24
	if dot := strings.IndexByte(minimum, '.'); dot >= 0 {
		if digits := len(minimum) - dot + 1; digits > precision {
			precision = digits
		}
	}
	formatted, err := totals.FormatPercentage(precision)
	if err != nil {
		return ""
	}
	formatted = strings.TrimRight(formatted, "0")
	formatted = strings.TrimRight(formatted, ".")
	if formatted == "" {
		return "0"
	}
	return formatted
}

func terminalPackage(
	analyzer *result.Analyzer,
	record protocol.Record,
) (result.Package, bool) {
	if record.Event == nil || record.Event.Kind != protocol.EventKindTest ||
		record.Event.Test == nil || record.Event.Test.Test != "" {
		return result.Package{}, false
	}
	switch record.Event.Test.Action {
	case protocol.ActionPass, protocol.ActionFail, protocol.ActionSkip:
	default:
		return result.Package{}, false
	}
	return analyzer.Package(record.Event.Test.Package)
}

func syncCloseFile(label string, file *os.File) error {
	if file == nil {
		return nil
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	switch {
	case syncErr != nil && closeErr != nil:
		return fmt.Errorf("%s: sync: %v; close: %w", label, syncErr, closeErr)
	case syncErr != nil:
		return fmt.Errorf("sync %s: %w", label, syncErr)
	case closeErr != nil:
		return fmt.Errorf("close %s: %w", label, closeErr)
	default:
		return nil
	}
}
