#!/bin/sh
# Build or install tested with provenance collected at build time.
set -eu

operation=${1:?expected info, build, or install}
version=${2:?expected release version}
case "$operation" in
    info|build|install) ;;
    *) echo "unsupported build operation: $operation" >&2; exit 1 ;;
esac

git_branch=unknown
git_commit=unknown
if git rev-parse --git-dir >/dev/null 2>&1; then
    git_commit=$(git rev-parse --verify HEAD)
    if git_branch=$(git symbolic-ref --quiet --short HEAD); then
        :
    else
        git_status_code=$?
        [ "$git_status_code" -eq 1 ] || exit "$git_status_code"
        git_branch=detached
    fi
    git_status=$(git status --porcelain --untracked-files=normal)
    if [ -n "$git_status" ]; then
        git_commit="$git_commit-dirty"
    fi
fi
build_user=$(id -un)
build_date=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

if [ "$operation" = info ]; then
    printf 'Version: %s, Branch: %s, Revision: %s\n' \
        "$version" "$git_branch" "$git_commit"
    printf 'Build on %s by %s\n' "$build_date" "$build_user"
    exit 0
fi

# Go's -ldflags parser does not perform shell expansion or unescaping.
# Quotes inside an unquoted argument are literal, so Git branch names (which
# cannot contain whitespace) are safe even when they contain both quote kinds.
stamp() {
    assignment="main.$1=$2"
    case "$assignment" in
        *[[:space:]]*)
            case "$assignment" in
                *"'"*)
                    case "$assignment" in
                        *'"'*) echo 'cannot quote build metadata containing whitespace and both quote kinds' >&2; return 1 ;;
                    esac
                    printf ' -X "%s"' "$assignment"
                    ;;
                *) printf " -X '%s'" "$assignment" ;;
            esac
            ;;
        *) printf ' -X %s' "$assignment" ;;
    esac
}

ldflags="-s -w$(stamp appVersion "$version")"
ldflags="$ldflags$(stamp gitBranch "$git_branch")"
ldflags="$ldflags$(stamp gitCommit "$git_commit")"
ldflags="$ldflags$(stamp buildUser "$build_user")"
ldflags="$ldflags$(stamp buildDate "$build_date")"

if [ "$operation" = install ]; then
    # Let Go own GOBIN/GOPATH selection and platform-specific executable names.
    CGO_ENABLED=0 go install -trimpath -ldflags="$ldflags" .
    binary=$(go list -f '{{.Target}}' .)
    printf 'Installed %s\n' "$binary"
else
    binary=${3:?expected output binary path}
    CGO_ENABLED=0 go build -trimpath -o "$binary" -ldflags="$ldflags" .
fi
"$binary" version
"$binary" help >/dev/null
