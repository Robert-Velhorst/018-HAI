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

EXPOSE 8080

CMD ["python", "app.py"]
