FROM golang:1.25-alpine AS builder 

WORKDIR /app

ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -trimpath -ldflags="-s -w" -o bin .

FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

RUN addgroup -S appuser \
    && adduser -S -G appuser -H -s /sbin/nologin appuser

COPY --from=builder --chown=appuser:appuser /app/bin /app/bin

USER appuser

ENTRYPOINT ["/app/bin"]