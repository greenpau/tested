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

// Command bundlecheck independently verifies a tested managed artifact bundle.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	manifestName    = "manifest.json"
	maxManifestSize = 1 << 20
)

var baseNames = []string{
	"index.html",
	"junit.xml",
	manifestName,
	"run.json",
	"stderr.log",
	"summary.json",
	"test_output.html",
	"test_output.jsonl",
}

var coverageNames = []string{
	"coverage.html",
	"coverage.out",
}

type manifestDocument struct {
	Version   int             `json:"version"`
	Algorithm string          `json:"algorithm"`
	Files     []manifestEntry `json:"files"`
}

type manifestEntry struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"media_type"`
	Role      string `json:"role"`
	Sensitive bool   `json:"sensitive"`
}

type bundleReport struct {
	files int
}

func main() {
	dir := flag.String("dir", "", "managed artifact directory")
	coverage := flag.Bool(
		"coverage",
		true,
		"expect coverage.out and coverage.html",
	)
	flag.Parse()
	if *dir == "" || flag.NArg() != 0 {
		fmt.Fprintln(
			os.Stderr,
			"usage: bundlecheck -dir PATH [-coverage=true|false]",
		)
		os.Exit(2)
	}

	report, err := validateBundle(*dir, *coverage)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bundlecheck: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf(
		"bundlecheck: verified %d files in %s\n",
		report.files,
		*dir,
	)
}

func validateBundle(dir string, coverage bool) (bundleReport, error) {
	absoluteDir, err := filepath.Abs(dir)
	if err != nil {
		return bundleReport{}, fmt.Errorf("resolve artifact directory: %w", err)
	}
	absoluteDir = filepath.Clean(absoluteDir)
	info, err := os.Lstat(absoluteDir)
	if err != nil {
		return bundleReport{}, fmt.Errorf("inspect artifact directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return bundleReport{}, errors.New("artifact directory is a symbolic link")
	}
	if !info.IsDir() {
		return bundleReport{}, errors.New("artifact path is not a directory")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		return bundleReport{}, fmt.Errorf(
			"artifact directory permissions = %s, want -rwx------",
			info.Mode().Perm(),
		)
	}

	expected := expectedNames(coverage)
	entries, err := os.ReadDir(absoluteDir)
	if err != nil {
		return bundleReport{}, fmt.Errorf("read artifact directory: %w", err)
	}
	actual := make([]string, 0, len(entries))
	for _, entry := range entries {
		actual = append(actual, entry.Name())
	}
	sort.Strings(actual)
	if !equalStrings(actual, expected) {
		return bundleReport{}, fmt.Errorf(
			"artifact inventory = [%s], want [%s]",
			strings.Join(actual, ", "),
			strings.Join(expected, ", "),
		)
	}

	for _, name := range expected {
		if err := validateManagedFile(filepath.Join(absoluteDir, name)); err != nil {
			return bundleReport{}, fmt.Errorf("%s: %w", name, err)
		}
	}

	manifest, err := readManifest(filepath.Join(absoluteDir, manifestName))
	if err != nil {
		return bundleReport{}, err
	}
	if manifest.Version != 1 {
		return bundleReport{}, fmt.Errorf(
			"manifest version = %d, want 1",
			manifest.Version,
		)
	}
	if manifest.Algorithm != "sha256" {
		return bundleReport{}, fmt.Errorf(
			"manifest algorithm = %q, want %q",
			manifest.Algorithm,
			"sha256",
		)
	}

	expectedManifestNames := make([]string, 0, len(expected)-1)
	for _, name := range expected {
		if name != manifestName {
			expectedManifestNames = append(expectedManifestNames, name)
		}
	}
	if len(manifest.Files) != len(expectedManifestNames) {
		return bundleReport{}, fmt.Errorf(
			"manifest contains %d entries, want %d",
			len(manifest.Files),
			len(expectedManifestNames),
		)
	}
	for index, entry := range manifest.Files {
		expectedName := expectedManifestNames[index]
		if entry.Name != expectedName {
			return bundleReport{}, fmt.Errorf(
				"manifest entry %d name = %q, want %q in lexical order",
				index,
				entry.Name,
				expectedName,
			)
		}
		if entry.MediaType == "" || entry.Role == "" || !entry.Sensitive {
			return bundleReport{}, fmt.Errorf(
				"manifest entry %q has incomplete security metadata",
				entry.Name,
			)
		}
		digest, size, err := digestFile(
			filepath.Join(absoluteDir, entry.Name),
		)
		if err != nil {
			return bundleReport{}, fmt.Errorf(
				"verify manifest entry %q: %w",
				entry.Name,
				err,
			)
		}
		if entry.Size != size {
			return bundleReport{}, fmt.Errorf(
				"manifest entry %q size = %d, want %d",
				entry.Name,
				entry.Size,
				size,
			)
		}
		if entry.SHA256 != digest {
			return bundleReport{}, fmt.Errorf(
				"manifest entry %q SHA-256 = %q, want %q",
				entry.Name,
				entry.SHA256,
				digest,
			)
		}
	}
	return bundleReport{files: len(expected)}, nil
}

func expectedNames(coverage bool) []string {
	names := append([]string(nil), baseNames...)
	if coverage {
		names = append(names, coverageNames...)
	}
	sort.Strings(names)
	return names
}

func validateManagedFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("managed artifact is a symbolic link")
	}
	if !info.Mode().IsRegular() {
		return errors.New("managed artifact is not a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		return fmt.Errorf(
			"permissions = %s, want -rw-------",
			info.Mode().Perm(),
		)
	}
	return nil
}

func readManifest(path string) (_ *manifestDocument, returnErr error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			returnErr = errors.Join(
				returnErr,
				fmt.Errorf("close manifest: %w", err),
			)
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect manifest: %w", err)
	}
	if info.Size() > maxManifestSize {
		return nil, fmt.Errorf(
			"manifest size %d exceeds %d-byte limit",
			info.Size(),
			maxManifestSize,
		)
	}

	decoder := json.NewDecoder(io.LimitReader(file, maxManifestSize+1))
	decoder.DisallowUnknownFields()
	var manifest manifestDocument
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, errors.New("decode manifest: trailing JSON value")
		}
		return nil, fmt.Errorf("decode manifest trailing data: %w", err)
	}
	return &manifest, nil
}

func digestFile(path string) (
	digest string,
	size int64,
	returnErr error,
) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return "", 0, err
	}
	hash := sha256.New()
	size, err = io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	if size != info.Size() {
		return "", 0, fmt.Errorf(
			"read %d bytes, file size is %d",
			size,
			info.Size(),
		)
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
