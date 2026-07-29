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

package artifact

import (
	"fmt"
	"os"
	"syscall"
)

func rejectMultipleLinks(info os.FileInfo, path string) error {
	if info == nil {
		return fmt.Errorf(
			"inspect hard-link count for %q: file information is nil",
			path,
		)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf(
			"inspect hard-link count for %q: POSIX stat metadata is unavailable",
			path,
		)
	}
	if stat.Nlink > 1 {
		return fmt.Errorf(
			"%w: managed artifact %q has %d filesystem links",
			ErrHardLink,
			path,
			stat.Nlink,
		)
	}
	return nil
}
