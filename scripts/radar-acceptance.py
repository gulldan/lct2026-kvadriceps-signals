#!/usr/bin/env python3
"""Ручная приёмка TOP-15 и контракта search-v32, не оценка точности модели.

STAND_URL и STAND_USER задают стенд; STAND_PASSWORD или STAND_TOKEN — вход.
Чувствительные значения читаются только из окружения.
python3 scripts/radar-acceptance.py --fresh --out result.json "новый запрос"
python3 scripts/radar-acceptance.py --detail-file result.json --require-model

По умолчанию нужны ровно 15 технологий; добор ниже планки (shortfall) принимается
только с причиной из белого списка и считается отдельно от проверенных.
--allow-shortfall допускает 1–15 только для диагностики; --expect-empty проверяет
отдельный пустой контроль.
--fresh запрашивает пересборку и требует доказательства нового сбора.
Код 0: выбранная проверка выполнена; только обычный режим принимает TOP-15.
Код 1: контракт или заданное ожидание не выполнены. Код 2: неверные аргументы
или ответ не получен/не прочитан; --out сохраняет последний промежуточный снимок,
если он был получен. Предметная проверка цитат и технологии обязательна.
"""

import argparse
import collections
import datetime
import hashlib
import json
import math
import re
import sys
import time
import unicodedata
import urllib.error
import urllib.parse
import urllib.request
from functools import partial
from pathlib import Path

from tools.stand_env import load_stand_env, redact, stand_origin

TIMEOUT = 60
POLL_SECONDS = 5
METHOD = "live-signals-v8"
SEARCH_CONTRACT = "search-v32"
RULE_METHOD = "evidence-coverage-rules-v4"
RULE_TOTAL = 8
TOP_COUNT = 15
FIELDS = ("summary", "advantage", "example")
DIRECT_ROLES = ("introduces", "implements", "evaluates", "deployment")
# Паспорт источника по ТЗ: без типа, жанра, языка и доверия с основанием карточка не показывается.
PASSPORT_FIELDS = ("language", "language_basis", "publisher_type", "genre", "trust", "trust_basis")
# Добор до пятнадцати берёт только непройденные проверки; зрелость, повтор и чужая тема исключают кандидата.
TOP_UP_CODES = ("below_top", "classifier_rejected", "classifier_unavailable", "incomplete_card", "thin_evidence",
                "one_group", "unassessed", "qualification_budget", "qualification_unavailable")
FORBIDDEN_TOP_UP = ("mature", "market_formed", "long_history", "well_known", "not_technology", "off_topic",
                    "duplicate", "incoherent_evidence", "hype")
# Метод одной статьи — наблюдение: вердикт «слабый сигнал» требует двух независимых подтверждений.
MIN_INDEPENDENT = 2
RECORD_CHANNELS = ("patents", "patents_cn", "registry", "web")


def call(base, path, token=None, payload=None, timeout=TIMEOUT):
    data = json.dumps(payload).encode() if payload is not None else None
    request = urllib.request.Request(base.rstrip("/") + path, data=data, method="POST" if data else "GET")
    request.add_header("Accept", "application/json")
    if data:
        request.add_header("Content-Type", "application/json")
    if token:
        request.add_header("Authorization", "Bearer " + token)
    with urllib.request.urlopen(request, timeout=timeout) as response:
        return json.load(response)


def login(base, user, password):
    answer = call(base, "/api/v1/auth/login", payload={"username": user, "password": password})
    token = answer.get("access_token")
    if not token:
        raise ValueError("вход не удался: нет access_token")
    return token


class SearchInterrupted(OSError):
    """Последний готовый снимок остаётся доказательством, даже если ожидание оборвалось."""

    def __init__(self, failure, response, timing):
        super().__init__("поиск не завершён")
        self.kind = type(failure).__name__
        self.response = response
        self.timing = timing


def search(base, token, query, timeout, *, fresh=False):
    started, observed = time.monotonic(), datetime.datetime.now(datetime.timezone.utc)
    polls, saw_running = 0, False
    refresh_pending, fresh_start = fresh, False
    first_running_elapsed = last_status = last_ready = None

    def timing(final):
        completed = datetime.datetime.now(datetime.timezone.utc)
        detail_observed = ((last_ready or {}).get("detail") or {}).get("observed_at")
        try:
            collected = datetime.datetime.fromisoformat(detail_observed)
            recent = collected.tzinfo is not None and observed <= collected <= completed
        except (ValueError, TypeError):
            recent = False
        return {"started_at": observed.isoformat(), "completed_at": completed.isoformat(),
                "seconds": round(time.monotonic() - started, 3), "polls": polls,
                "saw_running": saw_running, "last_status": last_status,
                "refresh_requested": fresh, "first_running_elapsed_seconds": first_running_elapsed,
                "observed_at": detail_observed,
                "fresh_collection_proven": final and fresh and saw_running and fresh_start and recent}

    try:
        while True:
            remaining = timeout - (time.monotonic() - started)
            if remaining <= 0:
                raise TimeoutError("истёк общий срок ожидания; состояние сервера автоматически не менялось")
            params = {"q": query}
            if refresh_pending:
                params["refresh"] = "1"
            polls += 1
            try:
                state = call(base, "/api/v1/trends/live/search?" + urllib.parse.urlencode(params),
                             token, timeout=min(TIMEOUT, remaining))
            except urllib.error.HTTPError as error:
                error.close()
                if error.code != 429:
                    raise
                state = {"status": "busy"}
            last_status = status = state.get("status")
            if status in ("running", "queued", "ready"):
                refresh_pending = False
            detail = state.get("detail") or {}
            pending = detail.get("digests_pending") or detail.get("rescoring")
            if status == "ready":
                if not detail:
                    raise ValueError("готовый ответ не содержит detail")
                last_ready = state
                if not pending:
                    return state, timing(final=True)
            if status == "failed":
                raise ValueError("поиск завершился отказом сервера; причина доступна в интерфейсе")
            if status not in ("running", "busy", "queued", "ready"):
                raise ValueError("неизвестный статус поиска")
            if status == "running" and not saw_running:
                first_running_elapsed = state.get("elapsed_seconds")
                # Старый запуск или пересчёт сохранённого ответа не доказывает новый сбор.
                fresh_start = (number(first_running_elapsed) and 0 <= first_running_elapsed <= POLL_SECONDS
                               and not state.get("reopening"))
            saw_running = saw_running or status == "running"
            progress = "ready, ожидание пересказов или модельной оценки" if status == "ready" else status
            print(f"  … {progress}, {int(time.monotonic() - started)} с", file=sys.stderr)
            time.sleep(max(0, min(POLL_SECONDS, timeout - (time.monotonic() - started))))
    except (OSError, ValueError, KeyError, TypeError, AttributeError) as failure:
        raise SearchInterrupted(failure, last_ready, timing(final=False)) from failure


def text(value):
    return isinstance(value, str) and bool(value.strip())


def russian(value):
    return text(value) and re.search("[А-Яа-яЁё]", value) is not None


def number(value):
    return type(value) in (int, float) and math.isfinite(value)


def model_decision(score):
    return score.get("label") in ("weak_signal", "mature", "noise", "failed") and not score.get("data_gap")


def normalized(value):
    return " ".join(value.split())


def validate_limitation_parts(claim, evidence, prefix, check):
    parts = evidence.get("parts")
    check(isinstance(parts, list) and bool(parts), f"{prefix}: ограничение не разобрано на проверенные части")
    if not isinstance(parts, list):
        return
    pieces = []
    quote = normalized(evidence.get("quote") or "")
    for part in parts:
        if not isinstance(part, dict):
            check(False, f"{prefix}: неверный формат части ограничения")
            continue
        piece, fact = part.get("claim"), part.get("source_fact")
        check(text(piece), f"{prefix}: пустая часть ограничения")
        pieces.append(piece if isinstance(piece, str) else "")
        check(all(part.get(key) is True for key in ("direct", "scope_preserved", "modality_preserved")),
              f"{prefix}: часть ограничения не подтверждена с сохранением области и модальности")
        check(text(fact) and "…" not in fact and "..." not in fact and normalized(fact) in quote,
              f"{prefix}: часть ограничения не имеет непрерывной дословной опоры")
    check(limitation_parts_cover(claim, pieces),
          f"{prefix}: проверка не покрывает всё ограничение")


def limitation_parts_cover(claim, pieces):
    claim, at = normalized(claim), 0

    def separator(start, end, conjunction):
        gap = claim[start:end]
        word = gap.strip(" .,;:!?()[]{}\"'«»“”‘’")
        if start and end < len(claim) and claim[start - 1].isdigit() and claim[end].isdigit():
            return False
        if not word:
            return True
        if not conjunction or word not in ("а", "и"):
            return False
        word_at = start + gap.index(word)
        return (not word_at or not claim[word_at - 1].isalnum()) and (
            word_at + len(word) == len(claim) or not claim[word_at + len(word)].isalnum())

    for index, piece in enumerate(pieces):
        piece = normalized(piece)
        found = claim.find(piece, at)
        if not piece or found < 0 or not separator(at, found, index > 0):
            return False
        at = found + len(piece)
    return bool(pieces) and separator(at, len(claim), False)


def publisher_host(address):
    raw = address.strip() if isinstance(address, str) else ""
    if not raw:
        return ""
    try:
        host = urllib.parse.urlsplit(raw if "//" in raw else "https://" + raw).hostname or ""
    except ValueError:
        return ""
    return host.removeprefix("www.")


def independent_sources(candidate):
    """Повторяет liveIndependentSources: все статьи одной авторской группы — один источник, записи — по издателям."""
    publishers = set()
    for channel in RECORD_CHANNELS:
        for record in (candidate.get(channel) or {}).get("records") or []:
            publisher = publisher_host(record.get("url") or record.get("canonical_url")) or record.get("source") or ""
            if publisher:
                publishers.add(publisher)
    papers = candidate.get("papers_count") if number(candidate.get("papers_count")) else 0
    groups = candidate.get("groups") if number(candidate.get("groups")) else 0
    return max(groups, min(papers, 1) + len(publishers))


def candidate_origin(candidate):
    qualified = {item.get("source_id") for item in (candidate.get("qualification") or {}).get("evidence") or []}
    if str(candidate.get("id", "")).startswith("direction:"):
        return "direction"
    if any(str(source).startswith("rec:") for source in qualified) or (
            not candidate.get("paper_ids") and candidate.get("evidence_ids")):
        return "market"
    return "science"


def label_key(value):
    return " ".join(re.findall(r"[^\W_]+", value.lower().replace("ё", "е")))


def arxiv_url_id(address):
    if not text(address):
        return ""
    try:
        parsed = urllib.parse.urlsplit(address)
    except ValueError:
        return ""
    if parsed.scheme not in ("http", "https") or parsed.netloc != "arxiv.org" or parsed.query or parsed.fragment:
        return ""
    matched = re.fullmatch(r"/(?:abs|html|pdf)/([0-9]{4}\.[0-9]{4,5}(?:v[1-9][0-9]{0,3})?)", parsed.path)
    return matched[1] if matched else ""


def publisher_metadata(detail, papers, check):
    """Авторское описание сохраняется целиком; его хеш не доказывает истинность выводов."""
    metadata = dict(detail.get("paper_metadata") or {})
    revisions = detail.get("source_revisions") or {}
    check(isinstance(revisions, dict), "неверный словарь версий первоисточников")
    if not isinstance(revisions, dict):
        return metadata
    for source, entry in revisions.items():
        prefix = f"версия источника {source}"
        paper = papers.get(source) or {}
        check(bool(paper) and not paper.get("document_id") and not paper.get("snapshot_id"),
              f"{prefix}: источник отсутствует или относится к приватному снимку")
        check(isinstance(entry, dict), f"{prefix}: неверный паспорт версии")
        if not isinstance(entry, dict):
            continue
        identifiers = [entry.get(key) for key in ("requested_doi", "resolved_doi", "concept_doi")]
        valid_dois = all(isinstance(value, str) and re.fullmatch(r"10\.5281/zenodo\.[1-9][0-9]{0,17}", value)
                         for value in identifiers)
        check(valid_dois, f"{prefix}: неверные DOI первоисточника")
        if not valid_dois:
            continue
        requested, resolved, concept = identifiers
        check(requested in (resolved, concept) and paper.get("canonical_id") == requested
              and isinstance(source, str) and source.startswith("openalex:"),
              f"{prefix}: версия не связана с выбранным источником")
        resolved_id = resolved.removeprefix("10.5281/zenodo.")
        requested_id = requested.removeprefix("10.5281/zenodo.")
        address = "https://zenodo.org/records/" + resolved_id
        check(entry.get("source_url") == address and paper.get("url") == address,
              f"{prefix}: ссылка не обозначает точную версию")
        check(entry.get("api_url") in {"https://zenodo.org/api/records/" + value
                                       for value in (requested_id, resolved_id)},
              f"{prefix}: недопустимый адрес получения метаданных")
        check(entry.get("coverage") == "publisher_metadata", f"{prefix}: неверно заявлено покрытие")
        content = entry.get("text")
        check(text(content) and len(content) <= 4000, f"{prefix}: отсутствует целое авторское описание")
        if text(content):
            check(hashlib.sha256(content.encode()).hexdigest() == entry.get("text_sha256"),
                  f"{prefix}: хеш не совпадает с описанием")
        raw_hash = entry.get("raw_sha256")
        check(isinstance(raw_hash, str) and re.fullmatch(r"[0-9a-f]{64}", raw_hash),
              f"{prefix}: отсутствует хеш исходного ответа издателя")
        try:
            updated = datetime.datetime.fromisoformat(entry.get("source_updated_at"))
            fetched = datetime.datetime.fromisoformat(entry.get("fetched_at"))
            ordered = updated.tzinfo is not None and fetched.tzinfo is not None and updated <= fetched
            if detail.get("observed_at"):
                observed = datetime.datetime.fromisoformat(detail["observed_at"])
                # Время готового ответа сериализуется с точностью до секунды.
                ordered = ordered and observed.tzinfo is not None and fetched.replace(microsecond=0) <= observed
            check(ordered, f"{prefix}: даты версии и получения несогласованы")
        except (TypeError, ValueError):
            check(False, f"{prefix}: даты версии или получения отсутствуют")
        if text(content):
            metadata[source] = {**(metadata.get(source) or {}), "abstract": content}
    for source, paper in papers.items():
        if (isinstance(source, str) and source.startswith("openalex:")
                and str(paper.get("canonical_id", "")).startswith("10.5281/zenodo.")
                and not paper.get("document_id") and not paper.get("snapshot_id")):
            check(source in revisions, f"{source}: актуальная версия первоисточника не подтверждена")
    return metadata


def validate_card_contexts(card, own, papers, metadata, prefix, check):
    """Сверяем сохранённый вход модели; хеш не доказывает истинность утверждения."""
    validate_prior_art(card.get("prior_art"), prefix, check)
    contexts = {}
    entries = card.get("source_contexts") or []
    check(isinstance(entries, list), f"{prefix}: неверный список контекстов источников")
    if not isinstance(entries, list):
        return contexts
    for entry in entries:
        check(isinstance(entry, dict), f"{prefix}: неверный контекст источника")
        if not isinstance(entry, dict):
            continue
        source = entry.get("source_id")
        if not text(source):
            check(False, f"{prefix}: у контекста нет ID источника")
            continue
        check(source in own and source not in contexts, f"{prefix}: чужой или повторный контекст источника")
        contexts[source] = entry
        content = entry.get("text")
        check(text(content), f"{prefix}: пустой сохранённый контекст")
        if text(content):
            check(hashlib.sha256(content.encode()).hexdigest() == entry.get("text_sha256"),
                  f"{prefix}: хеш контекста не совпадает с текстом")
        coverage = entry.get("coverage")
        check(coverage in ("abstract", "fulltext", "excerpted_fulltext"), f"{prefix}: неизвестное покрытие источника")
        arxiv_id = entry.get("arxiv_id")
        source_url = entry.get("source_url")
        paper, meta = papers.get(source) or {}, metadata.get(source) or {}
        linked_id = arxiv_url_id(source_url)
        if entry.get("doi"):
            validate_journal_context(entry, paper, meta, prefix, check)
        else:
            references = [value for address in (paper.get("url"), meta.get("source_url"))
                          if (value := arxiv_url_id(address))]
            if source.startswith("arxiv:"):
                references.append(source.removeprefix("arxiv:"))
            check(bool(linked_id) and linked_id == arxiv_id and source_url.startswith("https://"),
                  f"{prefix}: ID и адрес контекста не обозначают одну статью и версию")
            check(bool(linked_id) and bool(references) and all(
                reference == linked_id if "v" in reference else reference == linked_id.split("v")[0]
                for reference in references), f"{prefix}: контекст относится к другой статье или закреплённой версии")
        if coverage != "fulltext":
            check(russian(entry.get("limitation")), f"{prefix}: ограниченное покрытие не объяснено")
        if coverage in ("fulltext", "excerpted_fulltext"):
            if not entry.get("doi"):
                check(bool(linked_id) and "v" in linked_id, f"{prefix}: полный текст не привязан к точной версии")
            check(all(isinstance(entry.get(key), str) and re.fullmatch(r"[0-9a-f]{64}", entry[key])
                      for key in ("raw_sha256", "body_sha256")), f"{prefix}: нет хешей исходника и нормализованного тела")
            check(text(entry.get("parser_revision")) and text(entry.get("observed_at")),
                  f"{prefix}: нет версии разбора или времени получения полного текста")
            ranges, end = entry.get("ranges"), 0
            check(isinstance(ranges, list) and bool(ranges), f"{prefix}: не сохранены диапазоны полного текста")
            for span in ranges if isinstance(ranges, list) else []:
                valid = (isinstance(span, dict) and type(span.get("start_rune")) is int
                         and type(span.get("end_rune")) is int and end <= span["start_rune"] < span["end_rune"])
                check(valid, f"{prefix}: неверный или пересекающийся диапазон полного текста")
                if valid:
                    end = span["end_rune"]
    return contexts


def normalized_doi(value):
    doi = str(value or "").strip().lower()
    for prefix in ("https://doi.org/", "http://doi.org/", "doi:"):
        doi = doi.removeprefix(prefix)
    return doi if re.fullmatch(r'10\.[0-9]{4,9}/[^\s"<>?#\\]+', doi) else ""


def validate_journal_context(entry, paper, meta, prefix, check):
    doi = normalized_doi(entry.get("doi"))
    references = [value for raw in (meta.get("doi"), paper.get("canonical_id"))
                  if (value := normalized_doi(raw))]
    check(bool(doi) and bool(references) and all(value == doi for value in references)
          and not paper.get("document_id") and not paper.get("snapshot_id") and not entry.get("arxiv_id"),
          f"{prefix}: DOI контекста не совпадает с публичной статьёй")
    source = urllib.parse.urlsplit(entry.get("source_url") or "")
    check(source.scheme == "https" and bool(source.hostname) and not source.username,
          f"{prefix}: неверный адрес текста статьи")
    if entry.get("coverage") == "abstract":
        check(entry.get("source_url") == "https://doi.org/" + doi,
              f"{prefix}: аннотация не привязана к DOI")
        return
    lookup = urllib.parse.urlsplit(entry.get("lookup_url") or "")
    provider = entry.get("provider")
    if provider == "europepmc":
        valid = (source.netloc == "www.ebi.ac.uk" and re.fullmatch(
            r"/europepmc/webservices/rest/PMC[1-9][0-9]*/fullTextXML", source.path)
            and lookup.netloc == source.netloc and lookup.path == "/europepmc/webservices/rest/search"
            and urllib.parse.parse_qs(lookup.query).get("query") == [f'DOI:"{doi}"']
            and entry.get("parser_revision") == "jats-card-blocks-v1")
    elif provider == "unpaywall":
        valid = (lookup.netloc == "api.unpaywall.org" and urllib.parse.unquote(lookup.path) == "/v2/" + doi
                 and not lookup.query and entry.get("parser_revision") == "open-html-card-blocks-v1")
    else:
        valid = False
    check(bool(valid) and lookup.scheme == "https" and not lookup.username,
          f"{prefix}: нет проверяемого происхождения открытой копии")
    check(bool(re.fullmatch(r"[0-9a-f]{64}", str(entry.get("lookup_sha256", "")))),
          f"{prefix}: не сохранён хеш ответа каталога открытых копий")


def validate_prior_art(result, prefix, check):
    if result is None:
        return
    check(isinstance(result, dict), f"{prefix}: неверный результат поиска ранних работ")
    if not isinstance(result, dict):
        return
    status, works = result.get("status"), result.get("works") or []
    check(status in ("candidates", "not_found", "unavailable") and russian(result.get("note")),
          f"{prefix}: неизвестный статус или нет ограничения поиска ранних работ")
    check(isinstance(works, list) and len(works) <= 3, f"{prefix}: неверный список ранних работ")
    if status not in ("candidates", "not_found"):
        return
    try:
        before = datetime.date.fromisoformat(result.get("before"))
        observed = datetime.datetime.fromisoformat(result.get("observed_at"))
        valid = observed.tzinfo is not None
    except (ValueError, TypeError):
        valid, before = False, None
    source = urllib.parse.urlsplit(result.get("source_url") or "")
    check(valid and text(result.get("query")) and source.scheme == "https"
          and source.netloc == "api.openalex.org" and source.path == "/works"
          and bool(re.fullmatch(r"[0-9a-f]{64}", str(result.get("source_sha256", "")))),
          f"{prefix}: нет основания или дат поиска ранних работ")
    total = result.get("total")
    check(type(total) is int and total >= len(works) and
          (bool(works) if status == "candidates" else total == 0 and not works),
          f"{prefix}: ранние работы не согласованы со статусом и счётчиком")
    seen = set()
    for work in works if isinstance(works, list) else []:
        if not isinstance(work, dict):
            check(False, f"{prefix}: неверная ранняя работа")
            continue
        try:
            date = datetime.date.fromisoformat(work.get("date"))
            earlier = before is not None and date < before
        except (ValueError, TypeError):
            earlier = False
        identifier = work.get("id")
        address = "https://doi.org/" + normalized_doi(work["doi"]) if work.get("doi") else identifier
        check(earlier and text(work.get("title")) and identifier not in seen
              and bool(re.fullmatch(r"https://openalex.org/W[1-9][0-9]+", str(identifier)))
              and work.get("url") == address, f"{prefix}: ранняя работа не подтверждена датой и ссылкой")
        phrase = label_key(result.get("query") or "")
        check(len(phrase.split()) >= 2 and " " + phrase + " " in " " + label_key(work.get("title") or "") + " ",
              f"{prefix}: исходное название технологии отсутствует в заголовке ранней работы")
        seen.add(str(identifier))


def validate_scope(scope, check, expected_query=None):
    check(isinstance(scope, dict), "нет зафиксированной области исходного запроса")
    if not isinstance(scope, dict):
        return False
    query = scope.get("query")
    valid_query = (text(query) and query == normalized(query.lower()) and len(query) <= 200
                   and query not in ("undefined", "null", "[object object]"))
    valid_mode = scope.get("mode") in ("any", "all")
    check(valid_query, "область запроса: нет нормализованного исходного запроса")
    check(valid_mode, "область запроса: режим должен быть any или all")
    if expected_query is not None:
        check(text(expected_query) and query == normalized(expected_query.lower()),
              "область запроса относится к другому исходному запросу")
    valid = valid_query and valid_mode
    for key, label, minimum, maximum, prefix in (("branches", "task", 1, 4, "b"),
                                                ("required", "condition", 0, 3, "c")):
        rows = scope.get(key)
        bounded = isinstance(rows, list) and minimum <= len(rows) <= maximum
        check(bounded, f"область запроса: неверный список {key}")
        valid = valid and bounded
        for index, row in enumerate(rows if isinstance(rows, list) else [], 1):
            value = row.get(label) if isinstance(row, dict) else None
            correct = (isinstance(row, dict) and row.get("id") == f"{prefix}{index}"
                       and text(value) and value == value.strip() and 2 <= len(value) <= 160
                       and not any(unicodedata.category(char) == "Cc" for char in value))
            check(correct, f"область запроса: неверный ID или текст {key} {index}")
            valid = valid and correct
    return valid


def validate_qualification_scope(qualification, scope, valid_scope, prefix, check):
    check(qualification.get("scope") == scope and valid_scope,
          f"{prefix}: область квалификации не совпадает с областью исходного запроса")
    if not valid_scope:
        return
    branches = {branch["id"] for branch in scope["branches"]}
    required = {condition["id"] for condition in scope["required"]}
    known, supported = branches | required, set()
    for item in qualification.get("evidence") or []:
        criteria = item.get("scope_criteria")
        valid = (isinstance(criteria, list) and bool(criteria) and all(isinstance(key, str) for key in criteria)
                 and len(criteria) == len(set(criteria)) and set(criteria) <= known)
        check(valid, f"{prefix}: критерии источника отсутствуют, неизвестны или повторяются")
        if valid and item.get("same_technology") is True and item.get("role") in DIRECT_ROLES:
            supported.update(criteria)
    matched = branches & supported
    check(bool(matched) and (scope["mode"] == "any" or matched == branches) and required <= supported,
          f"{prefix}: прямые источники не подтверждают ветви и обязательные условия области запроса")


def validate_canonical_name(qualification, own, papers, metadata, prefix, check, warnings):
    name = qualification.get("canonical_name", "")
    check(isinstance(name, str), f"{prefix}: исходное имя должно быть строкой")
    if not isinstance(name, str):
        return
    name = normalized(name)
    if not name:
        return
    bounded = 2 <= len(re.findall(r"[^\W_]+", name)) <= 12 and len(name) <= 160
    check(bounded, f"{prefix}: исходное имя не является ограниченной фразой из источника")
    if not bounded:
        return
    phrase = " " + label_key(name) + " "
    missing_original = False
    for item in qualification.get("evidence") or []:
        source = item.get("source_id")
        if source not in own or source not in papers:
            continue
        paper, meta = papers[source], metadata.get(source) or {}
        title, abstract = paper.get("title") or "", meta.get("abstract") or paper.get("abstract") or ""
        original = normalized(title + " (" + (paper.get("date") or "")[:4] + "): " + abstract)
        if name in original and phrase in " " + label_key(original) + " ":
            return
        missing_original = missing_original or not text(abstract)
    if missing_original:
        warnings.append(f"{prefix}: исходное имя не сверено — оригинальные аннотации не переданы полностью; "
                        "нужна ручная проверка первоисточника.")
    else:
        check(False, f"{prefix}: исходное имя отсутствует в собственных источниках квалификации")


def validate(detail, expect_empty=False, require_model=False, *, allow_shortfall=False, expected_query=None):
    if expect_empty and allow_shortfall:
        raise ValueError("--expect-empty и --allow-shortfall несовместимы")
    mode = "empty_control" if expect_empty else "shortfall_diagnostic" if allow_shortfall else "top15"
    problems, warnings = [], []
    unchecked_quotes = set()

    def check(condition, message):
        if not condition:
            problems.append(message)

    candidates, rejected = detail.get("candidates") or [], detail.get("rejected") or []
    check(not (detail.get("digests_pending") or detail.get("rescoring")),
          "ответ промежуточный: пересказы или модельная оценка ещё не завершены")
    counters, passports = detail.get("counters") or {}, detail.get("passports") or {}
    papers = {paper.get("id"): paper for paper in detail.get("papers") or []}
    metadata = publisher_metadata(detail, papers, check)
    check(detail.get("method") == METHOD, f"метод ответа должен быть {METHOD}, получен {detail.get('method')!r}")
    check(detail.get("search_contract") == SEARCH_CONTRACT,
          f"контракт ответа должен быть {SEARCH_CONTRACT}, получен {detail.get('search_contract')!r}")
    scope = detail.get("scope")
    valid_scope = validate_scope(scope, check, expected_query)
    limits = ((detail.get("topic") or {}).get("limits") or [])
    unavailable = sorted({item.get("code") for item in limits
                          if item.get("code") in ("analysis_unavailable", "relevance_unmeasured")})
    check(not unavailable, "не выполнен семантический этап: " + ", ".join(unavailable))
    if expect_empty:
        check(not candidates, "ожидалась пустая выдача")
    elif allow_shortfall:
        check(1 <= len(candidates) <= TOP_COUNT,
              f"для диагностики допустимо от 1 до {TOP_COUNT} технологий, получено {len(candidates)}")
        warnings.append("Диагностика недобора не является приёмкой TOP-15; повторите проверку без --allow-shortfall.")
    else:
        check(len(candidates) == TOP_COUNT,
              f"TOP-15 требует ровно {TOP_COUNT} технологий, получено {len(candidates)}")
    for key, expected in (("selected", len(candidates)), ("rejected", len(rejected)),
                          ("candidates", len(candidates) + len(rejected))):
        check(counters.get(key) == expected, f"счётчик {key} не замкнут: {counters.get(key)!r} вместо {expected}")
    ids = [candidate.get("id") for candidate in candidates]
    check(all(map(text, ids)), "у принятой технологии отсутствует ID")
    check(len({value.strip() for value in ids if text(value)}) == len(ids), "повторяются ID технологий")
    labels = [label_key(candidate.get("label") or candidate.get("name") or "")
              for candidate in candidates]
    check(len(set(labels)) == len(labels), "повторяются названия технологий")
    for row in rejected:
        check(text(row.get("code")) and russian(row.get("reason")), "отклонённый кандидат без кода или русской причины")
        check(not (row.get("code") == "below_top" and len(candidates) < TOP_COUNT), "при недоборе заявлено отсутствие мест в TOP-15")
    if expect_empty:
        check(bool(rejected or (detail.get("topic") or {}).get("limits") or detail.get("limits")),
              "пустая выдача не объяснена")
        warnings.append("Пустой контроль не является приёмкой TOP-15 и не доказывает корректность фильтра зрелости.")

    def evidence_ok(item, own, prefix, require_verified=True, *, require_reason=False, contexts=None):
        source, quote = item.get("source_id"), item.get("quote")
        check(source in own and source in papers, f"{prefix}: источник цитаты отсутствует в принятом корпусе")
        check(text(quote), f"{prefix}: нет дословного фрагмента")
        if require_verified:
            check(item.get("verified") is True, f"{prefix}: утверждение не проверено")
        if require_reason:
            check(russian(item.get("verification_reason")),
                  f"{prefix}: нет русского обоснования проверки утверждения")
        if source not in papers or not text(quote):
            return
        if contexts and source in contexts:
            original = contexts[source].get("text")
            check(text(original) and normalized(quote) in normalized(original),
                  f"{prefix}: цитата отсутствует в сохранённом входе модели")
            return
        paper, meta = papers[source], metadata.get(source) or {}
        title, abstract = paper.get("title") or "", meta.get("abstract") or paper.get("abstract") or ""
        if normalized(quote) in normalized(title):
            return
        if abstract:
            original = title + " (" + (paper.get("date") or "")[:4] + "): " + abstract
            check(normalized(quote) in normalized(original), f"{prefix}: цитата не совпадает с возвращённым оригиналом")
        else:
            unchecked_quotes.add((source, quote))

    for index, candidate in enumerate(candidates, 1):
        prefix = f"карточка {index}"
        scientific, market_ids = candidate.get("paper_ids") or [], candidate.get("evidence_ids") or []
        ids = scientific + market_ids
        own = set(ids)
        check(len(ids) == len(own), f"{prefix}: повторяются ID источников")
        check(candidate.get("papers_count") == len(scientific) and candidate.get("records_count", 0) == len(market_ids)
              and candidate.get("documents_count") == len(own), f"{prefix}: состав источников и счётчики расходятся")
        check(russian(candidate.get("label")), f"{prefix}: нет русского названия")
        card = candidate.get("card") or {}
        contexts = validate_card_contexts(card, own, papers, metadata, prefix, check)
        card_evidence_ok = partial(evidence_ok, contexts=contexts)
        if card.get("study_limitations"):
            check(russian(card["study_limitations"]), f"{prefix}: ограничения исследования не на русском")
            supporting = [item for item in card.get("evidence") or []
                          if item.get("field") == "study_limitations" and item.get("verified") is True]
            check(len(supporting) == 1, f"{prefix}: ограничения исследования без единственной проверенной цитаты")
            for item in supporting:
                card_evidence_ok(item, own, prefix + " / study_limitations", require_reason=True)
                validate_limitation_parts(card["study_limitations"], item, prefix, check)
        # Добор до пятнадцати помечен и честно не прошёл одну из проверок: полной квалификации от него
        # не ждём, но зрелое, повторное или чужое по теме в пятнадцать не попадает ни в каком режиме.
        shortfall = candidate.get("shortfall")
        if shortfall is not None:
            code = shortfall.get("code") if isinstance(shortfall, dict) else None
            if code in FORBIDDEN_TOP_UP:
                check(False, f"{prefix}: добрана с запрещённой причиной {code!r}")
            else:
                check(code in TOP_UP_CODES, f"{prefix}: причина добора {code!r} вне белого списка")
            check(isinstance(shortfall, dict) and russian(shortfall.get("reason")), f"{prefix}: добрана без причины")
            check(russian(card.get("summary")), f"{prefix}: добрана без описания")
            check(bool(own), f"{prefix}: добрана без источников")
        else:
            validate_passed(candidate, card, own, prefix, check, evidence_ok, card_evidence_ok)
            qualification = candidate.get("qualification") or {}
            validate_qualification_scope(qualification, scope, valid_scope, prefix, check)
            validate_canonical_name(qualification, own, papers, metadata, prefix, check, warnings)
        # Порядок сообщений — порядок источников в карточке: сохранённый вывод сверяется с повторной проверкой.
        for source in dict.fromkeys(ids):
            paper, passport = papers.get(source) or {}, passports.get(source) or {}
            check(all(text(paper.get(field)) for field in ("title", "date", "url")), f"{prefix}: {source} без названия, даты или URL")
            check(all(text(passport.get(field)) for field in PASSPORT_FIELDS),
                  f"{prefix}: {source} без полного паспорта")
            if passport.get("language") != "ru":
                check(russian(passport.get("digest")) and text(passport.get("digest_model")),
                      f"{prefix}: {source} без русской интерпретации и имени модели")
        validate_scores(candidate, prefix, check, card_evidence_ok, own, warnings, detail, require_model)
    verified = [candidate for candidate in candidates if candidate.get("shortfall") is None]
    top_up = len(candidates) - len(verified)
    scored = [candidate["model_score"] for candidate in candidates if candidate.get("model_score")]
    verified_scored = [candidate["model_score"] for candidate in verified if candidate.get("model_score")]
    model_decisions = sum(model_decision(score) for score in scored)
    check(not verified_scored or len(verified_scored) == len(verified), "выдача смешивает модельные оценки и правила")
    # Добранная технология не входит в прошедшие порог правил, какой бы ни была её оценка.
    above_rules = sum((candidate.get("confidence") or {}).get("above_threshold") is True for candidate in verified)
    # «Выше 75 %» — то, что карточка называет слабым сигналом: без воздержаний и с двумя независимыми подтверждениями.
    above_model = sum(number((candidate.get("model_score") or {}).get("value"))
                      and round(candidate["model_score"]["value"] * 100) > 75
                      and candidate["model_score"].get("label") not in ("insufficient_data", "undecided")
                      and independent_sources(candidate) >= MIN_INDEPENDENT for candidate in candidates)
    # top_up сервер опускает при нуле, остальные счётчики присылает всегда.
    for key, expected in (("above_threshold", above_rules), ("model_scored", len(scored)),
                          ("model_above_threshold", above_model), ("top_up", top_up)):
        check(counters.get(key, 0 if key == "top_up" else None) == expected,
              f"счётчик {key} не соответствует итоговому списку")
    if require_model:
        check(text(detail.get("classifier_version")), "версия обученной модели отсутствует")
        limits = (detail.get("limits") or []) + ((detail.get("topic") or {}).get("limits") or [])
        check(not any(limit.get("code") == "classifier_not_connected" for limit in limits),
              "обученная модель не подключена")
        if any(limit.get("code") == "classifier_unavailable" for limit in limits):
            if verified and sum(model_decision(score) for score in verified_scored) == len(verified):
                warnings.append("В ходе отбора был отказ модели; итоговые оценки всех принятых технологий проверены отдельно.")
            else:
                check(False, "обученная модель не оценила все принятые технологии")
    if not scored:
        warnings.append("Обученная модель не проверена этим ответом; доля правил не является её оценкой или точностью.")
    if top_up:
        warnings.append(f"Добрано ниже планки: {top_up} из {len(candidates)} — кандидаты для экспертной проверки, "
                        "а не подтверждённые сигналы.")
    if unchecked_quotes:
        warnings.append(f"Для {len(unchecked_quotes)} цитат оригинальная аннотация не передана в API: нужна ручная проверка первоисточника.")
    warnings.append("Пройденный контракт не доказывает правильность технологии, новизну, применимость или Precision/Recall/F1.")
    return {"contract": SEARCH_CONTRACT, "passed": not problems, "problems": problems, "warnings": warnings,
            "acceptance_mode": mode, "top15_passed": mode == "top15" and not problems,
            "model_decisions": model_decisions, "verified": len(verified), "top_up": top_up,
            "verified_share": round(len(verified) / len(candidates), 3) if candidates else 0.0,
            "top_up_codes": dict(collections.Counter(str((candidate.get("shortfall") or {}).get("code"))
                                                     for candidate in candidates if candidate.get("shortfall"))),
            "origins": dict(collections.Counter(map(candidate_origin, candidates))),
            "semantic_accuracy_checked": False, "quotes_without_returned_original": len(unchecked_quotes)}


def validate_passed(candidate, card, own, prefix, check, evidence_ok, card_evidence_ok):
    """По ТЗ достаточно одного прямого источника, но все три поля карточки
    должны быть заполнены и подтверждены цитатами."""
    check(bool(own), f"{prefix}: нет подтверждающего документа")
    qualification = candidate.get("qualification") or {}
    # Название показывается по-русски и с именем метода впереди («GLASS: …»),
    # а проверенная сущность — исходной фразой: они совпадать не обязаны.
    check(text(qualification.get("label")) and qualification.get("method") == "grounded-entity-review-v7",
          f"{prefix}: нет проверенной сущности или метода проверки")
    check(all(qualification.get(flag) is True for flag in ("technology", "relevant", "coherent", "verified")),
          f"{prefix}: не завершена квалификация сущности и связи с запросом")
    check(all(text(qualification.get(field)) for field in ("method", "model", "verifier_model"))
          and russian(qualification.get("reason")), f"{prefix}: нет происхождения квалификации")
    check(russian(qualification.get("mechanism")), f"{prefix}: не указан общий отличительный механизм")
    qualified = qualification.get("evidence") or []
    # Записи рыночных событий карточка получает после проверки сущности: они
    # подтверждают стадию, а не то, что технология — та самая.
    qualified_ids = {item.get("source_id") for item in qualified}
    check(bool(qualified_ids) and set(candidate.get("paper_ids") or []) <= qualified_ids <= own
          and len(qualified) == len(qualified_ids),
          f"{prefix}: в измерениях остались непроверенные документы")
    for item in qualified:
        check(item.get("same_technology") is True
              and item.get("role") in DIRECT_ROLES
              and russian(item.get("identity_reason")),
              f"{prefix}: источник не подтверждает тот же конкретный механизм")
        evidence_ok(item, own, prefix + " / квалификация", require_verified=False)
        check(russian(item.get("relevance_reason")), f"{prefix}: нет отдельного основания связи с запросом")
        for field in ("mechanism_quote", "relevance_quote"):
            evidence_ok({"source_id": item.get("source_id"), "quote": item.get(field)}, own,
                        prefix + " / " + field, require_verified=False)
    check(text(card.get("model")) and text(card.get("verification_model")), f"{prefix}: не указаны модели текста и проверки")
    check(not card.get("missing"), f"{prefix}: карточка помечена неполной")
    claim_fields = FIELDS + (("report_assessment",) if card.get("report_assessment") else ())
    for field in claim_fields:
        check(russian(card.get(field)), f"{prefix}: {field} пустое или не русское")
        supporting = [item for item in card.get("evidence") or []
                      if item.get("field") == field and item.get("verified") is True]
        check(bool(supporting), f"{prefix}: {field} без проверенного источника и цитаты")
        for item in supporting:
            card_evidence_ok(item, own, prefix + " / " + field, require_reason=True)
        if field == "example":
            check(card.get("example_id") in {item.get("source_id") for item in supporting},
                  f"{prefix}: источник кейса не совпадает с проверенной цитатой")


def validate_scores(candidate, prefix, check, evidence_ok, own, warnings, detail, require_model):
    """Правила, рынок и модель — у любой показанной технологии."""
    top_up = candidate.get("shortfall") is not None
    confidence = candidate.get("confidence") or {}
    passed, total, value = confidence.get("passed"), confidence.get("total"), confidence.get("value")
    check(confidence.get("method") == RULE_METHOD and total == RULE_TOTAL,
          f"{prefix}: знаменатель правил не равен {RULE_TOTAL}")
    check(number(passed) and number(value) and math.isclose(value, passed / RULE_TOTAL, abs_tol=1e-9),
          f"{prefix}: доля правил не соответствует passed/{RULE_TOTAL}")
    reasons, missing, unmeasured = (confidence.get(field) or [] for field in ("reasons", "missing", "unmeasured"))
    check(len(reasons) == passed and len(reasons) + len(missing) + len(unmeasured) == RULE_TOTAL
          and confidence.get("measured") == RULE_TOTAL - len(unmeasured), f"{prefix}: причины правил не сходятся со счётчиками")
    check(confidence.get("threshold") == 0.75 and number(value)
          and confidence.get("above_threshold") is (value > 0.75), f"{prefix}: неверен порог правил")
    market = candidate.get("market") or {}
    check(market.get("status") in ("unknown", "early", "forming") and market.get("mature") is False,
          f"{prefix}: зрелый рынок в принятой выдаче или нет явного статуса рынка")
    check(russian(market.get("basis")), f"{prefix}: рынок без объяснения")
    if market.get("status") == "unknown":
        check(market.get("known") is False, f"{prefix}: неизвестный рынок помечен измеренным")
        warnings.append(f"{prefix}: стадия рынка не доказана, необходима проверка аналитиком.")
    elif market.get("status") in ("early", "forming"):
        # Ранний рынок доказывает цитата карточки или проверка зрелости:
        # стандарты, компании, продукты вендоров, энциклопедия — каждая названа.
        supporting = [item for item in market.get("evidence") or [] if item.get("field") == "market"]
        checked = market.get("checks") or []
        check(market.get("known") is True and (bool(supporting) or (bool(checked) and all(map(russian, checked)))),
              f"{prefix}: ранний рынок без основания")
        for item in supporting:
            evidence_ok(item, own, prefix + " / рынок")
    score = candidate.get("model_score")
    # Добор помечен непройденным: оценки модели у него может не быть, а отказ модели — его причина.
    if require_model and not top_up:
        check(bool(score), f"{prefix}: обученная модель не подключена")
    if score:
        if not model_decision(score):
            message = f"{prefix}: модель не вынесла решения по данным; воздержание не является модельной оценкой"
            # Добор не превращает «не могу решить» в решение обученной модели.
            if require_model and (not top_up or score.get("label") == "undecided"):
                check(False, message)
            else:
                warnings.append(message)
        value, threshold = score.get("value"), score.get("threshold")
        # Прошедшая карточка — слабый сигнал выше порога или воздержание; добранная может нести отказ
        # модели, но метку «зрелая» не несёт ни одна из пятнадцати.
        label = score.get("label")
        labels = ("weak_signal", "insufficient_data", "undecided") + (("noise", "failed") if top_up else ())
        check(number(value) and number(threshold) and 0 <= value <= 1 and 0 <= threshold <= 1
              and label in labels and (top_up or label != "weak_signal" or value >= threshold),
              f"{prefix}: неверная оценка/метка/порог классификатора")
        check(all(text(score.get(field)) for field in ("model", "version", "basis", "feature_schema"))
              and score.get("version") == detail.get("classifier_version"), f"{prefix}: нет версии и происхождения модели")
        check(russian(score.get("basis")), f"{prefix}: основание модели не на русском языке")
        check(score.get("feature_schema") == "weak-signal-features-v1", f"{prefix}: неизвестна схема признаков модели")
        check(bool(score.get("features")), f"{prefix}: нет объясняющих признаков модели")
        for feature in score.get("features") or []:
            check(russian(feature.get("name")) and text(feature.get("value")) and number(feature.get("contribution")),
                  f"{prefix}: некорректно объяснён признак модели")


def report(detail, timing, validation):
    counters = detail.get("counters") or {}
    mode = validation["acceptance_mode"]
    labels = {"top15": "строгий TOP-15", "shortfall_diagnostic": "диагностика недобора", "empty_control": "пустой контроль"}
    print("Режим проверки: " + labels[mode])
    print(f"Метод: {detail.get('method')}; дата сбора: {detail.get('observed_at')}")
    print(f"Отобрано: {counters.get('selected')}; отклонено: {counters.get('rejected')}; рассмотрено: {counters.get('candidates')}")
    codes = ", ".join(f"{code} {count}" for code, count in sorted(validation.get("top_up_codes", {}).items()))
    print(f"Проверенных: {validation.get('verified')}; доля {round(validation.get('verified_share', 0) * 100)} %; "
          f"добрано ниже планки: {validation.get('top_up')}" + (f" ({codes})" if codes else ""))
    origins = validation.get("origins", {})
    print(f"Кандидаты: из статей {origins.get('science', 0)}; направления карты {origins.get('direction', 0)}; "
          f"из рыночных записей {origins.get('market', 0)}")
    print(f"Источников обработано: {counters.get('sources_processed')}; выше 75 % правил: {counters.get('above_threshold')}")
    print(f"Ответов классификатора: {counters.get('model_scored')}; решений по данным: {validation['model_decisions']}; "
          f"слабых сигналов выше 75 % при двух независимых подтверждениях: {counters.get('model_above_threshold')}")
    if timing:
        freshness = "подтверждён" if timing.get("fresh_collection_proven") else "не подтверждён"
        print(f"Наблюдаемое ожидание: {timing.get('seconds')} с; новый сбор {freshness}")
    for problem in validation["problems"]:
        print("ОШИБКА: " + problem)
    for warning in validation["warnings"]:
        print("ПРОВЕРИТЬ: " + warning)
    print("КОНТРАКТ: " + ("ПРОЙДЕН" if validation["passed"] else "НЕ ПРОЙДЕН"))
    if mode == "top15":
        print("TOP-15: " + ("ПРОЙДЕН" if validation["top15_passed"] else "НЕ ПРОЙДЕН"))
    else:
        print("TOP-15: НЕ ПРИНИМАЛСЯ — выбран отдельный контрольный режим")


def write_result(path, envelope):
    if path:
        with path.open("x", encoding="utf-8") as output:
            json.dump(envelope, output, ensure_ascii=False, indent=2)


def interrupted_result(args, failure):
    response = failure.response if isinstance(failure, SearchInterrupted) else None
    return {"query": args.query, "response": None, "intermediate_response": response,
            "final_response_received": False,
            "interruption": failure.kind if isinstance(failure, SearchInterrupted) else type(failure).__name__,
            "timing": failure.timing if isinstance(failure, SearchInterrupted) else None,
            "validation": {"contract": SEARCH_CONTRACT, "passed": False, "top15_passed": False,
                           "status": "pending" if response else "unavailable",
                           "acceptance_mode": "empty_control" if args.expect_empty else
                           "shortfall_diagnostic" if args.allow_shortfall else "top15",
                           "problems": ["Финальный ответ не получен; промежуточный снимок не является приёмкой."],
                           "warnings": [], "semantic_accuracy_checked": False}}


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    try:
        env = load_stand_env()
    except ValueError as failure:
        parser.error(str(failure))
    parser.add_argument("query", nargs="?", help="открытый запрос; не требуется для --detail-file")
    parser.add_argument("--base", default=env.get("STAND_URL"), help="совместимость: только значение STAND_URL")
    parser.add_argument("--user", default=env.get("STAND_USER"), help="совместимость: только значение STAND_USER")
    parser.add_argument("--password-env", default="STAND_PASSWORD", help="имя переменной с паролем")
    parser.add_argument("--token-env", default="STAND_TOKEN", help="имя переменной с токеном")
    parser.add_argument("--password", help=argparse.SUPPRESS)
    parser.add_argument("--token", help=argparse.SUPPRESS)
    parser.add_argument("--detail-file", type=Path, help="полный detail, ready-ответ API или файл --out")
    parser.add_argument("--fresh", action="store_true", help="запросить пересборку и подтвердить новый сбор; только живой поиск")
    parser.add_argument("--out", type=Path, help="сохранить финальный ответ или снимок при ошибке и время; файл должен быть новым")
    parser.add_argument("--require-model", action="store_true", help="требовать результат обученной модели для всех принятых карточек")
    count_mode = parser.add_mutually_exclusive_group()
    count_mode.add_argument("--expect-empty", action="store_true", help="отдельный пустой контроль, не приёмка TOP-15")
    count_mode.add_argument("--allow-shortfall", action="store_true", help="диагностика 1–15 результатов, не приёмка TOP-15")
    parser.add_argument("--timeout", type=float, default=900, help="общий срок ожидания поиска, секунды (по умолчанию 900)")
    args = parser.parse_args()
    if args.base != env.get("STAND_URL") or args.user != env.get("STAND_USER"):
        parser.error("адрес и пользователь задаются только через STAND_URL и STAND_USER")
    if args.password is not None or args.token is not None:
        parser.error("пароль и токен задаются только через переменные окружения")
    if args.timeout <= 0 or not math.isfinite(args.timeout):
        parser.error("--timeout должен быть конечным положительным числом")
    if args.fresh and args.detail_file:
        parser.error("--fresh нельзя использовать с --detail-file")
    if args.out and args.out.exists():
        parser.error("файл --out уже существует; задайте новое имя для сохранения доказательства")
    token = None
    try:
        final_response_received = True
        interruption = None
        if args.detail_file:
            saved = json.loads(args.detail_file.read_text())
            final_response_received = saved.get("final_response_received", True) is True
            response = (saved.get("intermediate_response") if not final_response_received
                        else saved.get("response", saved))
            observed_response = response or {}
            detail = observed_response.get("detail", observed_response)
            timing = saved.get("timing")
            interruption = saved.get("interruption")
            expected_query = args.query or saved.get("query") or observed_response.get("query")
        else:
            if not args.query:
                parser.error("нужен query или --detail-file")
            if not args.base:
                parser.error("задайте адрес стенда в STAND_URL")
            token = env.get(args.token_env)
            password = env.get(args.password_env)
            if not token and not password:
                parser.error("задайте пароль или токен в указанной переменной окружения")
            args.base = stand_origin(env)
            if not token and not args.user:
                parser.error("задайте пользователя в STAND_USER")
            token = token or login(args.base, args.user, password)
            response, timing = search(args.base, token, args.query, args.timeout, fresh=args.fresh)
            detail = response["detail"]
            expected_query = args.query
        validation = validate(detail, args.expect_empty, args.require_model,
                              allow_shortfall=args.allow_shortfall, expected_query=expected_query)
        final_response_received = final_response_received and not (
            detail.get("digests_pending") or detail.get("rescoring"))
        if not final_response_received:
            validation["problems"].append("Финальный ответ не получен; сохранённый снимок остаётся промежуточным.")
            validation.update(passed=False, top15_passed=False, status="pending" if response else "unavailable")
        if args.fresh and not timing.get("fresh_collection_proven"):
            validation["problems"].append("Свежий сбор не подтверждён: нужны начало running и новая дата observed_at.")
            validation.update(passed=False, top15_passed=False)
        extra = (env.get(args.password_env), env.get(args.token_env), token)
        envelope = redact({"query": expected_query, "response": response if final_response_received else None,
                           "intermediate_response": None if final_response_received else response,
                           "final_response_received": final_response_received, "interruption": interruption,
                           "timing": timing, "validation": validation}, env, extra)
        write_result(args.out, envelope)
        report(redact(detail, env, extra), redact(timing, env, extra), redact(validation, env, extra))
        return 2 if not final_response_received else 0 if validation["passed"] else 1
    except (OSError, ValueError, KeyError, TypeError, AttributeError) as failure:
        extra = (env.get(args.password_env), env.get(args.token_env), token)
        envelope = redact(interrupted_result(args, failure), env, extra)
        try:
            write_result(args.out, envelope)
        except (OSError, ValueError, TypeError):
            print("Не удалось сохранить файл доказательства; проверьте путь и доступ для записи.", file=sys.stderr)
        print(f"Приёмка не завершена: {envelope['interruption']}; проверьте доступность, формат ответа и путь файла.", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
