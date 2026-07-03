# Build stage: compile the single ctx binary.
# CGO is disabled: both the SQLite (modernc.org/sqlite) and Postgres (pgx)
# backends are pure Go, so the result is a static binary.
FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /out/ctx .

# Runtime stage: minimal image with just the binary and CA certs
# (needed for HTTPS calls to embedding providers).
FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=build /out/ctx /usr/local/bin/ctx

# Config directory, mount a ctx.yaml here (see deploy/ctx.yaml).
ENV CTX_HOME=/config
VOLUME ["/config", "/data"]

ENTRYPOINT ["ctx"]
CMD ["--help"]
