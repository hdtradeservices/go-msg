PACKAGES=$(shell go list ./...)

all: lint test

lint:
	golangci-lint run ./...

test:
	go test -race -v ./...

tools:
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $(shell go env GOPATH)/bin v2.13.1

fmt:
	go fmt $(PACKAGES)

.PHONY: help lint test fmt tools
