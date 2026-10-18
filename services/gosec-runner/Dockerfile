FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS builder

ARG GOSEC_VERSION=v2.28.0
RUN CGO_ENABLED=0 go install -ldflags "-X main.Version=${GOSEC_VERSION#v} -X main.GitTag=${GOSEC_VERSION}" github.com/securego/gosec/v2/cmd/gosec@${GOSEC_VERSION}

FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d
COPY --from=builder /go/bin/gosec /usr/local/bin/gosec
COPY app.py /app/app.py
RUN apk add --no-cache python3 && addgroup -g 10004 -S hai && adduser -S -D -H -u 10004 -G hai hai
USER 10004:10004
EXPOSE 8080
ENTRYPOINT ["python3", "/app/app.py"]
