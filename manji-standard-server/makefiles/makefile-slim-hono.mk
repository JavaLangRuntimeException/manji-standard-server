SHELL := /bin/bash

HONO_DIR := manji-standard-server-ts-hono

.DEFAULT_GOAL := help
.PHONY: help hono-%

help: ## ターゲット一覧
	@grep -E '^[a-zA-Z_%/-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[32m%-20s\033[0m %s\n", $$1, $$2}'

hono-%: ## hono-<target>: Hono の make を実行 (例: make hono-dev, make hono-build)
	@$(MAKE) -C $(HONO_DIR) $*
