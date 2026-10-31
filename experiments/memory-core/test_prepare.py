import io
from pathlib import Path
import tarfile
import tempfile
import unittest

from prepare import unpack, verify_archive


class PrepareTests(unittest.TestCase):
    def test_changed_upstream_archive_is_refused(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "source.tar.gz"
            path.write_bytes(b"changed archive")
            with self.assertRaisesRegex(ValueError, "checksum"):
                verify_archive(path)

    def test_archive_cannot_escape_destination(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive = root / "source.tar.gz"
            with tarfile.open(archive, "w:gz") as bundle:
                info = tarfile.TarInfo("../escaped")
                info.size = 4
                bundle.addfile(info, io.BytesIO(b"oops"))
            with self.assertRaisesRegex(ValueError, "path"):
                unpack(archive, root / "unpacked")
            self.assertFalse((root / "escaped").exists())


if __name__ == "__main__":
    unittest.main()
