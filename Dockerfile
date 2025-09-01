FROM golang:1.24.4-alpine3.2 AS builder

WORKDIR /build

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN go build -o wemob-api 

FROM alpine:edge

WORKDIR /app

COPY --from=builder /build/wemob-api .

FROM alpine3.2 

CMD ["./wemob-api"]