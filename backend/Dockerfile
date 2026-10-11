FROM golang:1.25.13@sha256:14e75143c833c7398ea3a5e4c673aeaae35f40e781e6b060e2f97b72c475c975 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# The backend does not require libc. A static, stripped binary lets the runtime
# image stay small and removes the dynamic-library surface that CGO introduced.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -tags "netgo,osusergo" \
    -ldflags "-s -w" \
    -o /out/hai-backend \
    ./cmd

# Alpine supplies the CA bundle needed by outbound HTTPS and BusyBox wget for
# the Compose readiness probe. Pin the runtime digest so production rebuilds do
# not silently pick up a different base image.
FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce

RUN addgroup -S -g 10001 hai \
    && adduser -S -D -H -u 10001 -G hai hai \
    && mkdir -p /app/docs /root/images /root/automation-scripts \
        /root/connected-sources /root/agent-workspaces \
        /var/lib/hai/phase2-state \
    && chown -R 10001:10001 /root/images /root/agent-workspaces \
        /var/lib/hai/phase2-state \
    && chmod 0711 /root \
    && chmod 0750 /root/images /root/agent-workspaces \
        /var/lib/hai/phase2-state \
    && chmod 0550 /root/automation-scripts /root/connected-sources

WORKDIR /app

COPY --from=builder /out/hai-backend /app/hai-backend
COPY --from=builder /app/docs /app/docs

ENV HOME=/tmp \
    TMPDIR=/tmp

USER 10001:10001

ENTRYPOINT ["/app/hai-backend"]
