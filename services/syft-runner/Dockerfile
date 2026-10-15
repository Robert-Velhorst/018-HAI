FROM anchore/syft:v1.54.1 AS syft

FROM python:3.13-slim
COPY --from=syft /syft /usr/local/bin/syft
COPY app.py /app/app.py
RUN groupadd --gid 10002 hai && useradd --uid 10002 --gid hai --no-create-home --shell /usr/sbin/nologin hai
USER 10002:10002
EXPOSE 8080
ENTRYPOINT ["python", "/app/app.py"]
