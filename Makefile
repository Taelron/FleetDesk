.DEFAULT_GOAL := help
.PHONY: help test test-report build lint verify clean verify-fresh-clone verify-nolint verify-lint-baseline verify-baseline-shallow-abort regression

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

# Regression-only targets. verify's closing line and regression's run list
# share this one list so they cannot drift apart — the standing rule "every
# automated target added later joins regression" becomes a one-variable
# edit (TAE-83's guard is exactly that edit).
REGRESSION_CONTROLS := verify-nolint verify-lint-baseline verify-baseline-shallow-abort verify-fresh-clone

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
	@tmp=$$(mktemp -d); \
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

##@ Regression gate

regression: ## Every automated check, ascending cost: verify, then the verification controls
	$(MAKE) verify
	@for t in $(REGRESSION_CONTROLS); do $(MAKE) $$t || exit 1; done
	@echo "regression OK — verify $(REGRESSION_CONTROLS)"
