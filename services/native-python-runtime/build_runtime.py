"""Build pinned native clients and CPython TLS modules in a disposable SDK.

Only /native-build inside the explicitly marked Debian 13 container is allowed.
No operator SDK, provider, database server or user database is accessed.
"""
import ctypes
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import sysconfig
import tarfile
import tempfile
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, build_opener

ROOT = Path("/native-build")
PREFIX = Path("/opt/hai-native")
OUTPUT = Path("/native-out")


def check_url(value):
    parsed = urlsplit(value)
    if (parsed.scheme != "https" or parsed.username or parsed.password
            or parsed.port not in {None, 443}
            or parsed.hostname not in {"github.com", "release-assets.githubusercontent.com", "www.python.org", "www.sqlite.org"}):
        raise RuntimeError("Native source URL is outside the approved upstream hosts")


class Redirects(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        check_url(newurl)
        return super().redirect_request(req, fp, code, msg, headers, newurl)


def command(args, cwd=ROOT, env=None, timeout=1200):
    subprocess.run(args, cwd=cwd, env=env, check=True, timeout=timeout)


def extract_source(spec, folder):
    folder.mkdir()
    archive = folder / "source.archive"
    algorithm = spec.get("digest_algorithm", "sha256")
    if algorithm not in {"sha256", "sha3_256"}:
        raise RuntimeError("Unexpected source digest algorithm")
    digest = hashlib.new(algorithm)
    check_url(spec["url"])
    size = 0
    with build_opener(Redirects()).open(spec["url"], timeout=60) as response, archive.open("xb") as out:
        check_url(response.url)
        while chunk := response.read(1024 * 1024):
            size += len(chunk)
            if size > 100 * 1024 * 1024:
                raise RuntimeError("Source archive exceeds its download bound")
            digest.update(chunk)
            out.write(chunk)
    if digest.hexdigest() != spec["checksum"]:
        raise RuntimeError("Native source archive digest mismatch")
    destination = folder / "source"
    destination.mkdir()
    with tarfile.open(archive) as tar:
        members = tar.getmembers()
        if len(members) > 50000 or sum(m.size for m in members) > 1024**3:
            raise RuntimeError("Source archive exceeds extraction bounds")
        tar.extractall(destination, filter="data")
    roots = list(destination.iterdir())
    if len(roots) != 1 or not roots[0].is_dir() or roots[0].is_symlink():
        raise RuntimeError("Expected one source directory")
    return roots[0]


def copy_regular(source, target):
    if not source.is_file() or source.is_symlink():
        raise RuntimeError("Expected an actual generated native file")
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(source, target)
    return hashlib.sha256(target.read_bytes()).hexdigest()


def package(root, name, version, source_name, architecture):
    control = root / "DEBIAN"
    control.mkdir()
    (control / "control").write_text(
        f"Package: {name}\nVersion: {version}-1hai1\nArchitecture: {architecture}\n"
        f"Source: {source_name} ({version})\nMaintainer: HAI native runtime builder\n"
        "Section: libs\nPriority: optional\nDepends: libc6 (>= 2.41)\n"
        "Description: HAI source-verified native client runtime\n"
        " Built inside the disposable HAI SDK from the recorded upstream source.\n",
        encoding="utf-8")
    out = OUTPUT / (name + ".deb")
    command(["dpkg-deb", "--root-owner-group", "--build", str(root), str(out)])
    return {"file": out.name, "sha256": hashlib.sha256(out.read_bytes()).hexdigest()}


def build_openssl(spec, source, folder, architecture):
    command(["./Configure", "shared", "--prefix=" + str(PREFIX), "--libdir=lib",
             "--openssldir=/etc/ssl", "-Wl,-rpath," + str(PREFIX / "lib")], source)
    command(["make", "-j2"], source)
    command(["make", "install_sw"], source)
    root = folder / "openssl-package"
    libraries = {}
    for name in ["libssl.so.4", "libcrypto.so.4", "ossl-modules/legacy.so"]:
        original = PREFIX / "lib" / name
        target = root / str(PREFIX / "lib" / name).lstrip("/")
        libraries[str(PREFIX / "lib" / name)] = copy_regular(original, target)
    doc = root / "usr/share/doc/hai-openssl4/copyright"
    copy_regular(source / "LICENSE.txt", doc)
    return {"source": spec, "libraries": libraries,
            "package": package(root, "hai-openssl4", spec["version"], "openssl", architecture)}


def build_sqlite(spec, source, folder, architecture):
    library = PREFIX / "lib/libsqlite3.so.0"
    flags = ["-O2", "-fPIC", "-shared", "-Wl,-soname,libsqlite3.so.0",
             "-DSQLITE_THREADSAFE=1", "-DSQLITE_USE_URI=1", "-DSQLITE_ENABLE_COLUMN_METADATA",
             "-DSQLITE_SECURE_DELETE", "-DSQLITE_ENABLE_UNLOCK_NOTIFY", "-DSQLITE_ENABLE_RTREE",
             "-DSQLITE_ENABLE_GEOPOLY", "-DSQLITE_ENABLE_DBSTAT_VTAB", "-DSQLITE_SOUNDEX",
             "-DSQLITE_MAX_VARIABLE_NUMBER=250000", "-DSQLITE_ENABLE_FTS3",
             "-DSQLITE_ENABLE_FTS3_PARENTHESIS", "-DSQLITE_ENABLE_FTS4", "-DSQLITE_ENABLE_FTS5",
             "-DSQLITE_ENABLE_SESSION", "-DSQLITE_ENABLE_PREUPDATE_HOOK", "-DSQLITE_ENABLE_MATH_FUNCTIONS"]
    command(["cc", *flags, str(source / "sqlite3.c"), "-lm", "-ldl", "-pthread", "-o", str(library)], source)
    native = ctypes.CDLL(str(library))
    native.sqlite3_libversion.restype = ctypes.c_char_p
    if native.sqlite3_libversion().decode("ascii") != spec["version"]:
        raise RuntimeError("Generated SQLite library has the wrong version")
    root = folder / "sqlite-package"
    target = root / str(library).lstrip("/")
    digest = copy_regular(library, target)
    doc = root / "usr/share/doc/hai-sqlite3/copyright"
    doc.parent.mkdir(parents=True)
    notice = re.search(r"/\*.*?\*/", (source / "sqlite3.c").read_text(encoding="utf-8"), re.S)
    if notice is None:
        raise RuntimeError("Missing SQLite source copyright notice")
    doc.write_text(notice[0] + "\nhttps://www.sqlite.org/copyright.html\n", encoding="utf-8")
    return {"source": spec, "cflags": flags, "library_sha256": digest,
            "package": package(root, "hai-sqlite3", spec["version"], "sqlite3", architecture)}


def rebuild_python(spec, source):
    suffix = sysconfig.get_config_var("EXT_SUFFIX")
    abi = f"cpython-{sys.version_info.major}{sys.version_info.minor}"
    if not re.fullmatch(r"\." + abi + r"-[a-z0-9_-]+\.so", suffix or ""):
        raise RuntimeError("Unsupported CPython extension ABI")
    installed = Path(sysconfig.get_config_var("DESTSHARED"))
    expected = Path(f"/usr/local/lib/python{sys.version_info.major}.{sys.version_info.minor}/lib-dynload")
    if installed != expected:
        raise RuntimeError("Unexpected disposable SDK extension directory")
    export = OUTPUT / "lib-dynload"
    export.mkdir()
    result = {}
    for module, filename, libraries in [("_ssl", "_ssl.c", ["ssl", "crypto"]), ("_hashlib", "_hashopenssl.c", ["crypto"])]:
        original = installed / (module + suffix)
        if not original.is_file() or original.is_symlink():
            raise RuntimeError("Expected the original exact SDK extension")
        original_hash = hashlib.sha256(original.read_bytes()).hexdigest()
        output = export / original.name
        includes = [Path(sysconfig.get_path("include")), source / "Include", source / "Include/internal",
                    source / "Modules", source / "Modules/_ssl", PREFIX / "include"]
        command(["cc", "-shared", "-fPIC", "-O2", "-DNDEBUG", "-DPy_BUILD_CORE_MODULE=1",
                 *["-I" + str(p) for p in includes], str(source / "Modules" / filename),
                 "-L" + str(PREFIX / "lib"), "-Wl,-rpath," + str(PREFIX / "lib"),
                 *["-l" + name for name in libraries], "-o", str(output)], source, timeout=300)
        # Preserve mapped old inodes in this process; a fresh interpreter below
        # loads only the replacement modules and their OpenSSL 4 dependencies.
        staged = original.with_name(original.name + ".hai-new")
        if staged.exists() or staged.is_symlink():
            raise RuntimeError("Unexpected existing SDK staging file")
        shutil.copyfile(output, staged)
        os.replace(staged, original)
        result[original.name] = {"original_sha256": original_hash,
                                 "sha256": hashlib.sha256(output.read_bytes()).hexdigest()}
    copy_regular(source / "LICENSE", OUTPUT / "CPython-LICENSE")
    return {"source": spec, "modules": result,
            "headers_sha256": hashlib.sha256(Path(sysconfig.get_config_h_filename()).read_bytes()).hexdigest()}


def main():
    if (sys.platform != "linux" or Path(__file__).resolve().parent != ROOT
            or os.environ.get("HAI_DISPOSABLE_BUILD_SDK") != "1"):
        raise RuntimeError("Native compilation is restricted to the marked disposable SDK")
    os_release = Path("/etc/os-release").read_text()
    if 'VERSION_ID="13"' not in os_release or "VERSION_CODENAME=trixie" not in os_release:
        raise RuntimeError("The native SDK must use the selected Debian 13 base")
    manifest = json.loads((ROOT / "sources.json").read_text())
    profile = json.loads((ROOT / "profile.json").read_text())
    python = profile["python"]
    if python not in manifest["cpython"] or sys.version.split()[0] != python or manifest["prefix"] != str(PREFIX):
        raise RuntimeError("Native SDK version/profile does not match the pinned sources")
    if PREFIX.exists() or PREFIX.is_symlink() or OUTPUT.exists() or OUTPUT.is_symlink():
        raise RuntimeError("Native SDK output must be absent before this build")
    OUTPUT.mkdir()
    architecture = subprocess.check_output(["dpkg", "--print-architecture"], text=True).strip()
    with tempfile.TemporaryDirectory(prefix="hai-native-sources-") as temporary:
        folder = Path(temporary)
        ssl = extract_source(manifest["openssl"], folder / "openssl")
        sqlite = extract_source(manifest["sqlite"], folder / "sqlite")
        cpython = extract_source(manifest["cpython"][python], folder / "cpython")
        ssl_result = build_openssl(manifest["openssl"], ssl, folder, architecture)
        sqlite_result = build_sqlite(manifest["sqlite"], sqlite, folder, architecture)
        python_result = rebuild_python(manifest["cpython"][python], cpython)
    env = {**os.environ, "LD_LIBRARY_PATH": str(PREFIX / "lib"), "OPENSSL_MODULES": str(PREFIX / "lib/ossl-modules")}
    probe = json.loads(subprocess.check_output([sys.executable, "-I", "-c",
        "import hashlib,json,platform,sqlite3,ssl; c=ssl.create_default_context(); "
        "db=sqlite3.connect(':memory:'); print(json.dumps({'python':platform.python_version(),"
        "'openssl':ssl.OPENSSL_VERSION,'ca_roots':len(c.get_ca_certs()),'sqlite':sqlite3.sqlite_version,"
        "'sqlite_query':db.execute('select sqlite_version()').fetchone()[0],"
        "'sha256':hashlib.sha256(b'hai-native-runtime').hexdigest()}))"],
        env=env, text=True, timeout=30))
    if (probe["python"] != python or not probe["openssl"].startswith("OpenSSL " + manifest["openssl"]["version"] + " ")
            or probe["sqlite"] != manifest["sqlite"]["version"] or probe["sqlite_query"] != probe["sqlite"]
            or probe["ca_roots"] < 1 or probe["sha256"] != hashlib.sha256(b"hai-native-runtime").hexdigest()):
        raise RuntimeError("Fresh native SDK provenance verification failed")
    result = {"profile": profile, "architecture": architecture, "openssl": ssl_result,
              "sqlite": sqlite_result, "cpython": python_result, "runtime": probe,
              "compiler": subprocess.check_output(["cc", "--version"], text=True).splitlines()[0],
              "operator_database_opened": False, "host_sdk_changed": False}
    (OUTPUT / "provenance.json").write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(result))


if __name__ == "__main__":
    main()
