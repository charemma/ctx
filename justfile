default:
    @just --list

# Build the ctx binary
build:
    go build -o bin/ctx .

# Run tests
test:
    go test ./...

# Run linter
lint:
    golangci-lint run ./...

# Format code
fmt:
    gofmt -w .

# Run ctx directly (pass args after --)
dev *ARGS:
    go run . {{ARGS}}

# Start dev Postgres with pgvector
db-up:
    docker compose up -d postgres

# Stop dev Postgres
db-down:
    docker compose down
