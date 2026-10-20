"""Параметры доступа операторских утилит поступают только из окружения."""

import argparse
from pathlib import Path
import sys
from urllib.parse import quote, quote_plus

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from stand_env import load_stand_env, normalize_api_base, redact  # noqa: E402


PRIVATE_KEYS = (
    "RABBITMQ_API", "RABBITMQ_USER", "RABBITMQ_PASSWORD", "S3_ENDPOINT",
    "S3_ACCESS_KEY", "S3_SECRET_KEY", "OPENALEX_MAILTO", "OPENALEX_API_KEY",
    "RAG_API", "SIGNALGRAPH_TOKEN",
)


class EnvironmentOnly(argparse.Action):
    def __call__(self, parser, namespace, values, option_string=None):
        parser.error("connection settings and credentials are accepted only through environment variables")


def load_environment(parser):
    try:
        return load_stand_env()
    except ValueError as error:
        parser.error(str(error))


def add_env_argument(parser, env, flag, key, default=""):
    parser.add_argument(flag, default=env.get(key, default), action=EnvironmentOnly,
                        help=f"compatibility: set {key} in the environment")


def public_output(value, extra_sensitive=()):
    env = load_stand_env()
    private = [env.get(key, "") for key in PRIVATE_KEYS]
    private.extend(extra_sensitive)
    # Адреса и контакты могут вернуться в ошибке уже закодированными в запросе.
    encoded = [part for item in private if item for part in (item, quote(item, safe=""), quote_plus(item))]
    return redact(value, env, encoded)
