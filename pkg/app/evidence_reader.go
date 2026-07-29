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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"

	"github.com/greenpau/tested/pkg/artifact"
	"github.com/greenpau/tested/pkg/coverage"
	"github.com/greenpau/tested/pkg/runstatus"
)

type boundEvidenceReader struct {
	file     *os.File
	path     string
	binding  runstatus.File
	opened   os.FileInfo
	hash     hash.Hash
	consumed int64
}

func newBoundEvidenceReader(
	file *os.File,
	path string,
	binding runstatus.File,
) (*boundEvidenceReader, error) {
	if file == nil {
		return nil, errors.New("verify bound evidence: file is nil")
	}
	opened, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect opened bound evidence %q: %w", path, err)
	}
	if !opened.Mode().IsRegular() {
		return nil, fmt.Errorf("opened bound evidence %q is not a regular file", path)
	}
	return &boundEvidenceReader{
		file:    file,
		path:    path,
		binding: binding,
		opened:  opened,
		hash:    sha256.New(),
	}, nil
}

func (r *boundEvidenceReader) Read(buffer []byte) (int, error) {
	if r == nil || r.file == nil || r.hash == nil {
		return 0, errors.New("verify bound evidence: reader is not initialized")
	}
	n, err := r.file.Read(buffer)
	if n > 0 {
		r.consumed += int64(n)
		_, _ = r.hash.Write(buffer[:n])
	}
	return n, err
}

func (r *boundEvidenceReader) Verify() error {
	if r == nil || r.file == nil || r.hash == nil || r.opened == nil {
		return errors.New("verify bound evidence: reader is not initialized")
	}
	digest := hex.EncodeToString(r.hash.Sum(nil))
	if r.consumed != r.binding.Size || digest != r.binding.SHA256 {
		return fmt.Errorf(
			"verify consumed run metadata binding %q: got size %d SHA-256 %s, "+
				"want size %d SHA-256 %s",
			r.binding.Name,
			r.consumed,
			digest,
			r.binding.Size,
			r.binding.SHA256,
		)
	}
	after, err := r.file.Stat()
	if err != nil {
		return fmt.Errorf("reinspect consumed bound evidence %q: %w", r.path, err)
	}
	if !after.Mode().IsRegular() ||
		!os.SameFile(r.opened, after) ||
		after.Size() != r.opened.Size() ||
		!after.ModTime().Equal(r.opened.ModTime()) ||
		after.Size() != r.consumed {
		return fmt.Errorf(
			"verify consumed run metadata binding %q: opened file changed while reading",
			r.binding.Name,
		)
	}
	current, err := os.Lstat(r.path)
	if err != nil {
		return fmt.Errorf(
			"revalidate consumed run metadata binding %q: %w",
			r.binding.Name,
			err,
		)
	}
	if current.Mode()&os.ModeSymlink != 0 ||
		!current.Mode().IsRegular() ||
		!os.SameFile(after, current) ||
		current.Size() != after.Size() ||
		!current.ModTime().Equal(after.ModTime()) {
		return fmt.Errorf(
			"verify consumed run metadata binding %q: pathname changed while reading",
			r.binding.Name,
		)
	}
	return nil
}

func statusFileBinding(
	evidence runstatus.Evidence,
	name artifact.Name,
) (runstatus.File, bool) {
	for _, binding := range evidence.Files {
		if binding.Name == string(name) {
			return binding, true
		}
	}
	return runstatus.File{}, false
}

func parseCoverageSnapshot(
	layout *artifact.Layout,
	path string,
	evidence runstatus.Evidence,
	verifyBinding bool,
) (*coverage.Profile, string, error) {
	file, err := openRegularEvidence(path)
	if err != nil {
		return nil, "", err
	}
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, "", fmt.Errorf(
			"inspect opened coverage profile %q: %w",
			path,
			err,
		)
	}
	var source io.Reader = file
	var verifier *boundEvidenceReader
	if verifyBinding {
		binding, ok := statusFileBinding(
			evidence,
			artifact.CoverageProfile,
		)
		if !ok {
			_ = file.Close()
			return nil, "", errors.New(
				"parse coverage evidence: run metadata lacks coverage.out binding",
			)
		}
		verifier, err = newBoundEvidenceReader(file, path, binding)
		if err != nil {
			_ = file.Close()
			return nil, "", err
		}
		source = verifier
	}

	snapshot, err := os.CreateTemp(
		layout.OutputDir,
		".verified-coverage-*.out",
	)
	if err != nil {
		_ = file.Close()
		return nil, "", fmt.Errorf("create verified coverage snapshot: %w", err)
	}
	snapshotPath := snapshot.Name()
	keepSnapshot := false
	defer func() {
		if !keepSnapshot {
			_ = os.Remove(snapshotPath)
		}
	}()
	if err := snapshot.Chmod(0o600); err != nil {
		_ = snapshot.Close()
		_ = file.Close()
		return nil, "", fmt.Errorf(
			"set verified coverage snapshot permissions: %w",
			err,
		)
	}

	profile, parseErr := coverage.Parse(io.TeeReader(source, snapshot))
	verifyErr := error(nil)
	if verifier != nil {
		verifyErr = verifier.Verify()
	} else if parseErr == nil {
		snapshotInfo, statErr := snapshot.Stat()
		if statErr != nil {
			verifyErr = fmt.Errorf(
				"inspect verified coverage snapshot: %w",
				statErr,
			)
		} else {
			verifyErr = verifyStableOpenedEvidence(
				file,
				path,
				opened,
				snapshotInfo.Size(),
			)
		}
	}
	syncErr := snapshot.Sync()
	snapshotCloseErr := snapshot.Close()
	sourceCloseErr := file.Close()
	if parseErr != nil || verifyErr != nil || syncErr != nil ||
		snapshotCloseErr != nil || sourceCloseErr != nil {
		return nil, "", errors.Join(
			wrapError(parseErr, "parse coverage profile %q", path),
			verifyErr,
			wrapError(syncErr, "sync verified coverage snapshot"),
			wrapError(snapshotCloseErr, "close verified coverage snapshot"),
			wrapError(sourceCloseErr, "close coverage profile %q", path),
		)
	}
	keepSnapshot = true
	return profile, snapshotPath, nil
}

func verifyStableOpenedEvidence(
	file *os.File,
	path string,
	opened os.FileInfo,
	consumed int64,
) error {
	if file == nil || opened == nil {
		return errors.New("verify opened evidence: file information is unavailable")
	}
	after, err := file.Stat()
	if err != nil {
		return fmt.Errorf("reinspect opened evidence %q: %w", path, err)
	}
	if !after.Mode().IsRegular() ||
		!os.SameFile(opened, after) ||
		after.Size() != opened.Size() ||
		after.Mode() != opened.Mode() ||
		!after.ModTime().Equal(opened.ModTime()) ||
		consumed != opened.Size() {
		return fmt.Errorf(
			"verify opened evidence %q: file changed while reading",
			path,
		)
	}
	current, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("revalidate opened evidence %q: %w", path, err)
	}
	if current.Mode()&os.ModeSymlink != 0 ||
		!current.Mode().IsRegular() ||
		!os.SameFile(after, current) ||
		current.Size() != after.Size() ||
		current.Mode() != after.Mode() ||
		!current.ModTime().Equal(after.ModTime()) {
		return fmt.Errorf(
			"verify opened evidence %q: pathname changed while reading",
			path,
		)
	}
	return nil
}
