# syntax=docker/dockerfile:1

# --- Build stage: Go binaries and the goose CLI ---------------------------
FROM golang:1.24-alpine AS build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
# goose is pure Go; built here so the runtime image can run migrations
RUN CGO_ENABLED=0 go install github.com/pressly/goose/v3/cmd/goose@v3.24.1
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/seed ./cmd/seed \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/botcommands ./cmd/botcommands

# --- Runtime stage: API + webhook, plus seed, botcommands and goose for
# --- one-off runs -----------------------------------------------------------
# Migrations:  docker compose run --rm --entrypoint goose backend -dir /migrations ...
# Seed:        docker compose run --rm --entrypoint /app/seed backend -reset
# Bot menu:    docker compose run --rm --no-deps --entrypoint /app/botcommands backend
FROM alpine:3.20 AS runtime
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app && mkdir -p /app
COPY --from=build /go/bin/goose /usr/local/bin/goose
COPY --from=build /out/server /app/server
COPY --from=build /out/seed /app/seed
COPY --from=build /out/botcommands /app/botcommands
USER app
WORKDIR /app
EXPOSE 8080
ENTRYPOINT ["/app/server"]
