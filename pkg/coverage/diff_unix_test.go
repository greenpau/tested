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

//go:build unix

package coverage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildDiffCancelsGoPackageResolution(t *testing.T) {
	gitCommand, _ := requireDiffCommands(t)
	projectDir := t.TempDir()
	initializeDiffRepository(t, gitCommand, projectDir)
	writeDiffTestFile(t, projectDir, "go.mod", "module example.com/cancel-diff\n\ngo 1.25\n")
	writeDiffTestFile(t, projectDir, "main.go", "package main\n\nfunc main() {}\n")
	runDiffTestCommand(t, projectDir, gitCommand, "add", "--", ".")
	runDiffTestCommand(t, projectDir, gitCommand, "commit", "-q", "-m", "base")

	goCommand := filepath.Join(t.TempDir(), "slow-go")
	script := []byte("#!/bin/sh\nsleep 30\n")
	if err := os.WriteFile(goCommand, script, 0o700); err != nil {
		t.Fatalf("write slow Go helper: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := BuildDiff(ctx, DiffOptions{
		GitCommand:     gitCommand,
		GoCommand:      goCommand,
		ProjectDir:     projectDir,
		BaseRevision:   "HEAD",
		ProfileFiles:   []string{"example.com/cancel-diff/main.go"},
		InterruptGrace: 100 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("BuildDiff() error = %v, want context deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("BuildDiff() cancellation took %s, want at most 3s", elapsed)
	}
}

func TestReadCurrentDiffSourceRejectsSymlink(t *testing.T) {
	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "target.go")
	link := filepath.Join(tempDir, "link.go")
	if err := os.WriteFile(target, []byte("package source\n"), 0o600); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create source symlink: %v", err)
	}
	builder := diffBuilder{
		sourceBudget: byteBudget{maximum: maxDiffSourceTotalBytes},
	}
	if _, err := builder.readCurrentSource(link); err == nil {
		t.Fatal("readCurrentSource(symlink) error = nil")
	}
}
