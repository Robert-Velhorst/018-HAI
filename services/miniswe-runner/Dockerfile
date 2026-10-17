FROM python:3.14-slim

WORKDIR /app
COPY requirements.lock ./

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential cargo \
    && pip install --no-cache-dir --require-hashes -r requirements.lock \
    && apt-get purge -y --auto-remove build-essential cargo rustc \
    && rm -rf /var/lib/apt/lists/* /root/.cargo

COPY app.py ./

RUN useradd --system --uid 10001 --create-home hai

USER hai

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

EXPOSE 8080

CMD ["python", "app.py"]
