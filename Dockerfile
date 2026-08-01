# syntax=docker/dockerfile:1

FROM golang:1.23-alpine AS builder
WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum* ./
RUN go mod download || true

COPY . .
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/kubeevents ./cmd/kubeevents

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=builder /out/kubeevents /kubeevents
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/kubeevents"]
