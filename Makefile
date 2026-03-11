.PHONY: lint test test-integration build docker-agent clean setup check

# Go
lint:
	golangci-lint run ./...
	cd agent && ruff check src/ && mypy --strict src/

test:
	go test ./... -race -count=1
	cd agent && PYTHONPATH=src python -m pytest tests/ -v --tb=short

test-integration:
	go test -tags integration ./tests/integration/ -v -race -count=1

build:
	go build -o bin/mesh ./cmd/mesh/

docker-agent:
	docker build -t mesh-agent:latest -f agent/Dockerfile agent/

clean:
	rm -rf bin/
	go clean -cache

# Setup development environment (install git hooks)
setup:
	go install github.com/evilmartians/lefthook@latest
	go install honnef.co/go/tools/cmd/staticcheck@latest
	cd agent && uv sync --extra dev
	$$(go env GOPATH)/bin/lefthook install

# Run all pre-commit checks manually
check:
	$$(go env GOPATH)/bin/lefthook run pre-commit
