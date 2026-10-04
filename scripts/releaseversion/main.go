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

// Command releaseversion enforces tested's permanent major version of 1.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var versionPattern = regexp.MustCompile(`^1\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type releaseVersion struct {
	minor uint64
	patch uint64
}

func parseVersion(raw string) (releaseVersion, error) {
	value := strings.TrimSuffix(raw, "\n")
	if !versionPattern.MatchString(value) {
		return releaseVersion{}, errors.New("VERSION must be exactly 1.<minor>.<patch>, without leading zeros or suffixes")
	}
	parts := strings.Split(value, ".")
	minor, minorErr := strconv.ParseUint(parts[1], 10, 64)
	patch, patchErr := strconv.ParseUint(parts[2], 10, 64)
	if minorErr != nil || patchErr != nil {
		return releaseVersion{}, errors.New("VERSION components exceed the uint64 range")
	}
	return releaseVersion{minor: minor, patch: patch}, nil
}

func (v releaseVersion) String() string {
	return fmt.Sprintf("1.%d.%d", v.minor, v.patch)
}

func (v releaseVersion) next(kind string) (releaseVersion, error) {
	switch kind {
	case "patch":
		if v.patch == math.MaxUint64 {
			return releaseVersion{}, errors.New("patch increment exceeds the uint64 range")
		}
		v.patch++
	case "minor":
		if v.minor == math.MaxUint64 {
			return releaseVersion{}, errors.New("minor increment exceeds the uint64 range")
		}
		v.minor++
		v.patch = 0
	default:
		return releaseVersion{}, errors.New("only patch and minor releases are supported")
	}
	return v, nil
}

func readVersion(name string) (releaseVersion, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return releaseVersion{}, fmt.Errorf("inspect VERSION: %w", err)
	}
	if !info.Mode().IsRegular() {
		return releaseVersion{}, errors.New("VERSION must be a regular file, not a symlink")
	}
	file, err := os.Open(name)
	if err != nil {
		return releaseVersion{}, fmt.Errorf("open VERSION: %w", err)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 64))
	if err != nil {
		return releaseVersion{}, fmt.Errorf("read VERSION: %w", err)
	}
	if len(raw) == 64 {
		return releaseVersion{}, errors.New("VERSION exceeds the maximum release version length")
	}
	return parseVersion(string(raw))
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("releaseversion", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	file := flags.String("file", "VERSION", "version authority")
	if err := flags.Parse(args); err != nil {
		return err
	}
	args = flags.Args()
	if len(args) < 1 || len(args) > 2 || (args[0] != "check" && args[0] != "next") || (args[0] == "next" && len(args) != 2) {
		return errors.New("usage: releaseversion [-file PATH] check [TAG] | next patch|minor")
	}
	v, err := readVersion(*file)
	if err != nil {
		return err
	}
	if args[0] == "next" {
		v, err = v.next(args[1])
		if err != nil {
			return err
		}
	} else if len(args) == 2 && args[1] != "v"+v.String() {
		return fmt.Errorf("release tag must equal v%s", v)
	}
	_, err = fmt.Fprintln(out, v)
	return err
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "releaseversion: %v\n", err)
		os.Exit(1)
	}
}
