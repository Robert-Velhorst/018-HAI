FROM python:3.14-slim

WORKDIR /app

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

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
