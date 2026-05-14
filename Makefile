SHELL := /bin/bash
.SHELLFLAGS := -ec

.PHONY: test

test:
	GOWORK=off GOCACHE=$(CURDIR)/.tmp-go-cache go test ./...
