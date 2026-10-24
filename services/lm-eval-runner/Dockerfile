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
COPY compat ./compat
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential libyaml-dev libyaml-0-2 libstdc++6 libgomp1 \
    && python -m pip install --no-cache-dir --only-binary :all: --require-hashes -r requirements-build.lock \
    && python compat/build_port_wheel.py --output-directory /tmp/hai-lmeval-wheel \
    && python compat/build_python_parent_ports.py --output-directory /tmp/hai-lmeval-wheel \
    && echo "0cc1a5307a648a8f858f3181e5df8c23b07f6af6681e33e8b3d102dd3e8ddf7b  /tmp/hai-lmeval-wheel/lm_eval-0.4.13+hai.optionalcache.1-py3-none-any.whl" | sha256sum --check \
    && echo "c38f4c04391c1696121d7a9e777c6f598ff6766b7aaad70c68e98c1d7ab6511e  /tmp/hai-lmeval-wheel/datasets-5.1.0+hai.fsspec.1-py3-none-any.whl" | sha256sum --check \
    && echo "dfde69c71d1e9a79dd6461dfa6a752046f4cd07ae9a6b413311e012f31e48478  /tmp/hai-lmeval-wheel/mbstrdecoder-1.1.5+hai.chardet7.1-py3-none-any.whl" | sha256sum --check \
    && echo "e62ad605649888a75504d248ccafbc2af9cc2196835259785ede5236abb814b9  /tmp/hai-lmeval-wheel/pytablewriter-1.2.1+hai.typepy2.1-py3-none-any.whl" | sha256sum --check \
    && echo "03aee58a15216438933debc0bedf6027627e4783b5979adbab3320fdeff42bf5  /tmp/hai-lmeval-wheel/typepy-2.0.0+hai.dateutil3.1-py3-none-any.whl" | sha256sum --check \
    && python -m pip install --no-cache-dir --no-build-isolation --find-links /tmp/hai-lmeval-wheel --require-hashes -r requirements.lock \
    && python -I compat/verify_installed_profile.py \
    && rm /tmp/hai-lmeval-wheel/*.whl \
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

# Install the same-run source-qualified ACL candidate after all other package work.
RUN --mount=type=bind,from=hai-native-acl,source=/,target=/tmp/hai-acl,readonly \
    python -I /tmp/hai-acl/verify_candidate.py /tmp/hai-acl \
    && dpkg '--path-include=/usr/share/doc/libacl1/*' -i /tmp/hai-acl/libacl1_2.4.0-1_amd64.deb \
    && python -I /tmp/hai-acl/verify_candidate.py /tmp/hai-acl --installed \
    && mkdir -p /usr/share/doc/hai-acl \
    && cp /tmp/hai-acl/qualification.json /usr/share/doc/hai-acl/qualification.json
EXPOSE 8080

RUN python -c "import json,platform,ssl,sqlite3; from importlib.metadata import distributions; print(json.dumps({'python':platform.python_version(),'openssl':ssl.OPENSSL_VERSION,'sqlite':sqlite3.sqlite_version,'distributions':{d.metadata['Name']:d.version for d in distributions()}})); print(open('/etc/os-release',encoding='utf-8').read())"

CMD ["python", "app.py"]
