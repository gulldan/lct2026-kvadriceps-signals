"""Параметры операторских утилит не принимают секреты из командной строки."""

import contextlib
import importlib
import io
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock
from urllib.parse import quote

DATA = Path(__file__).parent / "tools/data"
sys.path.insert(0, str(DATA))
from operator_env import public_output  # noqa: E402


class OperatorEnvironmentTest(unittest.TestCase):
    def test_environment_boundary_and_public_outputs(self):
        env = {
            "RABBITMQ_API": "https://rabbit.example.test", "RABBITMQ_USER": "fixture-rabbit-user",
            "RABBITMQ_PASSWORD": "fixture-rabbit-password", "S3_ENDPOINT": "https://s3.example.test",
            "S3_ACCESS_KEY": "fixture-s3-access", "S3_SECRET_KEY": "fixture-s3-secret",
            "OPENALEX_MAILTO": "fixture-contact@example.test", "OPENALEX_API_KEY": "fixture-openalex-key",
            "RAG_API": "https://api.example.test", "SIGNALGRAPH_TOKEN": "fixture-token",
        }
        scripts = {
            "restore_dataset_objects_to_s3": {"--endpoint": "S3_ENDPOINT", "--access-key": "S3_ACCESS_KEY", "--secret-key": "S3_SECRET_KEY"},
            "check_s3_db_drift": {"--endpoint": "S3_ENDPOINT", "--access-key": "S3_ACCESS_KEY", "--secret-key": "S3_SECRET_KEY"},
            "import_signalgraph_source": {"--api": "RAG_API"},
        }
        with tempfile.TemporaryDirectory() as directory:
            private = Path(directory) / "operator.env"
            private.write_text("\n".join(f"{key}={value}" for key, value in env.items()) + "\n")
            with mock.patch.dict(os.environ, {"STAND_ENV_FILE": str(private)}, clear=True):
                for name, flags in scripts.items():
                    module = importlib.import_module(name)
                    if name != "import_signalgraph_source":
                        with mock.patch.object(sys, "argv", [name]):
                            args = module.parse_args()
                        for flag, key in flags.items():
                            self.assertEqual(getattr(args, flag[2:].replace("-", "_")), env[key])
                    for flag in flags:
                        for arguments in ([flag, 'cli-private-"value"&+@'], [flag + '=cli-private-"value"&+@']):
                            result = subprocess.run([sys.executable, str(DATA / f"{name}.py"), *arguments],
                                                    capture_output=True, text=True, timeout=5, check=False)
                            self.assertEqual(result.returncode, 2, name)
                            self.assertNotIn("cli-private", result.stdout + result.stderr, name)
                            self.assertIn("only through environment variables", result.stderr, name)

                source = importlib.import_module("import_signalgraph_source")
                with mock.patch.object(sys, "argv", ["import", "--file", "source.txt", "--source", "source.json", "--receipt", "receipt.json"]), \
                        mock.patch.object(source, "import_source", return_value={"id": "doc", "status": "queued"}) as upload, \
                        contextlib.redirect_stdout(io.StringIO()):
                    source.main()
                self.assertEqual(upload.call_args.args[:2], (env["RAG_API"] + "/api/v1", env["SIGNALGRAPH_TOKEN"]))

                message = " ".join((*env.values(), quote(env["OPENALEX_MAILTO"], safe="")))
                public = public_output({"error": message, "count": 3, "url": "https://doi.org/10.1/public"})
                self.assertEqual(public["count"], 3)
                self.assertEqual(public["url"], "https://doi.org/10.1/public")
                self.assertNotIn("fixture-", public["error"])
                self.assertNotIn("example.test", public["error"])



if __name__ == "__main__":
    unittest.main()
