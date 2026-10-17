FROM python:3.15-slim

WORKDIR /app
COPY requirements.lock ./

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential cargo \
    && PYO3_USE_ABI3_FORWARD_COMPATIBILITY=1 pip install --no-cache-dir --require-hashes -r requirements.lock \
    && apt-get purge -y --auto-remove build-essential cargo rustc \
    && rm -rf /var/lib/apt/lists/* /root/.cargo

COPY app.py ./

RUN useradd --system --uid 10001 --create-home hai

USER hai

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

EXPOSE 8080

CMD ["python", "app.py"]
