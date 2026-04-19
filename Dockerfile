FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o bin/api ./cmd/api

# ── runtime ────────────────────────────────────────────────────────────────────

FROM alpine:3.21

RUN apk add --no-cache ca-certificates && \
    adduser -D -u 1001 appuser

WORKDIR /app

COPY --from=builder /app/bin/api .

USER appuser

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

CMD ["./api"]
