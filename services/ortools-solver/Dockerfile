FROM python:3.15-slim AS ortools-build

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        build-essential \
        ca-certificates \
        cmake \
        git \
        lsb-release \
        swig \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /tmp/requirements
COPY requirements.runtime.lock requirements.build.lock ./
RUN python -m pip install --no-cache-dir --require-hashes \
    -r requirements.runtime.lock \
    -r requirements.build.lock

WORKDIR /opt/or-tools
RUN git clone --depth 1 --branch v9.15 https://github.com/google/or-tools.git . \
    && test "$(git rev-parse HEAD)" = "551ad10d94835c99e5e1e684500d3db398c0e345"

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

WORKDIR /app
COPY app.py test_app.py ./
RUN wheel="$(find /opt/or-tools/build/python/dist -maxdepth 1 -type f -name 'ortools-*.whl' -print -quit)" \
    && test -n "$wheel" \
    && python -m pip install --no-cache-dir --no-deps "$wheel" \
    && python -m unittest discover -v -s . -p 'test_app.py'

FROM python:3.15-slim AS runtime

WORKDIR /app
COPY requirements.runtime.lock ./
RUN python -m pip install --no-cache-dir --require-hashes -r requirements.runtime.lock
COPY --from=ortools-build /opt/or-tools/build/python/dist/ortools-*.whl /tmp/ortools.whl
RUN python -m pip install --no-cache-dir --no-deps /tmp/ortools.whl \
    && rm /tmp/ortools.whl
COPY app.py ./

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1
EXPOSE 8080
CMD ["python", "app.py"]
