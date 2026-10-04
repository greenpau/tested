#!/bin/sh
# Publish only the release branch and its exact annotated version tag.
set -eu

cd "$(dirname "$0")/.."
fail() { printf 'Release stopped: %s\n' "$*" >&2; exit 1; }
kind=${1:-patch}
case "$kind" in patch|minor|check) ;; *) fail 'expected patch, minor, or check' ;; esac
skip_tests=false
if [ "$#" -gt 1 ]; then
    [ "$#" -eq 2 ] && [ "$2" = --skip-tests ] && [ "$kind" != check ] ||
        fail 'usage: release.sh [patch|minor [--skip-tests]|check]'
    skip_tests=true
fi

release_branch=${RELEASE_BRANCH:-main}
release_remote=${RELEASE_REMOTE:-origin}
versioned=${VERSIONED:-versioned}
git check-ref-format "refs/heads/$release_branch"
git_dir=$(git rev-parse --git-path tested-release.lock)
mkdir "$git_dir" 2>/dev/null || fail 'another release is running (or a stale .git/tested-release.lock needs review)'
probe_dir=
cleanup() {
    if [ -n "$probe_dir" ]; then
        rm -f "$probe_dir/VERSION"
        rmdir "$probe_dir"
    fi
    rmdir "$git_dir"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

clean_branch() {
    branch=$(git symbolic-ref --quiet --short HEAD) || fail 'detached HEAD'
    [ "$branch" = "$release_branch" ] || fail "releases require $release_branch"
    status=$(git status --porcelain --untracked-files=all)
    [ -z "$status" ] || fail 'working tree and index must be clean'
}
clean_branch
original_head=$(git rev-parse HEAD)
current=$(go run ./scripts/releaseversion check)
bump=$kind
[ "$bump" != check ] || bump=patch
next=$(go run ./scripts/releaseversion next "$bump")
tag="v$next"

command -v "$versioned" >/dev/null 2>&1 || fail 'release requires versioned 1.0.36; install github.com/greenpau/versioned/cmd/versioned@v1.0.36'
banner=$("$versioned" -version)
case "$banner" in 'versioned 1.0.36,'*) ;; *) fail 'release requires versioned 1.0.36' ;; esac

# Inspect the actual push destination, including an explicitly configured pushurl.
# Multiple push destinations cannot provide an atomic release across repositories.
push_url=$(git remote get-url --push --all "$release_remote")
[ -n "$push_url" ] || fail 'release remote has no push URL'
case "$push_url" in *'
'*) fail 'release requires exactly one push URL' ;; esac

# A previous failed push leaves a valid local release commit/tag. Do not hide
# that failure by bumping again; require the operator to recover that release.
if local_current=$(git rev-parse --verify --quiet "refs/tags/v$current"); then
    remote_current=$(git ls-remote --tags "$push_url" "refs/tags/v$current")
    remote_current=$(printf '%s\n' "$remote_current" | cut -f1)
    [ "$local_current" = "$remote_current" ] || fail "current release v$current is not published with the same tag; recover it before releasing again"
elif [ "$(git log -1 --format=%s)" = "ops: release v$current" ]; then
    fail "current release v$current has no local tag; recover it before releasing again"
fi
available_tag() {
    if git show-ref --verify --quiet "refs/tags/$tag"; then
        fail "tag $tag already exists locally"
    else
        code=$?
        [ "$code" -eq 1 ] || exit "$code"
    fi
    remote_tag=$(git ls-remote --tags "$push_url" "refs/tags/$tag")
    [ -z "$remote_tag" ] || fail "tag $tag already exists on $release_remote"
}
current_remote() {
    git fetch --no-tags "$push_url" "refs/heads/$release_branch"
    git merge-base --is-ancestor FETCH_HEAD HEAD || fail "$release_branch is behind or diverged from $release_remote/$release_branch"
}
available_tag
current_remote

# Probe the pinned tool outside the worktree and verify its exact result before
# changing VERSION. A failed or unexpected increment never reaches the index.
probe_dir=$(mktemp -d "${TMPDIR:-/tmp}/tested-release.XXXXXX")
cp VERSION "$probe_dir/VERSION"
"$versioned" -source "$probe_dir/VERSION" "-$bump" -silent
go run ./scripts/releaseversion -file "$probe_dir/VERSION" check "$tag" >/dev/null
clean_branch
[ "$(git rev-parse HEAD)" = "$original_head" ] || fail 'HEAD changed during release checks'
[ "$kind" != check ] || { printf 'Release checks passed: %s is available\n' "$tag"; exit 0; }

target=release
[ "$kind" != minor ] || target=minor-release
if [ "$skip_tests" = true ]; then
    target="fast-$target"
    printf 'Skipping local release-check; tagged CI validation still runs.\n'
    tests='Tests: local release-check skipped explicitly; tagged CI must pass.'
else
    "${MAKE:-make}" release-check
    tests='Tests: make release-check passed before the version update.'
fi

# Recheck after the gate; its side effects or an intervening remote release must
# never be folded into this release's version-only commit.
clean_branch
[ "$(git rev-parse HEAD)" = "$original_head" ] || fail 'HEAD changed during release checks'
[ "$(go run ./scripts/releaseversion check)" = "$current" ] || fail 'VERSION changed during release checks'
available_tag
current_remote
cp "$probe_dir/VERSION" VERSION
staged=$(git diff --cached --name-only)
changed=$(git diff --name-only)
untracked=$(git ls-files --others --exclude-standard)
[ -z "$staged" ] && [ "$changed" = VERSION ] && [ -z "$untracked" ] || fail 'version update changed files other than VERSION'
git add -- VERSION
git commit \
    -m "ops: release $tag" \
    -m "Before this commit: VERSION identified the previous tested release." \
    -m "After this commit: VERSION identifies $tag." \
    -m "$tests" \
    -m "More info: make $target generated this version-only release commit."
clean_branch
[ "$(git rev-parse HEAD^)" = "$original_head" ] || fail 'unexpected release commit parent'
[ "$(git diff-tree --no-commit-id --name-only -r HEAD)" = VERSION ] || fail 'release commit changed files other than VERSION'
go run ./scripts/releaseversion check "$tag" >/dev/null
git tag -a "$tag" -m "$tag"
git push --atomic --no-follow-tags "$push_url" \
    "HEAD:refs/heads/$release_branch" "refs/tags/$tag:refs/tags/$tag"
printf 'Published %s to %s\n' "$tag" "$release_remote"
