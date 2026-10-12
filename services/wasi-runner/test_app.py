import ast
from pathlib import Path
import unittest


SOURCE = Path(__file__).with_name("server.py").read_text(encoding="utf-8")
TREE = ast.parse(SOURCE)


class WasiRunnerContractTest(unittest.TestCase):
    def test_runtime_command_has_no_guest_arguments_or_host_environment(self):
        calls = [
            node for node in ast.walk(TREE)
            if isinstance(node, ast.Call)
            and isinstance(node.func, ast.Attribute)
            and node.func.attr == "run"
        ]
        runtime = next(call for call in calls if isinstance(call.func.value, ast.Name)
                       and call.func.value.id == "subprocess")
        command = runtime.args[0]
        command_values = [
            ast.literal_eval(value) if isinstance(value, ast.Constant) else value.id
            for value in command.elts
        ]
        self.assertEqual(command_values, ["timeout", "5s", "wasmtime", "run", "path"])
        keywords = {item.arg: ast.unparse(item.value) for item in runtime.keywords}
        self.assertIn("'PATH'", keywords["env"])
        self.assertEqual(keywords["cwd"], "'/tmp'")
        self.assertIn("timeout", keywords)

    def test_submission_requires_digest_and_basename_only(self):
        self.assertIn("os.path.basename(name)!=name", SOURCE)
        self.assertIn("hashlib.sha256(source.read()).hexdigest()", SOURCE)
        self.assertIn("actual != expected", SOURCE)


if __name__ == "__main__":
    unittest.main()
