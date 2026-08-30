FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod ./
COPY cmd ./cmd
RUN go build -o /out/mcp-server ./cmd/mcp-server
RUN go build -o /out/bot ./cmd/bot

FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/mcp-server /out/bot ./
# Какой бинарник запускать — задаёт docker-compose через command.
