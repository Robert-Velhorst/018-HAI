FROM python:3.15-slim

WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential cargo \
    && pip install --no-cache-dir mini-swe-agent==2.4.6 \
    && apt-get purge -y --auto-remove build-essential cargo rustc \
    && rm -rf /var/lib/apt/lists/* /root/.cargo

COPY app.py ./

RUN useradd --system --uid 10001 --create-home hai

USER hai

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

EXPOSE 8080

CMD ["python", "app.py"]
