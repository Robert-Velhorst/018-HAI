"""Policy tests for Python service-runner CI coverage and dependency locks."""

from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")


class PythonRunnerCIPolicyTest(unittest.TestCase):
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
                    runtime_requirements.read_bytes().replace(b"\r\n", b"\n"),
                    lock.read_bytes().replace(b"\r\n", b"\n"),
                    "Docker-installed requirements.txt must exactly match the hash lock",
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
                    match = re.match(r"([A-Za-z0-9_.-]+)==([^;\s]+)", requirement)
                    if match:
                        package = re.sub(r"[-_.]+", "-", match.group(1)).lower()
                        version = match.group(2).lower()
                        self.assertTrue(
                            any(line.startswith(f"{package}=={version} ") for line in lines),
                            f"{package}=={version} is absent from {lock.relative_to(ROOT)}",
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
