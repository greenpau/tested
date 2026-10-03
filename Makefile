APP_VERSION:=$(shell awk 'NR == 1 && $$0 ~ /^[0-9]+\.[0-9]+\.[0-9]+$$/ { print; exit }' VERSION)
BINARY:=./bin/tested
COVERAGE_DIR:=./.coverage
TEST_DIR:=./...
RELEASE_BRANCH?=main
RELEASE_REMOTE?=origin
VERSIONED?=versioned
VERSIONED_VERSION:=1.0.36

ifeq ($(APP_VERSION),)
$(error VERSION must contain a major.minor.patch release version)
endif

.PHONY: all
all: info build
	@echo "$@: complete"

.PHONY: info
info:
	@git_branch="$$(git symbolic-ref --quiet --short HEAD 2>/dev/null || \
		printf '%s' unknown)"; \
	git_commit="$$(git describe --dirty --always 2>/dev/null || \
		printf '%s' unknown)"; \
	build_user="$$(whoami)"; \
	build_date="$$(date -u +"%Y-%m-%dT%H:%M:%SZ")"; \
	printf 'Version: %s, Branch: %s, Revision: %s\n' \
		'$(APP_VERSION)' "$$git_branch" "$$git_commit"; \
	printf 'Build on %s by %s\n' "$$build_date" "$$build_user"

.PHONY: version-check
version-check:
	@version_lines="$$(awk 'END { print NR }' VERSION)"; \
	version="$$(awk 'NR == 1 { sub(/\r$$/, ""); print }' VERSION)"; \
	if [ "$$version_lines" -ne 1 ] || \
		! printf '%s\n' "$$version" | \
			grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$$'; then \
		echo "VERSION must contain exactly one major.minor.patch line"; \
		exit 1; \
	fi

.PHONY: skills-check
skills-check:
	@go run ./scripts/skillcheck -root .

.PHONY: build
build:
	@mkdir -p ./bin
	@rm -f $(BINARY)
	@git_branch="$$(git symbolic-ref --quiet --short HEAD 2>/dev/null || \
		printf '%s' unknown)"; \
	git_commit="$$(git describe --dirty --always 2>/dev/null || \
		printf '%s' unknown)"; \
	build_user="$$(whoami)"; \
	build_date="$$(date -u +"%Y-%m-%dT%H:%M:%SZ")"; \
	CGO_ENABLED=0 go build -trimpath -o $(BINARY) \
		-ldflags="-s -w \
		-X main.appVersion=$(APP_VERSION) \
		-X main.gitBranch=$$git_branch \
		-X main.gitCommit=$$git_commit \
		-X main.buildUser=$$build_user \
		-X main.buildDate=$$build_date" \
		.
	@$(BINARY) version
	@$(BINARY) help >/dev/null
	@echo "$@: complete"

.PHONY: linter
linter:
	@echo "Running formatting and vet checks"
	@files="$$(gofmt -l $$(find . -type f -name '*.go' \
		-not -path './.git/*' -not -path './.coverage/*' \
		-not -path './bin/*' -not -path './dist/*'))"; \
	if [ -n "$$files" ]; then \
		echo "The following Go files require gofmt:"; \
		echo "$$files"; \
		exit 1; \
	fi
	@go vet $(TEST_DIR)
	@echo "$@: complete"

.PHONY: run-tests
run-tests:
	@echo "$@: started"
	@go test -v -p 1 -count=1 $(TEST_DIR)
	@echo "$@: complete"

.PHONY: run-race-tests
run-race-tests:
	@echo "$@: started"
	@go test -v -p 1 -race -count=1 $(TEST_DIR)
	@echo "$@: complete"

.PHONY: run-shuffle-tests
run-shuffle-tests:
	@echo "$@: started"
	@go test -v -p 1 -shuffle=on -count=3 $(TEST_DIR)
	@echo "$@: complete"

.PHONY: self-test
self-test: build
	@echo "$@: started"
	@rm -rf $(COVERAGE_DIR)
	@$(BINARY) run -C . -o $(COVERAGE_DIR) \
		--minimum-coverage 1 --coverage-diff-base HEAD \
		-- -count=1 $(TEST_DIR)
	@go run ./scripts/bundlecheck \
		-dir $(COVERAGE_DIR) -coverage=true
	@echo "$@: complete"

.PHONY: test
test:
	@rm -rf $(COVERAGE_DIR)
	@$(MAKE) linter run-tests run-race-tests run-shuffle-tests
	@$(MAKE) self-test
	@echo "$@: complete"

.PHONY: e2e
e2e: build
	@echo "$@: started"
	@rm -rf ./testdata/fixture/.coverage
	@$(BINARY) run -C ./testdata/fixture --minimum-coverage 1 -- -count=2 ./...
	@go run ./scripts/bundlecheck \
		-dir ./testdata/fixture/.coverage
	@mkdir -p ./tmp/e2e-rerender
	@for name in coverage.html index.html junit.xml manifest.json \
		summary.json test_output.html; do \
		cp "./testdata/fixture/.coverage/$$name" \
			"./tmp/e2e-rerender/$$name"; \
	done
	@$(BINARY) report -C ./testdata/fixture
	@for name in coverage.html index.html junit.xml manifest.json \
		summary.json test_output.html; do \
		if ! cmp -s "./tmp/e2e-rerender/$$name" \
			"./testdata/fixture/.coverage/$$name"; then \
			echo "offline rerender changed $$name"; \
			exit 1; \
		fi; \
	done
	@go run ./scripts/bundlecheck \
		-dir ./testdata/fixture/.coverage
	@for name in coverage.html index.html junit.xml manifest.json \
		summary.json test_output.html; do \
		rm -f "./tmp/e2e-rerender/$$name"; \
	done
	@rmdir ./tmp/e2e-rerender
	@$(MAKE) e2e-go126-metadata
	@set +e; \
	TESTED_FIXTURE_FAIL=1 $(BINARY) run -C ./testdata/fixture -- -count=1 ./...; \
	code="$$?"; \
	set -e; \
	if [ "$$code" -ne 1 ]; then \
		echo "expected controlled fixture failure to exit 1, got $$code"; \
		exit 1; \
	fi
	@set +e; \
	$(BINARY) run -C ./testdata/buildfail -- -count=1 ./...; \
	code="$$?"; \
	set -e; \
	if [ "$$code" -ne 1 ]; then \
		echo "expected build fixture failure to exit 1, got $$code"; \
		exit 1; \
	fi
	@test -s ./testdata/buildfail/.coverage/test_output.jsonl
	@test -s ./testdata/buildfail/.coverage/test_output.html
	@set -eu; \
	go_version="$$(go env GOVERSION)"; \
	go_minor="$$(printf '%s\n' "$$go_version" | \
		sed -n 's/^go1\.\([0-9][0-9]*\).*/\1/p')"; \
	if [ -n "$$go_minor" ] && [ "$$go_minor" -ge 26 ]; then \
		grep -q '"Action":"build-fail"' \
			./testdata/buildfail/.coverage/test_output.jsonl; \
		grep -q '"ImportPath":' \
			./testdata/buildfail/.coverage/test_output.jsonl; \
	else \
		echo "e2e: Go 1.26 BuildEvent assertions skipped on $$go_version"; \
	fi
	@rm -rf ./testdata/fixture/.coverage
	@$(BINARY) run -C ./testdata/fixture --no-coverage -- -count=1 ./...
	@go run ./scripts/bundlecheck \
		-dir ./testdata/fixture/.coverage -coverage=false
	@TESTED_FIXTURE_LARGE=1 $(BINARY) run -C ./testdata/fixture \
		--max-test-output-bytes 1024 -- -count=1 ./...
	@grep -q '"output_truncated":true' ./testdata/fixture/.coverage/summary.json
	@$(BINARY) report -C ./testdata/fixture
	@$(BINARY) run -C ./testdata/fixture --no-coverage --quiet -- \
		-run '^$$' -bench '^BenchmarkSum$$' -benchtime=1x -cpu 1,2 -count=2
	@grep -q '"benchmarked":4' ./testdata/fixture/.coverage/summary.json
	@! grep -q '"incomplete":' ./testdata/fixture/.coverage/summary.json
	@$(BINARY) run -C ./testdata/fixture --no-coverage --quiet -- \
		-run '^$$' -bench '^BenchmarkZeroMetric$$' -benchtime=1x -cpu 1
	@grep -q '"benchmarked":1' ./testdata/fixture/.coverage/summary.json
	@! grep -q '"incomplete":' ./testdata/fixture/.coverage/summary.json
	@echo "$@: complete"

.PHONY: e2e-browser
e2e-browser: build
	@npm --prefix scripts/browser test

.PHONY: e2e-go126-metadata
e2e-go126-metadata:
	@set -eu; \
	go_version="$$(go env GOVERSION)"; \
	go_minor="$$(printf '%s\n' "$$go_version" | \
		sed -n 's/^go1\.\([0-9][0-9]*\).*/\1/p')"; \
	if [ -z "$$go_minor" ] || [ "$$go_minor" -lt 26 ]; then \
		echo "e2e: Go 1.26 metadata assertions skipped on $$go_version"; \
		exit 0; \
	fi; \
	coverage_dir=./testdata/fixture/.coverage; \
	artifact_dir="$$coverage_dir/go-artifacts-fixture-secret"; \
	rm -rf ./testdata/fixture/.coverage; \
	mkdir -m 0700 "$$coverage_dir"; \
	mkdir -m 0700 "$$artifact_dir"; \
	$(BINARY) run -C ./testdata/fixture --no-coverage \
		--redact 'fixture-secret' -- \
		-count=1 -run '^Test(Attribute|Artifact)Metadata$$' \
		-artifacts \
		-outputdir .coverage/go-artifacts-fixture-secret \
		.; \
	grep -q '"Action":"attr"' "$$coverage_dir/test_output.jsonl"; \
	grep -q '"Key":"tested.fixture"' "$$coverage_dir/test_output.jsonl"; \
	grep -q '"Action":"artifacts"' "$$coverage_dir/test_output.jsonl"; \
	grep -q '"Path":' "$$coverage_dir/test_output.jsonl"; \
	grep -q 'fixture-secret' "$$coverage_dir/test_output.jsonl"; \
	grep -q '"attributes":' "$$coverage_dir/summary.json"; \
	grep -q '"artifacts":' "$$coverage_dir/summary.json"; \
	grep -Fq '[REDACTED]' "$$coverage_dir/summary.json"; \
	grep -q 'Occurrence metadata' "$$coverage_dir/test_output.html"; \
	grep -q 'Path (informational only)' "$$coverage_dir/test_output.html"; \
	grep -Fq '[REDACTED]' "$$coverage_dir/test_output.html"; \
	grep -q 'tested.attribute.1.key' "$$coverage_dir/junit.xml"; \
	grep -q 'tested.artifact.1.path' "$$coverage_dir/junit.xml"; \
	grep -Fq '[REDACTED]' "$$coverage_dir/junit.xml"; \
	for name in summary.json test_output.html junit.xml; do \
		if grep -q 'fixture-secret' "$$coverage_dir/$$name"; then \
			echo "$$name leaked fixture metadata"; \
			exit 1; \
		fi; \
	done; \
	if grep -Eq 'href="[^"]*(go-artifacts|REDACTED)' \
		"$$coverage_dir/test_output.html"; then \
		echo "test_output.html made an artifact path clickable"; \
		exit 1; \
	fi

.PHONY: coverage
coverage: test
	@echo "$@: complete"

.PHONY: cross-build
cross-build:
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...
	@CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...
	@CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build ./...
	@CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./...
	@for arch in amd64 386 arm64; do \
		CGO_ENABLED=0 GOOS=windows GOARCH=$$arch go build ./... || exit 1; \
		CGO_ENABLED=0 GOOS=windows GOARCH=$$arch \
			go test -run '^$$' -exec=true ./... || exit 1; \
	done
	@echo "$@: complete"

.PHONY: ci
ci:
	@$(MAKE) skills-check
	@$(MAKE) test
	@$(MAKE) e2e
	@$(MAKE) cross-build
	@echo "$@: complete"

.PHONY: release-check
release-check: version-check
	@status="$$(git status --porcelain --untracked-files=all)"; \
	if [ -n "$$status" ]; then \
		echo "release-check requires a clean worktree:"; \
		echo "$$status"; \
		exit 1; \
	fi
	@$(MAKE) ci
	@status="$$(git status --porcelain --untracked-files=all)"; \
	if [ -n "$$status" ]; then \
		echo "release-check modified the worktree:"; \
		echo "$$status"; \
		exit 1; \
	fi
	@echo "$@: complete"

.PHONY: release-git-check
release-git-check: version-check
	@echo "$@: started"
	@branch="$$(git symbolic-ref --quiet --short HEAD 2>/dev/null || true)"; \
	if [ "$$branch" != "$(RELEASE_BRANCH)" ]; then \
		echo "cannot release from branch '$$branch'; expected $(RELEASE_BRANCH)"; \
		exit 1; \
	fi
	@if ! command -v "$(VERSIONED)" >/dev/null 2>&1; then \
		echo "release requires versioned $(VERSIONED_VERSION)"; \
		echo "install it with:"; \
		echo "  go install github.com/greenpau/versioned/cmd/versioned@v$(VERSIONED_VERSION)"; \
		exit 1; \
	fi
	@versioned_banner="$$("$(VERSIONED)" -version 2>/dev/null)"; \
	case "$$versioned_banner" in \
		"versioned $(VERSIONED_VERSION),"*) ;; \
		*) \
			echo "release requires versioned $(VERSIONED_VERSION), got '$$versioned_banner'"; \
			exit 1; \
			;; \
	esac
	@if ! git remote get-url "$(RELEASE_REMOTE)" >/dev/null 2>&1; then \
		echo "release remote '$(RELEASE_REMOTE)' is not configured"; \
		exit 1; \
	fi
	@status="$$(git status --porcelain --untracked-files=all)"; \
	if [ -n "$$status" ]; then \
		echo "release requires a clean worktree:"; \
		echo "$$status"; \
		exit 1; \
	fi
	@probe="$$(mktemp "$${TMPDIR:-/tmp}/tested-release-version.XXXXXX")" || \
		exit 1; \
	trap 'rm -f "$$probe"' 0 1 2 15; \
	cp VERSION "$$probe"; \
	"$(VERSIONED)" -source "$$probe" -patch -silent; \
	version="$$(awk 'NR == 1 { sub(/\r$$/, ""); print; exit }' "$$probe")"; \
	tag="v$$version"; \
	if git show-ref --verify --quiet "refs/tags/$$tag"; then \
		echo "release tag $$tag already exists locally"; \
		exit 1; \
	fi; \
	if git ls-remote --exit-code --tags "$(RELEASE_REMOTE)" \
		"refs/tags/$$tag" >/dev/null 2>&1; then \
		echo "release tag $$tag already exists on $(RELEASE_REMOTE)"; \
		exit 1; \
	else \
		remote_status="$$?"; \
		if [ "$$remote_status" -ne 2 ]; then \
			echo "failed to inspect $$tag on $(RELEASE_REMOTE)"; \
			exit "$$remote_status"; \
		fi; \
	fi; \
	echo "release-git-check: $$tag is available"
	@echo "$@: complete"

.PHONY: release-update-version
release-update-version:
	@echo "$@: started"
	@previous="$$(awk 'NR == 1 { sub(/\r$$/, ""); print; exit }' VERSION)"; \
	"$(VERSIONED)" -patch; \
	version="$$(awk 'NR == 1 { sub(/\r$$/, ""); print; exit }' VERSION)"; \
	if [ "$$version" = "$$previous" ]; then \
		echo "versioned did not change VERSION"; \
		exit 1; \
	fi; \
	echo "release-update-version: $$previous -> $$version"
	@$(MAKE) version-check
	@git add -- VERSION
	@staged="$$(git diff --cached --name-only)"; \
	untracked="$$(git ls-files --others --exclude-standard)"; \
	if [ "$$staged" != "VERSION" ] || ! git diff --quiet -- || \
		[ -n "$$untracked" ]; then \
		echo "release version update changed files other than VERSION"; \
		git status --short; \
		exit 1; \
	fi
	@echo "$@: complete"

.PHONY: release-git-commit
release-git-commit:
	@echo "$@: started"
	@set -eu; \
	version="$$(awk 'NR == 1 { sub(/\r$$/, ""); print; exit }' VERSION)"; \
	tag="v$$version"; \
	subject="ops: release $$tag"; \
	if [ "$${#subject}" -ge 87 ]; then \
		echo "release commit subject is too long: $$subject"; \
		exit 1; \
	fi; \
	staged="$$(git diff --cached --name-only)"; \
	if [ "$$staged" != "VERSION" ]; then \
		echo "release commit must stage only VERSION"; \
		git status --short; \
		exit 1; \
	fi; \
	if git show-ref --verify --quiet "refs/tags/$$tag"; then \
		echo "release tag $$tag already exists locally"; \
		exit 1; \
	fi; \
	git commit \
		-m "$$subject" \
		-m "Before this commit: VERSION identified the current tested version." \
		-m "After this commit: VERSION identifies $$tag for tagged publication." \
		-m "Tests: make release-check passed before the version update." \
		-m "More info: make release generated this version-only release commit."; \
	git tag -a "$$tag" -m "$$tag"; \
	git push --atomic "$(RELEASE_REMOTE)" \
		"refs/heads/$(RELEASE_BRANCH):refs/heads/$(RELEASE_BRANCH)" \
		"refs/tags/$$tag:refs/tags/$$tag"; \
	echo "published $$tag to $(RELEASE_REMOTE)"; \
	echo "If retraction is necessary, review and run:"; \
	echo "  git push --delete $(RELEASE_REMOTE) $$tag"; \
	echo "  git tag --delete $$tag"; \
	echo "  go mod edit -retract $$tag"
	@echo "$@: complete"

.PHONY: release
release:
	@echo "$@: started"
	@$(MAKE) release-git-check
	@$(MAKE) release-check
	@$(MAKE) release-git-check
	@$(MAKE) release-update-version
	@$(MAKE) release-git-commit
	@echo "$@: complete"

.PHONY: docs
docs:
	@mkdir -p .doc
	@go doc -all > .doc/index.txt
	@echo "$@: complete"

.PHONY: mod-tidy
mod-tidy:
	@go mod tidy
	@go mod verify

.PHONY: clean
clean:
	@rm -rf .coverage
	@rm -rf ./bin
	@rm -rf ./dist
	@rm -rf .doc
	@rm -rf ./testdata/fixture/.coverage
	@rm -rf ./testdata/buildfail/.coverage
	@echo "$@: complete"
