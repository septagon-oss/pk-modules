SHELL := /bin/bash
.SHELLFLAGS := -ec
GO_ENV ?= GOWORK=off GOTMPDIR=$(CURDIR)/.tmp-go-tmp TMPDIR=$(CURDIR)/.tmp-go-tmp
STATICCHECK_VERSION ?= v0.7.0
STATICCHECK ?= go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
TMPDIRS := .tmp-go-tmp

.PHONY: test vet staticcheck race verify

$(TMPDIRS):
	@mkdir -p $@

test: | $(TMPDIRS)
	$(GO_ENV) go test ./...

vet: | $(TMPDIRS)
	$(GO_ENV) go vet ./...

# pk-guard: the composable guardrail vettool (safeerror, importboundary,
# noclockindomain, buildtags), extracted from the PlatformKit estate. Built
# from the pinned module so CI and clean clones run the identical guards.
guard: | $(TMPDIRS)
	$(GO_ENV) go tool pk-guard ./...

staticcheck: | $(TMPDIRS)
	$(GO_ENV) GOFLAGS=-buildvcs=false $(STATICCHECK) ./...

race: | $(TMPDIRS)
	$(GO_ENV) go test -race -count=1 ./...

verify: test vet guard staticcheck race
