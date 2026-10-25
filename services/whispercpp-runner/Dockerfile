FROM python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51 AS native-python-sdk
FROM gcc:16.2.0-trixie@sha256:3f73339bf0d3d64081cac3f6d9fdafc6dd4cef2b9c49e0a09f91e97cb0eb139a AS native-client-build

COPY --from=native-python-sdk /usr/local/ /usr/local/
WORKDIR /native-build
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get upgrade -y \
    && apt-get install -y --no-install-recommends ca-certificates libbluetooth-dev perl pkg-config \
    && useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin native-builder \
    && chown native-builder:native-builder /native-build
COPY --from=hai-native-source build_runtime.py sources.json ./
COPY native-runtime.profile.json ./profile.json
USER native-builder
ENV HOME=/tmp
RUN test "$(cc -dumpfullversion)" = "16.2.0" \
    && HAI_DISPOSABLE_BUILD_SDK=1 python -I build_runtime.py

FROM python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51 AS python-sdk
FROM gcc:16.2.0-trixie@sha256:3f73339bf0d3d64081cac3f6d9fdafc6dd4cef2b9c49e0a09f91e97cb0eb139a AS build

COPY --from=python-sdk /usr/local/ /usr/local/
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get upgrade -y \
    && apt-get install -y --no-install-recommends ca-certificates git \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 hai-build \
    && useradd --uid 10001 --gid hai-build --no-create-home --shell /usr/sbin/nologin hai-build \
    && install -d -o 10001 -g 10001 /src /out
COPY requirements.build.lock /tmp/requirements.build.lock
RUN python -m pip install --no-cache-dir --only-binary :all: --require-hashes -r /tmp/requirements.build.lock
COPY build_provenance.py /review/build_provenance.py
USER 10001:10001
ENV HOME=/tmp HAI_DISPOSABLE_BUILD_SDK=1
RUN git clone --depth 1 --branch v1.9.5 https://github.com/ggml-org/whisper.cpp.git /src/whisper.cpp \
    && test "$(git -C /src/whisper.cpp rev-parse HEAD)" = "d1be6fde11ac6e0407606b4e42fe72d34add8037" \
    && test "$(g++ -dumpfullversion)" = "16.2.0" \
    && cmake -S /src/whisper.cpp -B /src/whisper.cpp/build \
       -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_LIBDIR=lib \
       -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_IS_DEV=OFF -DGGML_NATIVE=OFF \
    && cmake --build /src/whisper.cpp/build --config Release --parallel 2 \
    && cmake --install /src/whisper.cpp/build --prefix /out/whisper \
    && mkdir -p /out/runtime-libs \
    && for lib in libstdc++.so.6 libgcc_s.so.1 libgomp.so.1; do \
         libpath="$(g++ -print-file-name="$lib")"; test -f "$libpath"; cp -L "$libpath" "/out/runtime-libs/$lib"; \
       done \
    && python -I /review/build_provenance.py

FROM python:3.15.1-slim-trixie@sha256:533b0b2f9b5d46fd097091cf826f27902f1d6c3bca714b6dc74551356aa97a51
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get upgrade -y \
    && apt-get install -y --no-install-recommends ca-certificates ffmpeg \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 65532 --create-home hai
COPY --from=build /out/whisper/ /opt/hai-whisper/
COPY --from=build /out/runtime-libs/ /opt/hai-whisper-runtime/lib/
COPY --from=build /out/build-provenance.json /usr/share/doc/hai-whisper/build-provenance.json
RUN ln -s /opt/hai-whisper/bin/whisper-cli /usr/local/bin/whisper-cli \
    && printf '%s\n' /opt/hai-whisper/lib /opt/hai-whisper-runtime/lib > /etc/ld.so.conf.d/hai-whisper.conf \
    && ldconfig
ENV LD_LIBRARY_PATH=/opt/hai-native/lib \
    SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt \
    SSL_CERT_DIR=/etc/ssl/certs
RUN --mount=type=bind,from=native-client-build,source=/native-build/out,target=/tmp/hai-native,readonly \
    dpkg -i /tmp/hai-native/hai-openssl4.deb /tmp/hai-native/hai-sqlite3.deb
COPY --chown=0:0 --from=native-client-build /native-build/out/lib-dynload/ /usr/local/lib/python3.15/lib-dynload/
COPY --chown=0:0 --from=native-client-build /native-build/out/provenance.json /native-build/out/CPython-LICENSE /usr/share/doc/hai-native/

RUN python -m pip uninstall --yes pip
RUN ["python", "-I", "-c", "import importlib.util,json,pathlib,sysconfig; bundled=pathlib.Path(sysconfig.get_path(\"stdlib\"))/\"ensurepip\"/\"_bundled\"; wheels=list(bundled.glob(\"pip-*.whl\")); [p.unlink() for p in wheels]; assert importlib.util.find_spec(\"pip\") is None; assert not list(bundled.glob(\"*.whl\")); print(json.dumps({\"package_installer\":\"absent\",\"bootstrap_wheels\":0,\"removed_bootstrap_wheels\":len(wheels)}))"]
COPY app.py /app/app.py
ENV LD_LIBRARY_PATH=/opt/hai-native/lib:/opt/hai-whisper/lib:/opt/hai-whisper-runtime/lib
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["python", "/app/app.py"]
