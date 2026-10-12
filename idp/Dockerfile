FROM golang:1.26.9-alpine@sha256:3082400e369fa24d5fc60bca20edab3f6d604e0c5a690ec66b295eff4dd87ade AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -tags "netgo,osusergo" \
    -ldflags "-s -w" \
    -o /out/idp \
    ./cmd

# The IDP needs only a CA trust store for OAuth/SMTP TLS and BusyBox wget for
# its HTTP health probe. Pinning Alpine by digest keeps rebuilds reproducible.
FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 hai \
    && adduser -S -D -H -u 10001 -G hai hai

WORKDIR /app

COPY --from=builder /out/idp /app/idp
COPY --from=builder /app/docs /app/docs

ENV HOME=/tmp \
    TMPDIR=/tmp

USER 10001:10001

ENTRYPOINT ["/app/idp"]
