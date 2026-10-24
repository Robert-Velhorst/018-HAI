# deepteam 1.0.10 currently declares Python <3.14 on PyPI; use Python 3.13 until upstream supports 3.15.
FROM python:3.13.17-slim@sha256:fe14fd57a4c0361110eaa0c284112b189750bcb43fb04bb5d1ee7b7744027ccf

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
    DEEPEVAL_UPDATE_WARNING_OPT_OUT=YES \
    DEEPEVAL_TELEMETRY_OPT_OUT=YES

EXPOSE 8080

USER 10001:10001

CMD ["python", "app.py"]
