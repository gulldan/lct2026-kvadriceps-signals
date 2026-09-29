"""Bounded S3 access through the maintained AWS SDK, using existing credentials."""
from __future__ import annotations

import os

import boto3
from botocore.config import Config


class Objects:
    def __init__(self, *, readiness: bool = False):
        self.bucket = os.getenv("S3_BUCKET", "documents")
        self.client = boto3.client(
            "s3", endpoint_url=os.getenv("S3_ENDPOINT") or None,
            region_name=os.getenv("S3_REGION", "us-east-1"),
            aws_access_key_id=os.getenv("S3_ACCESS_KEY") or None,
            aws_secret_access_key=os.getenv("S3_SECRET_KEY") or None,
            config=Config(connect_timeout=2 if readiness else 5, read_timeout=2 if readiness else 30,
                          retries={"max_attempts": 0 if readiness else 2, "mode": "standard"},
                          s3={"addressing_style": "path" if os.getenv("S3_USE_PATH_STYLE", "true").lower() == "true"
                              else "auto"}),
        )

    def read(self, key: str, maximum: int) -> bytes:
        response = self.client.get_object(Bucket=self.bucket, Key=key)
        stream = response["Body"]
        try:
            if response["ContentLength"] > maximum:
                raise ValueError("source artifact exceeds its declared budget")
            raw = stream.read(maximum + 1)
        finally:
            stream.close()
        if len(raw) > maximum or len(raw) != response["ContentLength"]:
            raise ValueError("source artifact is oversized or truncated")
        return raw

    def write_verified(self, key: str, raw: bytes) -> None:
        if not raw or len(raw) > 16 << 20:
            raise ValueError('invalid bounded analysis object')
        self.client.put_object(Bucket=self.bucket, Key=key, Body=raw, ContentType='application/json')
        if self.read(key, len(raw)) != raw:
            raise ValueError('analysis object verification failed')
