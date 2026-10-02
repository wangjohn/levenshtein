"""Fast regression tests for performance measurement provenance and isolation."""
import hashlib
import runpy
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

HARNESS = runpy.run_path(str(Path(__file__).with_name("measure-performance")))


class MeasurementTests(unittest.TestCase):
    def test_snapshot_copies_the_bytes_it_hashes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "source"
            root.mkdir()
            source = root / "sample.go"
            original = b"package original\n"
            source.write_bytes(original)
            destination = Path(directory) / "snapshot"
            read_bytes = Path.read_bytes

            def edit_after_read(path):
                data = read_bytes(path)
                if path == source:
                    source.write_bytes(b"package edited\n")
                return data

            snapshot = HARNESS["snapshot"]
            with patch.dict(snapshot.__globals__, ROOT=root), \
                    patch("subprocess.check_output", return_value=b"sample.go\0"), \
                    patch.object(Path, "read_bytes", edit_after_read):
                digest = snapshot(destination)

            self.assertEqual((destination / "sample.go").read_bytes(), original)
            expected = hashlib.sha256(b"sample.go\0" + hashlib.sha256(original).digest()).hexdigest()
            self.assertEqual(digest, expected)

    def test_external_compilation_cache_is_rejected_without_printing_setting(self):
        guard = HARNESS["require_local_build_cache"]
        with patch.dict(guard.__globals__, command=lambda *args: ("private-cache-program\n", 0)), \
                self.assertRaisesRegex(RuntimeError, "GOCACHEPROG must be unset") as failure:
            guard(Path("shared"), {})

        self.assertNotIn("private-cache-program", str(failure.exception))

    def test_local_compilation_cache_is_allowed(self):
        guard = HARNESS["require_local_build_cache"]
        with patch.dict(guard.__globals__, command=lambda *args: ("\n", 0)):
            guard(Path("shared"), {})


if __name__ == "__main__":
    unittest.main()
