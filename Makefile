SHELL := /bin/bash
.SHELLFLAGS := -ec
GO_ENV ?= GOWORK=off GOCACHE=$(CURDIR)/.tmp-go-cache GOTMPDIR=$(CURDIR)/.tmp-go-tmp
STATICCHECK ?= go run honnef.co/go/tools/cmd/staticcheck@latest
STATICCHECK_CACHE ?= $(CURDIR)/.tmp-staticcheck-cache

.PHONY: test vet staticcheck verify

test:
	$(GO_ENV) go test ./...

vet:
	$(GO_ENV) go vet ./...

staticcheck:
	XDG_CACHE_HOME=$(STATICCHECK_CACHE) $(GO_ENV) GOFLAGS=-buildvcs=false $(STATICCHECK) ./...

verify: test vet staticcheck
