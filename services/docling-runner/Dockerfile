FROM python:3.15.1-slim@sha256:08275f764e022331c6d3e3ca55d8cce01fa4684b02111adf0445d71806fe7941

WORKDIR /app

COPY requirements-build.lock requirements.lock ./

ENV PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

# Docling documents the PyTorch CPU index for Linux CPU-only deployments.
# Without it, a plain PyPI resolution can pull multi-gigabyte CUDA libraries
# even though this runner has no GPU device or GPU execution path.
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential libyaml-dev libyaml-0-2 libstdc++6 libgomp1 zlib1g-dev \
    && python -m pip install --no-cache-dir --only-binary :all: --require-hashes -r requirements-build.lock \
    && python -m pip install --no-cache-dir --no-build-isolation --require-hashes -r requirements.lock \
    && python -m pip uninstall --yes cython wheel \
    && apt-get purge -y --auto-remove build-essential libyaml-dev zlib1g-dev \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 hai \
    && useradd --uid 10001 --gid hai --no-create-home --shell /usr/sbin/nologin hai

COPY app.py /app/app.py

USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["python", "/app/app.py"]
