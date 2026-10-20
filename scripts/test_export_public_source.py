"""Публичная поставка включает рабочие правки и блокирует известные утечки."""

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock
import zipfile


SPEC = importlib.util.spec_from_file_location("public_export", Path(__file__).with_name("export-public-source.py"))
EXPORT = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(EXPORT)


class PublicExportTest(unittest.TestCase):
    def test_current_snapshot_and_publication_boundary(self):
        with tempfile.TemporaryDirectory() as directory, mock.patch.dict(
                os.environ, {"HOME": directory + "/private-home", "PATH": os.environ.get("PATH", os.defpath)}, clear=True):
            root = Path(directory) / "repo"
            root.mkdir()

            def git(*args):
                subprocess.run(["git", *args], cwd=root, check=True, capture_output=True)

            def write(name, content):
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(content)
                return path

            git("init", "--quiet")
            write(".gitignore", ".env\n*.pdf\n")
            tracked = write("tracked.py", "old source\n")
            deck = root / "docs/presentation/deck.pptx"
            deck.parent.mkdir(parents=True)
            with zipfile.ZipFile(deck, "w", compression=zipfile.ZIP_DEFLATED) as package:
                package.writestr("ppt/slides/_rels/slide1.xml.rels", '<Relationships Target="https://example.com"/>')
            original_deck = deck.read_bytes()
            git("add", ".gitignore", "tracked.py", "docs/presentation/deck.pptx")
            tracked.write_text("current uncommitted source\n")
            tracked.chmod(0o755)
            write("new.py", "new untracked source\n")
            write(".env", "API_KEY=fixture-private-credential\nSTAND_URL=https://fixture-stand.example.test\n")
            write(".env.example", "API_KEY=\nSTAND_URL=\n")
            write("frontend/node_modules/cache.js", "private dependency cache\n")
            write("datasets/private.json", "private runtime data\n")
            write(".claude/local-state.json", "private agent state\n")
            write(".codex/local-state.json", "private agent state\n")
            write(".agents/local-state.json", "private agent state\n")
            write(".claude/skills/public/SKILL.md", "public skill\n")
            write("backend/pkg/cache/source.py", "necessary source package\n")
            write("docs/presentation/deck.pdf", "%PDF-public fixture\n")
            write("docs/presentation/unmatched.pdf", "%PDF-private draft\n")

            target = root / "output/public.zip"
            count = EXPORT.export(root, target)
            with zipfile.ZipFile(target) as package:
                names = set(package.namelist())
                self.assertEqual(package.read("tracked.py"), b"current uncommitted source\n")
                self.assertIn("new.py", names)
                self.assertIn(".env.example", names)
                self.assertIn(".claude/skills/public/SKILL.md", names)
                self.assertIn("backend/pkg/cache/source.py", names)
                self.assertIn("docs/presentation/deck.pdf", names)
                self.assertNotIn("docs/presentation/unmatched.pdf", names)
                for excluded in (".env", "datasets/private.json", ".claude/local-state.json", ".codex/local-state.json",
                                 ".agents/local-state.json", "frontend/node_modules/cache.js"):
                    self.assertNotIn(excluded, names)
                self.assertFalse(any(name.startswith(".git/") for name in names))
                manifest = json.loads(package.read(EXPORT.MANIFEST))
                self.assertEqual(count, len(manifest))
                self.assertEqual([row["path"] for row in manifest], sorted(row["path"] for row in manifest))
                for row in manifest:
                    self.assertEqual(set(row), {"path", "sha256", "size"})
                    raw = package.read(row["path"])
                    self.assertEqual((row["sha256"], row["size"]), (hashlib.sha256(raw).hexdigest(), len(raw)))
                self.assertTrue(all(info.date_time == (1980, 1, 1, 0, 0, 0) and not info.extra for info in package.infolist()))
                self.assertEqual((package.getinfo("tracked.py").external_attr >> 16) & 0o777, 0o755)
            repeat = root / "output/repeat.zip"
            EXPORT.export(root, repeat)
            self.assertEqual(target.read_bytes(), repeat.read_bytes())
            with self.assertRaises(EXPORT.ExportError):
                EXPORT.export(root, target)

            blocked = root / "output/blocked.zip"
            leak = write("leak.py", 'credential = "fixture-private-credential"\n')
            git("add", "leak.py")
            with self.assertRaises(EXPORT.ExportError) as failure:
                EXPORT.export(root, blocked)
            self.assertIn("leak.py", str(failure.exception))
            self.assertNotIn("fixture-private-credential", str(failure.exception))
            self.assertFalse(blocked.exists())
            leak.unlink()

            with mock.patch.dict(os.environ, {"OPENALEX_MAILTO": "fixture-private@example.test", "SERVICE_TOKEN": "process-private-token"}):
                leak.write_text("fixture-private@example.test process-private-token\n")
                with self.assertRaisesRegex(EXPORT.ExportError, "configured private value"):
                    EXPORT.export(root, blocked)
                leak.unlink()

            external_env = Path(directory) / "operator.env"
            external_env.write_text("SERVICE_TOKEN=external-private-token\n"
                                    "STAND_URL=https://external-stand.example.test\n"
                                    "OPENALEX_MAILTO=external-private@example.test\n")
            with mock.patch.dict(os.environ, {"STAND_ENV_FILE": str(external_env)}):
                for value in ("external-private-token", "external-stand.example.test", "external-private@example.test"):
                    leak.write_text(value)
                    with self.assertRaisesRegex(EXPORT.ExportError, "configured private value") as failure:
                        EXPORT.export(root, blocked)
                    self.assertNotIn(value, str(failure.exception))
                leak.unlink()
                external_env.unlink()
                with self.assertRaisesRegex(EXPORT.ExportError, "STAND_ENV_FILE"):
                    EXPORT.export(root, blocked)

            deployment = {"VLLM_URL": "https://fixture-llm.example.test:8443", "RAG_API": "https://fixture-api.example.test/api/v1",
                          "PRESENTATION_RUNTIME_DIR": "/opt/fixture-presentation-runtime"}
            with mock.patch.dict(os.environ, deployment):
                for value in deployment.values():
                    leak.write_text(value)
                    with self.assertRaisesRegex(EXPORT.ExportError, "configured private value"):
                        EXPORT.export(root, blocked)
                leak.unlink()
            with mock.patch.dict(os.environ, {"VLLM_URL": "https://api.openai.com", "RAG_API": "http://localhost:8080/api/v1"}):
                EXPORT.inspect_content(Path("public.py"), b"https://api.openai.com http://localhost:8080/api/v1",
                                       EXPORT.known_private_values(root.resolve()))

            with zipfile.ZipFile(deck, "w", compression=zipfile.ZIP_DEFLATED) as package:
                package.writestr("ppt/slides/_rels/slide1.xml.rels", '<Relationships Target="https://fixture-stand.example.test"/>')
            with self.assertRaisesRegex(EXPORT.ExportError, "Office XML"):
                EXPORT.export(root, blocked)
            deck.write_bytes(original_deck)

            outside = Path(directory) / "outside.txt"
            outside.write_text("outside repository\n")
            link = root / "escape.py"
            link.symlink_to(outside)
            with self.assertRaisesRegex(EXPORT.ExportError, "symlink escapes"):
                EXPORT.export(root, blocked)
            link.unlink()
            git("add", "--force", ".env")
            with self.assertRaisesRegex(EXPORT.ExportError, "tracked private environment"):
                EXPORT.export(root, blocked)
            self.assertFalse(blocked.exists())
            self.assertFalse(list(blocked.parent.glob(".public-source-*.zip")))


if __name__ == "__main__":
    unittest.main()
