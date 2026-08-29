.DEFAULT_GOAL := help
.PHONY: help test test-report build lint verify clean verify-fresh-clone verify-fresh-clone-abort verify-nolint verify-lint-baseline verify-baseline-shallow-abort verify-license regression testhost-up testhost-down testhost-regen-host-key testhost-test

# Single pin for golangci-lint, shared by `lint` and `verify` (which
# bootstraps transitively via `lint`'s prerequisite). The recipe below
# downloads the official release binary into the git-ignored bin/tools/
# directory, checksum-verified, rather than building from source — building
# from source would pull golangci-lint's dependency graph into this module's
# go.sum for a tool this module doesn't otherwise need. Follows TAE-73's form.
GOLANGCI_LINT_VERSION := v2.11.4
GOLANGCI_LINT_DIR     := bin/tools
GOLANGCI_LINT_BIN     := $(GOLANGCI_LINT_DIR)/golangci-lint-$(GOLANGCI_LINT_VERSION)

# Expected sha256 of each release tarball, pinned here rather than fetched
# from the release's own checksums.txt at bootstrap time — that file is
# served from the same unauthenticated release as the binary, so comparing
# against it only catches transit corruption, not a substituted release.
# Regenerate when bumping GOLANGCI_LINT_VERSION:
#   curl -sSfL https://github.com/golangci/golangci-lint/releases/download/<ver>/golangci-lint-<vernum>-checksums.txt
GOLANGCI_LINT_SHA256_linux_amd64   := 200c5b7503f67b59a6743ccf32133026c174e272b930ee79aa2aa6f37aca7ef1
GOLANGCI_LINT_SHA256_linux_arm64   := 3bcfa2e6f3d32b2bf5cd75eaa876447507025e0303698633f722a05331988db4
GOLANGCI_LINT_SHA256_darwin_amd64  := c900d4048db75d1edfd550fd11cf6a9b3008e7caa8e119fcddbc700412d63e60
GOLANGCI_LINT_SHA256_darwin_arm64  := 02db2a2dae8b26812e53b0688a6f617e3ef1f489790e829ea22862cf76945675

# verify-nolint's exact suppression inventory (TAE-80's own record). Any
# issue that removes a suppression decrements the matching count here in the
# same change — a mismatch is the point, not a bug.
NOLINT_ERRCHECK_TAE82_WANT := 13
NOLINT_GOSEC_TAE22_WANT    := 2
NOLINT_GOSEC_TAE23_WANT    := 3
NOLINT_GOSEC_TAE42_WANT    := 4
NOLINT_GOSEC_CASE1_WANT    := 19
NOLINT_TOTAL_WANT          := 41

# verify-lint-baseline's negative control. BASELINE_EXPECT is a function of
# BOTH BASELINE_REF and this file's *current* enabled linter set, not of the
# ref alone: the target copies the working tree's .golangci.yml into the
# baseline clone before linting it. Any later change to the enabled set (for
# example TAE-83 adding depguard) re-scores the baseline tree and this
# number goes stale — update it in the same change that changes the set,
# the same way verify-nolint's expected counts move with their owning issues.
BASELINE_REF    ?= e11985e
BASELINE_EXPECT ?= 170

# verify-fresh-clone-abort's negative control (TAE-92). The Makefile the
# control copies into its non-repository test directory and runs
# verify-fresh-clone from. Default is the working tree's; point it at a
# historical Makefile (e.g. `git show 321273b^:Makefile`) to re-prove the
# control bites on that recipe, without a one-off transcript.
FRESH_CLONE_ABORT_MAKEFILE ?= Makefile

# verify-license's reference (TAE-93). LICENSE_CANONICAL_SHA256 is the
# sha256 of https://www.apache.org/licenses/LICENSE-2.0.txt as published; the
# repo's LICENSE differs from it only on the appendix copyright line, which
# the target restores to the placeholder before hashing. Regenerate with:
#   curl -sSfL https://www.apache.org/licenses/LICENSE-2.0.txt | sha256sum
LICENSE_CANONICAL_SHA256 := cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30
LICENSE_COPYRIGHT        := Copyright 2026 Cloud Infra Consult SRL
LICENSE_PLACEHOLDER      := Copyright [yyyy] [name of copyright owner]

# Regression-only targets. verify's closing line and regression's run list
# share this one list so they cannot drift apart — the standing rule "every
# automated target added later joins regression" becomes a one-variable
# edit (TAE-83's guard is exactly that edit).
REGRESSION_CONTROLS := verify-nolint verify-lint-baseline verify-baseline-shallow-abort verify-license verify-fresh-clone verify-fresh-clone-abort

# TAE-98 test host fixture. Deliberately NOT added to REGRESSION_CONTROLS:
# joining would make every machine's `regression` require rootless Podman
# and start containers on each invocation (D4) — the integration tier is
# TAE-99's to design. The `testhost` Go build tag on
# testhost_container_test.go keeps `make test`/`verify`/`regression` from
# ever compiling the harness.
TESTHOST_DIR       := test/testhost
TESTHOST_KEYS_DIR  := $(TESTHOST_DIR)/.keys
TESTHOST_IMAGE     := fleetdesk-testhost
TESTHOST_CONTAINER := fleetdesk-testhost
TESTHOST_PORT      := 2222

##@ General

help: ## List targets, grouped
	@awk 'BEGIN {FS = ":.*?## "} \
		/^##@/ {printf "\n%s\n", substr($$0, 5)} \
		/^[a-zA-Z_-]+:.*?## / {printf "  %-30s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

clean:          ## Remove build artifacts
	rm -f fleetdesk coverage.out coverage.html
	rm -rf bin

##@ Build & verify

build:          ## Build binary
	go build -o fleetdesk .

test:           ## Run unit tests
	go test ./... -race -v

test-report:    ## Run unit tests with coverage report
	go test ./... -race -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

$(GOLANGCI_LINT_BIN):
	@os=$$(go env GOOS); arch=$$(go env GOARCH); \
	ver=$(GOLANGCI_LINT_VERSION); vernum=$${ver#v}; \
	asset="golangci-lint-$${vernum}-$${os}-$${arch}"; \
	tarball="$${asset}.tar.gz"; \
	url="https://github.com/golangci/golangci-lint/releases/download/$${ver}/$${tarball}"; \
	case "$${os}-$${arch}" in \
		linux-amd64)  want="$(GOLANGCI_LINT_SHA256_linux_amd64)" ;; \
		linux-arm64)  want="$(GOLANGCI_LINT_SHA256_linux_arm64)" ;; \
		darwin-amd64) want="$(GOLANGCI_LINT_SHA256_darwin_amd64)" ;; \
		darwin-arm64) want="$(GOLANGCI_LINT_SHA256_darwin_arm64)" ;; \
		*) echo "no pinned checksum for $${os}/$${arch} — add one to the Makefile"; exit 1 ;; \
	esac; \
	tmp=$$(mktemp -d); \
	trap 'rm -rf "$$tmp"' EXIT; \
	echo "bootstrapping golangci-lint $$ver ($$os/$$arch)..."; \
	curl -sSfL -o "$$tmp/$$tarball" "$$url" || { echo "download failed: $$url"; exit 1; }; \
	if command -v sha256sum >/dev/null 2>&1; then \
		hasher=sha256sum; \
		got=$$(sha256sum "$$tmp/$$tarball" | awk '{print $$1}'); \
	elif command -v shasum >/dev/null 2>&1; then \
		hasher=shasum; \
		got=$$(shasum -a 256 "$$tmp/$$tarball" | awk '{print $$1}'); \
	else \
		echo "neither sha256sum nor shasum found on PATH — cannot verify checksum"; exit 1; \
	fi; \
	if [ "$$want" != "$$got" ]; then \
		echo "checksum mismatch for $$tarball: want $$want, got $$got"; exit 1; \
	fi; \
	tar -xzf "$$tmp/$$tarball" -C "$$tmp"; \
	mkdir -p $(GOLANGCI_LINT_DIR); \
	cp "$$tmp/$$asset/golangci-lint" "$(GOLANGCI_LINT_BIN).tmp"; \
	chmod +x "$(GOLANGCI_LINT_BIN).tmp"; \
	mv "$(GOLANGCI_LINT_BIN).tmp" "$(GOLANGCI_LINT_BIN)"; \
	echo "bootstrap OK — $(GOLANGCI_LINT_BIN), checksum verified ($$hasher)"

lint: $(GOLANGCI_LINT_BIN) ## Verify .golangci.yml, then run golangci-lint across the module
	@./$(GOLANGCI_LINT_BIN) config verify
	@./$(GOLANGCI_LINT_BIN) run
	@echo "lint OK — config verified, zero findings"

verify: ## Pre-PR gate: lint, then build, then test
	$(MAKE) lint
	$(MAKE) build
	$(MAKE) test
	@echo "verify OK — lint, build, test green"
	@echo "verify does NOT run: $(REGRESSION_CONTROLS) (make regression); integration against real SSH / Azure / K8s is manual, tracked in Linear"

##@ Verification controls (TAE-80)

# TAE-80: assert the //nolint suppression inventory matches the exact
# record this issue left behind.
verify-nolint: ## Assert the //nolint inventory matches TAE-80's record
	@lines="$$(grep -rn --include='*.go' --exclude='*_test.go' '//nolint:' . 2>/dev/null)"; \
	total=$$(printf '%s\n' "$$lines" | grep -c '//nolint:'); \
	errcheck_tae82=$$(printf '%s\n' "$$lines" | grep '//nolint:errcheck' | grep -c 'TAE-82'); \
	gosec_tae22=$$(printf '%s\n' "$$lines" | grep '//nolint:gosec' | grep -c 'TAE-22'); \
	gosec_tae23=$$(printf '%s\n' "$$lines" | grep '//nolint:gosec' | grep -c 'TAE-23'); \
	gosec_tae42=$$(printf '%s\n' "$$lines" | grep '//nolint:gosec' | grep -c 'TAE-42'); \
	gosec_case1=$$(printf '%s\n' "$$lines" | grep '//nolint:gosec' | grep -vc -E 'TAE-22|TAE-23|TAE-42'); \
	unknown="$$(printf '%s\n' "$$lines" | grep -oE 'TAE-[0-9]+' | grep -vE '^(TAE-82|TAE-22|TAE-23|TAE-42)$$')"; \
	fail=0; \
	if [ "$$errcheck_tae82" -ne $(NOLINT_ERRCHECK_TAE82_WANT) ]; then echo "FAIL errcheck/TAE-82: want $(NOLINT_ERRCHECK_TAE82_WANT), got $$errcheck_tae82"; printf '%s\n' "$$lines" | grep '//nolint:errcheck' | grep 'TAE-82'; fail=1; fi; \
	if [ "$$gosec_tae22" -ne $(NOLINT_GOSEC_TAE22_WANT) ]; then echo "FAIL gosec/TAE-22: want $(NOLINT_GOSEC_TAE22_WANT), got $$gosec_tae22"; printf '%s\n' "$$lines" | grep '//nolint:gosec' | grep 'TAE-22'; fail=1; fi; \
	if [ "$$gosec_tae23" -ne $(NOLINT_GOSEC_TAE23_WANT) ]; then echo "FAIL gosec/TAE-23: want $(NOLINT_GOSEC_TAE23_WANT), got $$gosec_tae23"; printf '%s\n' "$$lines" | grep '//nolint:gosec' | grep 'TAE-23'; fail=1; fi; \
	if [ "$$gosec_tae42" -ne $(NOLINT_GOSEC_TAE42_WANT) ]; then echo "FAIL gosec/TAE-42: want $(NOLINT_GOSEC_TAE42_WANT), got $$gosec_tae42"; printf '%s\n' "$$lines" | grep '//nolint:gosec' | grep 'TAE-42'; fail=1; fi; \
	if [ "$$gosec_case1" -ne $(NOLINT_GOSEC_CASE1_WANT) ]; then echo "FAIL gosec case-1 (no issue key): want $(NOLINT_GOSEC_CASE1_WANT), got $$gosec_case1"; printf '%s\n' "$$lines" | grep '//nolint:gosec' | grep -vE 'TAE-22|TAE-23|TAE-42'; fail=1; fi; \
	if [ "$$total" -ne $(NOLINT_TOTAL_WANT) ]; then echo "FAIL total nolint directives: want $(NOLINT_TOTAL_WANT), got $$total"; fail=1; fi; \
	if [ -n "$$unknown" ]; then echo "FAIL nolint directive(s) cite an issue key outside the inventory: $$unknown"; fail=1; fi; \
	if [ "$$fail" -ne 0 ]; then exit 1; fi; \
	echo "verify-nolint OK — $$total directives: errcheck/TAE-82 $$errcheck_tae82, gosec/TAE-22 $$gosec_tae22, gosec/TAE-23 $$gosec_tae23, gosec/TAE-42 $$gosec_tae42, gosec case-1 $$gosec_case1"

# TAE-80: negative control — confirm the pre-fix tree lints to
# BASELINE_EXPECT findings under the current config.
verify-lint-baseline: $(GOLANGCI_LINT_BIN) ## Negative control: the pre-TAE-80 tree lints to BASELINE_EXPECT findings
	@tmp=$$(mktemp -d $${MKTMPDIR:+-p "$$MKTMPDIR"}); \
	trap 'rm -rf "$$tmp"' EXIT; \
	if ! git clone --quiet . "$$tmp" >/dev/null 2>&1; then \
		echo "verify-lint-baseline FAIL — could not clone this repository into $$tmp"; exit 1; \
	fi; \
	if ! git -C "$$tmp" checkout --quiet $(BASELINE_REF); then \
		echo "verify-lint-baseline FAIL — BASELINE_REF $(BASELINE_REF) is not reachable in the clone. A shallow working copy (actions/checkout defaults to fetch-depth: 1) carries no history to clone, so the baseline ref is absent; fetch full history before running this target."; exit 1; \
	fi; \
	cp .golangci.yml "$$tmp/.golangci.yml"; \
	out=$$(cd "$$tmp" && $(abspath $(GOLANGCI_LINT_BIN)) run 2>&1); \
	n=$$(printf '%s\n' "$$out" | grep -oE '^[0-9]+ issues:' | grep -oE '^[0-9]+'); \
	if [ -z "$$n" ]; then echo "verify-lint-baseline FAIL — could not parse an issue count from golangci-lint output:"; printf '%s\n' "$$out"; exit 1; fi; \
	if [ "$$n" -ne "$(BASELINE_EXPECT)" ]; then echo "verify-lint-baseline FAIL — $(BASELINE_REF) lints to $$n findings under the current config, want $(BASELINE_EXPECT)"; exit 1; fi; \
	echo "verify-lint-baseline OK — $(BASELINE_REF) lints to $$n findings under the current config"

# TAE-91: regression — from a shallow working copy, verify-lint-baseline
# aborts naming BASELINE_REF instead of silently linting HEAD.
verify-baseline-shallow-abort: $(GOLANGCI_LINT_BIN) ## TAE-91: shallow clone forces verify-lint-baseline to abort, not mislint
	@tmp=$$(mktemp -d); \
	trap 'rm -rf "$$tmp"' EXIT; \
	mkdir -p "$$tmp/tmpdir"; \
	if ! git clone --quiet --depth 1 "file://$$(pwd)" "$$tmp/shallow" >/dev/null 2>&1; then \
		echo "verify-baseline-shallow-abort FAIL — could not make a depth-1 clone of this working copy"; exit 1; \
	fi; \
	depth=$$(git -C "$$tmp/shallow" rev-list --count HEAD); \
	if [ "$$depth" -ne 1 ]; then \
		echo "verify-baseline-shallow-abort FAIL — the clone is $$depth commits deep, not shallow; the case under test is not set up"; exit 1; \
	fi; \
	mkdir -p "$$tmp/shallow/$(GOLANGCI_LINT_DIR)"; \
	cp "$(GOLANGCI_LINT_BIN)" "$$tmp/shallow/$(GOLANGCI_LINT_BIN)"; \
	out=$$(cd "$$tmp/shallow" && MKTMPDIR="$$tmp/tmpdir" $(MAKE) --no-print-directory verify-lint-baseline 2>&1); rc=$$?; \
	printf '%s\n' "$$out" | sed 's/^/    | /'; \
	fail=0; \
	if [ "$$rc" -eq 0 ]; then echo "FAIL — verify-lint-baseline succeeded from a shallow clone; it must abort"; fail=1; fi; \
	if ! printf '%s\n' "$$out" | grep -q "BASELINE_REF $(BASELINE_REF) is not reachable"; then echo "FAIL — the failure does not name the unreachable ref $(BASELINE_REF)"; fail=1; fi; \
	if printf '%s\n' "$$out" | grep -q 'could not parse an issue count'; then echo "FAIL — the recipe carried on past the failed checkout and died at the issue-count parser (TAE-91 regression)"; fail=1; fi; \
	left=$$(ls -A "$$tmp/tmpdir" | wc -l); \
	if [ "$$left" -ne 0 ]; then echo "FAIL — $$left temp directory/directories left behind on the failure path:"; ls -A "$$tmp/tmpdir"; fail=1; fi; \
	if [ "$$fail" -ne 0 ]; then exit 1; fi; \
	echo "verify-baseline-shallow-abort OK — aborted (rc $$rc) naming $(BASELINE_REF), no parser fallthrough, temp directory cleaned up"

# TAE-80 AC 8: clone the committed tree, strip golangci-lint from PATH,
# confirm `make verify` bootstraps its own binary and passes. The clone is
# of the committed tree, not the working tree: uncommitted Makefile or test
# changes are invisible to this control.
verify-fresh-clone: ## Prove a clean machine bootstraps golangci-lint and passes verify
	@tmp=$$(mktemp -d $${MKTMPDIR:+-p "$$MKTMPDIR"}); \
	trap 'rm -rf "$$tmp"' EXIT; \
	if ! git clone --quiet . "$$tmp" >/dev/null 2>&1; then \
		echo "verify-fresh-clone FAIL — could not clone this repository into $$tmp"; exit 1; \
	fi; \
	strippedpath=""; \
	oldifs="$$IFS"; IFS=':'; \
	for d in $$PATH; do \
		if [ ! -x "$$d/golangci-lint" ]; then \
			strippedpath="$$strippedpath$${strippedpath:+:}$$d"; \
		fi; \
	done; \
	IFS="$$oldifs"; \
	if ( cd "$$tmp" && PATH="$$strippedpath" $(MAKE) verify ); then :; else \
		echo "verify-fresh-clone FAIL — make verify did not pass in the clone"; exit 1; \
	fi; \
	if [ ! -x "$$tmp/$(GOLANGCI_LINT_BIN)" ]; then \
		echo "verify-fresh-clone FAIL — $(GOLANGCI_LINT_BIN) was not bootstrapped inside the clone"; exit 1; \
	fi; \
	os=$$(go env GOOS); arch=$$(go env GOARCH); \
	echo "verify-fresh-clone OK — bootstrapped $(GOLANGCI_LINT_VERSION) on $$os/$$arch, make verify green"

# TAE-92: regression — from a non-repository directory, verify-fresh-clone
# aborts naming the clone instead of silently continuing into an empty
# directory and running make verify there. FRESH_CLONE_ABORT_MAKEFILE lets
# the same target re-prove itself against a historical (pre-fix) recipe, so
# the negative control stays a reproducible Make step, not a one-off
# transcript.
verify-fresh-clone-abort: ## TAE-92: non-repository forces verify-fresh-clone to abort, not fall through
	@tmp=$$(mktemp -d); \
	trap 'rm -rf "$$tmp"' EXIT; \
	mkdir -p "$$tmp/norepo" "$$tmp/tmpdir"; \
	if ! cp "$(FRESH_CLONE_ABORT_MAKEFILE)" "$$tmp/norepo/Makefile"; then \
		echo "verify-fresh-clone-abort FAIL — could not copy $(FRESH_CLONE_ABORT_MAKEFILE) into the test directory"; exit 1; \
	fi; \
	if git -C "$$tmp/norepo" rev-parse --is-inside-work-tree >/dev/null 2>&1; then \
		echo "verify-fresh-clone-abort FAIL — $$tmp/norepo is inside a git repository; the case under test is not set up"; exit 1; \
	fi; \
	out=$$(cd "$$tmp/norepo" && MKTMPDIR="$$tmp/tmpdir" $(MAKE) --no-print-directory verify-fresh-clone 2>&1); rc=$$?; \
	printf '%s\n' "$$out" | sed 's/^/    | /'; \
	fail=0; \
	if [ "$$rc" -eq 0 ]; then echo "FAIL — verify-fresh-clone succeeded from a non-repository; it must abort"; fail=1; fi; \
	if ! printf '%s\n' "$$out" | grep -q "could not clone this repository into"; then echo "FAIL — the failure does not name the clone"; fail=1; fi; \
	if printf '%s\n' "$$out" | grep -q "make verify did not pass in the clone"; then echo "FAIL — the recipe carried on past the failed clone and ran make verify in an empty directory (TAE-92 regression)"; fail=1; fi; \
	left=$$(ls -A "$$tmp/tmpdir" | wc -l); \
	if [ "$$left" -ne 0 ]; then echo "FAIL — $$left temp directory/directories left behind on the failure path:"; ls -A "$$tmp/tmpdir"; fail=1; fi; \
	if [ "$$fail" -ne 0 ]; then exit 1; fi; \
	echo "verify-fresh-clone-abort OK — aborted (rc $$rc) naming the clone failure, no fallthrough into make verify, temp directory cleaned up"

##@ Licence (TAE-93)

# TAE-93: the repository claims exactly one licence, Apache-2.0, and the
# LICENSE file is the canonical text with only the copyright line filled in.
verify-license: ## TAE-93: LICENSE is canonical Apache-2.0 plus the copyright line; README claims nothing else
	@test -f LICENSE || { echo "verify-license FAIL: LICENSE missing at repository root"; exit 1; }
	@grep -qxF '   $(LICENSE_COPYRIGHT)' LICENSE || { echo "verify-license FAIL: LICENSE lacks the line '$(LICENSE_COPYRIGHT)'"; exit 1; }
	@if command -v sha256sum >/dev/null 2>&1; then hasher="sha256sum"; \
	elif command -v shasum >/dev/null 2>&1; then hasher="shasum -a 256"; \
	else echo "verify-license FAIL: neither sha256sum nor shasum found on PATH"; exit 1; fi; \
	got=$$(sed 's/^   $(LICENSE_COPYRIGHT)$$/   $(LICENSE_PLACEHOLDER)/' LICENSE | $$hasher | awk '{print $$1}'); \
	if [ "$$got" != "$(LICENSE_CANONICAL_SHA256)" ]; then \
		echo "verify-license FAIL: LICENSE differs from canonical Apache-2.0 beyond the copyright line (want $(LICENSE_CANONICAL_SHA256), got $$got)"; exit 1; \
	fi
	@grep -qF '[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)' README.md || { echo "verify-license FAIL: README badge is not the Apache 2.0 badge linking LICENSE"; exit 1; }
	@sed -n '/^## License$$/,$$p' README.md | grep -q 'Apache-2.0' || { echo "verify-license FAIL: README ## License section does not say Apache-2.0"; exit 1; }
	@if stray="$$(grep -rnE --exclude-dir=.git --exclude-dir=bin --exclude=LICENSE 'License[:-] ?MIT|MIT Licen[cs]e|^MIT$$' . 2>/dev/null)" && [ -n "$$stray" ]; then \
		echo "verify-license FAIL: MIT licence claim still present:"; printf '%s\n' "$$stray"; exit 1; \
	fi
	@echo "verify-license OK — LICENSE is canonical Apache-2.0 with '$(LICENSE_COPYRIGHT)'; README and repo claim nothing else"

##@ Test host (M2 fixture, TAE-98)

testhost-up: ## Build/(re)start the TAE-98 sshd test host; prints how to reach it
	@rm -rf $(TESTHOST_KEYS_DIR)
	@mkdir -p $(TESTHOST_KEYS_DIR)
	@ssh-keygen -q -t ed25519 -N '' -f $(TESTHOST_KEYS_DIR)/client_ed25519
	@podman build -q -t $(TESTHOST_IMAGE) $(TESTHOST_DIR) >/dev/null
	@podman rm -f $(TESTHOST_CONTAINER) >/dev/null 2>&1 || true
	@podman run -d --name $(TESTHOST_CONTAINER) \
		-p 127.0.0.1:$(TESTHOST_PORT):22 \
		-v $(CURDIR)/$(TESTHOST_KEYS_DIR)/client_ed25519.pub:/run/testhost/client_ed25519.pub:ro,Z \
		$(TESTHOST_IMAGE) >/dev/null
	@for i in $$(seq 1 40); do \
		if ssh-keyscan -p $(TESTHOST_PORT) -T 1 127.0.0.1 2>/dev/null | grep -q .; then break; fi; \
		sleep 0.5; \
	done
	@echo "fleetdesk-testhost ready — fleet file: $(TESTHOST_DIR)/fleet.yaml"
	@echo "  ssh -i $(TESTHOST_KEYS_DIR)/client_ed25519 -p $(TESTHOST_PORT) testuser1@127.0.0.1   (or password: th98-testuser1-pw)"
	@echo "  ssh -p $(TESTHOST_PORT) testuser2@127.0.0.1   (password: th98-testuser2-pw)"

testhost-down: ## Stop/remove the TAE-98 test host and wipe its generated keys
	@podman rm -f $(TESTHOST_CONTAINER) >/dev/null 2>&1 || true
	@rm -rf $(TESTHOST_KEYS_DIR)

testhost-regen-host-key: ## Delete and regenerate the TAE-98 test host's SSH host keys in place
	podman exec $(TESTHOST_CONTAINER) sh -c 'rm -f /etc/ssh/ssh_host_* && ssh-keygen -A && kill -HUP 1'

testhost-test: ## Run the TAE-98 acceptance harness (requires Podman; starts/stops the fixture itself)
	go test -tags testhost -run TestTestHost -v .

##@ Regression gate

regression: ## Every automated check, ascending cost: verify, then the verification controls
	$(MAKE) verify
	@for t in $(REGRESSION_CONTROLS); do $(MAKE) $$t || exit 1; done
	@echo "regression OK — verify $(REGRESSION_CONTROLS)"
