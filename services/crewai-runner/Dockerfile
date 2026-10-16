FROM python:3.12-slim

WORKDIR /app

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY app.py ./

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    OTEL_SDK_DISABLED=true \
    DO_NOT_TRACK=true

EXPOSE 8080

CMD ["python", "app.py"]
