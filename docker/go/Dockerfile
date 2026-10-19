# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
RUN CGO_ENABLED=0 go install github.com/pressly/goose/v3/cmd/goose@v3.24.1
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/app \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/seed ./cmd/seed \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/botcommands ./cmd/botcommands

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
