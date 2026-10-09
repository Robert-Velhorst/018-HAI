# Stage 1: Compiling the application
FROM golang:1.25.13 AS builder

# Set the Current Working Directory inside the container
WORKDIR /app
ARG HAI_GO_BUILD_WORKERS=2

# Cache dependencies independently from application source. A normal code
# change should rebuild the binary, not redownload the complete module graph.
COPY go.mod go.sum ./
RUN go mod download

# Copy the application after the stable dependency layer.
COPY . .

# The provider fixture is a static, local-only compatibility server used by an
# opt-in Compose profile. It shares the module cache with the backend build but
# contains no model runtime or external dependency.
FROM builder AS provider-fixture-builder
WORKDIR /app
RUN case "$HAI_GO_BUILD_WORKERS" in 1|2|4|8) ;; *) exit 1 ;; esac \
    && GOMAXPROCS="$HAI_GO_BUILD_WORKERS" CGO_ENABLED=0 GOOS=linux \
    go build -p "$HAI_GO_BUILD_WORKERS" -o /provider-fixture ./cmd/provider-fixture

FROM scratch AS provider-fixture
COPY --from=provider-fixture-builder /provider-fixture /provider-fixture
USER 65532:65532
EXPOSE 11434
ENTRYPOINT ["/provider-fixture"]

# Navigate to the cmd directory where the main.go file resides
FROM builder AS backend-builder
WORKDIR /app/cmd

# Build the Go app
RUN case "$HAI_GO_BUILD_WORKERS" in 1|2|4|8) ;; *) exit 1 ;; esac \
    && GOMAXPROCS="$HAI_GO_BUILD_WORKERS" CGO_ENABLED=1 GOOS=linux \
    go build -p "$HAI_GO_BUILD_WORKERS" -o app ./main.go

# Stage 2: Build the minimal docker image
FROM ubuntu:24.04

# curl backs the container healthcheck, which polls /readyz. It has to live in
# the runtime image, not just the builder.
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl ca-certificates passwd \
    && rm -rf /var/lib/apt/lists/*

RUN groupadd --gid 10001 hai \
    && useradd --uid 10001 --gid 10001 --no-create-home --shell /usr/sbin/nologin hai \
    && chmod 0755 /root \
    && install -d -o 10001 -g 10001 /app /root/images /root/agent-workspaces /root/phase2-control-state

WORKDIR /app

# Copy the pre-built binary from the previous stage
COPY --from=backend-builder --chown=10001:10001 /app/cmd/app /app/app
COPY --from=backend-builder --chown=10001:10001 /app/docs /app/docs



USER 10001:10001

# Command to run
CMD ["/app/app"]
