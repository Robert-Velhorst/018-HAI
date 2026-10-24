FROM aquasec/trivy:0.76.1@sha256:790fd66d1697395d8693d1acaa1ea702406a20f109b6fc7bac8db623eca6c06c AS trivy

FROM python:3.15.1-alpine3.24@sha256:54448f67d7c31c39e5cdd3ea2d6382395ce142806c284425f8e897d78a0465b3
COPY --from=trivy /usr/local/bin/trivy /usr/local/bin/trivy
RUN apk upgrade --no-cache && trivy --version
RUN python -c "import json,platform,ssl,sqlite3; print(json.dumps({'python':platform.python_version(),'openssl':ssl.OPENSSL_VERSION,'sqlite':sqlite3.sqlite_version})); print(open('/etc/os-release',encoding='utf-8').read())"
# Install dependencies while building; the immutable runtime has no package installer.
RUN python -m pip uninstall --yes pip
RUN ["python", "-I", "-c", "import importlib.util,json,pathlib,sysconfig; bundled=pathlib.Path(sysconfig.get_path(\"stdlib\"))/\"ensurepip\"/\"_bundled\"; wheels=list(bundled.glob(\"pip-*.whl\")); [p.unlink() for p in wheels]; assert importlib.util.find_spec(\"pip\") is None; assert not list(bundled.glob(\"*.whl\")); print(json.dumps({\"package_installer\":\"absent\",\"bootstrap_wheels\":0,\"removed_bootstrap_wheels\":len(wheels)}))"]

COPY app.py /app/app.py
RUN addgroup -g 10005 -S hai && adduser -S -D -H -u 10005 -G hai hai
USER 10005:10005
EXPOSE 8080
ENTRYPOINT ["python3", "/app/app.py"]
