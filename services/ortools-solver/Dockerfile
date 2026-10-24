FROM python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51 AS ortools-build

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get upgrade -y \
    && apt-get install -y --no-install-recommends build-essential ca-certificates git lsb-release \
    && groupadd --gid 10001 hai-build \
    && useradd --uid 10001 --gid hai-build --no-create-home --shell /usr/sbin/nologin hai-build \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /tmp/requirements
COPY requirements.runtime.lock requirements.build.lock ./
COPY compat ./compat
RUN python -m pip install --no-cache-dir --require-hashes \
    -r requirements.runtime.lock -r requirements.build.lock
ENV HAI_DISPOSABLE_ORTOOLS_BUILD=1 OR_TOOLS_PATCH=6755 HOME=/tmp
WORKDIR /opt/or-tools
RUN git clone --depth 1 --branch v9.15 https://github.com/google/or-tools.git . \
    && test "$(git rev-parse HEAD)" = "551ad10d94835c99e5e1e684500d3db398c0e345" \
    && python /tmp/requirements/compat/apply_source_port.py --component ortools --source /opt/or-tools \
    && chown -R hai-build:hai-build /opt/or-tools
USER 10001:10001
RUN cmake --version && swig -version \
    && python -c "import json,os; from importlib.metadata import version; value={'OR_TOOLS_PATCH':os.environ['OR_TOOLS_PATCH'],'build_uid':os.getuid(),'cmake':version('cmake'),'swig':version('swig'),'protobuf_python':version('protobuf')}; assert value['build_uid']==10001 and value['cmake']=='4.4.4' and value['swig']=='4.5.1' and value['protobuf_python']=='7.36.2'; print(json.dumps(value))" > /opt/or-tools/build-tool-provenance.json \
    && cat /opt/or-tools/build-tool-provenance.json
RUN cmake -S . -B build \
        -DCMAKE_BUILD_TYPE=Release \
        -DBUILD_PYTHON=ON \
        -DBUILD_DEPS=ON \
        -DBUILD_TESTING=OFF \
        -DBUILD_SAMPLES=OFF \
        -DBUILD_EXAMPLES=OFF \
        -DBUILD_DOC=OFF \
        -DFETCH_PYTHON_DEPS=OFF \
        -DUSE_COINOR=OFF \
        -DUSE_HIGHS=OFF \
        -DUSE_SCIP=OFF
RUN cmake --build build --target python_package --config Release --parallel 2

USER root
WORKDIR /app
COPY app.py test_app.py ./
RUN wheel="$(find /opt/or-tools/build/python/dist -maxdepth 1 -type f -name 'ortools-*.whl' -print -quit)" \
    && test -n "$wheel" \
    && python -m pip install --no-cache-dir --no-deps "$wheel"
USER 10001:10001
RUN --network=none python -m unittest discover -v -s . -p 'test_app.py'
RUN python -c "import json,ortools,google.protobuf; from importlib.metadata import version; value={'ortools':version('ortools'),'ortools_module':ortools.__version__,'protobuf':google.protobuf.__version__}; assert value['ortools']==value['ortools_module']=='9.15.6755+hai.protobuf7.1' and value['protobuf']=='7.36.2'; print(json.dumps(value))"

FROM scratch AS wheel-export
COPY --from=ortools-build /opt/or-tools/build/python/dist/ /
COPY --from=ortools-build /opt/or-tools/HAI_PORT.json /ortools-source-port.json
COPY --from=ortools-build /opt/or-tools/build/_deps/protobuf-src/HAI_PORT.json /protobuf-source-port.json
COPY --from=ortools-build /opt/or-tools/build/_deps/pybind11_protobuf-src/HAI_PORT.json /pybind11-protobuf-source-port.json
COPY --from=ortools-build /opt/or-tools/build-tool-provenance.json /build-tool-provenance.json

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

ENV HOME=/tmp PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
COPY requirements.runtime.lock ./
RUN python -m pip install --no-cache-dir --require-hashes -r requirements.runtime.lock
COPY --from=ortools-build /opt/or-tools/build/python/dist/ortools-*.whl /tmp/
RUN wheel="$(find /tmp -maxdepth 1 -type f -name 'ortools-*.whl' -print -quit)" \
    && test -n "$wheel" \
    && python -m pip install --no-cache-dir --no-deps "$wheel" \
    && rm "$wheel"
COPY --from=ortools-build /opt/or-tools/HAI_PORT.json /usr/share/doc/hai-ortools/ortools-source-port.json
COPY --from=ortools-build /opt/or-tools/build/_deps/protobuf-src/HAI_PORT.json /usr/share/doc/hai-ortools/protobuf-source-port.json
COPY --from=ortools-build /opt/or-tools/build/_deps/pybind11_protobuf-src/HAI_PORT.json /usr/share/doc/hai-ortools/pybind11-protobuf-source-port.json
COPY --from=ortools-build /opt/or-tools/build-tool-provenance.json /usr/share/doc/hai-ortools/build-tool-provenance.json
# Install dependencies while building; the immutable runtime has no package installer.
RUN python -m pip uninstall --yes pip
RUN ["python", "-I", "-c", "import importlib.util,json,pathlib,sysconfig; bundled=pathlib.Path(sysconfig.get_path(\"stdlib\"))/\"ensurepip\"/\"_bundled\"; wheels=list(bundled.glob(\"pip-*.whl\")); [p.unlink() for p in wheels]; assert importlib.util.find_spec(\"pip\") is None; assert not list(bundled.glob(\"*.whl\")); print(json.dumps({\"package_installer\":\"absent\",\"bootstrap_wheels\":0,\"removed_bootstrap_wheels\":len(wheels)}))"]

COPY app.py test_app.py ./
RUN groupadd --gid 10001 hai \
    && useradd --uid 10001 --gid hai --no-create-home --shell /usr/sbin/nologin hai
USER 10001:10001
RUN --network=none python -m unittest discover -v -s . -p 'test_app.py'
RUN python -c "import json,ortools,google.protobuf; from importlib.metadata import version; value={'ortools':version('ortools'),'ortools_module':ortools.__version__,'protobuf':google.protobuf.__version__}; assert value['ortools']==value['ortools_module']=='9.15.6755+hai.protobuf7.1' and value['protobuf']=='7.36.2'; print(json.dumps(value))"
RUN python -c "import json,platform,ssl,sqlite3; from importlib.metadata import distributions; print(json.dumps({'python':platform.python_version(),'openssl':ssl.OPENSSL_VERSION,'sqlite':sqlite3.sqlite_version,'distributions':{d.metadata['Name']:d.version for d in distributions()}})); print(open('/etc/os-release',encoding='utf-8').read())"
EXPOSE 8080
CMD ["python", "app.py"]
