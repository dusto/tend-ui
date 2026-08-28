.PHONY: build run test generate vet fmt

build:
	go build -o tend-ui .

run: build
	./tend-ui

test:
	go test ./...

# Regenerate templ output and tidy modules.
generate:
	go generate ./...
	go mod tidy

vet:
	go vet ./...

fmt:
	gofmt -w .
