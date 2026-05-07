SHELL := /bin/bash

MSS_DIR  := manji-standard-server
GO_DIR   := manji-standard-server-go
HONO_DIR := manji-standard-server-ts-hono
NEXT_DIR := manji-standard-server-ts-next

ALL_DIRS := $(GO_DIR) $(HONO_DIR) $(NEXT_DIR)

CLAUDE_DIR := .claude
SKILLS_DST := $(CLAUDE_DIR)/skills
AGENTS_DST := $(CLAUDE_DIR)/agents

GREEN  := \033[0;32m
YELLOW := \033[0;33m
RED    := \033[0;31m
NC     := \033[0m

.DEFAULT_GOAL := help
.PHONY: help init-go init-hono init-next _clean-except _docs-init \
        install-skills doctor go-% hono-% next-%

help: ## ターゲット一覧
	@grep -E '^[a-zA-Z_%/-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[32m%-20s\033[0m %s\n", $$1, $$2}'

# ── 言語別セットアップ ────────────────────────────────────────────

init-go: ## Go BE 構成に一括初期化（Hono/Next 削除 + skills 配置 + docs 初期化）
	@$(MAKE) _clean-except KEEP_DIR=$(GO_DIR)
	@$(MAKE) install-skills
	@$(MAKE) _docs-init IMPL_DIR=$(GO_DIR)
	@cp $(MSS_DIR)/makefiles/makefile-slim-go.mk Makefile
	@rm -rf $(MSS_DIR)
	@printf "$(GREEN)✔$(NC) init-go 完了 — $(GO_DIR)/ で開発を始めてください\n"

init-hono: ## Hono TS 構成に一括初期化（Go/Next 削除 + skills 配置 + docs 初期化）
	@$(MAKE) _clean-except KEEP_DIR=$(HONO_DIR)
	@$(MAKE) install-skills
	@$(MAKE) _docs-init IMPL_DIR=$(HONO_DIR)
	@cp $(MSS_DIR)/makefiles/makefile-slim-hono.mk Makefile
	@rm -rf $(MSS_DIR)
	@printf "$(GREEN)✔$(NC) init-hono 完了 — $(HONO_DIR)/ で開発を始めてください\n"

init-next: ## Next.js TS 構成に一括初期化（Go/Hono 削除 + skills 配置 + docs 初期化）
	@$(MAKE) _clean-except KEEP_DIR=$(NEXT_DIR)
	@$(MAKE) install-skills
	@$(MAKE) _docs-init IMPL_DIR=$(NEXT_DIR)
	@cp $(MSS_DIR)/makefiles/makefile-slim-next.mk Makefile
	@rm -rf $(MSS_DIR)
	@printf "$(GREEN)✔$(NC) init-next 完了 — $(NEXT_DIR)/ で開発を始めてください\n"

# ── 内部ターゲット ────────────────────────────────────────────────

_clean-except:
	@targets=""; \
	for d in $(ALL_DIRS); do \
		[ "$$d" != "$(KEEP_DIR)" ] && [ -d $$d ] && targets="$$targets $$d"; \
	done; \
	if [ -z "$$targets" ]; then printf "$(GREEN)✔$(NC) 削除対象なし（既に削除済み）\n"; exit 0; fi; \
	printf "$(YELLOW)以下を削除します:$(NC)\n"; \
	for d in $$targets; do printf "  - $$d/\n"; done; \
	printf "  - $(MSS_DIR)/\n"; \
	printf "$(YELLOW)続行しますか？ [y/N]: $(NC)"; read ans; \
	case $$ans in [yY]*) ;; *) printf "$(RED)キャンセル$(NC)\n"; exit 1 ;; esac; \
	for d in $$targets; do rm -rf $$d && printf "  $(GREEN)✔$(NC) deleted $$d/\n"; done

_docs-init:
	@for sub in spec work knowledge; do \
		dir=$(IMPL_DIR)/docs/$$sub; \
		if [ ! -d $$dir ]; then mkdir -p $$dir && printf "  created: $$dir/\n"; \
		else printf "  exists:  $$dir/\n"; fi; \
	done
	@printf "$(GREEN)✔$(NC) docs layout ready\n"

install-skills: ## manji-standard-server/ の skills + agents を .claude/ に配置
	@$(MAKE) -f $(MSS_DIR)/Makefile install SOURCE_DIR=$(MSS_DIR)

# ── 診断 ──────────────────────────────────────────────────────────

doctor: ## プロジェクト構成の状態を診断
	@printf "\n  pachipachi-manager doctor\n  =========================\n\n"
	@for d in $(ALL_DIRS); do \
		[ -d $$d ] \
			&& printf "  $(GREEN)✔$(NC) $$d/\n" \
			|| printf "  $(YELLOW)-$(NC) $$d/ (削除済み)\n"; \
	done
	@printf "\n"
	@[ -d $(SKILLS_DST) ] \
		&& printf "  $(GREEN)✔$(NC) skills : $(SKILLS_DST)/ ($$(ls -1 $(SKILLS_DST) 2>/dev/null | wc -l | tr -d ' ') 個)\n" \
		|| printf "  $(YELLOW)⚠$(NC) $(SKILLS_DST)/ なし → make install-skills\n"
	@[ -d $(AGENTS_DST) ] \
		&& printf "  $(GREEN)✔$(NC) agents : $(AGENTS_DST)/ ($$(ls -1 $(AGENTS_DST) 2>/dev/null | wc -l | tr -d ' ') 個)\n" \
		|| printf "  $(YELLOW)⚠$(NC) $(AGENTS_DST)/ なし → make install-skills\n"
	@printf "\n"

# ── 各プロジェクトへのパススルー ──────────────────────────────────

go-%: ## go-<target>: Go プロジェクトの make を実行 (例: make go-build)
	@$(MAKE) -C $(GO_DIR) $*

hono-%: ## hono-<target>: Hono プロジェクトの make を実行 (例: make hono-dev)
	@$(MAKE) -C $(HONO_DIR) $*

next-%: ## next-<target>: Next プロジェクトの make を実行 (例: make next-dev)
	@$(MAKE) -C $(NEXT_DIR) $*
