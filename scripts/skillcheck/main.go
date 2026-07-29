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

// Command skillcheck validates the repository-local Codex skill handbook
// without requiring a YAML library or a machine-local Codex installation.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	rootNode      = "AGENTS.md"
	skillsDirName = ".codex/skills"
)

var (
	skillNamePattern = regexp.MustCompile(
		`^[a-z0-9]+(?:-[a-z0-9]+)*$`,
	)
	routePattern = regexp.MustCompile(
		`^(?:[-*+][[:space:]]+)?Use ` +
			`\[([a-z0-9]+(?:-[a-z0-9]+)*)\]` +
			`\(([^)]+/SKILL\.md)\) to (.+)$`,
	)
	placeholderPattern = regexp.MustCompile(`(?i)\b(?:TODO|TBD)\b`)
)

var canonicalRoutes = map[string][]string{
	rootNode: {
		"implementation-architecture",
		"skill-authoring",
		"source-code-management",
	},
	"implementation-architecture": {
		"coding-directives",
		"implementation-coverage",
		"implementation-reporting",
		"implementation-test-pipeline",
		"scripts-and-automation",
	},
}

type skillDefinition struct {
	name        string
	description string
	path        string
	content     string
}

type validationReport struct {
	skills int
	routes int
}

func main() {
	root := flag.String("root", ".", "repository root containing AGENTS.md")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "skillcheck: positional arguments are not supported")
		os.Exit(2)
	}

	report, err := validateRepository(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "skillcheck: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf(
		"skillcheck: validated %d skills and %d actionable routes\n",
		report.skills,
		report.routes,
	)
}

func validateRepository(root string) (validationReport, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return validationReport{}, fmt.Errorf("resolve repository root: %w", err)
	}
	absoluteRoot = filepath.Clean(absoluteRoot)

	agentsPath := filepath.Join(absoluteRoot, rootNode)
	agentsContent, err := readTextFile(agentsPath)
	if err != nil {
		return validationReport{}, fmt.Errorf("read %s: %w", rootNode, err)
	}
	if err := rejectPlaceholders(rootNode, agentsContent); err != nil {
		return validationReport{}, err
	}

	skills, err := loadSkills(absoluteRoot)
	if err != nil {
		return validationReport{}, err
	}

	nodes := make(map[string]string, len(skills)+1)
	nodes[rootNode] = agentsPath
	paths := make(map[string]string, len(skills))
	for name, skill := range skills {
		nodes[name] = skill.path
		paths[filepath.Clean(skill.path)] = name
	}

	graph := make(map[string][]string, len(nodes))
	graph[rootNode], err = parseRoutes(
		absoluteRoot,
		rootNode,
		agentsPath,
		agentsContent,
		paths,
	)
	if err != nil {
		return validationReport{}, err
	}
	routeCount := len(graph[rootNode])
	for name, skill := range skills {
		graph[name], err = parseRoutes(
			absoluteRoot,
			name,
			skill.path,
			skill.content,
			paths,
		)
		if err != nil {
			return validationReport{}, err
		}
		routeCount += len(graph[name])
	}

	if err := validateGraph(graph, nodes); err != nil {
		return validationReport{}, err
	}
	if err := validateCanonicalTopology(graph, nodes); err != nil {
		return validationReport{}, err
	}

	return validationReport{
		skills: len(skills),
		routes: routeCount,
	}, nil
}

func loadSkills(root string) (map[string]skillDefinition, error) {
	skillsRoot := filepath.Join(root, filepath.FromSlash(skillsDirName))
	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", skillsDirName, err)
	}

	skills := make(map[string]skillDefinition)
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf(
				"%s/%s: skill directory must not be a symbolic link",
				skillsDirName,
				entry.Name(),
			)
		}
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !skillNamePattern.MatchString(name) || len(name) > 64 {
			return nil, fmt.Errorf(
				"%s/%s: invalid skill directory name",
				skillsDirName,
				name,
			)
		}

		path := filepath.Join(skillsRoot, name, "SKILL.md")
		content, err := readTextFile(path)
		if err != nil {
			return nil, fmt.Errorf(
				"read %s/%s/SKILL.md: %w",
				skillsDirName,
				name,
				err,
			)
		}
		frontmatter, err := parseFrontmatter(content)
		if err != nil {
			return nil, fmt.Errorf(
				"%s/%s/SKILL.md: %w",
				skillsDirName,
				name,
				err,
			)
		}
		if frontmatter["name"] != name {
			return nil, fmt.Errorf(
				"%s/%s/SKILL.md: frontmatter name %q does not match directory",
				skillsDirName,
				name,
				frontmatter["name"],
			)
		}
		description := frontmatter["description"]
		if len(description) > 1024 {
			return nil, fmt.Errorf(
				"%s/%s/SKILL.md: description exceeds 1024 bytes",
				skillsDirName,
				name,
			)
		}
		if strings.ContainsAny(description, "<>") {
			return nil, fmt.Errorf(
				"%s/%s/SKILL.md: description contains an angle bracket",
				skillsDirName,
				name,
			)
		}
		if !strings.Contains(description, "Use when ") {
			return nil, fmt.Errorf(
				"%s/%s/SKILL.md: description must contain concrete "+
					"\"Use when\" triggers",
				skillsDirName,
				name,
			)
		}
		if err := rejectPlaceholders(
			filepath.ToSlash(filepath.Join(skillsDirName, name, "SKILL.md")),
			content,
		); err != nil {
			return nil, err
		}
		if err := validateOpenAIMetadata(
			filepath.Join(skillsRoot, name, "agents", "openai.yaml"),
			name,
		); err != nil {
			return nil, err
		}

		skills[name] = skillDefinition{
			name:        name,
			description: description,
			path:        path,
			content:     content,
		}
	}
	if len(skills) == 0 {
		return nil, fmt.Errorf("%s: no skills found", skillsDirName)
	}
	return skills, nil
}

func parseFrontmatter(content string) (map[string]string, error) {
	if !strings.HasPrefix(content, "---\n") {
		return nil, errors.New("frontmatter must start with \"---\"")
	}
	end := strings.Index(content[len("---\n"):], "\n---\n")
	if end < 0 {
		return nil, errors.New("frontmatter closing delimiter is missing")
	}
	raw := content[len("---\n") : len("---\n")+end]
	body := content[len("---\n")+end+len("\n---\n"):]
	if strings.TrimSpace(body) == "" {
		return nil, errors.New("skill body is empty")
	}

	values, err := parseFlatMapping(raw)
	if err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}
	if err := requireExactKeys(values, "name", "description"); err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	name := values["name"]
	if !skillNamePattern.MatchString(name) || len(name) > 64 {
		return nil, fmt.Errorf("frontmatter: invalid skill name %q", name)
	}
	if values["description"] == "" {
		return nil, errors.New("frontmatter: description is empty")
	}
	return values, nil
}

func validateOpenAIMetadata(path, skillName string) error {
	content, err := readTextFile(path)
	if err != nil {
		return fmt.Errorf(
			"%s/%s/agents/openai.yaml: %w",
			skillsDirName,
			skillName,
			err,
		)
	}
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "interface:" {
		return fmt.Errorf(
			"%s/%s/agents/openai.yaml: expected one interface mapping",
			skillsDirName,
			skillName,
		)
	}

	var mappingLines []string
	for lineNumber, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "  ") ||
			strings.HasPrefix(line, "   ") ||
			strings.TrimSpace(line) == "" {
			return fmt.Errorf(
				"%s/%s/agents/openai.yaml:%d: interface fields "+
					"must use two-space indentation",
				skillsDirName,
				skillName,
				lineNumber+2,
			)
		}
		mappingLines = append(mappingLines, strings.TrimPrefix(line, "  "))
	}
	values, err := parseFlatMapping(strings.Join(mappingLines, "\n"))
	if err != nil {
		return fmt.Errorf(
			"%s/%s/agents/openai.yaml: parse interface: %w",
			skillsDirName,
			skillName,
			err,
		)
	}
	if err := requireExactKeys(
		values,
		"display_name",
		"short_description",
		"default_prompt",
	); err != nil {
		return fmt.Errorf(
			"%s/%s/agents/openai.yaml: interface: %w",
			skillsDirName,
			skillName,
			err,
		)
	}
	if len(values["display_name"]) > 64 {
		return fmt.Errorf(
			"%s/%s/agents/openai.yaml: display_name exceeds 64 bytes",
			skillsDirName,
			skillName,
		)
	}
	if len(values["short_description"]) > 64 {
		return fmt.Errorf(
			"%s/%s/agents/openai.yaml: short_description exceeds 64 bytes",
			skillsDirName,
			skillName,
		)
	}
	if !strings.Contains(values["default_prompt"], "$"+skillName) {
		return fmt.Errorf(
			"%s/%s/agents/openai.yaml: default_prompt must invoke $%s",
			skillsDirName,
			skillName,
			skillName,
		)
	}
	return nil
}

func parseFlatMapping(content string) (map[string]string, error) {
	values := make(map[string]string)
	for index, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			return nil, fmt.Errorf("line %d: expected key: value", index+1)
		}
		key = strings.TrimSpace(key)
		if key == "" || strings.ContainsAny(key, " \t") {
			return nil, fmt.Errorf("line %d: invalid key %q", index+1, key)
		}
		if _, exists := values[key]; exists {
			return nil, fmt.Errorf("line %d: duplicate key %q", index+1, key)
		}
		parsed, err := parseScalar(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("line %d: key %q: %w", index+1, key, err)
		}
		values[key] = parsed
	}
	return values, nil
}

func parseScalar(value string) (string, error) {
	if value == "" {
		return "", errors.New("value is empty")
	}
	switch value[0] {
	case '"':
		parsed, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("invalid double-quoted scalar: %w", err)
		}
		return parsed, nil
	case '\'':
		if len(value) < 2 || value[len(value)-1] != '\'' {
			return "", errors.New("unterminated single-quoted scalar")
		}
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'"), nil
	case '|', '>', '[', '{', '&', '*', '!':
		return "", errors.New("complex YAML values are not supported")
	default:
		return value, nil
	}
}

func requireExactKeys(values map[string]string, required ...string) error {
	expected := make(map[string]struct{}, len(required))
	for _, key := range required {
		expected[key] = struct{}{}
		if values[key] == "" {
			return fmt.Errorf("required key %q is missing or empty", key)
		}
	}
	var unexpected []string
	for key := range values {
		if _, ok := expected[key]; !ok {
			unexpected = append(unexpected, key)
		}
	}
	if len(unexpected) != 0 {
		sort.Strings(unexpected)
		return fmt.Errorf("unexpected keys: %s", strings.Join(unexpected, ", "))
	}
	return nil
}

func parseRoutes(
	root string,
	sourceName string,
	sourcePath string,
	content string,
	targetPaths map[string]string,
) ([]string, error) {
	var routes []string
	seen := make(map[string]struct{})
	for index, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		candidate := strings.TrimSpace(
			strings.TrimLeft(trimmed, "-*+"),
		)
		if !strings.HasPrefix(candidate, "Use [") {
			continue
		}
		match := routePattern.FindStringSubmatch(trimmed)
		if match == nil {
			return nil, fmt.Errorf(
				"%s:%d: malformed actionable skill route",
				relativePath(root, sourcePath),
				index+1,
			)
		}
		label, link, action := match[1], match[2], strings.TrimSpace(match[3])
		if action == "" || action == "." || strings.Contains(action, "...") ||
			placeholderPattern.MatchString(action) {
			return nil, fmt.Errorf(
				"%s:%d: route to %q has no concrete action",
				relativePath(root, sourcePath),
				index+1,
				label,
			)
		}
		if filepath.IsAbs(filepath.FromSlash(link)) {
			return nil, fmt.Errorf(
				"%s:%d: route target must be relative",
				relativePath(root, sourcePath),
				index+1,
			)
		}
		targetPath := filepath.Clean(filepath.Join(
			filepath.Dir(sourcePath),
			filepath.FromSlash(link),
		))
		targetName, ok := targetPaths[targetPath]
		if !ok {
			return nil, fmt.Errorf(
				"%s:%d: route target %q is not a discovered skill",
				relativePath(root, sourcePath),
				index+1,
				link,
			)
		}
		if label != targetName {
			return nil, fmt.Errorf(
				"%s:%d: route label %q does not match target skill %q",
				relativePath(root, sourcePath),
				index+1,
				label,
				targetName,
			)
		}
		if targetName == sourceName {
			return nil, fmt.Errorf(
				"%s:%d: skill routes to itself",
				relativePath(root, sourcePath),
				index+1,
			)
		}
		if _, exists := seen[targetName]; exists {
			return nil, fmt.Errorf(
				"%s:%d: duplicate route to %q",
				relativePath(root, sourcePath),
				index+1,
				targetName,
			)
		}
		seen[targetName] = struct{}{}
		routes = append(routes, targetName)
	}
	sort.Strings(routes)
	return routes, nil
}

func validateGraph(graph map[string][]string, nodes map[string]string) error {
	state := make(map[string]uint8, len(nodes))
	var stack []string
	var visit func(string) error
	visit = func(node string) error {
		switch state[node] {
		case 1:
			cycle := append(append([]string(nil), stack...), node)
			return fmt.Errorf(
				"skill route cycle: %s",
				strings.Join(cycle, " -> "),
			)
		case 2:
			return nil
		}
		state[node] = 1
		stack = append(stack, node)
		for _, target := range graph[node] {
			if _, ok := nodes[target]; !ok {
				return fmt.Errorf("%s routes to unknown skill %s", node, target)
			}
			if err := visit(target); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[node] = 2
		return nil
	}

	nodeNames := sortedKeys(nodes)
	for _, node := range nodeNames {
		if err := visit(node); err != nil {
			return err
		}
	}

	reachable := make(map[string]bool, len(nodes))
	var mark func(string)
	mark = func(node string) {
		if reachable[node] {
			return
		}
		reachable[node] = true
		for _, target := range graph[node] {
			mark(target)
		}
	}
	mark(rootNode)
	var unreachable []string
	for _, node := range nodeNames {
		if !reachable[node] {
			unreachable = append(unreachable, node)
		}
	}
	if len(unreachable) != 0 {
		return fmt.Errorf(
			"skills unreachable from %s: %s",
			rootNode,
			strings.Join(unreachable, ", "),
		)
	}
	return nil
}

func validateCanonicalTopology(
	graph map[string][]string,
	nodes map[string]string,
) error {
	for node := range nodes {
		actual := append([]string(nil), graph[node]...)
		expected := append([]string(nil), canonicalRoutes[node]...)
		sort.Strings(actual)
		sort.Strings(expected)
		if strings.Join(actual, "\x00") != strings.Join(expected, "\x00") {
			return fmt.Errorf(
				"%s routes = [%s], want canonical [%s]",
				node,
				strings.Join(actual, ", "),
				strings.Join(expected, ", "),
			)
		}
	}
	for node := range canonicalRoutes {
		if _, ok := nodes[node]; !ok {
			return fmt.Errorf("canonical router %s does not exist", node)
		}
	}
	return nil
}

func rejectPlaceholders(path, content string) error {
	if match := placeholderPattern.FindString(content); match != "" {
		return fmt.Errorf("%s: contains unfinished placeholder %q", path, match)
	}
	return nil
}

func readTextFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("symbolic links are not allowed")
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("path is not a regular file")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(content) {
		return "", errors.New("file is not valid UTF-8")
	}
	return strings.ReplaceAll(string(content), "\r\n", "\n"), nil
}

func relativePath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
