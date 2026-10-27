"""Policy tests for Python service-runner CI coverage and dependency locks."""

from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")


def _pip_effective_lines(path: Path) -> tuple[str, ...]:
    """Return install-affecting lines, excluding resolver provenance comments."""
    return tuple(
        line.strip()
        for line in path.read_text(encoding="utf-8").splitlines()
        if line.strip() and not line.lstrip().startswith("#")
    )


def _pinned_requirement_key(requirement: str) -> tuple[str, tuple[str, ...], str] | None:
    """Identify the simple exact pins used by runner manifests, not resolve PEP 508."""
    match = re.match(
        r"([A-Za-z0-9_.-]+)(?:\[([A-Za-z0-9_., -]*)\])?\s*==\s*([^;\s]+)",
        requirement.strip(),
    )
    if not match:
        return None
    def canonical(value: str) -> str:
        return re.sub(r"[-_.]+", "-", value.strip()).lower()

    extras = tuple(sorted({
        canonical(extra) for extra in (match.group(2) or "").split(",") if extra.strip()
    }))
    return (canonical(match.group(1)), extras, match.group(3).lower())


class PythonRunnerCIPolicyTest(unittest.TestCase):
    def test_pinned_extras_cannot_disappear_from_dependency_policy_checks(self):
        self.assertEqual(
            _pinned_requirement_key("pydantic-ai-slim[openai]==2.56.0"),
            ("pydantic-ai-slim", ("openai",), "2.56.0"),
        )

    def test_pinned_extra_identity_is_canonical_but_not_optional(self):
        self.assertEqual(
            _pinned_requirement_key("Demo_Package[B_extra, a-extra]==1.2.3 ; python_version >= '3.12'"),
            ("demo-package", ("a-extra", "b-extra"), "1.2.3"),
        )
        self.assertNotEqual(
            _pinned_requirement_key("demo-package[openai]==1.2.3"),
            _pinned_requirement_key("demo-package==1.2.3"),
        )
        self.assertIsNone(_pinned_requirement_key("demo-package>=1.2.3"))

    def test_docling_pillow_security_cohort_matches_runtime_and_ci(self):
        service = ROOT / "services/docling-runner"
        source = (service / "requirements.in").read_text(encoding="utf-8")
        locked = (service / "requirements.lock").read_text(encoding="utf-8")
        dockerfile = (service / "Dockerfile").read_text(encoding="utf-8")
        self.assertIn("docling==2.142.1", source)
        self.assertIn("pillow==13.0.0", source)
        self.assertIn("COPY requirements-build.lock requirements.lock ./", dockerfile)
        self.assertIn("--no-build-isolation --require-hashes -r requirements.lock", dockerfile)
        self.assertIn("pillow==13.0.0 ", locked)
        self.assertRegex(locked, r"(?m)^torch==[^\s]+\+cpu ")
        self.assertRegex(locked, r"(?m)^torchvision==[^\s]+\+cpu ")
        self.assertNotRegex(locked, r"(?m)^nvidia-[a-z0-9-]+==")
        self.assertIn("HAI_REQUIRE_DOCLING_PACKAGE_TESTS:", WORKFLOW)

    def test_certifi_locks_exclude_compromised_emsign_roots(self):
        expected_hashes = {
            "42d35340cc5b9ba78e818ddbff0c5b1c2b779de448a340fd1b989f5fa8670e1b",
            "035c1e85572b141bf1c3a3eff0e33a8eb74917522f2559075ff8d2fea780a1ae",
        }
        found = 0
        for path in (ROOT / "services").glob("*/requirements.*"):
            if path.suffix not in (".lock", ".txt"):
                continue
            text = path.read_text(encoding="utf-8")
            match = re.search(r"(?m)^certifi==([^\s]+) \\\n((?:[ \t]+--hash=sha256:[0-9a-f]{64}[^\n]*\n)+)", text)
            if not match:
                self.assertNotRegex(text, r"(?m)^certifi[=<>!~]", str(path))
                continue
            found += 1
            with self.subTest(manifest=path.relative_to(ROOT)):
                version = tuple(int(part) for part in match.group(1).split("."))
                self.assertGreaterEqual(version, (2026, 10, 19))
                if version == (2026, 10, 19):
                    self.assertEqual(set(re.findall(r"sha256:([0-9a-f]{64})", match.group(2))), expected_hashes)
        self.assertGreater(found, 0, "certifi manifest coverage disappeared")
        # The image installs the same audited hash lock, including its CA bundle.
        dockerfile = (ROOT / "services/docling-runner/Dockerfile").read_text(encoding="utf-8")
        self.assertIn("--no-build-isolation --require-hashes -r requirements.lock", dockerfile)

    def test_every_shipped_python_runner_has_ci_contract_coverage(self):
        names = set(re.findall(r"(?m)^          - name: ([a-z0-9-]+)$", WORKFLOW))
        python_runners = set()
        for dockerfile in (ROOT / "services").glob("*/Dockerfile"):
            text = dockerfile.read_text(encoding="utf-8")
            if re.search(r"(?i)\bpython(?:3)?(?:-slim)?\b", text):
                python_runners.add(dockerfile.parent.name)

        self.assertEqual(names, python_runners)
        for name in sorted(python_runners):
            with self.subTest(runner=name):
                service = ROOT / "services" / name
                self.assertTrue((service / "test_app.py").is_file())
                self.assertRegex(WORKFLOW, rf"(?m)^          - name: {re.escape(name)}$")

    def test_all_declared_runtime_dependencies_have_hash_locked_ci_inputs(self):
        for service in (ROOT / "services").iterdir():
            if not service.is_dir():
                continue
            source = next(
                (service / name for name in ("requirements.in", "requirements.txt")
                 if (service / name).is_file()),
                None,
            )
            if source is None:
                continue

            lock = service / "requirements.lock"
            with self.subTest(runner=service.name):
                self.assertTrue(lock.is_file(), f"missing {lock.relative_to(ROOT)}")
                runtime_requirements = service / "requirements.txt"
                self.assertTrue(runtime_requirements.is_file())
                self.assertEqual(
                    _pip_effective_lines(runtime_requirements),
                    _pip_effective_lines(lock),
                    "Docker-installed requirements.txt must match the hash lock's "
                    "install directives, pinned versions, and hashes",
                )
                locked = lock.read_text(encoding="utf-8").lower()
                lines = lock.read_text(encoding="utf-8").splitlines()
                self.assertTrue(
                    any(
                        line.rstrip().endswith("\\")
                        and index + 1 < len(lines)
                        and re.match(r"\s+--hash=sha256:[0-9a-f]{64}", lines[index + 1])
                        for index, line in enumerate(lines)
                    ),
                    f"{lock.relative_to(ROOT)} has no hash-pinned package entries",
                )
                for requirement in source.read_text(encoding="utf-8").splitlines():
                    requirement = requirement.strip()
                    if not requirement or requirement.startswith(("#", "--")):
                        continue
                    pin = _pinned_requirement_key(requirement)
                    if pin:
                        self.assertTrue(
                            any(_pinned_requirement_key(line) == pin for line in lines),
                            f"{requirement} is absent from {lock.relative_to(ROOT)}",
                        )

    def test_docling_lock_uses_official_cpu_index_without_implicit_index_priority(self):
        source = (ROOT / "services/docling-runner/requirements.in").read_text(encoding="utf-8")
        lock = (ROOT / "services/docling-runner/requirements.lock").read_text(encoding="utf-8")
        self.assertIn("--index-url https://pypi.org/simple", source)
        self.assertIn("--extra-index-url https://download.pytorch.org/whl/cpu", source)
        self.assertIn("--index-url https://pypi.org/simple", lock)
        self.assertIn("--extra-index-url https://download.pytorch.org/whl/cpu", lock)

    def test_runner_ci_installs_lock_with_hash_enforcement_then_runs_tests(self):
        match = re.search(r"(?ms)^  python-runners:\n(.*?)(?=^  [a-z0-9-]+:\n)", WORKFLOW)
        self.assertIsNotNone(match)
        job = match.group(1)
        self.assertIn("--require-hashes", job)
        self.assertIn("python -m compileall -q .", job)
        self.assertIn("python -m unittest discover", job)
        self.assertIn("max-parallel: 4", job)


if __name__ == "__main__":
    unittest.main()
