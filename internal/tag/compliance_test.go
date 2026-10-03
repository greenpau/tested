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

package tag_test

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	structtag "github.com/greenpau/tested/internal/tag"
	"github.com/greenpau/tested/pkg/app"
	"github.com/greenpau/tested/pkg/artifact"
	"github.com/greenpau/tested/pkg/cli"
	"github.com/greenpau/tested/pkg/coverage"
	"github.com/greenpau/tested/pkg/protocol"
	"github.com/greenpau/tested/pkg/report"
	"github.com/greenpau/tested/pkg/result"
	"github.com/greenpau/tested/pkg/runner"
	"github.com/greenpau/tested/pkg/runstatus"
)

const repositoryModulePath = "github.com/greenpau/tested"

type registryEntry struct {
	resource        interface{}
	options         structtag.Options
	exemptionReason string
}

type genericRegistryResource[T any] struct{}

var complianceRegistry = []registryEntry{
	exempt(
		&structtag.Options{},
		"tag compliance policy is test-time configuration",
	),

	jsonSchema(&app.BuildInfo{}),
	exempt(
		&app.ExitState{},
		"application exit policy is runtime state, not a serialization schema",
	),

	exempt(
		&artifact.Layout{},
		"resolved managed filesystem paths are runtime state",
	),
	portableSchema(&artifact.ManifestEntry{}),
	portableSchemaWithOverride(
		&artifact.Manifest{},
		"Files",
		"xml",
		"files>file",
	),

	exempt(
		&cli.Options{},
		"parsed command-line configuration is not serialized directly",
	),
	exempt(
		&cli.UsageError{},
		"command-line usage errors are rendered behaviorally",
	),

	exempt(
		&coverage.HTMLOptions{},
		"coverage HTML generation options are runtime configuration",
	),
	exempt(
		&coverage.ParseOptions{},
		"coverage profile parsing options are runtime configuration",
	),
	exempt(
		&coverage.MergeOptions{},
		"coverage profile merge options are runtime configuration",
	),
	exempt(
		&coverage.DiffOptions{},
		"coverage source comparison options are runtime configuration",
	),
	exempt(
		&coverage.Threshold{},
		"exact coverage threshold internals are intentionally encapsulated",
	),
	jsonSchema(&coverage.Diff{}),
	jsonSchema(&coverage.DiffFile{}),
	jsonSchema(&coverage.DiffHunk{}),
	jsonSchema(&coverage.DiffLine{}),
	portableSchema(&coverage.Position{}),
	portableSchema(&coverage.Block{}),
	portableSchema(&coverage.Totals{}),
	portableSchemaWithOverride(
		&coverage.FileSummary{},
		"Blocks",
		"xml",
		"blocks>block",
	),
	portableSchemaWithOverride(
		&coverage.Profile{},
		"Files",
		"xml",
		"files>file",
	),

	exempt(
		&protocol.Diagnostic{},
		"framing diagnostics are projected through normalized result models",
	),
	exempt(
		&protocol.Record{},
		"raw framed records preserve evidence bytes outside a public schema",
	),
	exempt(
		&protocol.StreamOptions{},
		"stream bounds are runtime configuration",
	),
	exempt(
		&protocol.StreamSummary{},
		"stream counters are consumed directly by the analyzer",
	),
	exempt(
		&protocol.Stream{},
		"stream framing state is runtime-only",
	),
	exempt(
		&protocol.TestEvent{},
		"Go test events use presence-aware custom JSON decoding",
	),
	exempt(
		&protocol.BuildEvent{},
		"Go build events use forward-compatible custom JSON decoding",
	),
	exempt(
		&protocol.UnknownEvent{},
		"unknown Go events preserve dynamically named raw JSON fields",
	),
	exempt(
		&protocol.Event{},
		"the event union is assembled by custom protocol decoding",
	),

	jsonSchema(&report.Issue{}),
	jsonSchema(&report.CoveragePolicy{}),
	jsonSchema(&report.Assessment{}),
	exempt(
		&report.Options{},
		"renderer options are runtime configuration",
	),
	exempt(
		&report.Input{},
		"renderer input composes separately versioned result models",
	),
	exempt(
		&report.Renderer{},
		"renderer state contains compiled templates and embedded assets",
	),
	exempt(
		&report.ConsoleOptions{},
		"console presentation options are runtime configuration",
	),
	exempt(
		&report.Console{},
		"console renderer state is not serialized",
	),

	exempt(&result.Progress{}, "transient live progress is not a serialization schema"),
	jsonSchema(&result.OccurrenceID{}),
	jsonSchema(&result.Signals{}),
	jsonSchema(&result.Output{}),
	jsonSchema(&result.Attribute{}),
	jsonSchema(&result.Artifact{}),
	jsonSchema(&result.TestOccurrence{}),
	jsonSchema(&result.Package{}),
	jsonSchema(&result.Build{}),
	jsonSchema(&result.Diagnostic{}),
	jsonSchema(&result.UnknownAction{}),
	jsonSchema(&result.RunMetadata{}),
	jsonSchema(&result.Timing{}),
	jsonSchema(&result.OutcomeCounts{}),
	jsonSchema(&result.Summary{}),
	jsonSchema(&result.Result{}),
	exempt(
		&result.AnalyzerOptions{},
		"aggregation limits are runtime configuration",
	),
	exempt(
		&result.Analyzer{},
		"mutable aggregation state is not a serialization schema",
	),

	exempt(
		&runner.Options{},
		"child-process lifecycle options are runtime configuration",
	),
	exempt(
		&runner.Result{},
		"child-process results retain open-run evidence for app orchestration",
	),
	exempt(
		&runner.Runner{},
		"the child-process executor has no serialized state",
	),
	exempt(
		&runner.CommandOptions{},
		"command capture destinations are runtime configuration",
	),
	exempt(
		&runner.SignalCause{},
		"signal causes are inspected behaviorally across platforms",
	),
	exempt(
		&runner.CancellationDetails{},
		"cancellation details are normalized before durable serialization",
	),

	jsonSchema(&runstatus.Issue{}),
	jsonSchema(&runstatus.CoveragePolicy{}),
	jsonSchema(&runstatus.File{}),
	jsonSchema(&runstatus.Evidence{}),
}

func portableSchema(resource interface{}) registryEntry {
	return registryEntry{
		resource: resource,
		options: structtag.Options{
			DisableTagOnEmpty: true,
		},
	}
}

func portableSchemaWithOverride(
	resource interface{},
	fieldName string,
	tagName string,
	tagValue string,
) registryEntry {
	return registryEntry{
		resource: resource,
		options: structtag.Options{
			DisableTagOnEmpty: true,
			FieldTagOverrides: map[string]map[string]string{
				fieldName: {
					tagName: tagValue,
				},
			},
		},
	}
}

func jsonSchema(resource interface{}) registryEntry {
	return registryEntry{
		resource: resource,
		options: structtag.Options{
			RequiredTags:      []string{"json"},
			DisableTagOnEmpty: true,
			IgnoreTagOptions:  true,
		},
	}
}

func exempt(resource interface{}, reason string) registryEntry {
	return registryEntry{
		resource: resource,
		options: structtag.Options{
			Disabled: true,
		},
		exemptionReason: reason,
	}
}

func TestRepositoryTagCompliance(t *testing.T) {
	for _, entry := range complianceRegistry {
		typeName, err := registryTypeName(entry.resource)
		if err != nil {
			t.Fatalf("invalid compliance registry entry: %v", err)
		}
		t.Run(typeName, func(t *testing.T) {
			messages, err := structtag.GetTagCompliance(
				entry.resource,
				&entry.options,
			)
			if err != nil {
				t.Fatalf(
					"GetTagCompliance() error = %v\n%s",
					err,
					strings.Join(messages, "\n"),
				)
			}
		})
	}
}

func TestRepositoryExportedStructInventory(t *testing.T) {
	registered := make(map[string]registryEntry, len(complianceRegistry))
	for _, entry := range complianceRegistry {
		typeName, err := registryTypeName(entry.resource)
		if err != nil {
			t.Fatalf("invalid compliance registry entry: %v", err)
		}
		if _, exists := registered[typeName]; exists {
			t.Fatalf("duplicate compliance registry entry for %s", typeName)
		}
		if entry.options.Disabled {
			if strings.TrimSpace(entry.exemptionReason) == "" {
				t.Fatalf("disabled entry %s requires an exemption reason", typeName)
			}
		} else if entry.exemptionReason != "" {
			t.Fatalf(
				"active entry %s must not have an exemption reason",
				typeName,
			)
		}
		registered[typeName] = entry
	}

	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	discovered, err := discoverExportedStructs(repositoryRoot)
	if err != nil {
		t.Fatalf("discover exported structs: %v", err)
	}

	var missing []string
	for typeName, sourcePath := range discovered {
		if _, exists := registered[typeName]; !exists {
			missing = append(missing, typeName+" ("+sourcePath+")")
		}
	}
	sort.Strings(missing)

	var stale []string
	for typeName := range registered {
		if _, exists := discovered[typeName]; !exists {
			stale = append(stale, typeName)
		}
	}
	sort.Strings(stale)

	if len(missing) > 0 || len(stale) > 0 {
		var message strings.Builder
		if len(missing) > 0 {
			message.WriteString("unclassified exported structs:\n  ")
			message.WriteString(strings.Join(missing, "\n  "))
		}
		if len(stale) > 0 {
			if message.Len() > 0 {
				message.WriteByte('\n')
			}
			message.WriteString("stale compliance registry entries:\n  ")
			message.WriteString(strings.Join(stale, "\n  "))
		}
		t.Fatal(message.String())
	}
}

func registryTypeName(resource interface{}) (string, error) {
	if resource == nil {
		return "", fmt.Errorf("resource is nil")
	}
	resourceType := reflect.TypeOf(resource)
	if resourceType.Kind() != reflect.Ptr ||
		resourceType.Elem().Kind() != reflect.Struct {
		return "", fmt.Errorf(
			"resource type %s is not a pointer to struct",
			resourceType,
		)
	}
	resourceType = resourceType.Elem()
	resourceName := resourceType.Name()
	if genericStart := strings.IndexByte(resourceName, '['); genericStart >= 0 {
		resourceName = resourceName[:genericStart]
	}
	if resourceType.PkgPath() == "" || resourceName == "" {
		return "", fmt.Errorf("resource type %s is not named", resourceType)
	}
	return resourceType.PkgPath() + "." + resourceName, nil
}

func discoverExportedStructs(repositoryRoot string) (map[string]string, error) {
	discovered := make(map[string]string)
	err := filepath.WalkDir(
		repositoryRoot,
		func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relativePath, err := filepath.Rel(repositoryRoot, path)
			if err != nil {
				return fmt.Errorf("resolve relative path for %q: %w", path, err)
			}
			relativeSlashPath := filepath.ToSlash(relativePath)
			if entry.IsDir() {
				if shouldSkipDirectory(relativeSlashPath, entry.Name()) {
					return filepath.SkipDir
				}
				// A nested module owns its own API and cannot be represented by
				// this module's reflection-backed registry.
				if relativePath != "." {
					moduleFile := filepath.Join(path, "go.mod")
					moduleInfo, statErr := os.Stat(moduleFile)
					if statErr == nil && !moduleInfo.IsDir() {
						return filepath.SkipDir
					}
					if statErr != nil && !os.IsNotExist(statErr) {
						return fmt.Errorf(
							"inspect nested module boundary %q: %w",
							moduleFile,
							statErr,
						)
					}
				}
				return nil
			}
			if filepath.Ext(entry.Name()) != ".go" ||
				strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			// Reflection sees only the active build. Match the same file set;
			// tested's native Linux, Darwin, and Windows CI runs exercise each
			// supported platform's inventory.
			matches, err := build.Default.MatchFile(
				filepath.Dir(path),
				entry.Name(),
			)
			if err != nil {
				return fmt.Errorf(
					"evaluate build constraints for %q: %w",
					path,
					err,
				)
			}
			if !matches {
				return nil
			}

			fileSet := token.NewFileSet()
			source, err := parser.ParseFile(
				fileSet,
				path,
				nil,
				parser.SkipObjectResolution,
			)
			if err != nil {
				return fmt.Errorf("parse %q: %w", path, err)
			}
			packageDirectory := filepath.Dir(relativePath)
			packagePath := repositoryModulePath
			if packageDirectory != "." {
				packagePath += "/" + filepath.ToSlash(packageDirectory)
			}
			for _, declaration := range source.Decls {
				general, ok := declaration.(*ast.GenDecl)
				if !ok || general.Tok != token.TYPE {
					continue
				}
				for _, specification := range general.Specs {
					typeSpec, ok := specification.(*ast.TypeSpec)
					if !ok || typeSpec.Assign.IsValid() ||
						!ast.IsExported(typeSpec.Name.Name) {
						continue
					}
					if _, ok := typeSpec.Type.(*ast.StructType); !ok {
						continue
					}
					typeName := packagePath + "." + typeSpec.Name.Name
					if _, exists := discovered[typeName]; !exists {
						discovered[typeName] = relativeSlashPath
					}
				}
			}
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return discovered, nil
}

func TestRegistryTypeNameNormalizesGenericInstantiation(t *testing.T) {
	typeName, err := registryTypeName(&genericRegistryResource[int]{})
	if err != nil {
		t.Fatalf("registryTypeName() error = %v", err)
	}
	if strings.Contains(typeName, "[") ||
		!strings.HasSuffix(typeName, ".genericRegistryResource") {
		t.Fatalf(
			"registryTypeName() = %q, want uninstantiated generic name",
			typeName,
		)
	}
}

func TestDiscoverExportedStructsHonorsSourceBoundaries(t *testing.T) {
	repositoryRoot := t.TempDir()
	writeTestSource(
		t,
		filepath.Join(repositoryRoot, "pkg", "sample", "sample.go"),
		`package sample

type Exported struct {
	Value string
}

type Box[T any] struct{}
type private struct{}
type Alias = struct{}
`,
	)
	writeTestSource(
		t,
		filepath.Join(repositoryRoot, "pkg", "sample", "disabled_never.go"),
		`//go:build never

package sample

type BuildExcluded struct{}
`,
	)
	writeTestSource(
		t,
		filepath.Join(repositoryRoot, "testdata", "fixture", "fixture.go"),
		`package fixture

type Fixture struct{}
`,
	)
	writeTestSource(
		t,
		filepath.Join(repositoryRoot, "tools", "go.mod"),
		"module example.com/tool\n\ngo 1.25.0\n",
	)
	writeTestSource(
		t,
		filepath.Join(repositoryRoot, "tools", "tool", "tool.go"),
		`package tool

type NestedModuleType struct{}
`,
	)

	got, err := discoverExportedStructs(repositoryRoot)
	if err != nil {
		t.Fatalf("discoverExportedStructs() error = %v", err)
	}
	want := map[string]string{
		repositoryModulePath + "/pkg/sample.Box":      "pkg/sample/sample.go",
		repositoryModulePath + "/pkg/sample.Exported": "pkg/sample/sample.go",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverExportedStructs() = %#v, want %#v", got, want)
	}
}

func writeTestSource(t *testing.T, path, source string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create source directory for %q: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write source file %q: %v", path, err)
	}
}

func shouldSkipDirectory(relativePath, name string) bool {
	if relativePath == "." {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "bin", "dist", "node_modules", "testdata", "tmp", "vendor":
		return true
	default:
		return false
	}
}
