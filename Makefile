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
	test-app-real test-app-e2e test-b2-preflight test-env-up test-env-down test-e2e test-mainpath _test _test-contract _test-adapters _build _contract-status

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

# アプリの実物の部品と実物の核の結線の試験(mp-app)。受入試験の環境と試験用の保管先が要る(test/wiring/run.sh の冒頭)。
test-app-real:
	@test -n "$(NAME)" || { echo "NAME=<名前> が要ります"; exit 64; }
	@bash test/wiring/run.sh "$(NAME)"

# 試験用ドライバの通しの試験(偽の LLM の口。実物の Electron の試験ビルド × 核 × 受入試験の環境。LLM API キーも B2 も使わない)。test/wiring/e2e.sh の冒頭。
test-app-e2e:
	@test -n "$(NAME)" || { echo "NAME=<名前> が要ります"; exit 64; }
	@bash test/wiring/e2e.sh "$(NAME)"

# 予行: LLM を使わず、保管先の管理用鍵(環境変数 SITATE_ROLE_B2_*)だけで init-storage・bucket check・backup・extend・restore-test を実物の B2 に通す。
# 偽の口の確かめは FAKE=1(試験用の MinIO)。手順は test/acceptance/launch.md の先頭。test/wiring/b2-preflight.sh の冒頭。
test-b2-preflight:
	@test -n "$(NAME)" || { echo "NAME=<名前> が要ります"; exit 64; }
	@bash test/wiring/b2-preflight.sh "$(NAME)" $(if $(FAKE),fake)

# 主経路の通し(作業指示書 6 章)。make test に含めない。STAGE は 1〜3。DB は DB の対象の取得(第 3 段と同じ環境の前提で、別に作った環境で流す)。
# 第 1 段: make test-env-up NAME=x の後に SITATE_NESTED=1 make test-mainpath STAGE=1 NAME=x。
# 第 2・3 段: bash test/env/bin/accept-env.sh up x と LOCK_DEFAULT_DAYS=90 make test-env-up NAME=x の後に make test-mainpath STAGE=2 NAME=x(ACCEPT は NAME と同じ。終わったら両方 down)。
test-mainpath:
	@case "$(STAGE)" in 1|2|3|DB) ;; *) echo "STAGE は 1|2|3|DB" >&2; exit 64;; esac
	@$(if $(filter 2 3 DB,$(STAGE)),ACCEPT=$(or $(ACCEPT),$(NAME))) $(LINUX) 'go test -count=1 -v -timeout 40m -tags $(TEST_TAG),mainpath -run "^TestStage$(STAGE)" ./test/mainpath/'

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
	@cp data/prices/prices.json "dist/$(MODE)/bundle/$(VERSION)/prices/prices.json"

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

# 手順書の読み込みと判定の道具の試験。NAME=<WP> を付けると、受入試験の環境(accept-env.sh up と test-env-up を済ませたもの。test-mainpath の注を見よ)で
# 第 3 段の台本を走らせ、その終わりで手順 1〜13 の完了確認コマンドを実物の核に実行して手順書の期待する値に照らす(test/mainpath/stage3_e2e_test.go)。
test-e2e:
	@$(LINUX) 'go test -count=1 -tags $(TEST_TAG) ./test/e2e/...'
	@if [ -n "$(NAME)" ]; then $(MAKE) test-mainpath STAGE=3 NAME=$(NAME); fi
