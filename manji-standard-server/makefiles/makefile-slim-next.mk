SHELL := /bin/bash

NEXT_DIR := manji-standard-server-ts-next

.DEFAULT_GOAL := help
.PHONY: help next-%

help: ## ターゲット一覧
	@grep -E '^[a-zA-Z_%/-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[32m%-20s\033[0m %s\n", $$1, $$2}'

next-%: ## next-<target>: Next の make を実行 (例: make next-dev, make next-build)
	@$(MAKE) -C $(NEXT_DIR) $*
