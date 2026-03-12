GOTOOLCHAIN ?= go1.26.1
export GOTOOLCHAIN

.PHONY: build test test-unit test-e2e clean setup-e2e teardown-e2e

build:
	go build -o bin/cq-source-oci .

test: test-unit test-e2e

test-unit:
	go test ./client/ ./internal/... -v -timeout 30s

test-e2e:
	go test ./tests/e2e/ -v -timeout 300s

clean:
	rm -rf bin/

setup-e2e:
	./tests/e2e/setup.sh

teardown-e2e:
	./tests/e2e/setup.sh teardown
