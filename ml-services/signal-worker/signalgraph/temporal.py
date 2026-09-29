"""Retrospective period counts with explicit units, date precision and coverage.

These measurements describe the found corpus, not the world's first occurrence
or a forecast. Missing exposure is never replaced by zero or a growth score.
"""
from __future__ import annotations

from datetime import date, datetime, time, timedelta, timezone

UNITS = ("scientific_works", "patent_publications", "company_materials", "analyses",
         "independent_repositories", "published_releases")
KIND_UNIT = {"paper": "scientific_works", "patent": "patent_publications",
             "company_material": "company_materials", "analysis": "analyses"}


def date_interval(value: dict) -> tuple[date, date]:
    precision, raw = value["precision"], value["value"]
    if not value.get("basis"):
        raise ValueError("date has no documentary basis")
    if precision == "year" and len(raw) == 4:
        start = date(int(raw), 1, 1)
        return start, date(start.year + 1, 1, 1)
    if precision == "month" and len(raw) == 7:
        start = date.fromisoformat(raw + "-01")
        return start, date(start.year + (start.month == 12), start.month % 12 + 1, 1)
    if precision == "day" and len(raw) == 10:
        start = date.fromisoformat(raw)
        return start, start + timedelta(days=1)
    raise ValueError("date does not match its declared precision")


def quarters(start: str, cutoff: str) -> list[dict]:
    first = date.fromisoformat(start)
    deadline = datetime.fromisoformat(cutoff.replace("Z", "+00:00"))
    if deadline.tzinfo is None or first > deadline.date():
        raise ValueError("invalid period boundaries")
    cursor = date(first.year, ((first.month - 1) // 3) * 3 + 1, 1)
    result = []
    while cursor <= deadline.date():
        end = date(cursor.year + (cursor.month == 10), (cursor.month + 2) % 12 + 1, 1)
        complete = cursor >= first and datetime.combine(end, time(), timezone.utc) <= deadline + timedelta(seconds=1)
        result.append({"period": f"{cursor.year}-Q{(cursor.month-1)//3+1}", "start": cursor.isoformat(),
                       "end_exclusive": end.isoformat(), "complete_period": complete})
        cursor = end
    return result


def work_date(work: dict, records: dict[str, dict]) -> dict | None:
    dated = []
    for identifier in work["document_ids"]:
        record = records[identifier]
        publication = record["source"].get("publication")
        if publication:
            interval = date_interval(publication)
            dated.append((interval[0], interval[1], identifier, publication, record))
    if not dated:
        return None
    _, _, identifier, publication, record = min(dated, key=lambda item: item[:3])
    return {"date": publication, "document_id": identifier, "snapshot_id": record["snapshot_id"],
            "source_revision": record["source"]["revision"], "work_id": work["id"]}


def measure(corpus: dict, members: list[str], coverage: list[dict] | None = None) -> dict:
    """Coverage is verified separately from provider pagination, never guessed
    from these counts. A window carries exact population work_ids and basis.
    Missing/partial provider coverage leaves comparable counts and rates null.
    """
    records = {r["id"]: r for r in corpus["records"]}
    works = {w["id"]: w for w in corpus["works"]}
    if len(set(members)) != len(members) or not set(members) <= works.keys():
        raise ValueError("candidate membership differs from frozen canonical works")
    request = corpus["input"]["request"]
    series = quarters(request["start"], request["cutoff"])
    dates = {key: work_date(work, records) for key, work in works.items()}
    earliest = [dates[key] for key in members if dates[key] and works[key]["kind"] in ("paper", "company_material")]
    earliest.sort(key=lambda item: (*date_interval(item["date"]), item["work_id"]))
    scientific_earliest = [anchor for anchor in earliest if works[anchor['work_id']]['kind'] == 'paper']
    unallocated = []
    for key in members:
        work = works[key]
        if work["kind"] in KIND_UNIT and dates[key] is None:
            unallocated.append({"work_id": key, "reason": "publication_date_unknown"})
        elif dates[key] and dates[key]["date"]["precision"] == "year":
            unallocated.append({"work_id": key, "reason": "year_precision_crosses_quarters"})
    matched = {key for key, work in works.items()
               if any(records[doc].get("query_matched") for doc in work["document_ids"])}
    enrollment = corpus.get("coverage_enrollment")
    scientific_scopes = enrollment["scientific_scope_ids"] if enrollment is not None else []
    unscoped_works = enrollment["unscoped_scientific_work_ids"] if enrollment is not None else []
    for period in series:
        start, end = date.fromisoformat(period["start"]), date.fromisoformat(period["end_exclusive"])
        found = {unit: set() for unit in UNITS}
        exposure = set()
        for key, work in works.items():
            anchor = dates[key]
            if anchor:
                left, right = date_interval(anchor["date"])
                if not start <= left < right <= end:
                    continue
                if work["kind"] == "paper":
                    exposure.add(key)
                if key in members and work["kind"] in KIND_UNIT:
                    found[KIND_UNIT[work["kind"]]].add(key)
            elif key in members and work["kind"] == "software_repository":
                _repository_events(work, records, corpus.get("releases", []), start, end, found)
        windows = [window for window in coverage or [] if window.get("complete") and window.get("basis")
                   and window["start"] <= period["start"] and window["end_exclusive"] >= period["end_exclusive"]]
        population = set().union(*(set(window["work_ids"]) for window in windows))
        if not population <= works.keys():
            raise ValueError("coverage population outside immutable manifest")
        missing_scopes = []
        covered_scopes = set()
        if enrollment is not None:
            covered_scopes = {window["basis"]["scope_id"] for window in windows}
            missing_scopes = sorted(set(enrollment["scientific_scope_ids"]) - covered_scopes)
        comparable = bool(windows) and period["complete_period"] and not missing_scopes and not unscoped_works
        numerator = len(found["scientific_works"] & population)
        denominator = len(exposure & population)
        basis_kind = "examined_bounded_population" if comparable else "unknown_or_partial"
        # Without a verified sweep the question's own corpus — works whose
        # title carries a search term — bounds the share. Both sides come from
        # the same harvest, so its monthly caps cancel out; the share is of the
        # field as harvested, never of the world.
        if not comparable and matched and period["complete_period"]:
            numerator = len(found["scientific_works"] & matched)
            denominator = len(exposure & matched)
            comparable, basis_kind = True, "query_corpus"
        period.update({"found": {unit: len(values) for unit, values in found.items()},
                       "found_exposure_scientific_works": len(exposure),
                       "coverage": basis_kind,
                       "comparable_scientific_works": numerator if comparable else None,
                       "comparable_exposure": denominator if comparable else None,
                       "normalized_activity": numerator / denominator if comparable and denominator else None,
                       "covered_scientific_scope_ids": sorted(covered_scopes),
                       "coverage_basis": [window["basis"] for window in windows]})
    return {"history_kind": "retrospective_distribution_of_current_membership", "earliest": earliest[0] if earliest else None,
            "earliest_scientific": scientific_earliest[0] if scientific_earliest else None,
            "unallocated_scientific_work_ids": [row['work_id'] for row in unallocated
                                                if works[row['work_id']]['kind'] == 'paper'],
            "scientific_scope_ids": scientific_scopes,
            "unscoped_scientific_work_ids": unscoped_works,
            "earliest_meaning": "earliest_found_in_examined_corpus", "series": series,
            "unallocated": unallocated, "stars_history": "unknown_before_first_observation",
            "units": list(UNITS)}


def _repository_events(work: dict, records: dict, releases: list[dict], start: date, end: date, found: dict) -> None:
    root = work.get("implementation_root")
    for identifier in work["document_ids"]:
        github = records[identifier]["source"]["github"]
        created = date.fromisoformat(github["created_at"][:10])
        # A fork's creation is not the root implementation's first appearance.
        if root and not github["fork"] and start <= created < end:
            found["independent_repositories"].add(root)
    for release in releases:
        published = date.fromisoformat(release["published_at"][:10])
        if release["repository_work_id"] == work["id"] and start <= published < end:
            found["published_releases"].add((release["repository_id"], release["id"]))
