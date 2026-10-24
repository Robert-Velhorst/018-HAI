FROM python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51

WORKDIR /app

COPY requirements-build.lock requirements.lock ./
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential libyaml-dev libyaml-0-2 \
    && python -m pip install --no-cache-dir --only-binary :all: --require-hashes -r requirements-build.lock \
    && python -m pip install --no-cache-dir --no-build-isolation --require-hashes -r requirements.lock \
    && python -m pip uninstall --yes cython wheel \
    && apt-get purge -y --auto-remove build-essential libyaml-dev \
    && rm -rf /var/lib/apt/lists/*

COPY app.py ./

RUN python -c "import json,platform,ssl,sqlite3; from importlib.metadata import distributions; print(json.dumps({'python':platform.python_version(),'openssl':ssl.OPENSSL_VERSION,'sqlite':sqlite3.sqlite_version,'distributions':{d.metadata['Name']:d.version for d in distributions()}})); print(open('/etc/os-release',encoding='utf-8').read())"

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    OTEL_SDK_DISABLED=true \
    DO_NOT_TRACK=true

# Dependency installation is complete; the immutable runtime has no installer.
RUN python -m pip uninstall --yes pip
RUN ["python", "-I", "-c", "import importlib.util,json,pathlib,sysconfig; bundled=pathlib.Path(sysconfig.get_path(\"stdlib\"))/\"ensurepip\"/\"_bundled\"; wheels=list(bundled.glob(\"pip-*.whl\")); [p.unlink() for p in wheels]; assert importlib.util.find_spec(\"pip\") is None; assert not list(bundled.glob(\"*.whl\")); print(json.dumps({\"package_installer\":\"absent\",\"bootstrap_wheels\":0,\"removed_bootstrap_wheels\":len(wheels)}))"]

# Install the same-run source-qualified ACL candidate after all other package work.
RUN --mount=type=bind,from=hai-native-acl,source=/,target=/tmp/hai-acl,readonly \
    python -I /tmp/hai-acl/verify_candidate.py /tmp/hai-acl \
    && dpkg '--path-include=/usr/share/doc/libacl1/*' -i /tmp/hai-acl/libacl1_2.4.0-1_amd64.deb \
    && python -I /tmp/hai-acl/verify_candidate.py /tmp/hai-acl --installed \
    && mkdir -p /usr/share/doc/hai-acl \
    && cp /tmp/hai-acl/qualification.json /usr/share/doc/hai-acl/qualification.json
EXPOSE 8080

CMD ["python", "app.py"]
