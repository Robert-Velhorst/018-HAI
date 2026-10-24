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


COPY requirements-build.lock requirements.lock ./
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential libyaml-dev libyaml-0-2 libstdc++6 libgomp1 \
    && python -m pip install --no-cache-dir --only-binary :all: --require-hashes -r requirements-build.lock \
    && python -m pip install --no-cache-dir --no-build-isolation --require-hashes -r requirements.lock \
    && python -m pip uninstall --yes cython setuptools-scm wheel \
    && apt-get purge -y --auto-remove build-essential libyaml-dev \
    && rm -rf /var/lib/apt/lists/*

# Install dependencies while building; the immutable runtime has no package installer.
RUN python -m pip uninstall --yes pip
RUN ["python", "-I", "-c", "import importlib.util,json,pathlib,sysconfig; bundled=pathlib.Path(sysconfig.get_path(\"stdlib\"))/\"ensurepip\"/\"_bundled\"; wheels=list(bundled.glob(\"pip-*.whl\")); [p.unlink() for p in wheels]; assert importlib.util.find_spec(\"pip\") is None; assert not list(bundled.glob(\"*.whl\")); print(json.dumps({\"package_installer\":\"absent\",\"bootstrap_wheels\":0,\"removed_bootstrap_wheels\":len(wheels)}))"]

COPY app.py ./
COPY test_app.py ./
COPY tasks ./tasks

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    HF_HOME=/tmp/huggingface \
    XDG_CACHE_HOME=/tmp/cache \
    HF_HUB_OFFLINE=1 \
    TRANSFORMERS_OFFLINE=1 \
    DATASETS_OFFLINE=1

EXPOSE 8080

RUN python -c "import json,platform,ssl,sqlite3; from importlib.metadata import distributions; print(json.dumps({'python':platform.python_version(),'openssl':ssl.OPENSSL_VERSION,'sqlite':sqlite3.sqlite_version,'distributions':{d.metadata['Name']:d.version for d in distributions()}})); print(open('/etc/os-release',encoding='utf-8').read())"

CMD ["python", "app.py"]
