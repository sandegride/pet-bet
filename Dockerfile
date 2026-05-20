# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/app \
    ./cmd/bot/main.go

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

ENV APP_ENV=production

COPY --from=builder /out/app /app/app

USER nonroot:nonroot

ENTRYPOINT ["/app/app"]
