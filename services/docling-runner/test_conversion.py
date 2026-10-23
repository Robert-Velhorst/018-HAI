"""Offline acceptance for the installed Docling/Pillow dependency cohort."""

import io
import os
from importlib.metadata import PackageNotFoundError, version
from pathlib import Path
from tempfile import TemporaryDirectory
import unittest

import app


class InstalledConversionTests(unittest.TestCase):
    def require_package(self, name):
        try:
            return version(name)
        except PackageNotFoundError:
            if os.environ.get("HAI_REQUIRE_DOCLING_PACKAGE_TESTS") == "true":
                self.fail(f"CI must install the locked {name} package")
            self.skipTest(f"{name} is not installed locally")

    def test_patched_pillow_loads_native_image_codecs(self):
        installed = self.require_package("pillow")
        self.assertGreaterEqual(tuple(int(p) for p in installed.split(".")), (13, 0, 0))
        from PIL import Image

        for codec in ("PNG", "JPEG", "TIFF"):
            with self.subTest(codec=codec):
                with Image.new("RGB", (16, 12), (20, 80, 140)) as source:
                    data = io.BytesIO()
                    source.save(data, format=codec)
                data.seek(0)
                with Image.open(data) as decoded:
                    decoded.load()
                    self.assertEqual(decoded.size, (16, 12))
                    self.assertEqual(decoded.mode, "RGB")

    def test_installed_docling_converts_local_html_offline(self):
        installed = self.require_package("docling")
        self.require_package("pillow")
        self.assertGreaterEqual(tuple(int(p) for p in installed.split(".")), (2, 142, 0))
        self.assertEqual(os.environ["HF_HUB_OFFLINE"], "1")
        self.assertEqual(os.environ["TRANSFORMERS_OFFLINE"], "1")
        with TemporaryDirectory() as directory:
            root = Path(directory)
            document = root / "acceptance.html"
            document.write_text(
                "<!doctype html><html><body><h1>HAI intake</h1>"
                "<p>Offline conversion acceptance.</p></body></html>",
                encoding="utf-8",
            )
            converter = app.converter(False, root)
            text, pages = app.extract_text(document, "html", False, root, converter)
            self.assertIn("HAI intake", text)
            self.assertIn("Offline conversion acceptance.", text)
            self.assertGreaterEqual(pages, 0)


if __name__ == "__main__":
    unittest.main()
