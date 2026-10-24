FROM python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51

RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get upgrade -y \
    && rm -rf /var/lib/apt/lists/*
RUN python -m pip uninstall --yes pip
RUN ["python", "-I", "-c", "import importlib.util,json,pathlib,sysconfig; bundled=pathlib.Path(sysconfig.get_path(\"stdlib\"))/\"ensurepip\"/\"_bundled\"; wheels=list(bundled.glob(\"pip-*.whl\")); [p.unlink() for p in wheels]; assert importlib.util.find_spec(\"pip\") is None; assert not list(bundled.glob(\"*.whl\")); print(json.dumps({\"package_installer\":\"absent\",\"bootstrap_wheels\":0,\"removed_bootstrap_wheels\":len(wheels)}))"]

ARG WASMTIME_VERSION=50.0.0
ARG WASMTIME_SHA256=77c19ee0437991f9ac7837b9c269d9c7076744f85dad263fea5214cc2723c7ff
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
