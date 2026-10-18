FROM ghcr.io/gitleaks/gitleaks:v8.30.1 AS gitleaks

FROM python:3.15-slim
COPY --from=gitleaks /usr/bin/gitleaks /usr/local/bin/gitleaks
COPY app.py /app/app.py
RUN groupadd --gid 10001 hai && useradd --uid 10001 --gid hai --no-create-home --shell /usr/sbin/nologin hai
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["python", "/app/app.py"]
