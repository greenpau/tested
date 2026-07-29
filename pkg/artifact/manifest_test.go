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
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestSHA256File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact")
	data := []byte("artifact payload")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])

	got, size, err := SHA256File(path)
	if err != nil {
		t.Fatalf("SHA256File() error = %v", err)
	}
	if got != want || size != int64(len(data)) {
		t.Fatalf("SHA256File() = (%q, %d), want (%q, %d)", got, size, want, len(data))
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := SHA256File(path); err != nil {
			t.Fatalf("SHA256File(mode preservation) error = %v", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Fatalf("SHA256File() changed mode to %s, want 0644", got)
		}
	}

	if _, _, err := SHA256File(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("SHA256File(missing) error = %v, want os.ErrNotExist", err)
	}
	dir := t.TempDir()
	if _, _, err := SHA256File(dir); err == nil {
		t.Fatal("SHA256File(directory) error = nil, want error")
	}
}

func TestSHA256OpenedFileRejectsPathSwapWithoutChmoddingReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("renaming an open file and POSIX permission assertions are not portable to Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact")
	originalPath := filepath.Join(dir, "original")
	replacementPath := filepath.Join(dir, "replacement")
	if err := os.WriteFile(path, []byte("original payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacementPath, []byte("replacement payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(replacementPath, 0o644); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("close opened original: %v", err)
		}
	}()
	openedInfo, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, originalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacementPath, path); err != nil {
		t.Fatal(err)
	}

	if _, _, err := sha256OpenedFile(
		file,
		path,
		openedInfo,
		true,
	); !errors.Is(err, errArtifactChanged) {
		t.Fatalf(
			"sha256OpenedFile() error = %v, want errArtifactChanged",
			err,
		)
	}
	originalInfo, err := os.Stat(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := originalInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("opened original mode = %s, want 0600", got)
	}
	replacementInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := replacementInfo.Mode().Perm(); got != 0o644 {
		t.Fatalf("replacement mode = %s, want unchanged 0644", got)
	}
}

func TestSHA256FileRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, _, err := SHA256File(link); !errors.Is(err, ErrSymlink) {
		t.Fatalf("SHA256File(symlink) error = %v, want ErrSymlink", err)
	}
}

func TestInventoryAndWriteManifestAreDeterministic(t *testing.T) {
	layout := newLayout(t)
	if err := layout.prepareDirectory(); err != nil {
		t.Fatal(err)
	}
	files := map[Name]string{
		TestOutputJSONL: "jsonl evidence",
		CoverageProfile: "mode: set\n",
		IndexHTML:       "<html>index</html>",
	}
	for name, content := range files {
		path, err := layout.Path(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(layout.OutputDir, "unrelated"), []byte("ignore"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.ManifestJSON, []byte("old manifest"), 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := layout.Inventory()
	if err != nil {
		t.Fatalf("Inventory() error = %v", err)
	}
	second, err := layout.Inventory()
	if err != nil {
		t.Fatalf("Inventory() second error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("inventories differ:\nfirst: %#v\nsecond: %#v", first, second)
	}
	wantNames := []string{CoverageProfileName, IndexHTMLName, TestOutputJSONLName}
	if len(first) != len(wantNames) {
		t.Fatalf("len(Inventory()) = %d, want %d: %#v", len(first), len(wantNames), first)
	}
	for i, want := range wantNames {
		if first[i].Name != want {
			t.Errorf("Inventory()[%d].Name = %q, want %q", i, first[i].Name, want)
		}
		path, err := layout.Path(Name(want))
		if err != nil {
			t.Fatal(err)
		}
		assertPrivateFile(t, path)
		if first[i].MediaType == "" || first[i].Role == "" {
			t.Errorf("Inventory()[%d] lacks media metadata: %#v", i, first[i])
		}
		if !first[i].Sensitive {
			t.Errorf("Inventory()[%d] marks tested artifact as non-sensitive: %#v", i, first[i])
		}
		if Name(want) == CoverageProfile && (!first[i].Sensitive || first[i].Role != roleEvidence) {
			t.Errorf("raw coverage evidence metadata = %#v, want sensitive raw evidence", first[i])
		}
	}

	manifest, err := layout.WriteManifest()
	if err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}
	if manifest.Version != ManifestVersion || manifest.Algorithm != DigestAlgorithm {
		t.Fatalf("manifest header = %#v, want version %d and %q", manifest, ManifestVersion, DigestAlgorithm)
	}
	if !reflect.DeepEqual(manifest.Files, first) {
		t.Fatalf("manifest files = %#v, want %#v", manifest.Files, first)
	}
	assertPrivateFile(t, layout.ManifestJSON)

	data, err := os.ReadFile(layout.ManifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("manifest does not end with newline: %q", data)
	}
	var decoded Manifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if !reflect.DeepEqual(decoded, *manifest) {
		t.Fatalf("decoded manifest = %#v, want %#v", decoded, *manifest)
	}

	rebuilt, err := layout.BuildManifest()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rebuilt, manifest) {
		t.Fatalf("manifest includes itself or is unstable:\nfirst: %#v\nrebuilt: %#v", manifest, rebuilt)
	}
}

func TestWriteManifestValidatedRejectsInventoryContract(t *testing.T) {
	layout := newLayout(t)
	if err := layout.prepareDirectory(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		layout.TestOutputJSONL,
		[]byte("evidence\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	previous := []byte("previous manifest\n")
	if err := os.WriteFile(layout.ManifestJSON, previous, 0o600); err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("binding mismatch")
	called := false
	_, err := layout.WriteManifestValidated(
		func(entries []ManifestEntry) error {
			called = true
			if len(entries) != 1 ||
				entries[0].Name != TestOutputJSONLName {
				t.Fatalf("validator entries = %#v", entries)
			}
			entries[0].Name = "mutated-by-caller"
			return sentinel
		},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf(
			"WriteManifestValidated() error = %v, want %v",
			err,
			sentinel,
		)
	}
	if !called {
		t.Fatal("WriteManifestValidated() did not call validator")
	}
	got, err := os.ReadFile(layout.ManifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, previous) {
		t.Fatalf(
			"rejected validation replaced manifest: got %q, want %q",
			got,
			previous,
		)
	}
	assertNoArtifactTemps(t, layout.OutputDir)
}

func TestPublishManifestRejectsChangedArtifactSet(t *testing.T) {
	tests := []struct {
		name      string
		change    func(*testing.T, *Layout, artifactSnapshot)
		wantCause error
	}{
		{
			name: "same_object_contents_with_restored_metadata",
			change: func(
				t *testing.T,
				_ *Layout,
				expected artifactSnapshot,
			) {
				t.Helper()
				if err := os.WriteFile(
					expected.path,
					[]byte("omega"),
					expected.mode.Perm(),
				); err != nil {
					t.Fatal(err)
				}
				restoreSnapshotMetadata(t, expected.path, expected)
				assertSnapshotMetadata(t, expected.path, expected, true)

				digest, size, err := SHA256File(expected.path)
				if err != nil {
					t.Fatal(err)
				}
				if size != expected.size {
					t.Fatalf(
						"changed artifact size = %d, want preserved %d",
						size,
						expected.size,
					)
				}
				if digest == expected.digest {
					t.Fatal("changed artifact digest unexpectedly stayed equal")
				}
			},
		},
		{
			name: "same_contents_and_metadata_replacement",
			change: func(
				t *testing.T,
				layout *Layout,
				expected artifactSnapshot,
			) {
				t.Helper()
				originalPath := filepath.Join(
					layout.OutputDir,
					".inventoried-original",
				)
				replacementPath := filepath.Join(
					layout.OutputDir,
					".inventoried-replacement",
				)
				if err := os.WriteFile(
					replacementPath,
					[]byte("alpha"),
					expected.mode.Perm(),
				); err != nil {
					t.Fatal(err)
				}
				restoreSnapshotMetadata(t, replacementPath, expected)
				if err := os.Rename(expected.path, originalPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacementPath, expected.path); err != nil {
					t.Fatal(err)
				}
				assertSnapshotMetadata(t, expected.path, expected, false)

				digest, size, err := SHA256File(expected.path)
				if err != nil {
					t.Fatal(err)
				}
				if size != expected.size || digest != expected.digest {
					t.Fatalf(
						"replacement hash = (%q, %d), want (%q, %d)",
						digest,
						size,
						expected.digest,
						expected.size,
					)
				}
			},
		},
		{
			name: "inventoried_file_removed",
			change: func(
				t *testing.T,
				_ *Layout,
				expected artifactSnapshot,
			) {
				t.Helper()
				if err := os.Remove(expected.path); err != nil {
					t.Fatal(err)
				}
			},
			wantCause: os.ErrNotExist,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			layout := newLayout(t)
			if err := layout.prepareDirectory(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(
				layout.CoverageProfile,
				[]byte("alpha"),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(
				layout.IndexHTML,
				[]byte("later artifact"),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			oldManifest := []byte("previous manifest\n")
			if err := os.WriteFile(
				layout.ManifestJSON,
				oldManifest,
				0o600,
			); err != nil {
				t.Fatal(err)
			}

			inventory, err := layout.buildManifestInventory()
			if err != nil {
				t.Fatalf("buildManifestInventory() error = %v", err)
			}
			expected := inventorySnapshot(
				t,
				inventory,
				CoverageProfile,
			)
			test.change(t, layout, expected)

			if _, err := layout.publishManifest(
				inventory,
				nil,
			); !errors.Is(err, errArtifactChanged) {
				t.Fatalf(
					"publishManifest() error = %v, want "+
						"errArtifactChanged",
					err,
				)
			} else if test.wantCause != nil &&
				!errors.Is(err, test.wantCause) {
				t.Fatalf(
					"publishManifest() error = %v, want cause %v",
					err,
					test.wantCause,
				)
			}
			gotManifest, err := os.ReadFile(layout.ManifestJSON)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotManifest, oldManifest) {
				t.Fatalf(
					"manifest changed after rejected publication: "+
						"got %q, want %q",
					gotManifest,
					oldManifest,
				)
			}
			assertNoArtifactTemps(t, layout.OutputDir)
		})
	}
}

func TestWriteManifestOmitsArtifactsMissingThroughoutInventory(t *testing.T) {
	layout := newLayout(t)
	manifest, err := layout.WriteManifest()
	if err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}
	if len(manifest.Files) != 0 {
		t.Fatalf(
			"WriteManifest() files = %#v, want no initially missing files",
			manifest.Files,
		)
	}
	assertPrivateFile(t, layout.ManifestJSON)
}

func TestPublishManifestRejectsArtifactAppearingAfterInventory(t *testing.T) {
	layout := newLayout(t)
	if err := layout.prepareDirectory(); err != nil {
		t.Fatal(err)
	}
	oldManifest := []byte("previous manifest\n")
	if err := os.WriteFile(
		layout.ManifestJSON,
		oldManifest,
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	inventory, err := layout.buildManifestInventory()
	if err != nil {
		t.Fatalf("buildManifestInventory() error = %v", err)
	}
	if err := os.WriteFile(
		layout.CoverageProfile,
		[]byte("appeared"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := layout.publishManifest(
		inventory,
		nil,
	); !errors.Is(err, errArtifactChanged) {
		t.Fatalf(
			"publishManifest() error = %v, want errArtifactChanged",
			err,
		)
	}
	gotManifest, err := os.ReadFile(layout.ManifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotManifest, oldManifest) {
		t.Fatalf(
			"manifest changed after artifact appeared: got %q, want %q",
			gotManifest,
			oldManifest,
		)
	}
	assertNoArtifactTemps(t, layout.OutputDir)
}

func TestManifestMetadataMarksEveryArtifactSensitive(t *testing.T) {
	for _, name := range ManagedNames() {
		t.Run(string(name), func(t *testing.T) {
			mediaType, role, sensitive := manifestMetadata(name)
			if mediaType == "" || role == "" {
				t.Fatalf("manifestMetadata(%q) = (%q, %q, %t), want complete metadata", name, mediaType, role, sensitive)
			}
			if !sensitive {
				t.Fatalf("manifestMetadata(%q) sensitive = false, want true", name)
			}
		})
	}
}

func inventorySnapshot(
	t *testing.T,
	inventory *manifestInventory,
	name Name,
) artifactSnapshot {
	t.Helper()
	for _, snapshot := range inventory.snapshots {
		if snapshot.name == name {
			return snapshot
		}
	}
	t.Fatalf("inventory contains no snapshot for %q", name)
	return artifactSnapshot{}
}

func restoreSnapshotMetadata(
	t *testing.T,
	path string,
	expected artifactSnapshot,
) {
	t.Helper()
	if err := os.Chmod(path, expected.mode.Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(
		path,
		expected.modified,
		expected.modified,
	); err != nil {
		t.Fatal(err)
	}
}

func assertSnapshotMetadata(
	t *testing.T,
	path string,
	expected artifactSnapshot,
	wantSameIdentity bool,
) {
	t.Helper()
	current, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := os.SameFile(expected.identity, current); got != wantSameIdentity {
		t.Fatalf(
			"artifact identity match = %t, want %t",
			got,
			wantSameIdentity,
		)
	}
	if current.Size() != expected.size ||
		current.Mode() != expected.mode ||
		!current.ModTime().Equal(expected.modified) {
		t.Fatalf(
			"artifact metadata = (size=%d, mode=%s, mtime=%s), "+
				"want (size=%d, mode=%s, mtime=%s)",
			current.Size(),
			current.Mode(),
			current.ModTime(),
			expected.size,
			expected.mode,
			expected.modified,
		)
	}
}

func TestInventoryMarksEveryListedArtifactSensitive(t *testing.T) {
	layout := newLayout(t)
	if err := layout.prepareDirectory(); err != nil {
		t.Fatal(err)
	}
	wantEntries := 0
	for _, name := range ManagedNames() {
		if name == ManifestJSON {
			continue
		}
		path, err := layout.Path(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("potentially sensitive artifact"), 0o600); err != nil {
			t.Fatal(err)
		}
		wantEntries++
	}

	entries, err := layout.Inventory()
	if err != nil {
		t.Fatalf("Inventory() error = %v", err)
	}
	if len(entries) != wantEntries {
		t.Fatalf("Inventory() entries = %d, want %d", len(entries), wantEntries)
	}
	for _, entry := range entries {
		if !entry.Sensitive {
			t.Errorf("Inventory() entry %q sensitive = false, want true", entry.Name)
		}
	}
}

func TestInventoryRejectsManagedSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	layout := newLayout(t)
	if err := layout.prepareDirectory(); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, layout.IndexHTML); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := layout.Inventory(); !errors.Is(err, ErrSymlink) {
		t.Fatalf("Inventory() error = %v, want ErrSymlink", err)
	}
}

func TestInventoryRejectsManagedHardLinkBeforeChmodOrHash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX link counts and permission assertions are not portable to Windows")
	}
	layout := newLayout(t)
	if err := layout.prepareDirectory(); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside-report")
	const content = "outside report"
	if err := os.WriteFile(outside, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(outside, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, layout.SummaryJSON); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}

	if _, err := layout.Inventory(); !errors.Is(err, ErrHardLink) {
		t.Fatalf("Inventory() error = %v, want ErrHardLink", err)
	}
	data, readErr := os.ReadFile(outside)
	if readErr != nil || string(data) != content {
		t.Fatalf("outside report changed: data=%q err=%v", data, readErr)
	}
	info, statErr := os.Stat(outside)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("outside report mode = %s, want 0644", got)
	}
}
