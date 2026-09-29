"""Снимок явно выбранных аннотаций v1 без разметки и признаков ранжирования."""
from __future__ import annotations

import argparse
import hashlib
import json
import re
import time
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET
from datetime import datetime, timezone
from pathlib import Path


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise ValueError(f"unexpected API redirect ({code})")


def capture(ids: list[str], output: Path) -> None:
    if not 1 <= len(ids) <= 20 or len(set(ids)) != len(ids):
        raise ValueError("one request must contain 1..20 distinct explicit version-1 IDs")
    if any(not re.fullmatch(r"\d{4}\.\d{4,5}v1", native) for native in ids):
        raise ValueError("only explicit modern arXiv v1 IDs are accepted")
    output.mkdir(parents=True, exist_ok=True)
    names = [output / (native + ".json") for native in ids]
    if any(p.exists() for p in names):
        raise ValueError("capture files are immutable; select only uncaptured IDs")
    url = "https://export.arxiv.org/api/query?" + urllib.parse.urlencode(
        {"id_list": ",".join(ids), "max_results": len(ids)}
    )
    req = urllib.request.Request(url, headers={"User-Agent": "SignalGraph-eval/1.0 (bounded academic source capture)"})
    with urllib.request.build_opener(NoRedirect()).open(req, timeout=45) as response:
        raw = response.read((4 << 20) + 1)
    if len(raw) > 4 << 20 or b"<!DOCTYPE" in raw.upper() or b"<!ENTITY" in raw.upper():
        raise ValueError("unsafe or oversized Atom response")
    ns = {"a": "http://www.w3.org/2005/Atom"}
    root = ET.fromstring(raw)
    digest = hashlib.sha256(raw).hexdigest()
    records = {}
    observed = datetime.now(timezone.utc).isoformat()
    for entry in root.findall("a:entry", ns):
        def field(name):
            return entry.findtext("a:" + name, "", ns).strip()
        native = field("id").rsplit("/abs/", 1)[-1]
        if native not in ids or native in records:
            raise ValueError("API returned an unexpected or duplicate revision")
        published, updated = field("published"), field("updated")
        if published != updated:
            raise ValueError("version-1 metadata must identify its original publication")
        datetime.fromisoformat(published.replace("Z", "+00:00"))
        text = " ".join(field("summary").split())
        if not text or not field("title"):
            raise ValueError("missing primary abstract or title")
        records[native] = {
            "native_id": native, "canonical_id": "arxiv:" + native[:-2],
            "url": "https://arxiv.org/abs/" + native,
            "title": " ".join(field("title").split()),
            "authors": [e.findtext("a:name", "", ns) for e in entry.findall("a:author", ns)],
            "published_at": published, "version_updated_at": updated,
            "observed_at": observed, "available_at": published,
            "available_basis": "arXiv archived v1 release; historical reconstruction captured later",
            "source_kind": "paper", "provider": "arxiv", "access_scope": "public",
            "representation": "versioned_primary_abstract", "text": text,
            "text_sha256": hashlib.sha256(text.encode()).hexdigest(),
            "raw_sha256": digest, "raw_file": digest + ".xml",
            "limitations": ["Abstract-only evidence; author-reported results, not independent reproduction.",
                            "Later DOI, journal metadata and current citations are excluded from historical input."],
        }
    if set(records) != set(ids):
        raise ValueError("API omitted requested revisions")
    raw_path = output / (digest + ".xml")
    with raw_path.open("xb") as stream:
        stream.write(raw)
    for native, record in records.items():
        with (output / (native + ".json")).open("x") as stream:
            json.dump(record, stream, ensure_ascii=False, indent=2)
            stream.write("\n")
    print(json.dumps({"records": len(records), "raw_sha256": digest, "ids": ids}))
    # Последовательные вызовы держатся ниже общего лимита соединений arXiv.
    time.sleep(3)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("ids", nargs="+")
    parser.add_argument("--output", type=Path, default=Path(__file__).parent / "sources")
    args = parser.parse_args()
    capture(args.ids, args.output)
