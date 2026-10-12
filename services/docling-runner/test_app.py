import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

import app


class SelectedFolderTests(unittest.TestCase):
    def test_rejects_unbounded_paths_before_reading_any_document(self):
        for value in ("", ".", "../outside", "/absolute"):
            with self.assertRaises(ValueError):
                app.selected_folder(value)

    def test_candidate_files_are_bounded_to_allowed_documents(self):
        with TemporaryDirectory() as directory:
            folder = Path(directory)
            for index in range(app.MAX_DOCUMENTS + 3):
                (folder / f"brief-{index}.md").write_text("reviewable text", encoding="utf-8")

            candidates = app.candidate_files(folder, pdf_enabled=False)

            self.assertEqual(app.MAX_DOCUMENTS, len(candidates))
            self.assertTrue(all(file_format == "markdown" for _, file_format in candidates))

    def test_plain_text_extraction_does_not_initialize_docling_converter(self):
        with TemporaryDirectory() as directory:
            folder = Path(directory)
            document = folder / "brief.md"
            document.write_text("reviewable text", encoding="utf-8")

            original_selected_folder = app.selected_folder
            original_configured = app.configured
            original_candidate_files = app.candidate_files
            original_converter = app.converter
            original_docling_version = app.docling_version
            try:
                app.selected_folder = lambda _name: (folder, "legal/briefs")
                app.configured = lambda: ("a" * 16, folder, False, folder)
                app.candidate_files = lambda _folder, _pdf_enabled: [(document, "markdown")]
                app.converter = lambda *_args: self.fail("text-only extraction must not initialize Docling")
                app.docling_version = lambda: "2.114.0"

                result = app.extract("legal/briefs")

                self.assertEqual("completed", result["status"])
                self.assertEqual("reviewable text", result["documents"][0]["text"])
            finally:
                app.selected_folder = original_selected_folder
                app.configured = original_configured
                app.candidate_files = original_candidate_files
                app.converter = original_converter
                app.docling_version = original_docling_version


if __name__ == "__main__":
    unittest.main()
