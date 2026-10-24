# dynaconf 3.3.5 currently declares Python <3.15 on PyPI; use the newest compatible line.
FROM python:3.14.9-slim-bookworm@sha256:945c2676dc2b18e0c1f16879244122c57c0ffede60006bf3df159c507a31bcae

WORKDIR /app
COPY requirements.lock ./
RUN python -m pip install --no-cache-dir --only-binary :all: --require-hashes -r requirements.lock
COPY app.py ./
COPY test_app.py ./

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1
EXPOSE 8080
CMD ["python", "app.py"]
