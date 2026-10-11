from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]
SMOKE_SUITES = (
    "smoke-critical-path.sh",
    "smoke-background-operations.sh",
    "smoke-model-intelligence.sh",
    "smoke-runtime-lab.sh",
    "smoke-account-bridges.sh",
    "smoke-windows-runtime.sh",
)


class SmokeAuthContractTest(unittest.TestCase):
    def test_protected_smoke_suites_use_owner_jwt_and_reject_key_only(self) -> None:
        for name in SMOKE_SUITES:
            with self.subTest(script=name):
                script = (ROOT / "scripts" / name).read_text(encoding="utf-8")
                self.assertIn('source "${ROOT}/scripts/smoke-auth.sh"', script)
                self.assertIn(
                    'owner_jwt="$(hai_smoke_mint_jwt owner "${JWT_SECRET}")"',
                    script,
                )
                self.assertIn(
                    'hdr=("${key_hdr[@]}" -H "Authorization: Bearer ${owner_jwt}")',
                    script,
                )
                self.assertIn("API key alone", script)
                self.assertIn('"${key_hdr[@]}"', script)
                self.assertIn("-k ${WORKDIR}", script)

    def test_ci_aggregator_runs_the_five_current_phase_two_suites(self) -> None:
        aggregator = (ROOT / "scripts" / "smoke-all.sh").read_text(encoding="utf-8")
        for name in SMOKE_SUITES[1:]:
            self.assertIn(name.removesuffix(".sh"), aggregator)
        self.assertNotIn("smoke-critical-path", aggregator)
        self.assertIn(
            'out="$("${ROOT}/scripts/${s}.sh" 2>&1)"',
            aggregator,
        )

    def test_signed_session_helper_uses_expiring_hmac_sha256_jwts(self) -> None:
        helper = (ROOT / "scripts" / "smoke-auth.sh").read_text(encoding="utf-8")
        for contract in (
            '"alg": "HS256"',
            '"sub": subject',
            '"role": role',
            '"exp": int(time.time()) + 3600',
            "hmac.new(secret.encode()",
            "hashlib.sha256",
        ):
            with self.subTest(contract=contract):
                self.assertIn(contract, helper)

    def test_smoke_runtime_has_approval_proof_signing_authority(self) -> None:
        helper = (ROOT / "scripts" / "smoke-auth.sh").read_text(encoding="utf-8")
        self.assertIn("HAI_APPROVAL_PROOF_SIGNING_KEY", helper)
        self.assertIn("export HAI_APPROVAL_PROOF_SIGNING_KEY", helper)

    def test_aggregator_exposes_failed_suite_output(self) -> None:
        aggregator = (ROOT / "scripts" / "smoke-all.sh").read_text(
            encoding="utf-8"
        )
        self.assertIn('echo "--- ${s} failure output ---" >&2', aggregator)
        self.assertIn('printf \'%s\\n\' "${out}" >&2', aggregator)

    def test_smoke_checkout_and_temp_paths_are_space_safe(self) -> None:
        for name in SMOKE_SUITES:
            with self.subTest(script=name):
                script = (ROOT / "scripts" / name).read_text(encoding="utf-8")
                for contract in (
                    'ROOT="$(cd "$(dirname "$0")/.." && pwd)"',
                    'source "${ROOT}/scripts/smoke-auth.sh"',
                    'WORKDIR="$(mktemp -d)"',
                    '-k ${WORKDIR}',
                    '( cd "${ROOT}/backend" && go build',
                ):
                    self.assertIn(contract, script)

    def test_viewer_probe_does_not_reuse_owner_authorization_header(self) -> None:
        script = (ROOT / "scripts" / "smoke-critical-path.sh").read_text(
            encoding="utf-8"
        )
        self.assertIn(
            '"${key_hdr[@]}" -H "Authorization: Bearer ${viewer_jwt}"',
            script,
        )
        self.assertNotIn(
            '"${hdr[@]}" -H "Authorization: Bearer ${viewer_jwt}"',
            script,
        )

    def test_live_smoke_rejects_missing_key_and_bad_signature(self) -> None:
        script = (ROOT / "scripts" / "smoke-background-operations.sh").read_text(
            encoding="utf-8"
        )
        for contract in (
            'forged_jwt="$(hai_smoke_mint_jwt owner "not-${JWT_SECRET}" forged-operator)"',
            'jwt_hdr=(-H "Content-Type: application/json" '
            '-H "Authorization: Bearer ${owner_jwt}")',
            '"owner JWT alone is rejected"',
            '"wrongly signed owner JWT is rejected"',
            '"${key_hdr[@]}"',
            '"${jwt_hdr[@]}"',
        ):
            with self.subTest(contract=contract):
                self.assertIn(contract, script)


if __name__ == "__main__":
    unittest.main()
