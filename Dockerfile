FROM golang:1.25-alpine AS builder 

WORKDIR /app

ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -trimpath -ldflags="-s -w" -o bin cmd/main.go
RUN go build -trimpath -ldflags="-s -w" -o migrate_to_git cmd/migrate_to_git/main.go

FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata curl git

RUN addgroup -S appuser \
    && adduser -S -G appuser -H -s /sbin/nologin appuser \
    && mkdir -p /data/repos \
    && chown appuser:appuser /data/repos

COPY --from=builder --chown=appuser:appuser /app/bin /app/bin
COPY --from=builder --chown=appuser:appuser /app/migrate_to_git /app/migrate_to_git
COPY --from=builder --chown=appuser:appuser /app/storage /app/storage

USER appuser

RUN git config --global user.email "system@wemob.io" && \
    git config --global user.name "wemob" && \
    git config --global init.defaultBranch main

ENTRYPOINT ["/app/bin"]