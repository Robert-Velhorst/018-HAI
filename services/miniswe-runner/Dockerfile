FROM python:3.15-slim

WORKDIR /app

RUN pip install --no-cache-dir mini-swe-agent==2.4.5

COPY app.py ./

RUN useradd --system --uid 10001 --create-home hai

USER hai

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

EXPOSE 8080

CMD ["python", "app.py"]
