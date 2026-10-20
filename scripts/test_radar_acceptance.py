"""Один регрессионный набор контракта; семантика модели этими данными не оценивается."""

import copy
import contextlib
import hashlib
import importlib.util
import io
import json
import subprocess
import sys
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from unittest import mock
from pathlib import Path


SCRIPT = Path(__file__).with_name("radar-acceptance.py")
SPEC = importlib.util.spec_from_file_location("radar_acceptance", SCRIPT)
RADAR = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RADAR)


def fixture(count=15):
    scope = {"query": "технологии", "mode": "any",
             "branches": [{"id": "b1", "task": "Проверка фазовой согласованности"}], "required": []}
    papers = [{"id": str(i), "title": f"A technical trial report {i}",
               "date": "2026-09-01", "url": f"https://source-{i}.example/report"} for i in range(3)]
    quotes = [{"source_id": paper["id"], "quote": paper["title"]} for paper in papers]
    evidence = [{**quotes[i], "field": field, "verified": True,
                 "verification_reason": "Источник: проведено испытание. Утверждение: описано испытание. Основание: факты совпадают."}
                for i, field in enumerate(RADAR.FIELDS)]
    candidate = {
        "id": "tech", "label": "Проверяемая технология", "paper_ids": ["0", "1", "2"], "evidence_ids": [],
        "papers_count": 3, "records_count": 0, "documents_count": 3, "groups": 2,
        "qualification": {"technology": True, "relevant": True, "coherent": True, "verified": True,
                          "label": "Проверяемая технология", "method": "grounded-entity-review-v7",
                          "scope": copy.deepcopy(scope),
                          "mechanism": "Проверка фазовой согласованности",
                          "model": "reader", "verifier_model": "reviewer",
                          "reason": "Связный набор источников",
                          "evidence": [{**quote, "same_technology": True, "role": "evaluates",
                                        "identity_reason": "Испытан тот же механизм", "scope_criteria": ["b1"],
                                        "mechanism_quote": quote["quote"], "relevance_quote": quote["quote"],
                                        "relevance_reason": "Прямо описана требуемая задача"} for quote in quotes]},
        "card": {"summary": "Описание технологии", "advantage": "Подтверждённое преимущество",
                 "example": "Проведённое испытание", "example_id": "2", "model": "writer",
                 "verification_model": "reviewer", "evidence": evidence},
        "confidence": {"method": RADAR.RULE_METHOD, "passed": 4, "total": RADAR.RULE_TOTAL,
                       "value": 4 / RADAR.RULE_TOTAL, "measured": RADAR.RULE_TOTAL,
                       "reasons": ["а"] * 4, "missing": ["б"] * (RADAR.RULE_TOTAL - 4),
                       "threshold": 0.75, "above_threshold": False},
        "market": {"status": "unknown", "known": False, "mature": False, "basis": "Нет подтверждения стадии"},
    }
    candidates = []
    for index in range(count):
        item = copy.deepcopy(candidate)
        item.update(id=f"tech-{index}", label=f"Проверяемая технология {index + 1}")
        item["qualification"]["label"] = item["label"]
        candidates.append(item)
    return {"method": RADAR.METHOD, "search_contract": RADAR.SEARCH_CONTRACT, "scope": scope,
            "topic": {}, "candidates": candidates, "rejected": [], "papers": papers,
            "passports": {str(i): {"language": "en", "language_basis": "Язык заявлен издателем",
                                    "publisher_type": "academic", "genre": "peer_reviewed", "trust": "trusted",
                                    "trust_basis": "Академическое издание", "digest": "Русский пересказ",
                                    "digest_model": "reader"} for i in range(3)},
            "counters": {"selected": count, "rejected": 0, "candidates": count, "sources_processed": 3,
                         "above_threshold": 0, "model_scored": 0, "model_above_threshold": 0}}


def scored_fixture():
    detail = fixture()
    detail["classifier_version"] = "fixture-v1"
    for candidate in detail["candidates"]:
        candidate["model_score"] = {
            "label": "weak_signal", "value": 0.8, "threshold": 0.7, "model": "test-classifier",
            "version": "fixture-v1", "feature_schema": "weak-signal-features-v1", "basis": "Основание оценки",
            "features": [{"name": "Число документов", "value": "3", "contribution": 0.1}],
        }
    detail["counters"].update(model_scored=15, model_above_threshold=15)
    return detail


class AcceptanceContractTest(unittest.TestCase):
    def test_limitation_cannot_hide_an_unsupported_tail_or_change_qualifiers(self):
        claim = "Пять скринингов; метод может не подойти для редких образцов."
        evidence = {"quote": "Five screens were performed. The method may not be suitable for rare specimens.",
                    "parts": [
                        {"claim": "Пять скринингов;", "source_fact": "Five screens were performed.",
                         "direct": True, "scope_preserved": True, "modality_preserved": True},
                        {"claim": " метод может не подойти для редких образцов.",
                         "source_fact": "The method may not be suitable for rare specimens.",
                         "direct": True, "scope_preserved": True, "modality_preserved": True}]}

        def problems(value):
            found = []
            RADAR.validate_limitation_parts(claim, value, "карточка", lambda ok, message: found.append(message) if not ok else None)
            return found

        self.assertFalse(problems(evidence))
        for full, pieces, want in (
            ("Пять скринингов; метод может не подойти.", ["Пять скринингов", "метод может не подойти"], True),
            ("Пять скринингов, а метод может не подойти.", ["Пять скринингов", "метод может не подойти"], True),
            ("Модели не созданы (возможен отбор), а WES ограничен двумя образцами.",
             ["Модели не созданы", "возможен отбор", "WES ограничен двумя образцами"], True),
            ("Метод не подходит.", ["Метод", "подходит"], False),
            ("Метод подходит только для этих образцов.", ["Метод подходит"], False),
            ("Один или два протокола.", ["Один", "два протокола"], False),
            ("Срок 1,5 года.", ["Срок 1", "5 года"], False),
            ("Проверены варианты.", ["Проверены вар", "анты"], False),
        ):
            with self.subTest(claim=full):
                self.assertEqual(RADAR.limitation_parts_cover(full, pieces), want)
        for defect in ("missing", "tail", "direct", "scope_preserved", "modality_preserved", "paraphrase"):
            with self.subTest(defect=defect):
                broken = copy.deepcopy(evidence)
                if defect == "missing":
                    broken.pop("parts")
                elif defect == "tail":
                    broken["parts"].pop()
                elif defect == "paraphrase":
                    broken["parts"][0]["source_fact"] = "Five tumors were tested."
                else:
                    broken["parts"][0][defect] = False
                self.assertTrue(problems(broken), defect)

    def test_publisher_revision_binds_quotes_to_the_current_description(self):
        detail = fixture(1)
        requested, resolved = "10.5281/zenodo.123", "10.5281/zenodo.456"
        source = "openalex:" + requested
        description = "The corrected experiment found lower accuracy than the no-pruning baseline."
        detail["papers"][0].update(id=source, canonical_id=requested, url="https://zenodo.org/records/456")
        detail["passports"][source] = detail["passports"].pop("0")
        detail["observed_at"] = "2026-09-27T04:00:00Z"
        detail["source_revisions"] = {source: {
            "requested_doi": requested, "resolved_doi": resolved, "concept_doi": requested,
            "source_url": "https://zenodo.org/records/456", "api_url": "https://zenodo.org/api/records/456",
            "source_updated_at": "2026-08-28T00:00:00Z", "fetched_at": "2026-09-27T04:00:00.123Z",
            "raw_sha256": "a" * 64, "text_sha256": hashlib.sha256(description.encode()).hexdigest(),
            "text": description, "coverage": "publisher_metadata",
        }}
        candidate = detail["candidates"][0]
        candidate["paper_ids"][0] = source
        evidence = candidate["qualification"]["evidence"][0]
        evidence.update(source_id=source, quote=description, mechanism_quote=description, relevance_quote=description)
        candidate["card"]["evidence"][0].update(source_id=source, quote=description)

        def validate(answer):
            return RADAR.validate(answer, allow_shortfall=True)

        self.assertTrue(validate(detail)["passed"], validate(detail)["problems"])
        self.assertEqual(validate(detail)["quotes_without_returned_original"], 0)
        pinned = copy.deepcopy(detail)
        pinned["source_revisions"][source].update(resolved_doi=requested, source_url="https://zenodo.org/records/123",
                                                  api_url="https://zenodo.org/api/records/123")
        pinned["papers"][0]["url"] = "https://zenodo.org/records/123"
        self.assertTrue(validate(pinned)["passed"], validate(pinned)["problems"])
        for defect in ("missing_revision", "stale_quote", "hash", "identity", "private", "other_host",
                       "misleading_coverage", "future_source", "missing_time", "oversized", "invalid_doi"):
            with self.subTest(defect=defect):
                broken = copy.deepcopy(detail)
                revision = broken["source_revisions"][source]
                if defect == "missing_revision":
                    broken.pop("source_revisions")
                elif defect == "stale_quote":
                    broken["candidates"][0]["card"]["evidence"][0]["quote"] = "Pruning improves accuracy."
                elif defect == "hash":
                    revision["text"] += " Added claim."
                elif defect == "identity":
                    revision["concept_doi"] = "10.5281/zenodo.789"
                elif defect == "private":
                    broken["papers"][0]["document_id"] = "private-document"
                elif defect == "other_host":
                    revision["api_url"] = "https://zenodo.org.evil.example/api/records/456"
                elif defect == "misleading_coverage":
                    revision["coverage"] = "fulltext"
                elif defect == "future_source":
                    revision["source_updated_at"] = "2027-01-01T00:00:00Z"
                elif defect == "missing_time":
                    revision.pop("fetched_at")
                elif defect == "oversized":
                    revision["text"] = "x" * 4001
                    revision["text_sha256"] = hashlib.sha256(revision["text"].encode()).hexdigest()
                elif defect == "invalid_doi":
                    revision["resolved_doi"] = "10.5281/zenodo.456?url=http://127.0.0.1"
                self.assertFalse(validate(broken)["passed"], defect)

    def test_query_scope_is_grounded_and_canonical_name_uses_own_originals(self):
        query = "банковский антифрод или биометрия"
        scope = {"query": query, "mode": "any",
                 "branches": [{"id": "b1", "task": "Обнаружение мошенничества"},
                              {"id": "b2", "task": "Биометрическая проверка"}],
                 "required": [{"id": "c1", "condition": "Банковские платежи"}]}
        detail = scored_fixture()
        detail["scope"] = copy.deepcopy(scope)
        for candidate in detail["candidates"]:
            candidate["qualification"]["scope"] = copy.deepcopy(scope)
            for evidence in candidate["qualification"]["evidence"]:
                evidence["scope_criteria"] = ["b1", "c1"]

        def validate(answer):
            return RADAR.validate(answer, require_model=True, expected_query=query)

        self.assertTrue(validate(detail)["top15_passed"])
        both = copy.deepcopy(detail)
        both["scope"]["mode"] = "all"
        for candidate in both["candidates"]:
            candidate["qualification"]["scope"]["mode"] = "all"
            candidate["qualification"]["evidence"][1]["scope_criteria"] = ["b2", "c1"]
        self.assertTrue(validate(both)["top15_passed"])

        for mode in ("any", "all"):
            for defect in ("missing_domain", "missing_branches", "missing_criteria", "empty_criteria",
                           "unknown_id", "duplicate_id", "wrong_type", "context_domain", "different_mechanism_domain",
                           "changed_query", "changed_mode", "changed_branch", "changed_condition", "missing_scope"):
                with self.subTest(mode=mode, defect=defect):
                    broken = copy.deepcopy(both if mode == "all" else detail)
                    qualification = broken["candidates"][0]["qualification"]
                    evidence = qualification["evidence"]
                    if defect.endswith("domain"):
                        for item in evidence:
                            item["scope_criteria"].remove("c1")
                        if defect != "missing_domain":
                            evidence[0]["scope_criteria"].append("c1")
                            if defect == "context_domain":
                                evidence[0]["role"] = "context"
                            else:
                                evidence[0]["same_technology"] = False
                    elif defect == "missing_branches":
                        for item in evidence:
                            item["scope_criteria"] = ["c1"] if mode == "any" else ["b1", "c1"]
                    elif defect == "missing_criteria":
                        evidence[0].pop("scope_criteria")
                    elif defect in ("empty_criteria", "unknown_id", "duplicate_id", "wrong_type"):
                        evidence[0]["scope_criteria"] = {
                            "empty_criteria": [], "unknown_id": ["b1", "c9"],
                            "duplicate_id": ["b1", "b1", "c1"], "wrong_type": "b1 c1",
                        }[defect]
                    elif defect == "changed_query":
                        qualification["scope"]["query"] = "другая задача"
                    elif defect == "changed_mode":
                        qualification["scope"]["mode"] = "all" if mode == "any" else "any"
                    elif defect == "changed_branch":
                        qualification["scope"]["branches"][0]["task"] = "Сетевая безопасность"
                    elif defect == "changed_condition":
                        qualification["scope"]["required"][0]["condition"] = "Любая отрасль"
                    else:
                        qualification.pop("scope")
                    checked = validate(broken)
                    self.assertFalse(checked["top15_passed"], checked["problems"])
                    if defect.endswith("domain") or defect == "missing_branches":
                        self.assertTrue(any("не подтверждают ветви и обязательные условия" in message
                                            for message in checked["problems"]))

        scope_mutations = {
            "missing": lambda d: d.pop("scope"),
            "mode": lambda d: d["scope"].update(mode="or"),
            "query": lambda d: d["scope"].update(query="другой запрос"),
            "unnormalized": lambda d: d["scope"].update(query=query.upper()),
            "missing_required": lambda d: d["scope"].pop("required"),
            "no_branches": lambda d: d["scope"].update(branches=[]),
            "branch_id": lambda d: d["scope"]["branches"][0].update(id="b2"),
            "condition_id": lambda d: d["scope"]["required"][0].update(id="c2"),
            "control_text": lambda d: d["scope"]["branches"][0].update(task="Антифрод\x00в банках"),
            "long_text": lambda d: d["scope"]["required"][0].update(condition="я" * 161),
        }
        for defect, mutate in scope_mutations.items():
            with self.subTest(scope=defect):
                broken = copy.deepcopy(detail)
                mutate(broken)
                for candidate in broken["candidates"]:
                    candidate["qualification"]["scope"] = copy.deepcopy(broken.get("scope"))
                self.assertFalse(validate(broken)["top15_passed"])

        named = copy.deepcopy(detail)
        named["papers"][0]["title"] = "Fidelity-Driven Quantum Autoencoder: " + named["papers"][0]["title"]
        named["papers"].append({"id": "foreign", "title": "Foreign Mechanism Name"})
        named["paper_metadata"] = {str(i): {"abstract": "The technical mechanism was evaluated."} for i in range(3)}
        named["paper_metadata"]["0"]["abstract"] = "We evaluate the Fidelity-based Quantum Autoencoder."
        names = (("Fidelity-Driven Quantum Autoencoder", True), ("Fidelity-based Quantum Autoencoder", True),
                 (" Fidelity-based\nQuantum Autoencoder ", True), ("", True),
                 ("Fidelity-enhanced Quantum Autoencoder", False), ("Foreign Mechanism Name", False),
                 ("Fidelity-Driven Quantum Autoencoder FiD-QAE", False), ("Fidelity-Driven Quantum Auto", False),
                 ("Autoencoder", False), ("word " * 13, False), ("x" * 161 + " name", False), (None, False))
        for name, passed in names:
            with self.subTest(canonical_name=name):
                named["candidates"][0]["qualification"]["canonical_name"] = name
                checked = validate(named)
                self.assertEqual(checked["top15_passed"], passed, checked["problems"])

        unreturned = copy.deepcopy(detail)
        qualification = unreturned["candidates"][0]["qualification"]
        qualification["canonical_name"] = "Unreturned Technical Mechanism"
        qualification["evidence"][0]["quote"] = qualification["canonical_name"]
        checked = validate(unreturned)
        self.assertTrue(checked["top15_passed"], checked["problems"])
        self.assertFalse(checked["semantic_accuracy_checked"])
        self.assertEqual(checked["quotes_without_returned_original"], 1)
        self.assertTrue(any("исходное имя не сверено" in warning for warning in checked["warnings"]))

        with tempfile.TemporaryDirectory() as directory:
            source, output = Path(directory) / "scope.json", Path(directory) / "checked.json"
            source.write_text(json.dumps({"query": query, "response": {"detail": detail}}))
            with mock.patch.object(sys, "argv", ["radar", "--detail-file", str(source), "--out", str(output)]), \
                    contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(RADAR.main(), 0)
            self.assertEqual(json.loads(output.read_text())["query"], query)
            source.write_text(json.dumps({"query": "другая задача", "response": {"detail": detail}}))
            with mock.patch.object(sys, "argv", ["radar", "--detail-file", str(source)]), \
                    contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(RADAR.main(), 1)

    def test_fulltext_quotes_use_the_saved_source_context(self):
        detail = scored_fixture()
        card = detail["candidates"][0]["card"]
        body = "Table III. At T=40, accuracy changed from .83 to .75; AUC from .93 to .86; EER from .14 to .19."
        content = detail["papers"][0]["title"] + "\n" + body
        source = {"source_id": "0", "source_url": "https://arxiv.org/abs/2511.12739v1", "arxiv_id": "2511.12739v1",
                  "coverage": "excerpted_fulltext", "limitation": "Переданы целые фрагменты; остальное не поместилось",
                  "text": content, "text_sha256": hashlib.sha256(content.encode()).hexdigest(),
                  "raw_sha256": hashlib.sha256(("<p>" + body + "</p>").encode()).hexdigest(),
                  "body_sha256": hashlib.sha256(body.encode()).hexdigest(), "parser_revision": "fixture-v1",
                  "observed_at": "2026-09-27T00:00:00Z", "ranges": [{"start_rune": 0, "end_rune": len(body)}]}
        card["source_contexts"] = [source]
        card["evidence"][1].update(source_id="0", quote=body)
        detail["papers"][0]["url"] = "https://arxiv.org/abs/2511.12739"
        detail["paper_metadata"] = {"0": {"abstract": "The method was evaluated in a limited laboratory trial."}}
        detail["candidates"][0]["market"] = {
            "status": "early", "known": True, "mature": False, "basis": "Описан лабораторный эксперимент",
            "evidence": [{"field": "market", "source_id": "0", "quote": body, "verified": True}]}
        checked = RADAR.validate(detail, require_model=True)
        self.assertTrue(checked["top15_passed"], checked["problems"])
        self.assertEqual(checked["quotes_without_returned_original"], 0)
        self.assertFalse(checked["semantic_accuracy_checked"])
        mutations = {
            "changed_text": lambda d, c, s: s.update(text=s["text"] + " changed"),
            "wrong_source": lambda d, c, s: s.update(source_id="elsewhere"),
            "wrong_paper": lambda d, c, s: s.update(arxiv_id="9999.99999v1", source_url="https://arxiv.org/html/9999.99999v1"),
            "wrong_linked_version": lambda d, c, s: s.update(arxiv_id="2511.12739v2"),
            "lost_version": lambda d, c, s: s.update(arxiv_id="2511.12739", source_url="https://arxiv.org/html/2511.12739"),
            "untrusted_host": lambda d, c, s: s.update(source_url="https://arxiv.org.example/html/2511.12739v1"),
            "insecure_context": lambda d, c, s: s.update(source_url="http://arxiv.org/html/2511.12739v1"),
            "changed_pinned_version": lambda d, c, s: d["papers"][0].update(url="https://arxiv.org/abs/2511.12739v2"),
            "wrong_quote": lambda d, c, s: c["evidence"][1].update(quote="Accuracy was preserved"),
            "duplicate_source": lambda d, c, s: c["source_contexts"].append(copy.deepcopy(s)),
            "unknown_coverage": lambda d, c, s: s.update(coverage="verified"),
            "unexplained_excerpt": lambda d, c, s: s.pop("limitation"),
            "missing_original_hash": lambda d, c, s: s.pop("raw_sha256"),
            "reversed_range": lambda d, c, s: s.update(ranges=[{"start_rune": 4, "end_rune": 2}]),
            "overlapping_ranges": lambda d, c, s: s.update(ranges=[{"start_rune": 0, "end_rune": 4},
                                                                  {"start_rune": 3, "end_rune": 5}]),
            "qualification_did_not_read_body": lambda d, c, s: d["candidates"][0]["qualification"]["evidence"][0].update(quote=body),
        }
        for name, mutate in mutations.items():
            with self.subTest(name=name):
                broken = copy.deepcopy(detail)
                altered = broken["candidates"][0]["card"]
                mutate(broken, altered, altered["source_contexts"][0])
                self.assertFalse(RADAR.validate(broken, require_model=True)["passed"])
        # Только аннотация остаётся честным допустимым источником; полного текста может не быть.
        source.update(coverage="abstract", text=detail["papers"][0]["title"], limitation="Полный текст недоступен")
        for key in ("raw_sha256", "body_sha256", "parser_revision", "observed_at", "ranges"):
            source.pop(key)
        source["text_sha256"] = hashlib.sha256(source["text"].encode()).hexdigest()
        card["evidence"][1]["quote"] = source["text"]
        detail["candidates"][0]["market"]["evidence"][0]["quote"] = source["text"]
        self.assertTrue(RADAR.validate(detail, require_model=True)["passed"])

    def test_journal_context_and_prior_art_provenance(self):
        doi = "10.1234/paper"
        content = "The trial was limited to laboratory conditions."
        digest = hashlib.sha256(content.encode()).hexdigest()
        source = {"source_id": "paper", "doi": doi, "provider": "europepmc", "coverage": "fulltext",
                  "source_url": "https://www.ebi.ac.uk/europepmc/webservices/rest/PMC123/fullTextXML",
                  "lookup_url": "https://www.ebi.ac.uk/europepmc/webservices/rest/search?query=DOI%3A%22" + doi + "%22",
                  "lookup_sha256": digest, "text": content, "text_sha256": digest, "raw_sha256": digest,
                  "body_sha256": digest, "parser_revision": "jats-card-blocks-v1",
                  "observed_at": "2026-10-17T00:00:00Z", "ranges": [{"start_rune": 0, "end_rune": len(content)}]}
        prior = {"status": "candidates", "query": "Earlier Method", "before": "2024-01-01",
                 "note": "Совпадение названия требует проверки механизма.", "total": 1,
                 "source_url": "https://api.openalex.org/works?filter=earlier", "source_sha256": digest,
                 "observed_at": "2026-10-17T00:00:00Z", "works": [
                     {"id": "https://openalex.org/W123", "title": "Earlier Method", "date": "2010-01-01",
                      "doi": "10.1234/earlier", "url": "https://doi.org/10.1234/earlier"}]}
        card = {"source_contexts": [source], "prior_art": prior}

        def problems(candidate):
            failures = []
            RADAR.validate_card_contexts(candidate, {"paper"}, {"paper": {"canonical_id": doi}},
                                         {"paper": {"doi": doi}}, "journal",
                                         lambda valid, message: None if valid else failures.append(message))
            return failures

        self.assertEqual(problems(card), [])
        for mutate in (
            lambda c: c["source_contexts"][0].update(doi="10.1234/other"),
            lambda c: c["source_contexts"][0].update(lookup_sha256=""),
            lambda c: c["source_contexts"][0].update(source_url="https://example.org/fullTextXML"),
            lambda c: c["prior_art"]["works"][0].update(date="2025-01-01"),
            lambda c: c["prior_art"]["works"][0].update(title="Unrelated historical article"),
            lambda c: c["prior_art"].update(status="not_found"),
            lambda c: c["prior_art"]["works"][0].update(url="javascript:alert(1)"),
        ):
            broken = copy.deepcopy(card)
            mutate(broken)
            self.assertTrue(problems(broken))
        source.update(provider="unpaywall", source_url="https://publisher.example/article",
                      lookup_url="https://api.unpaywall.org/v2/10.1234%2Fpaper", parser_revision="open-html-card-blocks-v1")
        self.assertEqual(problems(card), [])
        source["lookup_url"] += "?email=private@example.org"
        self.assertTrue(problems(card))
        source.update(coverage="abstract", source_url="https://doi.org/" + doi, limitation="Полный текст недоступен")
        card["prior_art"] = {"status": "unavailable", "note": "Источник недоступен, отсутствие аналогов не установлено."}
        self.assertEqual(problems(card), [])

    def test_contract_and_offline_cli(self):
        valid = fixture()
        # Стадия из проверенного события хранит своё основание вне цитаты карточки.
        valid["candidates"][0]["adoption"] = {
            "code": "pilot", "rank": 3, "basis": "Источник подтверждает ограниченный пилот у заказчика",
            "evidence": [{"field": "adoption", "source_id": "0", "quote": valid["papers"][0]["title"],
                          "verified": True}],
        }
        checked = RADAR.validate(valid)
        self.assertTrue(checked["passed"], checked["problems"])
        self.assertFalse(checked["semantic_accuracy_checked"])
        missing_card_reason = copy.deepcopy(valid)
        missing_card_reason["candidates"][0]["card"]["evidence"][0].pop("verification_reason")
        self.assertFalse(RADAR.validate(missing_card_reason)["passed"])
        self.assertFalse(RADAR.validate(valid, require_model=True)["passed"])
        self.assertTrue(RADAR.validate(scored_fixture(), require_model=True)["passed"])

        mutations = {
            "old_contract": lambda d: d.update(search_contract="search-v17"),
            "wrong_denominator": lambda d: d["candidates"][0]["confidence"].update(total=6),
            "counter_mismatch": lambda d: d["counters"].update(selected=16),
            "missing_id": lambda d: d["candidates"][0].pop("id"),
            "empty_id": lambda d: d["candidates"][0].update(id=" "),
            "duplicate_id": lambda d: d["candidates"][1].update(id=d["candidates"][0]["id"]),
            "duplicate_name": lambda d: d["candidates"][1].update(label=d["candidates"][0]["label"]),
            "punctuation_alias": lambda d: d["candidates"][1].update(label=d["candidates"][0]["label"] + "!"),
            "typographic_alias": lambda d: d["candidates"][1].update(
                label=d["candidates"][0]["label"].upper().replace("Е", "Ё").replace(" ", " — ")),
            "card_marked_missing": lambda d: d["candidates"][0]["card"].update(missing=["кейс не подтверждён"]),
            "missing_advantage": lambda d: d["candidates"][0]["card"].update(advantage="", missing=["advantage"]),
            "missing_example": lambda d: d["candidates"][0]["card"].update(example="", missing=["example"]),
            "report_without_source": lambda d: d["candidates"][0]["card"].update(report_assessment="Оценка аналитического отчёта."),
            "ungrounded_claim": lambda d: d["candidates"][0]["card"]["evidence"][0].update(verified=False),
            "missing_verification_reason": lambda d: d["candidates"][0]["card"]["evidence"][0].pop("verification_reason"),
            "empty_verification_reason": lambda d: d["candidates"][0]["card"]["evidence"][1].update(verification_reason=" "),
            "non_russian_verification_reason": lambda d: d["candidates"][0]["card"]["evidence"][2].update(
                verification_reason="The source supports the claim"),
            "wrong_source": lambda d: d["candidates"][0]["card"]["evidence"][1].update(source_id="elsewhere"),
            "missing_translation": lambda d: d["passports"]["2"].pop("digest"),
            "unqualified": lambda d: d["candidates"][0]["qualification"].update(coherent=False),
            "old_qualification": lambda d: d["candidates"][0]["qualification"].update(method="grounded-entity-review-v3"),
            "missing_mechanism_proof": lambda d: d["candidates"][0]["qualification"]["evidence"][0].pop("mechanism_quote"),
            "missing_relevance_proof": lambda d: d["candidates"][0]["qualification"]["evidence"][0].pop("relevance_quote"),
            "missing_relevance_reason": lambda d: d["candidates"][0]["qualification"]["evidence"][0].pop("relevance_reason"),
            "context_is_not_evidence": lambda d: d["candidates"][0]["qualification"]["evidence"][0].update(role="context"),
            "different_mechanism": lambda d: d["candidates"][0]["qualification"]["evidence"][1].update(same_technology=False),
            "mature": lambda d: d["candidates"][0]["market"].update(status="mature", known=True, mature=True),
            "analysis_failed": lambda d: d.update(topic={"limits": [{"code": "analysis_unavailable"}]}),
        }
        for allow_shortfall in (False, True):
            for name, mutate in mutations.items():
                with self.subTest(name=name, allow_shortfall=allow_shortfall):
                    broken = fixture(7 if allow_shortfall else 15)
                    mutate(broken)
                    self.assertFalse(RADAR.validate(broken, allow_shortfall=allow_shortfall)["passed"])

        versions = fixture()
        versions["candidates"][0]["label"] = "Метод Qwen2.5"
        versions["candidates"][1]["label"] = "Метод Qwen3"
        self.assertTrue(RADAR.validate(versions)["passed"])

        negative = {"method": RADAR.METHOD, "search_contract": RADAR.SEARCH_CONTRACT,
                    "scope": fixture(0)["scope"],
                    "topic": {"limits": [{"code": "no_candidates"}]},
                    "counters": {"selected": 0, "rejected": 0, "candidates": 0,
                                 "above_threshold": 0, "model_scored": 0, "model_above_threshold": 0}}
        self.assertTrue(RADAR.validate(negative, expect_empty=True)["passed"])
        self.assertFalse(RADAR.validate(negative)["passed"])
        with mock.patch.object(RADAR, "call", return_value={"status": "running"}), \
                mock.patch.object(RADAR, "POLL_SECONDS", 0), contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(RADAR.SearchInterrupted) as interrupted:
                RADAR.search("http://example.test", "unused-token", "запрос", timeout=0.001)
            self.assertEqual(interrupted.exception.kind, "TimeoutError")
        with tempfile.TemporaryDirectory() as directory:
            source, output = Path(directory) / "detail.json", Path(directory) / "result.json"
            source.write_text(json.dumps(valid))
            run = subprocess.run([sys.executable, str(SCRIPT), "--detail-file", str(source), "--out", str(output)],
                                 capture_output=True, text=True, timeout=10, check=False)
            self.assertEqual(run.returncode, 0, run.stdout + run.stderr)
            envelope = json.loads(output.read_text())
            self.assertEqual(envelope["response"], valid)
            rerun = subprocess.run([sys.executable, str(SCRIPT), "--detail-file", str(output), "--require-model"],
                                   capture_output=True, text=True, timeout=10, check=False)
            self.assertEqual(rerun.returncode, 1, rerun.stdout + rerun.stderr)

    def test_recovered_classifier_failure_requires_complete_current_scores(self):
        for location in ("detail", "topic"):
            with self.subTest(location=location):
                recovered = scored_fixture()
                target = recovered if location == "detail" else recovered["topic"]
                target["limits"] = [{"code": "classifier_unavailable", "message": "Вторая волна не оценена"}]
                checked = RADAR.validate(recovered, require_model=True)
                self.assertTrue(checked["top15_passed"], checked["problems"])
                self.assertEqual(checked["model_decisions"], 15)
                self.assertTrue(any("В ходе отбора был отказ модели" in warning for warning in checked["warnings"]))
                for defect in ("missing_score", "old_version", "missing_features", "not_connected"):
                    with self.subTest(defect=defect):
                        broken = copy.deepcopy(recovered)
                        candidate = broken["candidates"][0]
                        if defect == "missing_score":
                            candidate.pop("model_score")
                            broken["counters"].update(model_scored=14, model_above_threshold=14)
                        elif defect == "old_version":
                            candidate["model_score"]["version"] = "old-version"
                        elif defect == "missing_features":
                            candidate["model_score"]["features"] = []
                        else:
                            broken["limits"] = [{"code": "classifier_not_connected"}]
                        self.assertFalse(RADAR.validate(broken, require_model=True)["passed"])

        for label, gap in (("insufficient_data", ""), ("insufficient_data", "not_collected"),
                           ("insufficient_data", "not_found"), ("insufficient_data", "too_early"),
                           ("weak_signal", "not_collected"), ("undecided", "")):
            for count in (1, 15):
                with self.subTest(label=label, gap=gap, count=count):
                    abstained = scored_fixture()
                    for candidate in abstained["candidates"][:count]:
                        score = candidate["model_score"]
                        score.update(label=label, data_gap=gap, basis="Мало данных: модель не вынесла решения")
                        score["features"][0]["value"] = "не измерено"
                    if label in ("insufficient_data", "undecided"):
                        abstained["counters"]["model_above_threshold"] -= count
                    strict = RADAR.validate(abstained, require_model=True)
                    self.assertFalse(strict["top15_passed"])
                    self.assertEqual(len(strict["problems"]), count)
                    self.assertEqual(strict["model_decisions"], 15-count)
                    diagnostic = RADAR.validate(abstained)
                    self.assertTrue(diagnostic["passed"], diagnostic["problems"])
                    self.assertEqual(diagnostic["model_decisions"], 15-count)
                    self.assertEqual(sum("модель не вынесла решения" in warning for warning in diagnostic["warnings"]), count)
                    output = io.StringIO()
                    with contextlib.redirect_stdout(output):
                        RADAR.report(abstained, None, diagnostic)
                    self.assertIn(f"Ответов классификатора: 15; решений по данным: {15-count};", output.getvalue())
                    self.assertNotIn("Оценено моделью", output.getvalue())

    def test_top15_and_explicit_control_modes(self):
        cases = (
            (7, (), False, "top15"),
            (15, (), True, "top15"),
            (16, (), False, "top15"),
            (7, ("--allow-shortfall",), True, "shortfall_diagnostic"),
            (15, ("--allow-shortfall",), True, "shortfall_diagnostic"),
            (16, ("--allow-shortfall",), False, "shortfall_diagnostic"),
            (0, (), False, "top15"),
            (0, ("--allow-shortfall",), False, "shortfall_diagnostic"),
            (0, ("--expect-empty",), True, "empty_control"),
            (7, ("--expect-empty",), False, "empty_control"),
        )
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "detail.json"
            for index, (count, flags, passed, mode) in enumerate(cases):
                with self.subTest(count=count, flags=flags):
                    detail = fixture(count)
                    if count == 0:
                        detail["topic"]["limits"] = [{"code": "no_candidates"}]
                    checked = RADAR.validate(detail, expect_empty="--expect-empty" in flags,
                                             allow_shortfall="--allow-shortfall" in flags)
                    self.assertEqual(checked["passed"], passed, checked["problems"])
                    self.assertEqual(checked["acceptance_mode"], mode)
                    self.assertEqual(checked["top15_passed"], passed and mode == "top15")
                    source.write_text(json.dumps(detail))
                    output = Path(directory) / f"result-{index}.json"
                    run = subprocess.run([sys.executable, str(SCRIPT), "--detail-file", str(source),
                                          "--out", str(output), *flags], capture_output=True,
                                         text=True, timeout=10, check=False)
                    self.assertEqual(run.returncode, 0 if passed else 1, run.stdout + run.stderr)
                    self.assertEqual(json.loads(output.read_text())["validation"], checked)
                    if mode != "top15":
                        self.assertIn("TOP-15: НЕ ПРИНИМАЛСЯ", run.stdout)
                        self.assertNotIn("TOP-15: ПРОЙДЕН", run.stdout)
                    else:
                        self.assertIn("TOP-15: " + ("ПРОЙДЕН" if passed else "НЕ ПРОЙДЕН"), run.stdout)

            run = subprocess.run([sys.executable, str(SCRIPT), "--expect-empty", "--allow-shortfall"],
                                 capture_output=True, text=True, timeout=10, check=False)
            self.assertEqual(run.returncode, 2)
            self.assertIn("not allowed with", run.stderr)
            with self.assertRaisesRegex(ValueError, "несовместимы"):
                RADAR.validate(fixture(), expect_empty=True, allow_shortfall=True)
            run = subprocess.run([sys.executable, str(SCRIPT), "--fresh", "--detail-file", str(source)],
                                 capture_output=True, text=True, timeout=10, check=False)
            self.assertEqual(run.returncode, 2)
            self.assertIn("--fresh нельзя использовать с --detail-file", run.stderr)

    def test_marked_top_up_passes_strict_only_below_the_bar(self):
        detail = scored_fixture()
        for candidate in detail["candidates"][8:]:
            candidate["shortfall"] = {"code": "incomplete_card", "reason": "Кейс не подтверждён цитатой"}
            candidate.pop("qualification")
            candidate["card"].update(evidence=[], example="", missing=["кейс"])
            candidate["model_score"].update(label="noise", value=0.2)
        detail["counters"].update(top_up=7, model_above_threshold=8)
        checked = RADAR.validate(detail, require_model=True)
        self.assertTrue(checked["top15_passed"], checked["problems"])
        self.assertEqual((checked["verified"], checked["top_up"], checked["verified_share"]), (8, 7, 0.533))
        undecided = copy.deepcopy(detail)
        undecided["candidates"][-1]["model_score"].update(label="undecided", value=0.9)
        diagnostic = RADAR.validate(undecided)
        self.assertTrue(diagnostic["passed"], diagnostic["problems"])
        self.assertEqual(diagnostic["model_decisions"], 14)
        strict = RADAR.validate(undecided, require_model=True)
        self.assertFalse(strict["top15_passed"])
        self.assertEqual(len(strict["problems"]), 1)
        self.assertIn("воздержание", strict["problems"][0])
        for defect in ("mature", "hype", "outside_list", "mature_label", "no_summary", "no_passport", "unmarked",
                       "single_source_verdict"):
            with self.subTest(defect=defect):
                broken = copy.deepcopy(detail)
                reserve = broken["candidates"][-1]
                if defect in ("mature", "hype"):
                    reserve["shortfall"]["code"] = defect
                elif defect == "outside_list":
                    reserve["shortfall"]["code"] = "unqualified"
                elif defect == "mature_label":
                    reserve["model_score"]["label"] = "mature"
                elif defect == "no_summary":
                    reserve["card"]["summary"] = ""
                elif defect == "no_passport":
                    broken["passports"]["0"].pop("genre")
                elif defect == "unmarked":
                    reserve.pop("shortfall")
                    broken["counters"]["top_up"] = 6
                else:
                    # Вторая статья той же группы не второе подтверждение: вердикт и счётчик его не засчитывают.
                    broken["candidates"][0]["groups"] = 1
                self.assertFalse(RADAR.validate(broken)["passed"])
        single = {"groups": 1, "papers_count": 1}
        single["web"] = {"records": [{"url": "https://www.reuters.com/a"}, {"url": "https://reuters.com/b"}]}
        self.assertEqual(RADAR.independent_sources(single), 2)
        self.assertFalse(RADAR.validate(fixture(7), allow_shortfall=True, require_model=True)["passed"])

    def test_single_direct_source_supports_all_required_fields(self):
        detail = fixture()
        detail["passports"]["0"].update(trust="limited", trust_basis="Один первичный источник, нужно независимое подтверждение")
        for candidate in detail["candidates"]:
            candidate.update(paper_ids=["0"], papers_count=1, documents_count=1)
            candidate["qualification"]["evidence"] = candidate["qualification"]["evidence"][:1]
            for evidence in candidate["card"]["evidence"]:
                evidence.update(source_id="0", quote=detail["papers"][0]["title"])
            candidate["card"]["example_id"] = "0"
        checked = RADAR.validate(detail)
        self.assertTrue(checked["passed"], checked["problems"])
        self.assertTrue(checked["top15_passed"])
        for candidate in detail["candidates"]:
            candidate["qualification"]["verified"] = False
        self.assertFalse(RADAR.validate(detail)["passed"])

    def test_fresh_refresh_is_admitted_once_and_requires_observed_collection(self):
        started = datetime(2026, 9, 27, 12, tzinfo=timezone.utc)
        completed = started + timedelta(seconds=20)
        detail = fixture()
        detail["observed_at"] = (started + timedelta(seconds=10)).isoformat()
        ready = {"status": "ready", "detail": {**detail, "digests_pending": False, "rescoring": False}}
        responses = [
            RADAR.urllib.error.HTTPError("http://example.test", 429, "busy", {}, None),
            io.StringIO(json.dumps({"status": "running", "elapsed_seconds": 0})),
            io.StringIO(json.dumps({"status": "ready", "detail": {**detail, "digests_pending": True}})),
            io.StringIO(json.dumps({"status": "ready", "detail": {**detail, "rescoring": True}})),
            io.StringIO(json.dumps(ready)),
        ]
        elapsed = 0

        def advance(seconds):
            nonlocal elapsed
            elapsed += seconds

        with mock.patch.object(RADAR.urllib.request, "urlopen", side_effect=responses) as http, \
                mock.patch.object(RADAR.datetime, "datetime", wraps=datetime) as clock, \
                mock.patch.object(RADAR.time, "monotonic", side_effect=lambda: elapsed), \
                mock.patch.object(RADAR.time, "sleep", side_effect=advance), contextlib.redirect_stderr(io.StringIO()):
            clock.now.side_effect = [started, completed]
            response, timing = RADAR.search("http://example.test", "unused-token", "технологии", 60, fresh=True)
        params = [RADAR.urllib.parse.parse_qs(RADAR.urllib.parse.urlsplit(call.args[0].full_url).query)
                  for call in http.call_args_list]
        self.assertEqual(params, [{"q": ["технологии"], "refresh": ["1"]},
                                  {"q": ["технологии"], "refresh": ["1"]},
                                  {"q": ["технологии"]}, {"q": ["технологии"]}, {"q": ["технологии"]}])
        self.assertEqual(response, ready)
        self.assertTrue(timing["refresh_requested"])
        self.assertTrue(timing["saw_running"])
        self.assertEqual(timing["polls"], 5)
        self.assertEqual(timing["seconds"], 20)
        self.assertTrue(timing["fresh_collection_proven"])

        cases = (
            (False, {"elapsed_seconds": 0}, detail["observed_at"], False),
            (True, None, detail["observed_at"], False),
            (True, {"elapsed_seconds": 5}, detail["observed_at"], True),
            (True, {"elapsed_seconds": 6}, detail["observed_at"], False),
            (True, {"elapsed_seconds": 0, "reopening": True}, detail["observed_at"], False),
            (True, {}, detail["observed_at"], False),
            (True, {"elapsed_seconds": 0}, (started - timedelta(seconds=1)).isoformat(), False),
            (True, {"elapsed_seconds": 0}, None, False),
            (True, {"elapsed_seconds": 0}, "not-a-time", False),
            (True, {"elapsed_seconds": 0}, (completed + timedelta(seconds=1)).isoformat(), False),
        )
        for fresh, running, observed_at, proven in cases:
            with self.subTest(fresh=fresh, running=running, observed_at=observed_at):
                states = [{"status": "running", **running}] if running is not None else []
                states.append({"status": "ready", "detail": {**detail, "observed_at": observed_at}})
                with mock.patch.object(RADAR, "call", side_effect=states) as calls, \
                        mock.patch.object(RADAR.datetime, "datetime", wraps=datetime) as clock, \
                        mock.patch.object(RADAR.time, "sleep"), contextlib.redirect_stderr(io.StringIO()):
                    clock.now.side_effect = [started, completed]
                    _, checked = RADAR.search("http://example.test", "unused-token", "технологии", 60, fresh=fresh)
                self.assertEqual(checked["refresh_requested"], fresh)
                self.assertEqual(checked["fresh_collection_proven"], proven)
                if not fresh:
                    self.assertTrue(all("refresh=" not in call.args[1] for call in calls.call_args_list))

        for proven in (False, True):
            console = io.StringIO()
            with mock.patch.object(RADAR, "load_stand_env", return_value={"STAND_URL": "http://example.test", "STAND_TOKEN": "unused-token"}), \
                    mock.patch.object(sys, "argv", ["radar", "--fresh", "технологии"]), \
                    mock.patch.object(RADAR, "search", return_value=(ready, {**timing, "fresh_collection_proven": proven})) as search, \
                    contextlib.redirect_stdout(console):
                self.assertEqual(RADAR.main(), 0 if proven else 1)
            search.assert_called_once_with("http://example.test", "unused-token", "технологии", 900, fresh=True)
            self.assertIn("TOP-15: " + ("ПРОЙДЕН" if proven else "НЕ ПРОЙДЕН"), console.getvalue())

    def test_interrupted_ready_is_saved_separately_and_cannot_pass_offline(self):
        started = datetime(2026, 9, 27, 12, tzinfo=timezone.utc)
        detail = scored_fixture()
        detail["observed_at"] = (started + timedelta(seconds=10)).isoformat()
        first = {"status": "ready", "detail": {**detail, "digests_pending": True}}
        latest = {"status": "ready", "detail": {**detail, "rescoring": True}}
        env = {"STAND_URL": "http://example.test", "STAND_TOKEN": "private-test-token"}
        for ending in ("timeout", "http_error", "invalid_ready", "no_ready", "complete"):
            with self.subTest(ending=ending), tempfile.TemporaryDirectory() as directory:
                output = Path(directory) / "result.json"
                states = [{"status": "running", "elapsed_seconds": 0}, first, latest]
                states.append({"status": "ready", "detail": detail} if ending == "complete"
                              else {"status": "running", "elapsed_seconds": 0})
                if ending == "invalid_ready":
                    states[-1] = {"status": "ready"}
                responses = [io.StringIO(json.dumps(state)) for state in states]
                if ending == "http_error":
                    responses[-1] = RADAR.urllib.error.HTTPError("http://example.test/private-test-token", 503,
                                                                 "private-test-token", {}, None)
                elif ending == "no_ready":
                    responses = [RADAR.urllib.error.HTTPError("http://example.test/private-test-token", 503,
                                                              "private-test-token", {}, None)]
                elapsed = 0

                def advance(seconds):
                    nonlocal elapsed
                    elapsed += seconds

                with mock.patch.object(RADAR, "load_stand_env", return_value=env), \
                        mock.patch.object(sys, "argv", ["radar", "технологии", "--fresh", "--require-model",
                                                        "--timeout", "20", "--out", str(output)]), \
                        mock.patch.object(RADAR.urllib.request, "urlopen", side_effect=responses) as http, \
                        mock.patch.object(RADAR.datetime, "datetime", wraps=datetime) as clock, \
                        mock.patch.object(RADAR.time, "monotonic", side_effect=lambda: elapsed), \
                        mock.patch.object(RADAR.time, "sleep", side_effect=advance), \
                        contextlib.redirect_stdout(io.StringIO()) as console, \
                        contextlib.redirect_stderr(io.StringIO()) as errors:
                    clock.now.side_effect = [started, started + timedelta(seconds=20)]
                    self.assertEqual(RADAR.main(), 0 if ending == "complete" else 2)
                self.assertTrue(output.exists(), "ошибка ожидания потеряла наблюдённые доказательства")
                envelope = json.loads(output.read_text())
                complete = ending == "complete"
                self.assertEqual(envelope["final_response_received"], complete)
                self.assertEqual(envelope["validation"]["passed"], complete)
                self.assertEqual(envelope["validation"]["top15_passed"], complete)
                self.assertEqual(envelope["response"], states[-1] if complete else None)
                self.assertEqual(envelope["intermediate_response"],
                                 latest if ending in ("timeout", "http_error", "invalid_ready") else None)
                self.assertEqual(envelope["timing"]["seconds"], elapsed)
                self.assertNotIn("private-test-token", output.read_text() + console.getvalue() + errors.getvalue())
                params = [RADAR.urllib.parse.parse_qs(RADAR.urllib.parse.urlsplit(call.args[0].full_url).query)
                          for call in http.call_args_list]
                self.assertEqual(sum("refresh" in value for value in params), 1)
                if ending != "complete":
                    self.assertFalse(envelope["timing"]["fresh_collection_proven"])
                    self.assertNotIn("TOP-15: ПРОЙДЕН", console.getvalue())
                    replay = Path(directory) / "replay.json"
                    with mock.patch.object(sys, "argv", ["radar", "--detail-file", str(output), "--out", str(replay)]), \
                            contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                        self.assertNotEqual(RADAR.main(), 0)
                    repeated = json.loads(replay.read_text())
                    self.assertEqual(repeated["intermediate_response"], envelope["intermediate_response"])
                    self.assertEqual(repeated["validation"]["status"], envelope["validation"]["status"])
        self.assertFalse(RADAR.validate(latest["detail"], require_model=True)["passed"])


if __name__ == "__main__":
    unittest.main()
