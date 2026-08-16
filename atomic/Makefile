.PHONY: render bundle test build vet fmt tidy frontend

# render: render templates/ into context/commands/ and context/agents/.
# Run this after editing any file under templates/.
render:
	go run ./cmd/render-templates -repo ../ -outdir ../context

# bundle: regenerate the embedded artifact bundle from context/.
# The output (internal/embedded/bundle/ + manifest.go) is a build artifact and
# is gitignored: go:embed cannot reach ../../../context, so the tree has to be
# mirrored into the package, but it never needs to be committed.
bundle: render
	go run ./cmd/bundle-mirror -repo ../ -outdir ./internal/embedded

# test: run all tests. Depends on bundle — internal/embedded does not compile
# until the mirror exists.
test: bundle
	go test ./...

# build: compile the binary. Depends on bundle for the same reason as test.
build: bundle
	go build -o ../bin/atomic ./cmd/atomic

# vet: run go vet. Depends on bundle for the same reason as test.
vet: bundle
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
