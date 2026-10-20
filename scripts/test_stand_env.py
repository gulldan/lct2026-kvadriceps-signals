"""Проверка границы: настройки стенда не становятся публичным доказательством."""

import argparse
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

from test_radar_acceptance import RADAR, fixture
from tools.stand_env import configure_dataset_args, load_stand_env, normalize_api_base, redact


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    loaded = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(loaded)
    return loaded


EVIDENCE = module("acceptance_evidence", Path(__file__).with_name("acceptance-evidence.py"))
SMOKE = module("demo_smoke", Path(__file__).parent / "tools/demo/demo_smoke.py")


class StandPrivacyTest(unittest.TestCase):
    def test_env_only_and_public_evidence(self):
        env = {"STAND_URL": "https://fixture-stand.example.test", "STAND_USER": "fixture-operator",
               "STAND_PASSWORD": 'fixture-password-"quoted"', "STAND_TOKEN": "fixture-access-token"}
        with tempfile.TemporaryDirectory() as directory, mock.patch.dict(os.environ, env, clear=True):
            private = Path(directory) / "private.env"
            private.write_text('STAND_USER=dotenv-user\nSTAND_DATASET_DIR=/fixture/data\n')
            with mock.patch.dict(os.environ, {"STAND_ENV_FILE": str(private)}):
                resolved = load_stand_env()
                self.assertEqual(resolved["STAND_USER"], env["STAND_USER"])
                self.assertEqual(resolved["STAND_DATASET_DIR"], "/fixture/data")
            nested = {"count": 7, "rows": [{"password": "unconfigured-password", "text": env["STAND_URL"]}],
                      "encoded": json.dumps({"value": env["STAND_PASSWORD"]})}
            safe = redact(nested, env)
            self.assertEqual(safe["count"], 7)
            self.assertNotIn("unconfigured-password", json.dumps(safe))
            self.assertNotIn("fixture-password", safe["encoded"])

            detail = fixture()
            detail.update(observed_at=env["STAND_PASSWORD"], private_diagnostics=nested,
                          private_token=env["STAND_TOKEN"])
            output = Path(directory) / "result.json"
            console = io.StringIO()
            with mock.patch.object(sys, "argv", ["radar", detail["scope"]["query"], "--out", str(output)]), \
                    mock.patch.object(RADAR, "search", return_value=({"detail": detail}, {})), \
                    mock.patch.object(RADAR, "login", side_effect=AssertionError("token avoids login")), \
                    contextlib.redirect_stdout(console), contextlib.redirect_stderr(console):
                self.assertEqual(RADAR.main(), 0)
            public = console.getvalue() + output.read_text()
            saved = json.loads(output.read_text())
            self.assertEqual(saved["response"]["detail"]["counters"], detail["counters"])
            self.assertTrue(saved["validation"]["passed"])
            for value in env.values():
                self.assertNotIn(value, public)
            self.assertNotIn("fixture-stand.example.test", public)

            console = io.StringIO()
            with mock.patch.object(sys, "argv", ["evidence"]), \
                    mock.patch.object(EVIDENCE, "command", return_value="fixture-version"), \
                    mock.patch.object(EVIDENCE, "memory_bytes", return_value=1024), \
                    mock.patch.object(EVIDENCE, "probe", return_value={"passed": True, "detail": nested}), \
                    contextlib.redirect_stdout(console):
                self.assertEqual(EVIDENCE.main(), 0)
            self.assertEqual(json.loads(console.getvalue())["base"], "[configured via STAND_URL]")
            for value in env.values():
                self.assertNotIn(value, console.getvalue())

            console = io.StringIO()
            with mock.patch.object(sys, "argv", ["radar", "query", "--password", "cli-private-value"]), \
                    contextlib.redirect_stderr(console), self.assertRaises(SystemExit):
                RADAR.main()
            self.assertNotIn("cli-private-value", console.getvalue())

            parser = argparse.ArgumentParser()
            args = argparse.Namespace(api_base=None, username=None, password=None)
            configured = configure_dataset_args(parser, args, {"RAG_DATASET_USER": "dataset-user", "RAG_DATASET_PASS": "dataset-pass"})
            self.assertEqual((configured.username, configured.password), ("dataset-user", "dataset-pass"))
            local = configure_dataset_args(parser, argparse.Namespace(api_base=None, username=None, password=None),
                                           {"RAG_API": "", "RAG_DATASET_USER": "", "RAG_DATASET_PASS": ""})
            self.assertEqual(normalize_api_base(local.api_base), "http://localhost:8080/api/v1")
            with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                configure_dataset_args(parser, argparse.Namespace(api_base=None, username=None, password="cli-secret"), {})
            with mock.patch.object(SMOKE, "ROOT", Path(directory)):
                effective = SMOKE.effective_demo_env(Path(directory) / "absent.env")
                self.assertNotIn("ADMIN_PASSWORD", effective)
                with mock.patch.object(SMOKE.subprocess, "run", return_value=argparse.Namespace(
                        returncode=1, stdout=env["STAND_PASSWORD"], stderr=env["STAND_TOKEN"])):
                    with self.assertRaises(RuntimeError) as failed:
                        SMOKE.run(["operator", env["STAND_PASSWORD"]])
                    self.assertNotIn("fixture-", str(failed.exception))


if __name__ == "__main__":
    unittest.main()
