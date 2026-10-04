APP_VERSION:=$(shell awk 'NR == 1 && $$0 ~ /^[0-9]+\.[0-9]+\.[0-9]+$$/ { print; exit }' VERSION)
BINARY:=./bin/tested
COVERAGE_DIR:=./.coverage
TEST_DIR:=./...
RELEASE_BRANCH?=main
RELEASE_REMOTE?=origin
VERSIONED?=versioned
export RELEASE_BRANCH RELEASE_REMOTE VERSIONED

ifeq ($(APP_VERSION),)
$(error VERSION must contain a major.minor.patch release version)
endif

.PHONY: all
all: info build
	@echo "$@: complete"

.PHONY: info
info: version-check
	@sh ./scripts/build.sh info '$(APP_VERSION)'

.PHONY: version-check
version-check:
	@go run ./scripts/releaseversion check

.PHONY: skills-check
skills-check:
	@go run ./scripts/skillcheck -root .

.PHONY: build
build: version-check
	@sh ./scripts/build.sh build '$(APP_VERSION)' '$(BINARY)'
	@echo "$@: complete"

.PHONY: install
install: version-check
	@sh ./scripts/build.sh install '$(APP_VERSION)'
	@echo "$@: complete"

.PHONY: e2e-version
e2e-version:
	@go test -v -count=1 -tags=integration -run '^TestVersionBuild' .

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
	@$(MAKE) e2e-version e2e-release
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
ci: version-check
	@$(MAKE) skills-check
	@$(MAKE) test
	@$(MAKE) e2e
	@$(MAKE) cross-build
	@echo "$@: complete"

.PHONY: release-check
release-check: version-check
	@set -eu; status="$$(git status --porcelain --untracked-files=all)"; \
	if [ -n "$$status" ]; then \
		echo "release-check requires a clean worktree:"; \
		echo "$$status"; \
		exit 1; \
	fi
	@$(MAKE) ci
	@set -eu; status="$$(git status --porcelain --untracked-files=all)"; \
	if [ -n "$$status" ]; then \
		echo "release-check modified the worktree:"; \
		echo "$$status"; \
		exit 1; \
	fi
	@echo "$@: complete"

# These targets publish only when explicitly requested by an operator.
.PHONY: release minor-release fast-release fast-minor-release release-git-check
release:
	@sh ./scripts/release.sh patch

minor-release:
	@sh ./scripts/release.sh minor

fast-release:
	@sh ./scripts/release.sh patch --skip-tests

fast-minor-release:
	@sh ./scripts/release.sh minor --skip-tests

release-git-check:
	@sh ./scripts/release.sh check

# Partial entry points cannot bypass the complete release workflow.
.PHONY: release-update-version release-git-commit
release-update-version release-git-commit:
	@echo "Use make release, minor-release, fast-release, or fast-minor-release." >&2
	@exit 1

.PHONY: e2e-release
e2e-release:
	@go test -v -count=1 -tags=integration -run '^TestReleaseWorkflow' ./scripts/releaseversion

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
