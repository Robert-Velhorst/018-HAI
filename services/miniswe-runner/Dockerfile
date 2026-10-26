FROM ubuntu:26.10@sha256:93e29beff5257b2cdf1c024d831f6d2997e53655a653ff76a811fc9e6564a719 AS pebble-publisher
ADD --checksum=sha256:e9d84f9e461a89abb4437af26a784917bd85cf1612c8de142a3b4449dff332d0 https://github.com/canonical/pebble/releases/download/v1.33.2/pebble_v1.33.2_linux_amd64.tar.gz /tmp/pebble.tar.gz
RUN mkdir -p /publisher \
    && tar --extract --gzip --file /tmp/pebble.tar.gz --directory /publisher pebble COPYING README.md \
    && echo "b14482f9c98f55bbcebc84affc7cac7291540b62bf6895fd480ebbf2a3c88f24  /publisher/pebble" | sha256sum -c

FROM ubuntu:26.10@sha256:93e29beff5257b2cdf1c024d831f6d2997e53655a653ff76a811fc9e6564a719 AS hai-ubuntu-base
# Normal signed repository updates preserve the complete supported base.
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get upgrade -y \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates
# Trivy's current Ubuntu analyzer requires this legacy interface. Derive it
# from the genuine publisher OS identity and preserve all existing OS files.
RUN set -eu; . /usr/lib/os-release; \
    test "$ID" = ubuntu; test "$VERSION_ID" = 26.10; \
    if [ ! -e /etc/lsb-release ]; then \
      printf 'DISTRIB_ID=Ubuntu\nDISTRIB_RELEASE=%s\nDISTRIB_CODENAME=%s\nDISTRIB_DESCRIPTION="%s"\n' \
        "$VERSION_ID" "$UBUNTU_CODENAME" "$PRETTY_NAME" > /etc/lsb-release; \
    fi
# Install the complete current publisher binary, preserving its license and
# build provenance. No supervisor feature or module is removed.
COPY --chown=0:0 --from=pebble-publisher /publisher/pebble /usr/bin/pebble
COPY --chown=0:0 --from=pebble-publisher /publisher/COPYING /usr/share/doc/hai-base/pebble-COPYING
COPY --chown=0:0 --from=pebble-publisher /publisher/README.md /usr/share/doc/hai-base/pebble-README.md

# Preserve the independently qualified complete Ubuntu base recipe.
FROM hai-ubuntu-base AS native-build
WORKDIR /native-build
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get upgrade -y \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
       python3 build-essential libbluetooth-dev perl pkg-config libbz2-dev \
       libexpat1-dev libffi-dev libgdbm-dev libgdbm-compat-dev liblzma-dev libncurses-dev \
       libreadline-dev libssl-dev libmpdec-dev tk-dev uuid-dev zlib1g-dev libzstd-dev libyaml-dev \
    && groupadd --gid 10001 native-builder \
    && useradd --uid 10001 --gid 10001 --no-create-home --shell /usr/sbin/nologin native-builder \
    && chown 10001:10001 /native-build
COPY --from=hai-native-source build_runtime.py build_ubuntu_runtime.py sources.json ubuntu-native-reference.json ./
COPY native-runtime.profile.json ./profile.json
USER 10001:10001
ENV HOME=/tmp
RUN HAI_DISPOSABLE_UBUNTU_SDK=1 python3 -I build_ubuntu_runtime.py

FROM scratch AS native-export
COPY --from=native-build /native-build/out/ /
COPY --from=native-build /native-build/install/ /installed/
COPY --from=native-build /native-build/sources.json /sources.json

FROM native-build AS python-installed
USER 0:0
RUN cp -a /native-build/install/usr/local/. /usr/local/ \
    && dpkg -i /native-build/out/hai-openssl4.deb /native-build/out/hai-sqlite3.deb \
    && mkdir -p /app /opt/hai-venv /opt/hai-wheels \
    && chown 10001:10001 /app /opt/hai-venv /opt/hai-wheels
WORKDIR /app
ENV LD_LIBRARY_PATH=/opt/hai-native/lib:/usr/local/lib \
    OPENSSL_MODULES=/opt/hai-native/lib/ossl-modules \
    SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt SSL_CERT_DIR=/etc/ssl/certs \
    PYTHONDONTWRITEBYTECODE=1
COPY requirements.lock ./
COPY --from=hai-miniswe-wheels /litellm-1.106.3+hai.py315.2-cp310-abi3-manylinux_2_28_x86_64.whl /opt/hai-wheels/
COPY --from=hai-miniswe-wheels /fastuuid-0.14.0+hai.py315.2-cp315-cp315-linux_x86_64.whl /opt/hai-wheels/
COPY --from=hai-miniswe-wheels /datasets-5.1.0+hai.fsspec.1-py3-none-any.whl /opt/hai-wheels/
COPY --from=hai-miniswe-wheels /botocore-1.43.118+hai.dateutil3.1-py3-none-any.whl /opt/hai-wheels/
USER 10001:10001
ENV HOME=/tmp
RUN /usr/local/bin/python3.15 -m venv /opt/hai-venv
ENV VIRTUAL_ENV=/opt/hai-venv PATH=/opt/hai-venv/bin:/usr/local/bin:$PATH
RUN PYYAML_FORCE_CYTHON=1 PYYAML_FORCE_LIBYAML=1 python -m pip install --verbose --no-cache-dir --only-binary numpy,scipy,pandas,pyarrow,tokenizers \
    --report /app/installation-report.json --find-links /opt/hai-wheels --require-hashes -r requirements.lock

# The SDK check inherits exactly the runtime installation, with its native
# extension required. Test tools stay in this separate disposable build stage.
FROM python-installed AS pyyaml-sdk
RUN python -m pip install --no-cache-dir --report /app/pyyaml-test-sdk-installation-report.json pytest==9.1.1 pluggy==1.7.0 iniconfig==2.3.1

FROM scratch AS pyyaml-test-tools-export
# Full installed package directories, metadata/licences and both CLI scripts.
# No PyYAML or other runtime distribution is exported into this test-only bundle.
COPY --from=pyyaml-sdk /opt/hai-venv/lib/python3.15/site-packages/_pytest/ /lib/python3.15/site-packages/_pytest/
COPY --from=pyyaml-sdk /opt/hai-venv/lib/python3.15/site-packages/pytest/ /lib/python3.15/site-packages/pytest/
COPY --from=pyyaml-sdk /opt/hai-venv/lib/python3.15/site-packages/pluggy/ /lib/python3.15/site-packages/pluggy/
COPY --from=pyyaml-sdk /opt/hai-venv/lib/python3.15/site-packages/iniconfig/ /lib/python3.15/site-packages/iniconfig/
COPY --from=pyyaml-sdk /opt/hai-venv/lib/python3.15/site-packages/pytest-9.1.1.dist-info/ /lib/python3.15/site-packages/pytest-9.1.1.dist-info/
COPY --from=pyyaml-sdk /opt/hai-venv/lib/python3.15/site-packages/pluggy-1.7.0.dist-info/ /lib/python3.15/site-packages/pluggy-1.7.0.dist-info/
COPY --from=pyyaml-sdk /opt/hai-venv/lib/python3.15/site-packages/iniconfig-2.3.1.dist-info/ /lib/python3.15/site-packages/iniconfig-2.3.1.dist-info/
COPY --from=pyyaml-sdk /opt/hai-venv/bin/pytest /opt/hai-venv/bin/py.test /bin/
COPY --from=pyyaml-sdk /app/pyyaml-test-sdk-installation-report.json /installation-report.json

FROM python-installed AS python-dependencies
RUN python -m pip uninstall --yes pip
USER 0:0
RUN /usr/local/bin/python3.15 -m pip uninstall --yes pip
RUN /usr/local/bin/python3.15 -I - <<'PY'
from pathlib import Path
import importlib.util,json,re,subprocess,sysconfig
bundled=Path(sysconfig.get_path('stdlib'))/'ensurepip/_bundled'
for path in bundled.glob('*.whl'):
    path.unlink()
assert importlib.util.find_spec('pip') is None and not list(bundled.glob('*.whl'))
roots=[Path('/usr/local'),Path('/opt/hai-venv'),Path('/opt/hai-native')]
paths=[Path('/usr/local/bin/python3.15')]
paths += [p for root in roots for p in root.rglob('*') if p.is_file() and (p.name.endswith('.so') or '.so.' in p.name)]
providers=set()
observed={}
for path in paths:
    result=subprocess.run(['ldd',str(path)],text=True,capture_output=True,check=True)
    if 'not found' in result.stdout:
        raise RuntimeError('Every linked native library must have an actual provider: '+str(path))
    libraries=[]
    for line in result.stdout.splitlines():
        match=re.search(r'=> (/\S+)',line) or re.match(r'\s*(/\S+)',line)
        if not match:
            continue
        library=Path(match[1]).resolve(strict=True)
        libraries.append(str(library))
        if any(library.is_relative_to(root) for root in roots):
            continue
        owner=subprocess.check_output(['dpkg-query','--search',str(library)],text=True).strip()
        if '\n' in owner or ': /' not in owner:
            raise RuntimeError('Unknown or ambiguous system library provider: '+str(library))
        name=owner.split(': /',1)[0]
        if not re.fullmatch(r'[a-z0-9][a-z0-9+.-]*(?::[a-z0-9]+)?',name):
            raise RuntimeError('Unexpected package identity')
        providers.add(name)
    observed[str(path)]=libraries
Path('/runtime-dependencies.txt').write_text('\n'.join(sorted(providers))+'\n')
Path('/runtime-dependency-provenance.json').write_text(json.dumps({'all_linked_paths':observed,
    'runtime_packages':sorted(providers),'unknown_dependencies_ignored':False},indent=2)+'\n')
PY

FROM hai-ubuntu-base AS runtime
WORKDIR /app
COPY --from=python-dependencies /runtime-dependencies.txt /usr/share/doc/hai-native/runtime-dependencies.txt
COPY --from=python-dependencies /runtime-dependency-provenance.json /usr/share/doc/hai-native/runtime-dependency-provenance.json
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get upgrade -y \
    && xargs -r apt-get install -y --no-install-recommends < /usr/share/doc/hai-native/runtime-dependencies.txt
RUN --mount=type=bind,from=native-build,source=/native-build/out,target=/tmp/native-pack,readonly \
    dpkg -i /tmp/native-pack/hai-openssl4.deb /tmp/native-pack/hai-sqlite3.deb
COPY --chown=0:0 --from=python-dependencies /usr/local/ /usr/local/
COPY --chown=0:0 --from=python-dependencies /opt/hai-venv/ /opt/hai-venv/
COPY --chown=0:0 --from=python-dependencies /app/installation-report.json /usr/share/doc/hai-native/installation-report.json
COPY --chown=0:0 --from=native-build /native-build/out/provenance.json /native-build/out/CPython-LICENSE /usr/share/doc/hai-native/
COPY --chown=0:0 --from=hai-miniswe-wheels /fastuuid-build.json /upstream-case-qualification.json /usr/share/doc/hai-miniswe/
COPY --chown=0:0 compat/verify_sdk_import.py ./compat/verify_sdk_import.py
COPY --chown=0:0 app.py test_app.py requirements.lock native-runtime.profile.json ./
ENV PATH=/opt/hai-venv/bin:/usr/local/bin:$PATH \
    VIRTUAL_ENV=/opt/hai-venv HOME=/tmp \
    LD_LIBRARY_PATH=/opt/hai-native/lib:/usr/local/lib \
    OPENSSL_MODULES=/opt/hai-native/lib/ossl-modules \
    SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt SSL_CERT_DIR=/etc/ssl/certs \
    PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 \
    LITELLM_LOCAL_MODEL_COST_MAP=True MSWEA_SILENT_STARTUP=1 \
    HF_HUB_OFFLINE=1 HF_HUB_DISABLE_TELEMETRY=1
RUN mkdir /qualification && chown 10001:10001 /qualification
USER 10001:10001
RUN --network=none --mount=type=bind,from=python-dependencies,source=/opt/hai-venv/lib/python3.15/site-packages,target=/installed,readonly --mount=type=bind,from=hai-miniswe-wheels,target=/inputs,readonly \
    HAI_DISPOSABLE_PUBLIC_SDK=1 python -I compat/verify_sdk_import.py --site-packages /installed --receipt /qualification/sdk-import.json
RUN --network=none python -m unittest discover -v -s . -p 'test_app.py'
EXPOSE 8080
CMD ["python", "app.py"]
