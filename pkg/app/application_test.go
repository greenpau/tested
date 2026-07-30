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
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/greenpau/tested/pkg/artifact"
	"github.com/greenpau/tested/pkg/runstatus"
)

func TestMain(m *testing.M) {
	if os.Getenv("TESTED_APP_HELPER_EXIT_ONE") == "1" {
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestExecuteHelpVersionAndUsage(t *testing.T) {
	t.Run("help", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Execute(
			context.Background(),
			[]string{"help"},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitSuccess {
			t.Fatalf("Execute(help) code = %d, stderr = %q", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "tested report") ||
			!strings.Contains(stdout.String(), "--minimum-coverage") ||
			!strings.Contains(stdout.String(), "--coverage-diff-base") {
			t.Fatalf("Execute(help) output is incomplete:\n%s", stdout.String())
		}
	})

	t.Run("version", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Execute(
			context.Background(),
			[]string{"version"},
			&stdout,
			&stderr,
			BuildInfo{Version: "1.2.3", GitCommit: "abc"},
		)
		if code != ExitSuccess {
			t.Fatalf("Execute(version) code = %d, stderr = %q", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "tested 1.2.3") ||
			!strings.Contains(stdout.String(), "commit: abc") {
			t.Fatalf("Execute(version) output = %q", stdout.String())
		}
	})

	t.Run("usage error", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Execute(
			context.Background(),
			[]string{"run", "--minimum-coverage", "NaN"},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitInfrastructure {
			t.Fatalf("Execute(invalid) code = %d, want %d", code, ExitInfrastructure)
		}
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "plain decimal percentage") {
			t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
		}
	})

	t.Run("coverage diff requires coverage", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Execute(
			context.Background(),
			[]string{
				"report",
				"--no-coverage",
				"--coverage-diff-base", "HEAD",
			},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitInfrastructure {
			t.Fatalf(
				"Execute(invalid coverage diff) code = %d, want %d",
				code,
				ExitInfrastructure,
			)
		}
		if stdout.Len() != 0 ||
			!strings.Contains(
				stderr.String(),
				"--coverage-diff-base cannot be combined with --no-coverage",
			) {
			t.Fatalf(
				"stdout = %q, stderr = %q",
				stdout.String(),
				stderr.String(),
			)
		}
	})
}

func TestExecuteReportHonorsPreCancelledContext(t *testing.T) {
	workDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var stdout, stderr bytes.Buffer
	code := Execute(
		ctx,
		[]string{
			"report",
			"-C", workDir,
			"--no-coverage",
			"--quiet",
		},
		&stdout,
		&stderr,
		BuildInfo{},
	)
	if code != ExitInterrupted {
		t.Fatalf(
			"Execute(cancelled report) code = %d, want %d; stderr=%q",
			code,
			ExitInterrupted,
			stderr.String(),
		)
	}
	if !strings.Contains(stderr.String(), "cancelled before artifact preparation") {
		t.Fatalf("cancellation diagnostic = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("cancelled report stdout = %q, want empty", stdout.String())
	}
	manifestPath := filepath.Join(
		workDir,
		artifact.DefaultOutputDir,
		artifact.ManifestJSONName,
	)
	if _, err := os.Lstat(manifestPath); !os.IsNotExist(err) {
		t.Fatalf("cancelled report manifest exists: %v", err)
	}
}

func TestExecuteOfflineReport(t *testing.T) {
	const passingEvents = "" +
		"{\"Time\":\"2026-07-29T12:00:00Z\",\"Action\":\"start\",\"Package\":\"example.test/unit\"}\n" +
		"{\"Time\":\"2026-07-29T12:00:00Z\",\"Action\":\"run\",\"Package\":\"example.test/unit\",\"Test\":\"TestPass\"}\n" +
		"{\"Time\":\"2026-07-29T12:00:00.01Z\",\"Action\":\"pass\",\"Package\":\"example.test/unit\",\"Test\":\"TestPass\",\"Elapsed\":0.01}\n" +
		"{\"Time\":\"2026-07-29T12:00:00.02Z\",\"Action\":\"pass\",\"Package\":\"example.test/unit\",\"Elapsed\":0.02}\n"
	const failingEvents = "" +
		"{\"Action\":\"start\",\"Package\":\"example.test/unit\"}\n" +
		"{\"Action\":\"run\",\"Package\":\"example.test/unit\",\"Test\":\"TestFail\"}\n" +
		"{\"Action\":\"output\",\"Package\":\"example.test/unit\",\"Test\":\"TestFail\",\"Output\":\"secret failure\\n\"}\n" +
		"{\"Action\":\"fail\",\"Package\":\"example.test/unit\",\"Test\":\"TestFail\",\"Elapsed\":0.01}\n" +
		"{\"Action\":\"fail\",\"Package\":\"example.test/unit\",\"Elapsed\":0.02}\n"
	const incompleteEvents = "" +
		"{\"Action\":\"start\",\"Package\":\"example.test/unit\"}\n" +
		"{\"Action\":\"run\",\"Package\":\"example.test/unit\",\"Test\":\"TestIncomplete\"}\n"
	largePackage := strings.Repeat("p", 1100)
	largeIdentityEvents := "" +
		"{\"Action\":\"start\",\"Package\":\"" + largePackage + "\"}\n" +
		"{\"Action\":\"pass\",\"Package\":\"" + largePackage + "\"}\n"
	largeOutputEvents := "" +
		"{\"Action\":\"start\",\"Package\":\"example.test/unit\"}\n" +
		"{\"Action\":\"output\",\"Package\":\"example.test/unit\",\"Output\":\"" +
		strings.Repeat("x", 2048) + "\"}\n" +
		"{\"Action\":\"pass\",\"Package\":\"example.test/unit\"}\n"

	tests := []struct {
		name       string
		events     string
		extraArgs  []string
		wantCode   int
		wantStatus string
		redacted   bool
		exitCode   int
		noStatus   bool
		manifest   bool
	}{
		{
			name:       "passing",
			events:     passingEvents,
			wantCode:   ExitSuccess,
			wantStatus: "passed",
			exitCode:   0,
			manifest:   true,
		},
		{
			name:       "failing",
			events:     failingEvents,
			wantCode:   ExitTestsFailed,
			wantStatus: "failed",
			exitCode:   1,
			manifest:   true,
		},
		{
			name:       "allowed failure",
			events:     failingEvents,
			extraArgs:  []string{"--allow-failures"},
			wantCode:   ExitSuccess,
			wantStatus: "failed",
			exitCode:   1,
			manifest:   true,
		},
		{
			name:       "redaction leaves raw evidence intact",
			events:     failingEvents,
			extraArgs:  []string{"--redact", "secret"},
			wantCode:   ExitTestsFailed,
			wantStatus: "failed",
			redacted:   true,
			exitCode:   1,
			manifest:   true,
		},
		{
			name:       "malformed",
			events:     passingEvents + "not JSON\n",
			wantCode:   ExitInfrastructure,
			wantStatus: "incomplete",
			exitCode:   0,
		},
		{
			name:       "allowed flag does not hide incomplete evidence",
			events:     incompleteEvents,
			extraArgs:  []string{"--allow-failures"},
			wantCode:   ExitInfrastructure,
			wantStatus: "incomplete",
			exitCode:   0,
		},
		{
			name:       "missing status never implies success",
			events:     passingEvents,
			wantCode:   ExitInfrastructure,
			wantStatus: "incomplete",
			noStatus:   true,
		},
		{
			name:       "normalized entry exhaustion is incomplete",
			events:     passingEvents,
			extraArgs:  []string{"--max-result-entries", "1"},
			wantCode:   ExitInfrastructure,
			wantStatus: "incomplete",
			exitCode:   0,
		},
		{
			name:       "normalized byte exhaustion is incomplete",
			events:     largeIdentityEvents,
			extraArgs:  []string{"--max-normalized-bytes", "1024"},
			wantCode:   ExitInfrastructure,
			wantStatus: "incomplete",
			exitCode:   0,
		},
		{
			name:       "aggregate output clipping preserves success",
			events:     largeOutputEvents,
			extraArgs:  []string{"--max-total-output-bytes", "1024"},
			wantCode:   ExitSuccess,
			wantStatus: "passed",
			exitCode:   0,
			manifest:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			workDir := t.TempDir()
			outputDir := filepath.Join(workDir, ".coverage")
			if err := os.Mkdir(outputDir, 0o700); err != nil {
				t.Fatal(err)
			}
			eventPath := filepath.Join(outputDir, "test_output.jsonl")
			if err := os.WriteFile(eventPath, []byte(tc.events), 0o600); err != nil {
				t.Fatal(err)
			}
			if !tc.noStatus {
				writeRunStatusFixture(t, outputDir, eventPath, tc.exitCode)
			}

			args := []string{
				"report",
				"-C", workDir,
				"--no-coverage",
				"--format", "json",
			}
			args = append(args, tc.extraArgs...)
			var stdout, stderr bytes.Buffer
			code := Execute(
				context.Background(),
				args,
				&stdout,
				&stderr,
				BuildInfo{},
			)
			if code != tc.wantCode {
				t.Fatalf(
					"Execute(report) code = %d, want %d; stderr = %q",
					code,
					tc.wantCode,
					stderr.String(),
				)
			}
			var summary struct {
				Schema  string `json:"schema"`
				Outcome string `json:"outcome"`
			}
			decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
			if err := decoder.Decode(&summary); err != nil {
				t.Fatalf("decode console JSON %q: %v", stdout.String(), err)
			}
			var extra any
			if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
				t.Fatalf("console JSON contains trailing content: %v", err)
			}
			if summary.Schema != reportSummarySchema ||
				summary.Outcome != tc.wantStatus {
				t.Fatalf("summary = %#v", summary)
			}
			for _, name := range []string{
				"test_output.jsonl",
				"test_output.html",
				"summary.json",
				"junit.xml",
				"index.html",
			} {
				if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
					t.Errorf("artifact %s: %v", name, err)
				}
			}
			_, manifestErr := os.Stat(filepath.Join(outputDir, "manifest.json"))
			if tc.manifest && manifestErr != nil {
				t.Errorf("artifact manifest.json: %v", manifestErr)
			}
			if !tc.manifest && !os.IsNotExist(manifestErr) {
				t.Errorf("manifest.json exists for incoherent evidence: %v", manifestErr)
			}
			if tc.redacted {
				raw, err := os.ReadFile(eventPath)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(raw, []byte("secret failure")) {
					t.Fatal("redaction modified primary event evidence")
				}
				for _, name := range []string{
					"summary.json",
					"junit.xml",
					"test_output.html",
				} {
					derived, err := os.ReadFile(filepath.Join(outputDir, name))
					if err != nil {
						t.Fatal(err)
					}
					if bytes.Contains(derived, []byte("secret failure")) {
						t.Errorf("redaction did not protect %s", name)
					}
				}
			}
		})
	}
}

func TestExecuteOfflineStatusBindingAndCustomImports(t *testing.T) {
	const passingEvents = "" +
		"{\"Action\":\"start\",\"Package\":\"example.test/unit\"}\n" +
		"{\"Action\":\"run\",\"Package\":\"example.test/unit\",\"Test\":\"TestPass\"}\n" +
		"{\"Action\":\"pass\",\"Package\":\"example.test/unit\",\"Test\":\"TestPass\"}\n" +
		"{\"Action\":\"pass\",\"Package\":\"example.test/unit\"}\n"
	const failingEvents = "" +
		"{\"Action\":\"start\",\"Package\":\"example.test/unit\"}\n" +
		"{\"Action\":\"run\",\"Package\":\"example.test/unit\",\"Test\":\"TestFail\"}\n" +
		"{\"Action\":\"fail\",\"Package\":\"example.test/unit\",\"Test\":\"TestFail\"}\n" +
		"{\"Action\":\"fail\",\"Package\":\"example.test/unit\"}\n"

	t.Run("tampered event stream invalidates status", func(t *testing.T) {
		workDir := t.TempDir()
		outputDir := filepath.Join(workDir, ".coverage")
		if err := os.Mkdir(outputDir, 0o700); err != nil {
			t.Fatal(err)
		}
		eventPath := filepath.Join(outputDir, artifact.TestOutputJSONLName)
		if err := os.WriteFile(eventPath, []byte(passingEvents), 0o600); err != nil {
			t.Fatal(err)
		}
		writeRunStatusFixture(t, outputDir, eventPath, 0)
		file, err := os.OpenFile(eventPath, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString("\n"); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}

		var stdout, stderr bytes.Buffer
		code := Execute(
			context.Background(),
			[]string{
				"report",
				"-C", workDir,
				"--no-coverage",
				"--format", "json",
			},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitInfrastructure {
			t.Fatalf(
				"Execute(tampered) code = %d, want %d; stderr=%q",
				code,
				ExitInfrastructure,
				stderr.String(),
			)
		}
		if !strings.Contains(stderr.String(), "SHA-256") {
			t.Fatalf("tamper diagnostic lacks binding details: %q", stderr.String())
		}
		if _, err := os.Stat(filepath.Join(outputDir, artifact.ManifestJSONName)); !os.IsNotExist(err) {
			t.Fatalf("manifest exists for tampered evidence: %v", err)
		}
	})

	t.Run("custom events ignore unrelated default status", func(t *testing.T) {
		workDir := t.TempDir()
		outputDir := filepath.Join(workDir, ".coverage")
		if err := os.Mkdir(outputDir, 0o700); err != nil {
			t.Fatal(err)
		}
		defaultEvents := filepath.Join(outputDir, artifact.TestOutputJSONLName)
		if err := os.WriteFile(defaultEvents, []byte(passingEvents), 0o600); err != nil {
			t.Fatal(err)
		}
		writeRunStatusFixture(t, outputDir, defaultEvents, 0)

		archiveDir := t.TempDir()
		archiveEvents := filepath.Join(archiveDir, artifact.TestOutputJSONLName)
		if err := os.WriteFile(archiveEvents, []byte(failingEvents), 0o600); err != nil {
			t.Fatal(err)
		}

		var stdout, stderr bytes.Buffer
		code := Execute(
			context.Background(),
			[]string{
				"report",
				"-C", workDir,
				"--events", archiveEvents,
				"--no-coverage",
				"--format", "json",
			},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitInfrastructure {
			t.Fatalf(
				"Execute(custom without status) code = %d, want %d; stderr=%q",
				code,
				ExitInfrastructure,
				stderr.String(),
			)
		}
		if _, err := os.Stat(filepath.Join(outputDir, artifact.RunJSONName)); !os.IsNotExist(err) {
			t.Fatalf("unrelated default run.json was retained: %v", err)
		}
	})

	t.Run("matching custom bundle is trusted and cleans stale evidence", func(t *testing.T) {
		workDir := t.TempDir()
		outputDir := filepath.Join(workDir, ".coverage")
		if err := os.Mkdir(outputDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(outputDir, artifact.StderrLogName),
			[]byte("stale stderr"),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(outputDir, artifact.CoverageProfileName),
			[]byte("stale profile"),
			0o600,
		); err != nil {
			t.Fatal(err)
		}

		archiveDir := t.TempDir()
		archiveEvents := filepath.Join(archiveDir, artifact.TestOutputJSONLName)
		if err := os.WriteFile(archiveEvents, []byte(failingEvents), 0o600); err != nil {
			t.Fatal(err)
		}
		archiveStderr := filepath.Join(archiveDir, artifact.StderrLogName)
		if err := os.WriteFile(
			archiveStderr,
			[]byte("archived diagnostic\n"),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
		writeRunStatusFixture(t, archiveDir, archiveEvents, 1)

		var stdout, stderr bytes.Buffer
		code := Execute(
			context.Background(),
			[]string{
				"report",
				"-C", workDir,
				"--events", archiveEvents,
				"--run-metadata", filepath.Join(
					archiveDir,
					artifact.RunJSONName,
				),
				"--no-coverage",
				"--format", "json",
			},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitTestsFailed {
			t.Fatalf(
				"Execute(custom bundle) code = %d, want %d; stderr=%q",
				code,
				ExitTestsFailed,
				stderr.String(),
			)
		}
		importedStderr, err := os.ReadFile(filepath.Join(
			outputDir,
			artifact.StderrLogName,
		))
		if err != nil || string(importedStderr) != "archived diagnostic\n" {
			t.Errorf(
				"imported stderr = %q, %v; want archived diagnostic",
				importedStderr,
				err,
			)
		}
		if _, err := os.Stat(filepath.Join(
			outputDir,
			artifact.CoverageProfileName,
		)); !os.IsNotExist(err) {
			t.Errorf("stale coverage profile retained: %v", err)
		}
		if _, err := os.Stat(filepath.Join(outputDir, artifact.ManifestJSONName)); err != nil {
			t.Fatalf("manifest missing for coherent imported bundle: %v", err)
		}
	})

	t.Run("no coverage projects a policy-free bound default bundle", func(t *testing.T) {
		workDir := t.TempDir()
		outputDir := filepath.Join(workDir, artifact.DefaultOutputDir)
		if err := os.Mkdir(outputDir, 0o700); err != nil {
			t.Fatal(err)
		}
		eventPath := filepath.Join(outputDir, artifact.TestOutputJSONLName)
		if err := os.WriteFile(eventPath, []byte(passingEvents), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(outputDir, artifact.CoverageProfileName),
			[]byte("mode: set\nexample.go:1.1,1.2 1 1\n"),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(outputDir, artifact.CoverageHTMLName),
			[]byte("stale coverage report"),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
		writeRunStatusFixture(t, outputDir, eventPath, 0)
		statusPath := filepath.Join(outputDir, artifact.RunJSONName)
		originalStatus, err := os.ReadFile(statusPath)
		if err != nil {
			t.Fatal(err)
		}

		var stdout, stderr bytes.Buffer
		code := Execute(
			context.Background(),
			[]string{
				"report",
				"-C", workDir,
				"--no-coverage",
				"--quiet",
			},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitSuccess {
			t.Fatalf(
				"Execute(no-coverage projection) code = %d; stderr=%q",
				code,
				stderr.String(),
			)
		}
		for _, name := range []string{
			artifact.CoverageProfileName,
			artifact.CoverageHTMLName,
		} {
			if _, err := os.Lstat(filepath.Join(outputDir, name)); !errors.Is(
				err,
				os.ErrNotExist,
			) {
				t.Errorf("coverage artifact %s still exists: %v", name, err)
			}
		}
		projectedStatus, err := loadRunStatus(statusPath)
		if err != nil {
			t.Fatal(err)
		}
		if projectedStatus.CoveragePolicy != nil ||
			runStatusBinds(
				projectedStatus,
				artifact.CoverageProfile,
			) ||
			!runStatusBinds(
				projectedStatus,
				artifact.TestOutputJSONL,
			) {
			t.Fatalf("projected run status = %#v", projectedStatus)
		}
		projectedStatusBytes, err := os.ReadFile(statusPath)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(projectedStatusBytes, originalStatus) {
			t.Fatal("coverage-bound run status was not rewritten")
		}
		manifestPath := filepath.Join(outputDir, artifact.ManifestJSONName)
		firstManifest, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		var manifest artifact.Manifest
		if err := json.Unmarshal(firstManifest, &manifest); err != nil {
			t.Fatal(err)
		}
		for _, entry := range manifest.Files {
			if entry.Name == artifact.CoverageProfileName ||
				entry.Name == artifact.CoverageHTMLName {
				t.Errorf("manifest retained coverage entry %#v", entry)
			}
		}
		index, err := os.ReadFile(filepath.Join(
			outputDir,
			artifact.IndexHTMLName,
		))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(index, []byte(artifact.CoverageProfileName)) ||
			bytes.Contains(index, []byte(artifact.CoverageHTMLName)) {
			t.Fatalf("index links omitted coverage:\n%s", index)
		}

		stdout.Reset()
		stderr.Reset()
		code = Execute(
			context.Background(),
			[]string{
				"report",
				"-C", workDir,
				"--no-coverage",
				"--quiet",
			},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitSuccess {
			t.Fatalf(
				"Execute(repeated no-coverage projection) code = %d; stderr=%q",
				code,
				stderr.String(),
			)
		}
		secondStatus, err := os.ReadFile(statusPath)
		if err != nil {
			t.Fatal(err)
		}
		secondManifest, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(secondStatus, projectedStatusBytes) ||
			!bytes.Equal(secondManifest, firstManifest) {
			t.Fatal("repeated no-coverage projection is not deterministic")
		}

		stdout.Reset()
		stderr.Reset()
		code = Execute(
			context.Background(),
			[]string{
				"report",
				"-C", workDir,
				"--coverage-diff-base", "HEAD",
				"--quiet",
			},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitInfrastructure {
			t.Fatalf(
				"Execute(comparison without coverage) code = %d, want %d; stderr=%q",
				code,
				ExitInfrastructure,
				stderr.String(),
			)
		}
		if !strings.Contains(
			stderr.String(),
			"coverage source comparison requested, but coverage evidence is unavailable",
		) {
			t.Fatalf(
				"comparison without coverage diagnostic = %q",
				stderr.String(),
			)
		}
		if _, err := os.Lstat(manifestPath); !errors.Is(
			err,
			os.ErrNotExist,
		) {
			t.Fatalf(
				"comparison without coverage retained manifest: %v",
				err,
			)
		}
	})

	t.Run("hard-linked external events are copied without changing aliases", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("POSIX link counts and permission assertions are not portable to Windows")
		}
		workDir := t.TempDir()
		archiveDir := t.TempDir()
		archiveEvents := filepath.Join(
			archiveDir,
			artifact.TestOutputJSONLName,
		)
		if err := os.WriteFile(
			archiveEvents,
			[]byte(passingEvents),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(archiveEvents, 0o644); err != nil {
			t.Fatal(err)
		}
		writeRunStatusFixture(t, archiveDir, archiveEvents, 0)
		aliasDir := t.TempDir()
		aliasEvents := filepath.Join(aliasDir, "events.jsonl")
		if err := os.Link(archiveEvents, aliasEvents); err != nil {
			t.Skipf("hard links unavailable: %v", err)
		}

		var stdout, stderr bytes.Buffer
		code := Execute(
			context.Background(),
			[]string{
				"report",
				"-C", workDir,
				"--events", aliasEvents,
				"--run-metadata", filepath.Join(
					archiveDir,
					artifact.RunJSONName,
				),
				"--no-coverage",
				"--quiet",
			},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitSuccess {
			t.Fatalf(
				"Execute(hard-linked import) code = %d; stderr=%q",
				code,
				stderr.String(),
			)
		}
		data, err := os.ReadFile(archiveEvents)
		if err != nil || string(data) != passingEvents {
			t.Fatalf("external event source changed: data=%q err=%v", data, err)
		}
		sourceInfo, err := os.Stat(archiveEvents)
		if err != nil {
			t.Fatal(err)
		}
		aliasInfo, err := os.Stat(aliasEvents)
		if err != nil {
			t.Fatal(err)
		}
		managedInfo, err := os.Stat(filepath.Join(
			workDir,
			artifact.DefaultOutputDir,
			artifact.TestOutputJSONLName,
		))
		if err != nil {
			t.Fatal(err)
		}
		if got := sourceInfo.Mode().Perm(); got != 0o644 {
			t.Fatalf("external event mode = %s, want 0644", got)
		}
		if !os.SameFile(sourceInfo, aliasInfo) {
			t.Fatal("external hard-link aliases no longer identify the same file")
		}
		if os.SameFile(sourceInfo, managedInfo) {
			t.Fatal("managed event evidence was not copied to an independent inode")
		}
		if runtime.GOOS != "windows" &&
			managedInfo.Mode().Perm() != 0o600 {
			t.Fatalf("managed event mode = %s, want 0600", managedInfo.Mode())
		}
	})

	t.Run("explicit in-place status bytes are preserved", func(t *testing.T) {
		workDir := t.TempDir()
		outputDir := filepath.Join(workDir, ".coverage")
		if err := os.Mkdir(outputDir, 0o700); err != nil {
			t.Fatal(err)
		}
		eventPath := filepath.Join(outputDir, artifact.TestOutputJSONLName)
		if err := os.WriteFile(eventPath, []byte(passingEvents), 0o600); err != nil {
			t.Fatal(err)
		}
		writeRunStatusFixture(t, outputDir, eventPath, 0)
		statusPath := filepath.Join(outputDir, artifact.RunJSONName)
		status, err := loadRunStatus(statusPath)
		if err != nil {
			t.Fatal(err)
		}
		var noncanonical bytes.Buffer
		if err := json.NewEncoder(&noncanonical).Encode(status); err != nil {
			t.Fatal(err)
		}
		original := append([]byte("\n \t"), noncanonical.Bytes()...)
		if err := os.WriteFile(statusPath, original, 0o600); err != nil {
			t.Fatal(err)
		}

		var stdout, stderr bytes.Buffer
		code := Execute(
			context.Background(),
			[]string{
				"report",
				"-C", workDir,
				"--run-metadata", statusPath,
				"--no-coverage",
				"--quiet",
			},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitSuccess {
			t.Fatalf(
				"Execute(in-place status) code = %d, stderr=%q",
				code,
				stderr.String(),
			)
		}
		after, err := os.ReadFile(statusPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, original) {
			t.Fatalf("in-place status bytes were rewritten")
		}
	})

	t.Run("malformed explicit in-place status is not deleted", func(t *testing.T) {
		workDir := t.TempDir()
		outputDir := filepath.Join(workDir, ".coverage")
		if err := os.Mkdir(outputDir, 0o700); err != nil {
			t.Fatal(err)
		}
		eventPath := filepath.Join(outputDir, artifact.TestOutputJSONLName)
		if err := os.WriteFile(eventPath, []byte(passingEvents), 0o600); err != nil {
			t.Fatal(err)
		}
		statusPath := filepath.Join(outputDir, artifact.RunJSONName)
		original := []byte("{malformed status\n")
		if err := os.WriteFile(statusPath, original, 0o600); err != nil {
			t.Fatal(err)
		}

		var stdout, stderr bytes.Buffer
		code := Execute(
			context.Background(),
			[]string{
				"report",
				"-C", workDir,
				"--run-metadata", statusPath,
				"--no-coverage",
				"--quiet",
			},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitInfrastructure {
			t.Fatalf(
				"Execute(malformed in-place status) code = %d, stderr=%q",
				code,
				stderr.String(),
			)
		}
		after, err := os.ReadFile(statusPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, original) {
			t.Fatalf("malformed in-place status bytes changed")
		}
	})
}

func TestExecuteRunCoveragePolicyAndFailure(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, workDir, "go.mod", "module example.test/application\n\ngo 1.25\n")
	writeTestFile(t, workDir, "sample.go", `package application

func Covered() int { return 1 }
func Uncovered() int { return 2 }
`)
	writeTestFile(t, workDir, "sample_test.go", `package application

import (
	"os"
	"testing"
)

func TestCovered(t *testing.T) {
	if Covered() != 1 {
		t.Fatal("unexpected value")
	}
	if os.Getenv("TESTED_APPLICATION_FAIL") != "" {
		t.Fatal("controlled failure")
	}
}
`)

	t.Run("process status truthfulness", func(t *testing.T) {
		testExecutable, err := os.Executable()
		if err != nil {
			t.Fatalf("resolve app test executable: %v", err)
		}
		workDir := t.TempDir()

		t.Run("empty event stream with child exit one", func(t *testing.T) {
			t.Setenv("TESTED_APP_HELPER_EXIT_ONE", "1")
			var stdout, stderr bytes.Buffer
			code := Execute(
				context.Background(),
				[]string{
					"run",
					"-C", workDir,
					"--go", testExecutable,
					"--no-coverage",
					"--quiet",
					"--format", "json",
				},
				&stdout,
				&stderr,
				BuildInfo{},
			)
			if code != ExitTestsFailed {
				t.Fatalf(
					"Execute(exit-one helper) code = %d, want %d; stderr=%q",
					code,
					ExitTestsFailed,
					stderr.String(),
				)
			}
			var summary struct {
				Outcome string `json:"outcome"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
				t.Fatalf("decode console summary: %v\n%s", err, stdout.String())
			}
			if summary.Outcome != "failed" {
				t.Fatalf("summary outcome = %q, want failed", summary.Outcome)
			}
			junit, err := os.ReadFile(filepath.Join(
				workDir,
				".coverage",
				"junit.xml",
			))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(junit, []byte("<failure")) {
				t.Fatalf("JUnit lacks process failure:\n%s", junit)
			}

			stdout.Reset()
			stderr.Reset()
			code = Execute(
				context.Background(),
				[]string{
					"report",
					"-C", workDir,
					"--no-coverage",
					"--format", "json",
				},
				&stdout,
				&stderr,
				BuildInfo{},
			)
			if code != ExitTestsFailed {
				t.Fatalf(
					"Execute(offline exit-one helper) code = %d, want %d; stderr=%q",
					code,
					ExitTestsFailed,
					stderr.String(),
				)
			}
			if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
				t.Fatal(err)
			}
			if summary.Outcome != "failed" {
				t.Fatalf("offline summary outcome = %q, want failed", summary.Outcome)
			}
		})

		t.Run("start failure has no fabricated child", func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Execute(
				context.Background(),
				[]string{
					"run",
					"-C", workDir,
					"--go", filepath.Join(workDir, "missing-go"),
					"--no-coverage",
					"--quiet",
				},
				&stdout,
				&stderr,
				BuildInfo{},
			)
			if code != ExitInfrastructure {
				t.Fatalf(
					"Execute(start failure) code = %d, want %d; stderr=%q",
					code,
					ExitInfrastructure,
					stderr.String(),
				)
			}
			statusPath := filepath.Join(workDir, ".coverage", "run.json")
			statusFile, err := os.Open(statusPath)
			if err != nil {
				t.Fatal(err)
			}
			status, decodeErr := runstatus.Decode(statusFile)
			closeErr := statusFile.Close()
			if decodeErr != nil || closeErr != nil {
				t.Fatalf(
					"decode run.json: %v",
					errors.Join(decodeErr, closeErr),
				)
			}
			if status.ChildStarted || status.ExitCode != nil {
				t.Fatalf(
					"start failure status = started %t exit %#v",
					status.ChildStarted,
					status.ExitCode,
				)
			}
			junit, err := os.ReadFile(filepath.Join(
				workDir,
				".coverage",
				"junit.xml",
			))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(junit, []byte("<error")) {
				t.Fatalf("JUnit lacks infrastructure error:\n%s", junit)
			}
		})
	})

	t.Run("coverage policy", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Execute(
			context.Background(),
			[]string{
				"run",
				"-C", workDir,
				"--quiet",
				"--minimum-coverage", "75",
				"--", "-count=1", "./...",
			},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitCoverage {
			t.Fatalf(
				"Execute(run policy) code = %d, want %d; stdout = %q stderr = %q",
				code,
				ExitCoverage,
				stdout.String(),
				stderr.String(),
			)
		}
		for _, name := range []string{
			"coverage.out",
			"coverage.html",
			"test_output.jsonl",
			"test_output.html",
			"summary.json",
			"junit.xml",
			"stderr.log",
			"run.json",
			"index.html",
			"manifest.json",
		} {
			if _, err := os.Stat(filepath.Join(workDir, ".coverage", name)); err != nil {
				t.Errorf("artifact %s: %v", name, err)
			}
		}
		for _, name := range []string{
			"coverage.html",
			"test_output.html",
			"index.html",
		} {
			htmlData, err := os.ReadFile(filepath.Join(
				workDir,
				".coverage",
				name,
			))
			if err != nil {
				t.Fatal(err)
			}
			for _, expected := range [][]byte{
				[]byte("Content-Security-Policy"),
				[]byte("--primary: oklch(0.5 0.134 242.749)"),
				[]byte("@media (prefers-color-scheme: dark)"),
			} {
				if !bytes.Contains(htmlData, expected) {
					t.Errorf("%s lacks shared report theme marker %q", name, expected)
				}
			}
			if name == "coverage.html" &&
				!bytes.Contains(
					htmlData,
					[]byte(`id="tested-coverage-theme-v1"`),
				) {
				t.Error("coverage.html lacks the tested coverage theme")
			}
			if name == "coverage.html" {
				for _, expected := range [][]byte{
					[]byte(`id="tested-coverage-explorer-v1"`),
					[]byte(`"tested-coverage-package"`),
					[]byte(`"tested-coverage-tab-changes"`),
					[]byte(`"tested-coverage-layout-split"`),
					[]byte(`"Uncovered regions"`),
					[]byte(`<select id="files">`),
					[]byte(`<pre class="file"`),
					[]byte(`class="cov`),
					[]byte(`files.addEventListener('change'`),
				} {
					if !bytes.Contains(htmlData, expected) {
						t.Errorf(
							"coverage.html lost Go-authored behavior %q",
							expected,
						)
					}
				}
				if bytes.Contains(
					htmlData,
					[]byte(`id="tested-coverage-data-v1"`),
				) {
					t.Error(
						"coverage.html embedded comparison data without a baseline",
					)
				}
			}
		}
		summaryData, err := os.ReadFile(filepath.Join(
			workDir,
			".coverage",
			"summary.json",
		))
		if err != nil {
			t.Fatal(err)
		}
		var summary struct {
			Outcome    string `json:"outcome"`
			Assessment struct {
				CoveragePolicy struct {
					Minimum   string `json:"minimum"`
					Satisfied bool   `json:"satisfied"`
				} `json:"coverage_policy"`
			} `json:"assessment"`
		}
		if err := json.Unmarshal(summaryData, &summary); err != nil {
			t.Fatal(err)
		}
		if summary.Outcome != "coverage_failed" ||
			summary.Assessment.CoveragePolicy.Minimum != "75" ||
			summary.Assessment.CoveragePolicy.Satisfied {
			t.Fatalf("coverage summary = %#v", summary)
		}
		junit, err := os.ReadFile(filepath.Join(
			workDir,
			".coverage",
			"junit.xml",
		))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(junit, []byte("tested coverage policy")) ||
			!bytes.Contains(junit, []byte("<failure")) {
			t.Fatalf("JUnit lacks coverage-policy failure:\n%s", junit)
		}

		stdout.Reset()
		stderr.Reset()
		code = Execute(
			context.Background(),
			[]string{
				"report",
				"-C", workDir,
				"--quiet",
			},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitCoverage {
			t.Fatalf(
				"Execute(offline policy) code = %d, want %d; stderr=%q",
				code,
				ExitCoverage,
				stderr.String(),
			)
		}
		offlineSummary, err := os.ReadFile(filepath.Join(
			workDir,
			".coverage",
			"summary.json",
		))
		if err != nil {
			t.Fatal(err)
		}
		offlineJUnit, err := os.ReadFile(filepath.Join(
			workDir,
			".coverage",
			"junit.xml",
		))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(offlineSummary, summaryData) {
			t.Fatalf("offline summary changed bound live timing or outcome")
		}
		if !bytes.Equal(offlineJUnit, junit) {
			t.Fatalf("offline JUnit changed bound live timing or outcome")
		}

		type managedSnapshot struct {
			data []byte
			info os.FileInfo
		}
		outputDir := filepath.Join(workDir, artifact.DefaultOutputDir)
		before := make(map[string]managedSnapshot)
		entries, err := os.ReadDir(outputDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if !entry.Type().IsRegular() {
				continue
			}
			path := filepath.Join(outputDir, entry.Name())
			data, readErr := os.ReadFile(path)
			info, statErr := os.Stat(path)
			if readErr != nil || statErr != nil {
				t.Fatalf(
					"snapshot %s: %v",
					entry.Name(),
					errors.Join(readErr, statErr),
				)
			}
			before[entry.Name()] = managedSnapshot{
				data: data,
				info: info,
			}
		}

		stdout.Reset()
		stderr.Reset()
		code = Execute(
			context.Background(),
			[]string{
				"report",
				"-C", workDir,
				"--no-coverage",
				"--quiet",
			},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitInfrastructure {
			t.Fatalf(
				"Execute(offline no-coverage policy) code = %d, want %d; stderr=%q",
				code,
				ExitInfrastructure,
				stderr.String(),
			)
		}
		if !strings.Contains(stderr.String(), "recorded coverage policy") {
			t.Fatalf(
				"offline no-coverage policy diagnostic = %q",
				stderr.String(),
			)
		}
		afterEntries, err := os.ReadDir(outputDir)
		if err != nil {
			t.Fatal(err)
		}
		afterCount := 0
		for _, entry := range afterEntries {
			if !entry.Type().IsRegular() {
				continue
			}
			afterCount++
			want, ok := before[entry.Name()]
			if !ok {
				t.Errorf("report --no-coverage created %s", entry.Name())
				continue
			}
			path := filepath.Join(outputDir, entry.Name())
			data, readErr := os.ReadFile(path)
			info, statErr := os.Stat(path)
			if readErr != nil || statErr != nil {
				t.Errorf(
					"inspect unchanged %s: %v",
					entry.Name(),
					errors.Join(readErr, statErr),
				)
				continue
			}
			if !bytes.Equal(data, want.data) ||
				info.Mode() != want.info.Mode() ||
				!os.SameFile(info, want.info) ||
				!info.ModTime().Equal(want.info.ModTime()) {
				t.Errorf(
					"report --no-coverage mutated %s",
					entry.Name(),
				)
			}
		}
		if afterCount != len(before) {
			t.Fatalf(
				"managed file count after rejected --no-coverage = %d, want %d",
				afterCount,
				len(before),
			)
		}
		preservedStatus, err := loadRunStatus(filepath.Join(
			outputDir,
			artifact.RunJSONName,
		))
		if err != nil {
			t.Fatal(err)
		}
		if preservedStatus.CoveragePolicy == nil ||
			!runStatusBinds(
				preservedStatus,
				artifact.CoverageProfile,
			) {
			t.Fatalf(
				"rejected --no-coverage changed authoritative status: %#v",
				preservedStatus,
			)
		}
	})

	t.Run("live semantic budget can be recovered from raw evidence", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Execute(
			context.Background(),
			[]string{
				"run",
				"-C", workDir,
				"--no-coverage",
				"--quiet",
				"--format", "json",
				"--max-result-entries", "1",
				"--", "-count=1", "./...",
			},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitInfrastructure {
			t.Fatalf(
				"Execute(resource-limited run) code = %d, want %d; stderr=%q",
				code,
				ExitInfrastructure,
				stderr.String(),
			)
		}
		var summary struct {
			Outcome string `json:"outcome"`
			Counts  struct {
				Incomplete bool `json:"incomplete"`
			} `json:"counts"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
			t.Fatalf("decode resource-limited summary: %v", err)
		}
		if summary.Outcome != "incomplete" || !summary.Counts.Incomplete {
			t.Fatalf("resource-limited summary = %#v", summary)
		}
		outputDir := filepath.Join(workDir, artifact.DefaultOutputDir)
		if _, err := os.Lstat(filepath.Join(
			outputDir,
			artifact.ManifestJSONName,
		)); !os.IsNotExist(err) {
			t.Fatalf("manifest exists for incomplete normalized result: %v", err)
		}

		stdout.Reset()
		stderr.Reset()
		code = Execute(
			context.Background(),
			[]string{
				"report",
				"-C", workDir,
				"--no-coverage",
				"--quiet",
				"--format", "json",
			},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitSuccess {
			t.Fatalf(
				"Execute(recovery report) code = %d, want %d; stderr=%q",
				code,
				ExitSuccess,
				stderr.String(),
			)
		}
		summary = struct {
			Outcome string `json:"outcome"`
			Counts  struct {
				Incomplete bool `json:"incomplete"`
			} `json:"counts"`
		}{}
		if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
			t.Fatalf("decode recovered summary: %v", err)
		}
		if summary.Outcome != "passed" || summary.Counts.Incomplete {
			t.Fatalf("recovered summary = %#v", summary)
		}
		if _, err := os.Stat(filepath.Join(
			outputDir,
			artifact.ManifestJSONName,
		)); err != nil {
			t.Fatalf("recovery report did not publish manifest: %v", err)
		}
	})

	t.Run("console failure is not durable run evidence", func(t *testing.T) {
		consoleErr := errors.New("controlled console write failure")
		code := Execute(
			context.Background(),
			[]string{
				"run",
				"-C", workDir,
				"--no-coverage",
				"--quiet",
				"--format", "json",
				"--", "-count=1", "./...",
			},
			errorWriter{err: consoleErr},
			io.Discard,
			BuildInfo{},
		)
		if code != ExitInfrastructure {
			t.Fatalf(
				"Execute(run with failed console) code = %d, want %d",
				code,
				ExitInfrastructure,
			)
		}
		outputDir := filepath.Join(workDir, artifact.DefaultOutputDir)
		if _, err := os.Lstat(filepath.Join(
			outputDir,
			artifact.ManifestJSONName,
		)); !os.IsNotExist(err) {
			t.Fatalf("manifest exists after console failure: %v", err)
		}
		status, err := loadRunStatus(filepath.Join(
			outputDir,
			artifact.RunJSONName,
		))
		if err != nil {
			t.Fatal(err)
		}
		for _, issue := range status.Issues {
			if issue.Fatal {
				t.Fatalf(
					"presentation failure contaminated run.json: %#v",
					issue,
				)
			}
		}

		var stdout, stderr bytes.Buffer
		code = Execute(
			context.Background(),
			[]string{
				"report",
				"-C", workDir,
				"--no-coverage",
				"--quiet",
				"--format", "json",
			},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitSuccess {
			t.Fatalf(
				"Execute(report after console failure) code = %d, "+
					"want %d; stderr=%q",
				code,
				ExitSuccess,
				stderr.String(),
			)
		}
		if _, err := os.Stat(filepath.Join(
			outputDir,
			artifact.ManifestJSONName,
		)); err != nil {
			t.Fatalf("offline report did not recover manifest: %v", err)
		}
	})

	t.Run("child failure wins", func(t *testing.T) {
		t.Setenv("TESTED_APPLICATION_FAIL", "1")
		var stdout, stderr bytes.Buffer
		code := Execute(
			context.Background(),
			[]string{
				"run",
				"-C", workDir,
				"--no-coverage",
				"--quiet",
				"--", "-count=1", "./...",
			},
			&stdout,
			&stderr,
			BuildInfo{},
		)
		if code != ExitTestsFailed {
			t.Fatalf(
				"Execute(run failure) code = %d, want %d; stdout = %q stderr = %q",
				code,
				ExitTestsFailed,
				stdout.String(),
				stderr.String(),
			)
		}
		if _, err := os.Stat(filepath.Join(workDir, ".coverage", "coverage.out")); !os.IsNotExist(err) {
			t.Fatalf("coverage.out exists after --no-coverage: %v", err)
		}
	})
}

const reportSummarySchema = "tested/summary/v1"

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeRunStatusFixture(
	t *testing.T,
	outputDir string,
	eventPath string,
	exitCode int,
) {
	t.Helper()
	digest, size, err := artifact.SHA256File(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(outputDir, artifact.RunJSONName)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	bindings := []runstatus.File{{
		Name:   artifact.TestOutputJSONLName,
		Size:   size,
		SHA256: digest,
	}}
	for _, name := range []string{
		artifact.StderrLogName,
		artifact.CoverageProfileName,
	} {
		companionPath := filepath.Join(outputDir, name)
		companionDigest, companionSize, digestErr :=
			artifact.SHA256File(companionPath)
		if errors.Is(digestErr, os.ErrNotExist) {
			continue
		}
		if digestErr != nil {
			t.Fatal(digestErr)
		}
		bindings = append(bindings, runstatus.File{
			Name:   name,
			Size:   companionSize,
			SHA256: companionDigest,
		})
	}
	encodeErr := runstatus.Encode(file, runstatus.Evidence{
		Schema:          runstatus.Schema,
		ChildStarted:    true,
		ExitCode:        &exitCode,
		CaptureComplete: true,
		Files:           bindings,
	})
	closeErr := file.Close()
	if encodeErr != nil || closeErr != nil {
		t.Fatalf("write run status: %v", errors.Join(encodeErr, closeErr))
	}
}
