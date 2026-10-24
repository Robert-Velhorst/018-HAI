FROM python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51 AS packages
WORKDIR /app
COPY requirements.lock ./
# Prepared from pinned public sources by the hosted restricted compiler SDK.
# pip verifies every wheel against the canonical lock, including all local ports.
COPY --from=hai-miniswe-wheels /litellm-1.106.3+hai.py315.2-cp310-abi3-manylinux_2_28_x86_64.whl /wheels/
COPY --from=hai-miniswe-wheels /fastuuid-0.14.0+hai.py315.2-cp315-cp315-linux_x86_64.whl /wheels/
COPY --from=hai-miniswe-wheels /datasets-5.1.0+hai.fsspec.1-py3-none-any.whl /wheels/
COPY --from=hai-miniswe-wheels /botocore-1.43.118+hai.dateutil3.1-py3-none-any.whl /wheels/
RUN python -m pip install --no-cache-dir --only-binary numpy,scipy,pandas,pyarrow,tokenizers --find-links /wheels --require-hashes -r requirements.lock

FROM python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51 AS native-build

WORKDIR /native-build
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get upgrade -y \
    && apt-get install -y --no-install-recommends build-essential libbluetooth-dev perl pkg-config \
    && useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin native-builder \
    && chown native-builder:native-builder /native-build
COPY --from=hai-native-source build_runtime.py sources.json ./
COPY native-runtime.profile.json ./profile.json
USER native-builder
ENV HOME=/tmp
RUN HAI_DISPOSABLE_BUILD_SDK=1 python -I build_runtime.py

FROM python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51 AS runtime

WORKDIR /app
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get upgrade -y
ENV LD_LIBRARY_PATH=/opt/hai-native/lib \
    SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt \
    SSL_CERT_DIR=/etc/ssl/certs
RUN --mount=type=bind,from=native-build,source=/native-build/out,target=/tmp/hai-native,readonly \
    dpkg -i /tmp/hai-native/hai-openssl4.deb /tmp/hai-native/hai-sqlite3.deb
COPY --chown=0:0 --from=native-build /native-build/out/lib-dynload/ /usr/local/lib/python3.15/lib-dynload/
COPY --chown=0:0 --from=native-build /native-build/out/provenance.json /native-build/out/CPython-LICENSE /usr/share/doc/hai-native/

ENV HOME=/tmp \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    LITELLM_LOCAL_MODEL_COST_MAP=True \
    MSWEA_SILENT_STARTUP=1 \
    HF_HUB_OFFLINE=1 \
    HF_HUB_DISABLE_TELEMETRY=1
COPY --from=packages /usr/local/lib/python3.15/site-packages/ /usr/local/lib/python3.15/site-packages/
COPY --from=packages /usr/local/bin/ /usr/local/bin/
COPY compat/verify_sdk_import.py ./compat/verify_sdk_import.py
COPY --from=hai-miniswe-wheels /fastuuid-build.json /usr/share/doc/hai-miniswe/fastuuid-build.json
COPY --from=hai-miniswe-wheels /upstream-case-qualification.json /usr/share/doc/hai-miniswe/upstream-case-qualification.json
# Install dependencies while building; the immutable runtime has no package installer.
RUN python -m pip uninstall --yes pip
RUN ["python", "-I", "-c", "import importlib.util,json,pathlib,sysconfig; bundled=pathlib.Path(sysconfig.get_path(\"stdlib\"))/\"ensurepip\"/\"_bundled\"; wheels=list(bundled.glob(\"pip-*.whl\")); [p.unlink() for p in wheels]; assert importlib.util.find_spec(\"pip\") is None; assert not list(bundled.glob(\"*.whl\")); print(json.dumps({\"package_installer\":\"absent\",\"bootstrap_wheels\":0,\"removed_bootstrap_wheels\":len(wheels)}))"]

COPY app.py test_app.py ./
RUN groupadd --gid 10001 hai \
    && useradd --uid 10001 --gid hai --no-create-home --shell /usr/sbin/nologin hai \
    && mkdir /qualification && chown 10001:10001 /qualification
# Install the same-run source-qualified ACL candidate after all other package work.
RUN --mount=type=bind,from=hai-native-acl,source=/,target=/tmp/hai-acl,readonly \
    python -I /tmp/hai-acl/verify_candidate.py /tmp/hai-acl \
    && dpkg '--path-include=/usr/share/doc/libacl1/*' -i /tmp/hai-acl/libacl1_2.4.0-1_amd64.deb \
    && python -I /tmp/hai-acl/verify_candidate.py /tmp/hai-acl --installed \
    && mkdir -p /usr/share/doc/hai-acl \
    && cp /tmp/hai-acl/qualification.json /usr/share/doc/hai-acl/qualification.json
USER 10001:10001
# Read-only package view uses the exact bytes copied into the final runtime.
RUN --network=none --mount=type=bind,from=packages,source=/usr/local/lib/python3.15/site-packages,target=/installed,readonly --mount=type=bind,from=hai-miniswe-wheels,target=/inputs,readonly \
    HAI_DISPOSABLE_PUBLIC_SDK=1 python -I compat/verify_sdk_import.py --site-packages /installed --receipt /qualification/sdk-import.json
RUN --network=none python -m unittest discover -v -s . -p 'test_app.py'
RUN python -c "import json,platform,ssl,sqlite3; from importlib.metadata import distributions; print(json.dumps({'python':platform.python_version(),'openssl':ssl.OPENSSL_VERSION,'sqlite':sqlite3.sqlite_version,'distributions':{d.metadata['Name']:d.version for d in distributions()}})); print(open('/etc/os-release',encoding='utf-8').read())"
EXPOSE 8080
CMD ["python", "app.py"]
