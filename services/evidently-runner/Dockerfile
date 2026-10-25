# The source-bound Dynaconf profile and current external libraries target Python 3.15.1.
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
ENV HOME=/tmp DO_NOT_TRACK=1
COPY compat ./compat
RUN python compat/build_port_wheel.py --output-directory /app/wheels \
    && echo "2db131ed9ddfc1dc15a25635c8a6e58d51e608fe7459c504f091c5bd2168bd52  /app/wheels/dynaconf-3.3.5+hai.evidently.1-py3-none-any.whl" | sha256sum --check
RUN python compat/build_evidently_wheel.py --output-directory /app/wheels \
    && echo "e604b98d65f00e043ffdf1a2c33a059050f131848f79c04b17d0a2d38bdbc72c  /app/wheels/evidently-0.7.23+hai.plotly.1-py3-none-any.whl" | sha256sum --check
COPY requirements-build.lock requirements.lock ./
# PyYAML 6.0.3 has no CPython 3.15 wheel. Build its verified source with pinned tools.
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential libyaml-dev libyaml-0-2 \
    && python -m pip install --no-cache-dir --only-binary :all: --require-hashes -r requirements-build.lock \
    && python -m pip install --no-cache-dir --no-build-isolation --find-links /app/wheels --require-hashes -r requirements.lock \
    && python -m pip uninstall --yes cython setuptools-scm vcs-versioning wheel \
    && apt-get purge -y --auto-remove build-essential libyaml-dev \
    && rm -rf /var/lib/apt/lists/*
COPY compat_pandas.py apply_vendor_compat.py ./
RUN python apply_vendor_compat.py --apply-to-sdk
# Install dependencies while building; the immutable runtime has no package installer.
RUN python -m pip uninstall --yes pip
RUN ["python", "-I", "-c", "import importlib.util,json,pathlib,sysconfig; bundled=pathlib.Path(sysconfig.get_path(\"stdlib\"))/\"ensurepip\"/\"_bundled\"; wheels=list(bundled.glob(\"pip-*.whl\")); [p.unlink() for p in wheels]; assert importlib.util.find_spec(\"pip\") is None; assert not list(bundled.glob(\"*.whl\")); print(json.dumps({\"package_installer\":\"absent\",\"bootstrap_wheels\":0,\"removed_bootstrap_wheels\":len(wheels)}))"]

COPY app.py ./
COPY test_app.py ./
RUN groupadd --gid 10001 hai \
    && useradd --uid 10001 --gid hai --no-create-home --shell /usr/sbin/nologin hai
# Install the same-run source-qualified ACL candidate after all other package work.
RUN --mount=type=bind,from=hai-native-acl,source=/,target=/tmp/hai-acl,readonly \
    python -I /tmp/hai-acl/verify_candidate.py /tmp/hai-acl \
    && dpkg '--path-include=/usr/share/doc/libacl1/*' -i /tmp/hai-acl/libacl1_2.4.0-1_amd64.deb \
    && python -I /tmp/hai-acl/verify_candidate.py /tmp/hai-acl --installed \
    && mkdir -p /usr/share/doc/hai-acl \
    && cp /tmp/hai-acl/qualification.json /usr/share/doc/hai-acl/qualification.json
USER 10001:10001
RUN --network=none python -m unittest discover -v -s . -p 'test_app.py'

RUN python -c "import json,platform,ssl,sqlite3; from importlib.metadata import distributions; print(json.dumps({'python':platform.python_version(),'openssl':ssl.OPENSSL_VERSION,'sqlite':sqlite3.sqlite_version,'distributions':{d.metadata['Name']:d.version for d in distributions()}})); print(open('/etc/os-release',encoding='utf-8').read())"

ENV DO_NOT_TRACK=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1
EXPOSE 8080
CMD ["python", "app.py"]
