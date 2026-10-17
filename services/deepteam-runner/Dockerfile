# deepteam 1.0.10 currently declares Python <3.14 on PyPI; use Python 3.13 until upstream supports 3.15.
FROM python:3.13-slim

WORKDIR /app

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

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
