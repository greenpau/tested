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

package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"time"
)

const (
	// ManifestVersion is the current deterministic artifact manifest schema.
	ManifestVersion = 1
	// DigestAlgorithm names the hash used for every manifest entry.
	DigestAlgorithm = "sha256"

	roleEvidence = "raw_evidence"
	roleReport   = "report"
	roleIndex    = "index"
)

var errArtifactChanged = errors.New("artifact changed while hashing")

// ManifestEntry records the size, SHA-256 digest, and security metadata of one
// managed artifact. Every tested artifact is sensitive because even derived
// reports can expose test output, source paths, or project identity.
type ManifestEntry struct {
	Name      string `json:"name" xml:"name" yaml:"name"`
	Size      int64  `json:"size" xml:"size" yaml:"size"`
	SHA256    string `json:"sha256" xml:"sha256" yaml:"sha256"`
	MediaType string `json:"media_type" xml:"media_type" yaml:"media_type"`
	Role      string `json:"role" xml:"role" yaml:"role"`
	Sensitive bool   `json:"sensitive" xml:"sensitive" yaml:"sensitive"`
}

// Manifest is a deterministic inventory of managed artifacts. The manifest
// file itself is omitted to avoid a self-referential digest.
type Manifest struct {
	Version   int             `json:"version" xml:"version" yaml:"version"`
	Algorithm string          `json:"algorithm" xml:"algorithm" yaml:"algorithm"`
	Files     []ManifestEntry `json:"files" xml:"files>file" yaml:"files"`
}

type artifactSnapshot struct {
	name     Name
	path     string
	identity os.FileInfo
	size     int64
	mode     os.FileMode
	modified time.Time
	digest   string
}

type absentArtifact struct {
	name Name
	path string
}

type manifestInventory struct {
	manifest  Manifest
	snapshots []artifactSnapshot
	absent    []absentArtifact
}

// SHA256File computes the lowercase hexadecimal SHA-256 digest and size of a
// regular, non-symlink, single-linked file.
func SHA256File(path string) (string, int64, error) {
	return sha256File(path, false)
}

func sha256File(path string, secure bool) (string, int64, error) {
	snapshot, err := snapshotFile(path, secure)
	if err != nil {
		return "", 0, err
	}
	return snapshot.digest, snapshot.size, nil
}

func snapshotFile(path string, secure bool) (artifactSnapshot, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return artifactSnapshot{}, fmt.Errorf(
			"inspect artifact for SHA-256 %q: %w",
			path,
			err,
		)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return artifactSnapshot{}, fmt.Errorf(
			"%w: digest target %q",
			ErrSymlink,
			path,
		)
	}
	if !info.Mode().IsRegular() {
		return artifactSnapshot{}, fmt.Errorf(
			"digest target %q is not a regular file",
			path,
		)
	}
	if err := rejectMultipleLinks(info, path); err != nil {
		return artifactSnapshot{}, err
	}

	f, err := os.Open(path)
	if err != nil {
		return artifactSnapshot{}, errors.Join(
			errArtifactChanged,
			fmt.Errorf("open artifact for SHA-256 %q after inspection: %w", path, err),
		)
	}
	openedInfo, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return artifactSnapshot{}, errors.Join(
			errArtifactChanged,
			fmt.Errorf("inspect opened artifact for SHA-256 %q: %w", path, err),
		)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		_ = f.Close()
		return artifactSnapshot{}, fmt.Errorf(
			"%w: digest target %q changed before hashing",
			errArtifactChanged,
			path,
		)
	}
	if err := rejectMultipleLinks(openedInfo, path); err != nil {
		_ = f.Close()
		return artifactSnapshot{}, err
	}

	snapshot, hashErr := snapshotOpenedFile(f, path, openedInfo, secure)
	closeErr := f.Close()
	if hashErr != nil || closeErr != nil {
		return artifactSnapshot{}, errors.Join(
			hashErr,
			wrapHashedFileCloseError(closeErr, path),
		)
	}
	return snapshot, nil
}

func sha256OpenedFile(
	f *os.File,
	path string,
	openedInfo os.FileInfo,
	secure bool,
) (string, int64, error) {
	snapshot, err := snapshotOpenedFile(f, path, openedInfo, secure)
	if err != nil {
		return "", 0, err
	}
	return snapshot.digest, snapshot.size, nil
}

func snapshotOpenedFile(
	f *os.File,
	path string,
	openedInfo os.FileInfo,
	secure bool,
) (artifactSnapshot, error) {
	if f == nil {
		return artifactSnapshot{}, errors.New("hash artifact: opened file is nil")
	}
	if openedInfo == nil {
		return artifactSnapshot{}, errors.New(
			"hash artifact: opened file information is nil",
		)
	}
	preOperation, err := f.Stat()
	if err != nil {
		return artifactSnapshot{}, fmt.Errorf(
			"inspect opened artifact before securing or hashing %q: %w",
			path,
			err,
		)
	}
	if !preOperation.Mode().IsRegular() ||
		!os.SameFile(openedInfo, preOperation) {
		return artifactSnapshot{}, fmt.Errorf(
			"%w: digest target %q changed before securing or hashing",
			errArtifactChanged,
			path,
		)
	}
	if err := rejectMultipleLinks(preOperation, path); err != nil {
		return artifactSnapshot{}, err
	}
	// Chmod through the opened descriptor so a concurrent pathname replacement
	// cannot redirect the permission change. Windows maps this to its writable
	// attribute rather than Unix permission bits, so exact 0600 verification is
	// meaningful only on non-Windows systems.
	if secure {
		if err := f.Chmod(privateFileMode); err != nil {
			return artifactSnapshot{}, fmt.Errorf(
				"set managed artifact permissions on %q: %w",
				path,
				err,
			)
		}
	}
	verifyPrivateMode := secure && runtime.GOOS != "windows"

	before, err := f.Stat()
	if err != nil {
		return artifactSnapshot{}, fmt.Errorf(
			"inspect opened artifact before SHA-256 %q: %w",
			path,
			err,
		)
	}
	if !before.Mode().IsRegular() || !os.SameFile(openedInfo, before) {
		return artifactSnapshot{}, fmt.Errorf(
			"%w: digest target %q changed before hashing",
			errArtifactChanged,
			path,
		)
	}
	if err := rejectMultipleLinks(before, path); err != nil {
		return artifactSnapshot{}, err
	}
	if verifyPrivateMode && before.Mode().Perm() != privateFileMode {
		return artifactSnapshot{}, fmt.Errorf(
			"managed artifact %q has permissions %s after securing, want %s",
			path,
			before.Mode().Perm(),
			os.FileMode(privateFileMode),
		)
	}

	hash := sha256.New()
	written, copyErr := io.Copy(hash, f)
	if copyErr != nil {
		return artifactSnapshot{}, fmt.Errorf(
			"hash artifact %q: %w",
			path,
			copyErr,
		)
	}
	after, err := f.Stat()
	if err != nil {
		return artifactSnapshot{}, fmt.Errorf(
			"inspect opened artifact after SHA-256 %q: %w",
			path,
			err,
		)
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) ||
		after.Size() != before.Size() ||
		after.Mode() != before.Mode() ||
		!after.ModTime().Equal(before.ModTime()) ||
		written != before.Size() {
		return artifactSnapshot{}, fmt.Errorf(
			"%w: hash artifact %q: file changed while reading "+
				"(before size %d, after size %d, read %d)",
			errArtifactChanged,
			path,
			before.Size(),
			after.Size(),
			written,
		)
	}
	if err := rejectMultipleLinks(after, path); err != nil {
		return artifactSnapshot{}, errors.Join(
			errArtifactChanged,
			err,
		)
	}

	current, err := os.Lstat(path)
	if err != nil {
		return artifactSnapshot{}, errors.Join(
			errArtifactChanged,
			fmt.Errorf("revalidate hashed artifact %q: %w", path, err),
		)
	}
	if current.Mode()&os.ModeSymlink != 0 {
		return artifactSnapshot{}, fmt.Errorf(
			"%w: %w: digest target %q changed to a symbolic link",
			errArtifactChanged,
			ErrSymlink,
			path,
		)
	}
	if err := rejectMultipleLinks(current, path); err != nil {
		return artifactSnapshot{}, errors.Join(
			errArtifactChanged,
			err,
		)
	}
	if !current.Mode().IsRegular() || !os.SameFile(after, current) ||
		current.Size() != after.Size() ||
		current.Mode() != after.Mode() ||
		!current.ModTime().Equal(after.ModTime()) {
		return artifactSnapshot{}, fmt.Errorf(
			"%w: digest target %q changed during hashing",
			errArtifactChanged,
			path,
		)
	}
	if verifyPrivateMode && current.Mode().Perm() != privateFileMode {
		return artifactSnapshot{}, fmt.Errorf(
			"managed artifact %q permissions changed during hashing",
			path,
		)
	}
	return artifactSnapshot{
		path:     path,
		identity: current,
		size:     written,
		mode:     current.Mode(),
		modified: current.ModTime(),
		digest:   hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func wrapHashedFileCloseError(err error, path string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("close hashed artifact %q: %w", path, err)
}

// Inventory returns existing managed files in lexical name order, excluding
// manifest.json. Missing managed artifacts are omitted.
func (l *Layout) Inventory() ([]ManifestEntry, error) {
	inventory, err := l.buildManifestInventory()
	if err != nil {
		return nil, err
	}
	return append([]ManifestEntry(nil), inventory.manifest.Files...), nil
}

func (l *Layout) buildManifestInventory() (*manifestInventory, error) {
	if err := l.prepareDirectory(); err != nil {
		return nil, err
	}

	names := ManagedNames()
	entries := make([]ManifestEntry, 0, len(names)-1)
	snapshots := make([]artifactSnapshot, 0, len(names)-1)
	absent := make([]absentArtifact, 0, len(names)-1)
	for _, name := range names {
		if name == ManifestJSON {
			continue
		}
		path, err := l.Path(name)
		if err != nil {
			return nil, err
		}
		if err := rejectSymlinkComponents(l.OutputDir, path); err != nil {
			return nil, err
		}
		snapshot, err := snapshotFile(path, true)
		if errors.Is(err, os.ErrNotExist) &&
			!errors.Is(err, errArtifactChanged) {
			absent = append(absent, absentArtifact{
				name: name,
				path: path,
			})
			continue
		}
		if err != nil {
			return nil, err
		}
		snapshot.name = name
		snapshots = append(snapshots, snapshot)
		mediaType, role, sensitive := manifestMetadata(name)
		entries = append(entries, ManifestEntry{
			Name:      string(name),
			Size:      snapshot.size,
			SHA256:    snapshot.digest,
			MediaType: mediaType,
			Role:      role,
			Sensitive: sensitive,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].name < snapshots[j].name
	})
	sort.Slice(absent, func(i, j int) bool {
		return absent[i].name < absent[j].name
	})
	return &manifestInventory{
		manifest: Manifest{
			Version:   ManifestVersion,
			Algorithm: DigestAlgorithm,
			Files:     entries,
		},
		snapshots: snapshots,
		absent:    absent,
	}, nil
}

func manifestMetadata(name Name) (string, string, bool) {
	switch name {
	case CoverageProfile:
		return "text/plain; charset=utf-8", roleEvidence, true
	case StderrLog:
		return "text/plain; charset=utf-8", roleEvidence, true
	case RunJSON:
		return "application/json", roleEvidence, true
	case TestOutputJSONL:
		return "application/x-ndjson", roleEvidence, true
	case CoverageHTML, TestOutputHTML:
		return "text/html; charset=utf-8", roleReport, true
	case IndexHTML:
		return "text/html; charset=utf-8", roleIndex, true
	case JUnitXML:
		return "application/xml", roleReport, true
	case SummaryJSON, ManifestJSON:
		return "application/json", roleReport, true
	default:
		return "application/octet-stream", roleReport, true
	}
}

func (l *Layout) revalidateManifestInventory(
	inventory *manifestInventory,
) error {
	if inventory == nil {
		return errors.New("revalidate artifact manifest: inventory is nil")
	}
	if len(inventory.manifest.Files) != len(inventory.snapshots) {
		return fmt.Errorf(
			"revalidate artifact manifest: %d entries have %d snapshots",
			len(inventory.manifest.Files),
			len(inventory.snapshots),
		)
	}

	for i, expected := range inventory.snapshots {
		entry := inventory.manifest.Files[i]
		if entry.Name != string(expected.name) ||
			entry.Size != expected.size ||
			entry.SHA256 != expected.digest {
			return fmt.Errorf(
				"revalidate artifact manifest: entry %d does not match "+
					"its inventory snapshot",
				i,
			)
		}
		if err := rejectSymlinkComponents(l.OutputDir, expected.path); err != nil {
			return errors.Join(
				errArtifactChanged,
				fmt.Errorf(
					"revalidate manifest artifact %q: %w",
					expected.path,
					err,
				),
			)
		}
		current, err := snapshotFile(expected.path, false)
		if err != nil {
			return errors.Join(
				errArtifactChanged,
				fmt.Errorf(
					"revalidate manifest artifact %q: %w",
					expected.path,
					err,
				),
			)
		}

		reason := changedSnapshotField(expected, current)
		if reason != "" {
			return fmt.Errorf(
				"%w: manifest artifact %q changed %s after inventory",
				errArtifactChanged,
				expected.path,
				reason,
			)
		}
	}
	for _, expected := range inventory.absent {
		if err := rejectSymlinkComponents(l.OutputDir, expected.path); err != nil {
			return errors.Join(
				errArtifactChanged,
				fmt.Errorf(
					"revalidate missing manifest artifact %q: %w",
					expected.path,
					err,
				),
			)
		}
		_, err := os.Lstat(expected.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return errors.Join(
				errArtifactChanged,
				fmt.Errorf(
					"revalidate missing manifest artifact %q: %w",
					expected.path,
					err,
				),
			)
		}
		return fmt.Errorf(
			"%w: manifest artifact %q appeared after inventory",
			errArtifactChanged,
			expected.path,
		)
	}
	return nil
}

func changedSnapshotField(
	expected artifactSnapshot,
	current artifactSnapshot,
) string {
	switch {
	case expected.identity == nil || current.identity == nil:
		return "identity"
	case !os.SameFile(expected.identity, current.identity):
		return "identity"
	case expected.size != current.size:
		return "size"
	case expected.mode != current.mode:
		return "mode"
	case !expected.modified.Equal(current.modified):
		return "modification time"
	case expected.digest != current.digest:
		return "contents"
	default:
		return ""
	}
}

func cloneManifest(manifest Manifest) *Manifest {
	manifest.Files = append([]ManifestEntry(nil), manifest.Files...)
	return &manifest
}

// BuildManifest computes a deterministic manifest without writing it.
func (l *Layout) BuildManifest() (*Manifest, error) {
	inventory, err := l.buildManifestInventory()
	if err != nil {
		return nil, err
	}
	return cloneManifest(inventory.manifest), nil
}

// WriteManifest computes and atomically writes manifest.json.
func (l *Layout) WriteManifest() (*Manifest, error) {
	return l.WriteManifestValidated(nil)
}

// WriteManifestValidated computes and atomically writes manifest.json after
// validate accepts the exact inventory that will be published. The inventory
// is revalidated again immediately before the atomic replacement, so callers
// can enforce cross-file contracts without introducing a gap between their
// validation and manifest publication.
func (l *Layout) WriteManifestValidated(
	validate func([]ManifestEntry) error,
) (*Manifest, error) {
	inventory, err := l.buildManifestInventory()
	if err != nil {
		return nil, err
	}
	return l.publishManifest(inventory, validate)
}

func (l *Layout) publishManifest(
	inventory *manifestInventory,
	validate func([]ManifestEntry) error,
) (*Manifest, error) {
	if inventory == nil {
		return nil, errors.New("publish artifact manifest: inventory is nil")
	}
	data, err := json.MarshalIndent(&inventory.manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode artifact manifest: %w", err)
	}
	data = append(data, '\n')
	if err := l.writeAtomic(
		ManifestJSON,
		func(w io.Writer) error {
			_, err := w.Write(data)
			return err
		},
		func() error {
			if validate != nil {
				entries := append(
					[]ManifestEntry(nil),
					inventory.manifest.Files...,
				)
				if err := validate(entries); err != nil {
					return fmt.Errorf(
						"validate manifest inventory contract: %w",
						err,
					)
				}
			}
			return l.revalidateManifestInventory(inventory)
		},
	); err != nil {
		return nil, err
	}
	return cloneManifest(inventory.manifest), nil
}
