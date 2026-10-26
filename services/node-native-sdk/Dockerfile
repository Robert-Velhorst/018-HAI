ARG HAI_NATIVE_SUPPORT_IMAGE=hai-node-native-support:ci
FROM ${HAI_NATIVE_SUPPORT_IMAGE}

USER 0:0
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends git gnupg gpgv rustup procps \
    && cp -a /native-build/install/usr/local/. /usr/local/ \
    && dpkg -i /native-build/out/hai-openssl4.deb /native-build/out/hai-sqlite3.deb \
    && mkdir -p /node-build /home/native-builder \
    && chown 10001:10001 /node-build /home/native-builder
WORKDIR /node-build
COPY --chown=0:0 source-spec.json release-signer.asc active-release-keys.kbx run_native_sdk.py /node-sdk-inputs/
USER 10001:10001
ENV HOME=/home/native-builder \
    PATH=/home/native-builder/.cargo/bin:/usr/local/bin:$PATH \
    LD_LIBRARY_PATH=/opt/hai-native/lib:/usr/local/lib \
    OPENSSL_MODULES=/opt/hai-native/lib/ossl-modules \
    SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt SSL_CERT_DIR=/etc/ssl/certs \
    PYTHONDONTWRITEBYTECODE=1
ENTRYPOINT ["python3.15", "-I", "/node-sdk-inputs/run_native_sdk.py"]
