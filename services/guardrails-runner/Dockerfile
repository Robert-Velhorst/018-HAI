FROM python:3.15.1-alpine3.24@sha256:54448f67d7c31c39e5cdd3ea2d6382395ce142806c284425f8e897d78a0465b3 AS native-build
WORKDIR /native-build
RUN apk upgrade --no-cache \
    && apk add --no-cache build-base linux-headers bluez-dev perl pkgconf \
    && addgroup -g 10001 native-builder \
    && adduser -D -H -u 10001 -G native-builder native-builder \
    && chown native-builder:native-builder /native-build
COPY --from=hai-native-source build_runtime.py build_alpine_runtime.py sources.json ./
COPY native-runtime.profile.json ./profile.json
USER 10001:10001
ENV HOME=/tmp
RUN HAI_DISPOSABLE_ALPINE_SDK=1 python -I build_alpine_runtime.py

FROM python:3.15.1-alpine3.24@sha256:54448f67d7c31c39e5cdd3ea2d6382395ce142806c284425f8e897d78a0465b3 AS native-pack
WORKDIR /native-pack
RUN apk upgrade --no-cache && apk add --no-cache openssl
COPY --chown=0:0 --from=native-build /native-build/out/ ./input/
COPY --from=hai-native-source package_alpine_runtime.py ./
RUN HAI_DISPOSABLE_APK_PACKAGER=1 python -I package_alpine_runtime.py

FROM scratch AS native-export
COPY --from=native-pack /native-pack/out/ /
COPY --from=native-build /native-build/out/provenance.json /native-provenance.json
COPY --from=native-build /native-build/out/lib-dynload/ /lib-dynload/
COPY --from=native-build /native-build/sources.json /sources.json

FROM python:3.15.1-alpine3.24@sha256:54448f67d7c31c39e5cdd3ea2d6382395ce142806c284425f8e897d78a0465b3 AS python-dependencies
WORKDIR /app
RUN apk upgrade --no-cache \
    && apk add --no-cache pax-utils
COPY --from=native-pack /native-pack/out/keys/hai-native-runtime.rsa.pub /etc/apk/keys/
RUN --mount=type=bind,from=native-pack,source=/native-pack/out,target=/tmp/native-pack,readonly \
    apk verify /tmp/native-pack/hai-openssl4.apk /tmp/native-pack/hai-sqlite3.apk \
    && apk add --no-cache /tmp/native-pack/hai-openssl4.apk /tmp/native-pack/hai-sqlite3.apk
COPY --chown=0:0 --from=native-build /native-build/out/lib-dynload/ /usr/local/lib/python3.15/lib-dynload/
ENV LD_LIBRARY_PATH=/opt/hai-native/lib \
    OPENSSL_MODULES=/opt/hai-native/lib/ossl-modules \
    SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt \
    SSL_CERT_DIR=/etc/ssl/certs
COPY --chown=0:0 requirements.lock ./
COPY --chown=0:0 compat ./compat
RUN addgroup -g 10001 dependency-builder \
    && adduser -D -H -u 10001 -G dependency-builder dependency-builder \
    && mkdir /opt/hai-venv /opt/hai-wheels \
    && chown dependency-builder:dependency-builder /opt/hai-venv /opt/hai-wheels
USER 10001:10001
ENV HOME=/tmp
RUN python -m venv /opt/hai-venv
ENV VIRTUAL_ENV=/opt/hai-venv \
    PATH=/opt/hai-venv/bin:$PATH
# Build the already-reviewed offline port without importing its vendor modules.
# The original lock binds the exact ported wheel and every published dependency.
RUN python -I /app/compat/build_port_wheel.py --output-directory /opt/hai-wheels \
    && echo "c16222e9b42be2b593e19e97649058e92ad5df2fa3b47cb4f912525a1ad86fa4  /opt/hai-wheels/guardrails_ai-0.11.0+hai.offline.1-py3-none-any.whl" | sha256sum -c \
    && python -m pip install --no-cache-dir --only-binary :all: --find-links /opt/hai-wheels --require-hashes -r requirements.lock \
    && python -m pip uninstall --yes pip
USER 0:0
# Only installer cleanup and dependency metadata run here. Native compilation
# and offline-port wheel construction use UID10001; dependency wheels are locked.
RUN /usr/local/bin/python -m pip uninstall --yes pip
RUN python -I - <<'PY'
import importlib.util,json,pathlib,re,subprocess,sysconfig
bundled=pathlib.Path(sysconfig.get_path('stdlib'))/'ensurepip/_bundled'
for path in bundled.glob('*.whl'):
    path.unlink()
assert importlib.util.find_spec('pip') is None
assert not list(bundled.glob('*.whl'))
raw=subprocess.check_output(['scanelf','--needed','--nobanner','--format','%n#p','--recursive','/usr/local','/opt/hai-venv','/opt/hai-native/lib'],text=True)
names={name.strip() for line in raw.splitlines() for name in line.split(',') if name.strip() not in {'','-'}}
assert all(re.fullmatch(r'[A-Za-z0-9_.+-]+',name) for name in names)
provided={p.name for root in ['/usr/local','/opt/hai-venv','/opt/hai-native/lib'] for p in pathlib.Path(root).rglob('*') if p.is_file()}
dependencies={'so:'+name for name in names if name not in provided and name!='libc.so'}
# musl1.2.6 ldso/dynlink.c names the already-loaded interpreter libc.so.
# Record its real package provider; it is not an APK virtual soname package.
if 'libc.so' in names:
    assert str(sysconfig.get_config_var('HOST_GNU_TYPE')).endswith('musl')
    subprocess.run(['apk','info','--exists','musl'],check=True)
    dependencies.add('musl')
dependencies=sorted(dependencies)
pathlib.Path('/runtime-dependencies.txt').write_text('\n'.join(dependencies)+'\n')
pathlib.Path('/runtime-dependency-provenance.json').write_text(json.dumps({'all_needed_sonames':sorted(names),'runtime_packages':dependencies,'musl_loader_alias_provider':{'libc.so':'musl'} if 'libc.so' in names else {},'unknown_dependencies_ignored':False},indent=2)+'\n')
PY

FROM alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a AS runtime
WORKDIR /app
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates
COPY --from=native-pack /native-pack/out/keys/hai-native-runtime.rsa.pub /etc/apk/keys/
RUN --mount=type=bind,from=native-pack,source=/native-pack/out,target=/tmp/native-pack,readonly \
    apk verify /tmp/native-pack/hai-openssl4.apk /tmp/native-pack/hai-sqlite3.apk \
    && apk add --no-cache /tmp/native-pack/hai-openssl4.apk /tmp/native-pack/hai-sqlite3.apk
COPY --from=python-dependencies /runtime-dependencies.txt /usr/share/doc/hai-native/runtime-dependencies.txt
COPY --from=python-dependencies /runtime-dependency-provenance.json /usr/share/doc/hai-native/runtime-dependency-provenance.json
RUN xargs -r apk add --no-cache < /usr/share/doc/hai-native/runtime-dependencies.txt
COPY --chown=0:0 --from=python-dependencies /usr/local/ /usr/local/
COPY --chown=0:0 --from=python-dependencies /opt/hai-venv/ /opt/hai-venv/
COPY --chown=0:0 --from=native-build /native-build/out/provenance.json /native-build/out/CPython-LICENSE /usr/share/doc/hai-native/
COPY --chown=0:0 --from=native-pack /native-pack/out/signing-provenance.json /usr/share/doc/hai-native/
COPY --chown=0:0 app.py test_app.py requirements.lock native-runtime.profile.json ./
COPY --chown=0:0 compat/port-spec.json compat/README.md /usr/share/doc/hai-guardrails/
ENV PATH=/opt/hai-venv/bin:/usr/local/bin:$PATH \
    VIRTUAL_ENV=/opt/hai-venv \
    HOME=/tmp \
    LD_LIBRARY_PATH=/opt/hai-native/lib \
    OPENSSL_MODULES=/opt/hai-native/lib/ossl-modules \
    SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt \
    SSL_CERT_DIR=/etc/ssl/certs \
    OTEL_SDK_DISABLED=true \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1
USER 10001:10001
EXPOSE 8080
CMD ["python", "app.py"]
