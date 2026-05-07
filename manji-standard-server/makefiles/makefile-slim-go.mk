SHELL := /bin/bash

GO_DIR := manji-standard-server-go

.DEFAULT_GOAL := help
.PHONY: help go-%

help: ## ターゲット一覧
	@grep -E '^[a-zA-Z_%/-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[32m%-20s\033[0m %s\n", $$1, $$2}'

go-%: ## go-<target>: Go の make を実行 (例: make go-build, make go-test)
	@$(MAKE) -C $(GO_DIR) $*
