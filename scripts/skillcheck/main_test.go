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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRepository(t *testing.T) {
	root := writeCanonicalRepository(t)
	report, err := validateRepository(root)
	if err != nil {
		t.Fatalf("validateRepository() error = %v", err)
	}
	if report.skills != 8 || report.routes != 8 {
		t.Fatalf(
			"validateRepository() report = %#v, want 8 skills and 8 routes",
			report,
		)
	}
}

func TestValidateRepositoryRejectsInvalidHandbook(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*testing.T, string)
		wantErr string
	}{
		{
			name: "unexpected frontmatter field",
			mutate: func(t *testing.T, root string) {
				path := skillPath(root, "coding-directives")
				replaceFile(t, path, "description:", "license: Apache-2.0\ndescription:")
			},
			wantErr: "unexpected keys: license",
		},
		{
			name: "stale UI prompt",
			mutate: func(t *testing.T, root string) {
				path := filepath.Join(
					root,
					".codex",
					"skills",
					"coding-directives",
					"agents",
					"openai.yaml",
				)
				replaceFile(t, path, "$coding-directives", "$wrong-skill")
			},
			wantErr: "default_prompt must invoke $coding-directives",
		},
		{
			name: "missing route target",
			mutate: func(t *testing.T, root string) {
				path := filepath.Join(root, rootNode)
				replaceFile(
					t,
					path,
					"implementation-architecture/SKILL.md",
					"missing/SKILL.md",
				)
			},
			wantErr: "is not a discovered skill",
		},
		{
			name: "cycle",
			mutate: func(t *testing.T, root string) {
				path := skillPath(root, "coding-directives")
				appendFile(
					t,
					path,
					"\n- Use [implementation-architecture]"+
						"(../implementation-architecture/SKILL.md) "+
						"to create a route cycle.\n",
				)
			},
			wantErr: "skill route cycle",
		},
		{
			name: "unreachable skill",
			mutate: func(t *testing.T, root string) {
				path := filepath.Join(root, rootNode)
				content := readFile(t, path)
				var kept []string
				for _, line := range strings.Split(content, "\n") {
					if !strings.Contains(line, "[skill-authoring]") {
						kept = append(kept, line)
					}
				}
				writeFile(t, path, strings.Join(kept, "\n"))
			},
			wantErr: "skills unreachable from AGENTS.md: skill-authoring",
		},
		{
			name: "placeholder",
			mutate: func(t *testing.T, root string) {
				appendFile(t, skillPath(root, "coding-directives"), "\nTODO\n")
			},
			wantErr: "contains unfinished placeholder",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeCanonicalRepository(t)
			test.mutate(t, root)
			_, err := validateRepository(root)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf(
					"validateRepository() error = %v, want containing %q",
					err,
					test.wantErr,
				)
			}
		})
	}
}

func writeCanonicalRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	var agents strings.Builder
	agents.WriteString("# Repository Instructions\n\n")
	for _, target := range canonicalRoutes[rootNode] {
		fmt.Fprintf(
			&agents,
			"- Use [%s](.codex/skills/%s/SKILL.md) to handle %s work.\n",
			target,
			target,
			target,
		)
	}
	writeFile(t, filepath.Join(root, rootNode), agents.String())

	names := []string{
		"coding-directives",
		"implementation-architecture",
		"implementation-coverage",
		"implementation-reporting",
		"implementation-test-pipeline",
		"scripts-and-automation",
		"skill-authoring",
		"source-code-management",
	}
	for _, name := range names {
		var body strings.Builder
		fmt.Fprintf(
			&body,
			"---\nname: %s\ndescription: Validate %s. Use when testing %s.\n---\n\n"+
				"# %s\n",
			name,
			name,
			name,
			name,
		)
		for _, target := range canonicalRoutes[name] {
			fmt.Fprintf(
				&body,
				"\n- Use [%s](../%s/SKILL.md) to handle %s work.\n",
				target,
				target,
				target,
			)
		}
		writeFile(t, skillPath(root, name), body.String())
		writeFile(
			t,
			filepath.Join(
				root,
				".codex",
				"skills",
				name,
				"agents",
				"openai.yaml",
			),
			fmt.Sprintf(
				"interface:\n"+
					"  display_name: %q\n"+
					"  short_description: %q\n"+
					"  default_prompt: %q\n",
				name,
				"Validate "+name,
				"Use $"+name+" to validate this skill.",
			),
		)
	}
	return root
}

func skillPath(root, name string) string {
	return filepath.Join(root, ".codex", "skills", name, "SKILL.md")
}

func replaceFile(t *testing.T, path, old, replacement string) {
	t.Helper()
	content := readFile(t, path)
	if !strings.Contains(content, old) {
		t.Fatalf("%s does not contain %q", path, old)
	}
	writeFile(t, path, strings.Replace(content, old, replacement, 1))
}

func appendFile(t *testing.T, path, suffix string) {
	t.Helper()
	writeFile(t, path, readFile(t, path)+suffix)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
