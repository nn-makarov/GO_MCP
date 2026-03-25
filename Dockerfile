FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o mcp-server main.go
RUN go build -o telegram-bot bot.go

FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /app/mcp-server .
COPY --from=builder /app/telegram-bot .

EXPOSE 8080 8081

CMD ["sh", "-c", "./mcp-server & ./telegram-bot"]