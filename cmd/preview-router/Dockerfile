FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /router ./cmd/preview-router/

FROM alpine:3.20

COPY --from=builder /router /usr/local/bin/router

EXPOSE 8080

CMD ["router"]
