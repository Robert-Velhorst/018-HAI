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

FROM ghcr.io/gitleaks/gitleaks:v8.30.1@sha256:c00b6bd0aeb3071cbcb79009cb16a60dd9e0a7c60e2be9ab65d25e6bc8abbb7f AS gitleaks

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
COPY --from=gitleaks /usr/bin/gitleaks /usr/local/bin/gitleaks
COPY app.py /app/app.py
RUN groupadd --gid 10001 hai && useradd --uid 10001 --gid hai --no-create-home --shell /usr/sbin/nologin hai
# Install the same-run source-qualified ACL candidate after all other package work.
RUN --mount=type=bind,from=hai-native-acl,source=/,target=/tmp/hai-acl,readonly \
    python -I /tmp/hai-acl/verify_candidate.py /tmp/hai-acl \
    && dpkg '--path-include=/usr/share/doc/libacl1/*' -i /tmp/hai-acl/libacl1_2.4.0-1_amd64.deb \
    && python -I /tmp/hai-acl/verify_candidate.py /tmp/hai-acl --installed \
    && mkdir -p /usr/share/doc/hai-acl \
    && cp /tmp/hai-acl/qualification.json /usr/share/doc/hai-acl/qualification.json
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["python", "/app/app.py"]
