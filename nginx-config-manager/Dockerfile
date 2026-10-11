FROM golang:1.25.13@sha256:14e75143c833c7398ea3a5e4c673aeaae35f40e781e6b060e2f97b72c475c975 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -tags "netgo,osusergo" \
    -ldflags "-s -w" \
    -o /out/nginx-config-manager \
    ./cmd

FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce

RUN addgroup -S -g 10001 hai \
    && adduser -S -D -H -u 10001 -G hai hai

WORKDIR /app

COPY --from=builder /out/nginx-config-manager /app/nginx-config-manager

ENV HOME=/tmp \
    TMPDIR=/tmp

USER 10001:10001

ENTRYPOINT ["/app/nginx-config-manager"]
