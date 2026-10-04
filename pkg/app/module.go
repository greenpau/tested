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
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// modulePackage is optional presentation context, never execution evidence.
// Read the nearest local module without invoking Go or resolving dependencies.
// An unavailable/invalid module leaves full import paths visible. Do not cross
// an existing module boundary, follow go.mod symlinks, or read special files.
func modulePackage(workDir string) string {
	const maxModuleBytes = 1 << 20
	for dir := workDir; ; dir = filepath.Dir(dir) {
		name := filepath.Join(dir, "go.mod")
		info, err := os.Lstat(name)
		if err == nil {
			if !info.Mode().IsRegular() || info.Size() > maxModuleBytes {
				return ""
			}
			file, err := openRegularEvidence(name)
			if err != nil {
				return ""
			}
			defer file.Close()
			source, readErr := io.ReadAll(io.LimitReader(file, maxModuleBytes+1))
			after, statErr := file.Stat()
			current, pathErr := os.Lstat(name)
			if readErr != nil || statErr != nil || pathErr != nil || len(source) > maxModuleBytes ||
				!os.SameFile(info, after) || !os.SameFile(after, current) ||
				after.Size() != int64(len(source)) || current.Size() != after.Size() ||
				!info.ModTime().Equal(after.ModTime()) || !after.ModTime().Equal(current.ModTime()) {
				return ""
			}
			return moduleDirective(source)
		}
		if !errors.Is(err, os.ErrNotExist) || filepath.Dir(dir) == dir {
			return ""
		}
	}
}

// moduleDirective extracts only a top-level module directive. Dependency paths
// can themselves be named "module" inside require/replace blocks. This is an
// optional context reader, not a validator for the rest of Go's module grammar.
func moduleDirective(source []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(source))
	scanner.Buffer(make([]byte, 4096), len(source)+1)
	var module string
	var inBlock bool
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "//")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == ")" {
			if !inBlock || len(fields) != 1 {
				return ""
			}
			inBlock = false
			continue
		}
		if inBlock {
			continue
		}
		if fields[0] != "module" {
			// Go accepts both "require (" and "require(".
			inBlock = strings.HasSuffix(strings.TrimSpace(line), "(")
			continue
		}
		if len(fields) != 2 || module != "" || fields[1] == "(" {
			return ""
		}
		module = fields[1]
		if strings.HasPrefix(module, `"`) || strings.HasPrefix(module, "`") {
			var err error
			module, err = strconv.Unquote(module)
			if err != nil {
				return ""
			}
		}
		if module == "" || strings.HasPrefix(module, "/") ||
			strings.HasSuffix(module, "/") || strings.ContainsAny(module, `\"`+"`") ||
			strings.IndexFunc(module, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return ""
		}
		for _, part := range strings.Split(module, "/") {
			if part == "" || part == "." || part == ".." {
				return ""
			}
		}
	}
	if scanner.Err() != nil || inBlock {
		return ""
	}
	return module
}
