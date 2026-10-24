FROM python:3.15.1-slim@sha256:08275f764e022331c6d3e3ca55d8cce01fa4684b02111adf0445d71806fe7941

WORKDIR /app

COPY requirements-build.lock requirements.lock ./
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential libyaml-dev libyaml-0-2 \
    && python -m pip install --no-cache-dir --only-binary :all: --require-hashes -r requirements-build.lock \
    && python -m pip install --no-cache-dir --no-build-isolation --require-hashes -r requirements.lock \
    && python -m pip uninstall --yes cython \
    && apt-get purge -y --auto-remove build-essential libyaml-dev \
    && rm -rf /var/lib/apt/lists/*

COPY app.py ./
COPY test_app.py ./

RUN useradd --create-home --uid 10001 --shell /usr/sbin/nologin hai

ENV HOME=/tmp \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    DEEPEVAL_DISABLE_DOTENV=1 \
    DEEPEVAL_DISABLE_LEGACY_KEYFILE=1 \
    DEEPEVAL_TELEMETRY_OPT_OUT=YES

EXPOSE 8080

USER 10001:10001

# DeepEval persists a small local identity/config directory even when telemetry
# export is disabled. Keep that ephemeral state inside the capped tmpfs mount.
WORKDIR /tmp

CMD ["python", "/app/app.py"]
