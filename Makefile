PACKAGES=$(shell go list ./...)

all: lint test

lint:
	golangci-lint run ./...

test:
	go test -race -v ./...

tools:
	curl -sfL https://install.goreleaser.com/github.com/golangci/golangci-lint.sh | sh -s -- -b $(GOPATH)/bin v1.17.0

fmt: tools
	go fmt $(PACKAGES)

.PHONY: help lint test fmt tools
