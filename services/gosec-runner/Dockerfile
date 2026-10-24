FROM golang:1.27.3-alpine@sha256:f2c872bcee949b7be9c490efd02b48a8a2fbf947cdf533f1c58525ec9d8170fd AS builder

ARG GOSEC_VERSION=v2.30.0
RUN CGO_ENABLED=0 go install -ldflags "-X main.Version=${GOSEC_VERSION#v} -X main.GitTag=${GOSEC_VERSION}" github.com/securego/gosec/v2/cmd/gosec@${GOSEC_VERSION}

FROM python:3.15.1-alpine3.24@sha256:54448f67d7c31c39e5cdd3ea2d6382395ce142806c284425f8e897d78a0465b3
RUN apk upgrade --no-cache
RUN python -m pip uninstall --yes pip
RUN ["python", "-I", "-c", "import importlib.util,json,pathlib,sysconfig; bundled=pathlib.Path(sysconfig.get_path(\"stdlib\"))/\"ensurepip\"/\"_bundled\"; wheels=list(bundled.glob(\"pip-*.whl\")); [p.unlink() for p in wheels]; assert importlib.util.find_spec(\"pip\") is None; assert not list(bundled.glob(\"*.whl\")); print(json.dumps({\"package_installer\":\"absent\",\"bootstrap_wheels\":0,\"removed_bootstrap_wheels\":len(wheels)}))"]
COPY --from=builder /usr/local/go/ /usr/local/go/
COPY --from=builder /go/bin/gosec /usr/local/bin/gosec
ENV GOPATH=/go GOTOOLCHAIN=local PATH=/usr/local/go/bin:$PATH
COPY app.py /app/app.py
RUN addgroup -g 10004 -S hai && adduser -S -D -H -u 10004 -G hai hai
USER 10004:10004
EXPOSE 8080
ENTRYPOINT ["python", "/app/app.py"]
