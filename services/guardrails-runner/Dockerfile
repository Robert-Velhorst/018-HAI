FROM python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51

WORKDIR /app
ENV HOME=/tmp \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    OTEL_SDK_DISABLED=true
COPY compat ./compat
RUN python compat/build_port_wheel.py --output-directory /app/wheels \
    && echo "4cfe4d4d7cd4ab044bda2b47cafafe8f7a7c1a9007351ba75994ebed89176353  /app/wheels/guardrails_ai-0.11.0+hai.offline.1-py3-none-any.whl" | sha256sum --check
COPY requirements.lock ./
RUN python -m pip install --no-cache-dir --only-binary :all: --find-links /app/wheels --require-hashes -r requirements.lock
COPY app.py test_app.py ./
RUN groupadd --gid 10001 hai \
    && useradd --uid 10001 --gid hai --no-create-home --shell /usr/sbin/nologin hai
USER 10001:10001
RUN --network=none python -m unittest discover -v -s . -p 'test_app.py'
RUN python -c "import json,platform,ssl,sqlite3; from importlib.metadata import distributions; print(json.dumps({'python':platform.python_version(),'openssl':ssl.OPENSSL_VERSION,'sqlite':sqlite3.sqlite_version,'distributions':{d.metadata['Name']:d.version for d in distributions()}})); print(open('/etc/os-release',encoding='utf-8').read())"
EXPOSE 8080
CMD ["python", "app.py"]
