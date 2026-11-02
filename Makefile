# Sitate-backup の Makefile(作業指示書 3.1)。共有ファイル: 編集するのは U0-core と監督だけ。
# Linux の開発用コンテナで走るターゲットは LINUX 経由で呼ぶ(コンテナの中なら直接実行)。
SHELL := /bin/bash
VERSION ?= 0.1.0-dev
TEST_TAG := sitate_testbuild
ARCHES := linux-arm64:arm64 linux-x86_64:amd64
LINUX := bash test/env/bin/in-linux.sh
NAME ?=
export NAME

.PHONY: shell-linux test test-app test-contract contract-status test-adapters lint-install lint-size \
	build build-test build-app build-app-test sign-test sign sign-installer sign-test-installer \
	test-env-up test-env-down test-e2e test-mainpath _test _test-contract _test-adapters _build _contract-status

shell-linux:
	@$(LINUX) '$(CMD)'

# --- 試験 ---
test:
	@$(LINUX) 'make _test'
_test:
	@rc=0; \
	go vet ./... || rc=1; go vet -tags $(TEST_TAG) ./... || rc=1; \
	go test ./... || rc=1; go test -tags $(TEST_TAG) ./... || rc=1; \
	$(MAKE) _test-contract PART=go || rc=1; $(MAKE) _test-contract PART=prices || rc=1; $(MAKE) _test-adapters || rc=1; \
	exit $$rc

test-contract:
ifeq ($(PART),app)
	npm --prefix app run test:contract
else
	@$(LINUX) 'make _test-contract PART=$(if $(PART),$(PART),go)$(if $(PART),, && make _test-contract PART=prices)'
endif
_test-contract:
	@case "$(PART)" in \
	  go) rc=0; go test ./internal/contract/... || rc=1; \
	      go test -run '^TestContract' $$(go list ./internal/... | grep -v /internal/contract) || rc=1; exit $$rc ;; \
	  prices) go test ./data/prices/... ;; \
	  *) echo "PART は go|app|prices" >&2; exit 64;; esac

test-app:
	npm --prefix app test

# 主経路の通し(作業指示書 6 章)。make test に含めない。STAGE は 1〜3。
# 第 1 段: make test-env-up NAME=x の後に SITATE_NESTED=1 make test-mainpath STAGE=1 NAME=x。
# 第 2・3 段: bash test/env/bin/accept-env.sh up x と LOCK_DEFAULT_DAYS=90 make test-env-up NAME=x の後に make test-mainpath STAGE=2 NAME=x(ACCEPT は NAME と同じ。終わったら両方 down)。
test-mainpath:
	@case "$(STAGE)" in 1|2|3) ;; *) echo "STAGE は 1|2|3" >&2; exit 64;; esac
	@$(if $(filter 2 3,$(STAGE)),ACCEPT=$(or $(ACCEPT),$(NAME))) $(LINUX) 'go test -count=1 -v -timeout 40m -tags $(TEST_TAG),mainpath -run "^TestStage$(STAGE)" ./test/mainpath/'

contract-status:
	@mkdir -p test/env/.run && (cd app && node scripts/contract-status.mjs) > test/env/.run/app-status.jsonl
	@$(LINUX) 'make _contract-status'
_contract-status:
	@go run ./tools/contractstatus -app test/env/.run/app-status.jsonl

test-adapters:
	@$(LINUX) 'make _test-adapters ADAPTER=$(ADAPTER)'
_test-adapters:
	@go test ./adapters/$(if $(ADAPTER),$(ADAPTER)/,...)

lint-install:
	@$(LINUX) 'go run ./tools/lintinstall'

# コードの大きさ。Go の分は開発用コンテナ、app/ の TypeScript の分は作業端末。
lint-size:
	@$(LINUX) 'LINT_BASE=$(LINT_BASE) go run ./tools/lintsize'
	@if [ -f app/package.json ] && [ -d app/node_modules ]; then npm --prefix app run lint:size; else echo "lint-size(app): app/node_modules が無いので飛ばす"; fi

# --- ビルド ---
build:
	@$(LINUX) 'make _build MODE=product VERSION=$(VERSION)'
build-test:
	@$(LINUX) 'make _build MODE=test VERSION=$(VERSION)'
_build:
	@bash tools/build.sh "$(MODE)" "$(VERSION)" "$(TEST_TAG)"

build-app:
	npm --prefix app run build
build-app-test:
	npm --prefix app run build:test

# --- 署名 ---
# 秘密鍵はリポジトリの外から渡す(SECRET_KEY=<path>)。実行は利用者だけ(sign / sign-installer)。
sign-test:
	@$(LINUX) 'bash tools/sign.sh test $(VERSION)'
sign:
	@bash tools/sign.sh product $(VERSION)
sign-installer:
	@bash tools/sign-installer.sh product $(INSTALLER)
sign-test-installer:
	@bash tools/sign-installer.sh test $(INSTALLER)

# --- 試験用の保管先 ---
test-env-up:
	@test -n "$(NAME)" || { echo "NAME=<WP> が要ります"; exit 64; }
	@bash test/env/bin/test-env.sh up "$(NAME)" "$(or $(KIND),minio)"
test-env-down:
	@test -n "$(NAME)" || { echo "NAME=<WP> が要ります"; exit 64; }
	@bash test/env/bin/test-env.sh down "$(NAME)"

test-e2e:
	@$(LINUX) 'go test -tags $(TEST_TAG) ./test/e2e/...'
