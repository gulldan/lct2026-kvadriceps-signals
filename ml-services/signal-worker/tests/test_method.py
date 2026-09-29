"""Поставляемый артефакт метода должен соответствовать установленному коду."""

from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from signalgraph import method


class MethodArtifactTest(unittest.TestCase):
    def test_bundled_method_matches_installed_implementation(self):
        artifact = Path(method.__file__).resolve().parent.parent / "method.json"
        method.verify(artifact.read_bytes())


if __name__ == "__main__":
    unittest.main()
