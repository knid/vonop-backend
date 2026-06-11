FROM golang:1.25-alpine AS builder

WORKDIR /app

ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -trimpath -ldflags="-s -w" -o bin cmd/main.go
RUN go build -trimpath -ldflags="-s -w" -o worker cmd/worker/main.go
RUN go build -trimpath -ldflags="-s -w" -o loadtest_publish cmd/loadtest-publish/main.go

# flyctl is shelled out by the worker to build + push images via Fly's
# managed builder. Pulled from the official Fly distribution image.
FROM flyio/flyctl:latest AS flyctl

FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata curl git nodejs npm

RUN addgroup -S appuser \
    && adduser -S -G appuser -h /home/appuser -s /sbin/nologin appuser \
    && mkdir -p /data/repos /data/wemob-git /data/custom-domains \
    && chown -R appuser:appuser /data/repos /data/wemob-git /data/custom-domains

COPY --from=flyctl /flyctl /usr/local/bin/flyctl
COPY --from=builder --chown=appuser:appuser /app/bin /app/bin
COPY --from=builder --chown=appuser:appuser /app/worker /app/worker
COPY --from=builder --chown=appuser:appuser /app/loadtest_publish /app/loadtest_publish
COPY --from=builder --chown=appuser:appuser /app/storage /app/storage

USER appuser

RUN git config --global user.email "system@wemob.io" && \
    git config --global user.name "wemob" && \
    git config --global init.defaultBranch main

ENTRYPOINT ["/app/bin"]
