.PHONY: build run test js-test lint vet fmt generate check

build:
	go build -o tend-ui .

run: build
	./tend-ui

test:
	go test ./...

js-test:
	npm test

lint:
	golangci-lint run

vet:
	go vet ./...

fmt:
	gofmt -w $$(git ls-files '*.go' | grep -v '^third_party/')

# Regenerate templ output (from the package dir, matching CI) and tidy modules.
generate:
	go generate ./...
	go mod tidy

# The full pre-PR gate — mirrors CI.
check: build vet test js-test lint
	@test -z "$$(gofmt -l . | grep -v '^third_party/')" || (echo "needs gofmt"; exit 1)
	@go generate ./... && git diff --exit-code web/ && echo "no templ drift"
	@echo "all checks passed"
