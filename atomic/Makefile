.PHONY: bundle test build vet fmt tidy

# bundle: regenerate the embedded artifact bundle from the repo root.
# Run this after editing any source artifact (agents/, commands/, skills/, etc.).
bundle:
	go run ./cmd/bundle-mirror -repo ../ -outdir ./internal/embedded

# test: run all tests.
test:
	go test ./...

# build: compile the binary.
build:
	go build -o ../bin/atomic ./cmd/atomic

# vet: run go vet.
vet:
	go vet ./...

# fmt: check formatting (print unformatted files).
fmt:
	gofmt -l .

# tidy: tidy module dependencies.
tidy:
	go mod tidy
