.PHONY: render bundle test build vet fmt tidy frontend

# render: render templates/ into commands/ (and any other future kinds).
# Run this after editing any file under templates/.
render:
	go run ./cmd/render-templates -repo ../ -outdir ../

# bundle: regenerate the embedded artifact bundle from the repo root.
# Run this after editing any source artifact (agents/, commands/, skills/, etc.).
bundle: render
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

# frontend: build the atomic serve React app into internal/serve/frontend/dist.
# Run this after editing anything under internal/serve/frontend/ (excl. dist/).
frontend:
	cd internal/serve/frontend && bun install --frozen-lockfile && bun run build.ts
