FROM anchore/grype:v0.121.0 AS grype

FROM python:3.15-slim
COPY --from=grype /grype /usr/local/bin/grype
COPY app.py /app/app.py
RUN groupadd --gid 10003 hai && useradd --uid 10003 --gid hai --no-create-home --shell /usr/sbin/nologin hai
USER 10003:10003
EXPOSE 8080
ENTRYPOINT ["python", "/app/app.py"]
