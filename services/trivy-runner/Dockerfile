FROM aquasec/trivy:0.75.0 AS trivy

FROM python:3.15-alpine
COPY --from=trivy /usr/local/bin/trivy /usr/local/bin/trivy
COPY app.py /app/app.py
RUN addgroup -g 10005 -S hai && adduser -S -D -H -u 10005 -G hai hai
USER 10005:10005
EXPOSE 8080
ENTRYPOINT ["python3", "/app/app.py"]
