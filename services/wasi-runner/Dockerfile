FROM python:3.15-slim

ARG WASMTIME_VERSION=45.0.0
ARG WASMTIME_SHA256=9d92e6dc04630f617e0e5d532327a5a917ac4898587e07f4fb7a5fc7fffef760
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl xz-utils coreutils \
    && curl -fsSL -o /tmp/wasmtime.tar.xz "https://github.com/bytecodealliance/wasmtime/releases/download/v${WASMTIME_VERSION}/wasmtime-v${WASMTIME_VERSION}-x86_64-linux.tar.xz" \
    && echo "${WASMTIME_SHA256}  /tmp/wasmtime.tar.xz" | sha256sum -c - \
    && tar -xJf /tmp/wasmtime.tar.xz -C /tmp \
    && install -m 0755 /tmp/wasmtime-v${WASMTIME_VERSION}-x86_64-linux/wasmtime /usr/local/bin/wasmtime \
    && rm -rf /tmp/wasmtime* /var/lib/apt/lists/*
WORKDIR /app
COPY server.py ./
RUN useradd --system --uid 10001 wasi
USER wasi
EXPOSE 8080
CMD ["python", "server.py"]
