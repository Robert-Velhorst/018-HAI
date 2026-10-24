# dynaconf 3.3.5 currently declares Python <3.15 on PyPI; use the newest compatible line.
FROM python:3.14.9-slim-trixie@sha256:35caba83af2fe6da9a5d922f9b0156f21d1246a48278c58a08bd253303ba42e8 AS native-build

WORKDIR /native-build
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential perl pkg-config
COPY --from=hai-native-source build_runtime.py sources.json ./
COPY native-runtime.profile.json ./profile.json
RUN HAI_DISPOSABLE_BUILD_SDK=1 python -I build_runtime.py

FROM python:3.14.9-slim-trixie@sha256:35caba83af2fe6da9a5d922f9b0156f21d1246a48278c58a08bd253303ba42e8 AS runtime

WORKDIR /app
ENV LD_LIBRARY_PATH=/opt/hai-native/lib
RUN --mount=type=bind,from=native-build,source=/native-out,target=/tmp/hai-native,readonly \
    dpkg -i /tmp/hai-native/hai-openssl4.deb /tmp/hai-native/hai-sqlite3.deb
COPY --from=native-build /native-out/lib-dynload/ /usr/local/lib/python3.14/lib-dynload/
COPY --from=native-build /native-out/provenance.json /native-out/CPython-LICENSE /usr/share/doc/hai-native/
COPY requirements.lock ./
RUN python -m pip install --no-cache-dir --only-binary :all: --require-hashes -r requirements.lock
COPY compat_pandas.py apply_vendor_compat.py ./
RUN python apply_vendor_compat.py --apply-to-sdk
COPY app.py ./
COPY test_app.py ./

RUN python -c "import json,platform,ssl,sqlite3; from importlib.metadata import distributions; print(json.dumps({'python':platform.python_version(),'openssl':ssl.OPENSSL_VERSION,'sqlite':sqlite3.sqlite_version,'distributions':{d.metadata['Name']:d.version for d in distributions()}})); print(open('/etc/os-release',encoding='utf-8').read())"

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1
EXPOSE 8080
CMD ["python", "app.py"]
