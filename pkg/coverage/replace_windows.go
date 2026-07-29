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

//go:build windows

package coverage

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

const (
	moveFileReplaceExisting = 0x1
	moveFileWriteThrough    = 0x8
)

var (
	kernel32ReplaceFile = syscall.NewLazyDLL("kernel32.dll").NewProc("ReplaceFileW")
	kernel32MoveFileEx  = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")
)

func replaceFile(source, destination string) error {
	sourcePointer, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return fmt.Errorf("encode replacement source path: %w", err)
	}
	destinationPointer, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return fmt.Errorf("encode replacement destination path: %w", err)
	}

	// ReplaceFileW performs an existing-file replacement as one filesystem
	// operation. In particular, do not fall back to removing destination first:
	// that creates an externally observable absence and can lose the prior
	// report when the following rename fails.
	result, _, replaceErr := kernel32ReplaceFile.Call(
		uintptr(unsafe.Pointer(destinationPointer)),
		uintptr(unsafe.Pointer(sourcePointer)),
		0,
		0,
		0,
		0,
	)
	if result != 0 {
		return nil
	}
	replaceErr = windowsCallError(replaceErr)
	if !errors.Is(replaceErr, syscall.ERROR_FILE_NOT_FOUND) {
		return fmt.Errorf("ReplaceFileW: %w", replaceErr)
	}

	// ReplaceFileW requires an existing destination. MoveFileExW publishes a new
	// destination in one operation and also handles a destination that appeared
	// between the calls. COPY_ALLOWED is intentionally absent, so publication
	// can never degrade into a cross-volume copy-and-delete sequence.
	result, _, moveErr := kernel32MoveFileEx.Call(
		uintptr(unsafe.Pointer(sourcePointer)),
		uintptr(unsafe.Pointer(destinationPointer)),
		moveFileReplaceExisting|moveFileWriteThrough,
	)
	if result != 0 {
		return nil
	}
	return fmt.Errorf(
		"MoveFileExW after ReplaceFileW reported a missing destination: %w",
		errors.Join(replaceErr, windowsCallError(moveErr)),
	)
}

func windowsCallError(err error) error {
	if err == nil || errors.Is(err, syscall.Errno(0)) {
		return syscall.EINVAL
	}
	return err
}
