FROM python:3.15-slim

ARG DOCLING_VERSION=2.114.0

ENV PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

# Docling documents the PyTorch CPU index for Linux CPU-only deployments.
# Without it, a plain PyPI resolution can pull multi-gigabyte CUDA libraries
# even though this runner has no GPU device or GPU execution path.
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential \
    && pip install --no-cache-dir "docling==${DOCLING_VERSION}" \
        --extra-index-url https://download.pytorch.org/whl/cpu \
    && apt-get purge -y --auto-remove build-essential \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 hai \
    && useradd --uid 10001 --gid hai --no-create-home --shell /usr/sbin/nologin hai

COPY app.py /app/app.py

USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["python", "/app/app.py"]
