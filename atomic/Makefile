.PHONY: bundle frontend test build vet fmt tidy

# bundle: expand context/ into the embedded artifact bundle.
# One step: shared partials from context/_partials/ are expanded on the way in,
# so no rendered copy of an artifact is ever written back into context/.
# The output (internal/embedded/bundle/ + manifest.go) is a build artifact and
# is gitignored: go:embed cannot reach ../../../context, so the tree has to be
# mirrored into the package, but it never needs to be committed.
bundle:
	go run ./internal/tools/bundle-mirror -repo ../ -outdir ./internal/embedded

# frontend: build the atomic serve React app into internal/serve/frontend/dist.
# The output is gitignored, like the bundle: go:embed in internal/serve needs
# it on disk, so build/test/vet depend on it, but it is never committed.
frontend:
	cd internal/serve/frontend && bun install --frozen-lockfile && bun run build.ts

# test: run all tests. Depends on bundle and frontend — internal/embedded and
# internal/serve do not compile until their embedded trees exist.
test: bundle frontend
	go test ./...

# build: compile the binary. Same prerequisites as test.
build: bundle frontend
	go build -o ../bin/atomic ./cmd/atomic

# vet: run go vet. Same prerequisites as test.
vet: bundle frontend
	go vet ./...

# fmt: check formatting (print unformatted files).
fmt:
	gofmt -l .

# tidy: tidy module dependencies.
tidy:
	go mod tidy
