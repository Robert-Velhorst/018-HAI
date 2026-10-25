FROM python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51 AS native-python-sdk
FROM gcc:16.2.0-trixie@sha256:3f73339bf0d3d64081cac3f6d9fdafc6dd4cef2b9c49e0a09f91e97cb0eb139a AS native-client-build

COPY --from=native-python-sdk /usr/local/ /usr/local/
WORKDIR /native-build
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get upgrade -y \
    && apt-get install -y --no-install-recommends ca-certificates libbluetooth-dev perl pkg-config \
    && useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin native-builder \
    && chown native-builder:native-builder /native-build
COPY --from=hai-native-source build_runtime.py sources.json ./
COPY native-runtime.profile.json ./profile.json
USER native-builder
ENV HOME=/tmp
RUN test "$(cc -dumpfullversion)" = "16.2.0" \
    && HAI_DISPOSABLE_BUILD_SDK=1 python -I build_runtime.py

FROM python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51

RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get upgrade -y \
    && rm -rf /var/lib/apt/lists/*
ENV LD_LIBRARY_PATH=/opt/hai-native/lib \
    SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt \
    SSL_CERT_DIR=/etc/ssl/certs
RUN --mount=type=bind,from=native-client-build,source=/native-build/out,target=/tmp/hai-native,readonly \
    dpkg -i /tmp/hai-native/hai-openssl4.deb /tmp/hai-native/hai-sqlite3.deb
COPY --chown=0:0 --from=native-client-build /native-build/out/lib-dynload/ /usr/local/lib/python3.15/lib-dynload/
COPY --chown=0:0 --from=native-client-build /native-build/out/provenance.json /native-build/out/CPython-LICENSE /usr/share/doc/hai-native/

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
# Install the same-run source-qualified ACL candidate after all other package work.
RUN --mount=type=bind,from=hai-native-acl,source=/,target=/tmp/hai-acl,readonly \
    python -I /tmp/hai-acl/verify_candidate.py /tmp/hai-acl \
    && dpkg '--path-include=/usr/share/doc/libacl1/*' -i /tmp/hai-acl/libacl1_2.4.0-1_amd64.deb \
    && python -I /tmp/hai-acl/verify_candidate.py /tmp/hai-acl --installed \
    && mkdir -p /usr/share/doc/hai-acl \
    && cp /tmp/hai-acl/qualification.json /usr/share/doc/hai-acl/qualification.json
USER wasi
EXPOSE 8080
CMD ["python", "server.py"]
