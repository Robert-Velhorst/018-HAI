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

ENV PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

# Docling documents the PyTorch CPU index for Linux CPU-only deployments.
# Without it, a plain PyPI resolution can pull multi-gigabyte CUDA libraries
# even though this runner has no GPU device or GPU execution path.
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential libyaml-dev libyaml-0-2 libstdc++6 libgomp1 zlib1g-dev \
    && python -m pip install --no-cache-dir --only-binary :all: --require-hashes -r requirements-build.lock \
    && python -m pip install --no-cache-dir --no-build-isolation --require-hashes -r requirements.lock \
    && python -m pip uninstall --yes cython setuptools-scm wheel \
    && apt-get purge -y --auto-remove build-essential libyaml-dev zlib1g-dev \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 hai \
    && useradd --uid 10001 --gid hai --no-create-home --shell /usr/sbin/nologin hai

COPY app.py /app/app.py

USER 10001:10001
EXPOSE 8080
RUN python -c "import json,platform,ssl,sqlite3; from importlib.metadata import distributions; print(json.dumps({'python':platform.python_version(),'openssl':ssl.OPENSSL_VERSION,'sqlite':sqlite3.sqlite_version,'distributions':{d.metadata['Name']:d.version for d in distributions()}})); print(open('/etc/os-release',encoding='utf-8').read())"

ENTRYPOINT ["python", "/app/app.py"]
