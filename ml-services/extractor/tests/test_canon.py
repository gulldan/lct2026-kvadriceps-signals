from __future__ import annotations

import math


from extractor.canon import (
    Canonicalizer,
    normalize_mention,
)
from extractor.interfaces import (
    EmbeddingClient,
    EmbeddingClientConfig,
    SpanTagger,
)
from extractor.pipeline import Components, ExtractPipeline
from extractor.schemas import CandidateClass, Document, ExtractRequest, Span, SpanRole, dumps_canonical
from extractor.tests.fakes import DeterministicEmbeddingClient


def span(text, canonical=None, start=0, **kwargs):
    return Span(
        text=text,
        start=start,
        end=start + len(text),
        canonical_ru=canonical or text,
        subtype="method",
        role=SpanRole.CONTRIBUTION,
        is_candidate=kwargs.pop("is_candidate", True),
        confidence=kwargs.pop("confidence", 0.9),
        **kwargs,
    )


class Vectors(EmbeddingClient):
    config = EmbeddingClientConfig(dim=3)

    def __init__(self, values):
        self.values = {normalize_mention(text): value for text, value in values.items()}
        self.calls = []

    def embed(self, texts):
        self.calls.append(list(texts))
        return [self.values[text] for text in texts]

    def health(self):
        return True


def vector(cosine):
    return [cosine, math.sqrt(1 - cosine**2), 0.0]


def test_golden_mentions_keep_provenance_and_exclude_background_only_technology(
    annotations, documents
):
    canonicalizer = Canonicalizer(
        DeterministicEmbeddingClient(),
        documents={doc.doc_id: doc for doc in documents},
    )
    mapping = {annotation.doc_id: annotation.spans for annotation in annotations}
    result = canonicalizer.group(mapping)
    named = {item.canonical_ru: item for item in result}
    assert "холодная листовая штамповка" not in named
    assert "штамповка (штамповочные способы)" not in named
    assert "периодическая (батч) ферментация" not in named
    mixed = named["олигосахариды L. acidophilus DSMZ 20079"]
    assert len(mixed.evidence) == 2
    assert {item.role for item in mixed.evidence} == {
        SpanRole.BACKGROUND,
        SpanRole.CONTRIBUTION,
    }
    assert "—" not in named
    assert all(item.label is CandidateClass.INSUFFICIENT_DATA for item in result)
    assert all(item.stage.code == 0 and item.explanation == [] for item in result)
    originals = {(a.doc_id, s.start, s.end): s for a in annotations for s in a.spans}
    for candidate in result:
        assert candidate.name in {s.text for a in annotations for s in a.spans}
        assert candidate.doc_ids == sorted(set(candidate.doc_ids))
        assert [e.sort_key for e in candidate.evidence] == sorted(
            e.sort_key for e in candidate.evidence
        )
        for evidence in candidate.evidence:
            original = next(
                s
                for (doc_id, start, end), s in originals.items()
                if doc_id == evidence.doc_id
                and evidence.start <= start
                and end <= evidence.end
                and s.role == evidence.role
            )
            assert evidence.role == original.role
            assert evidence.in_title == original.in_title
            document = next(d for d in documents if d.doc_id == evidence.doc_id)
            assert document.text[evidence.start : evidence.end] == evidence.sentence
    reordered = {
        key: list(reversed(values)) for key, values in reversed(list(mapping.items()))
    }
    assert dumps_canonical(result) == dumps_canonical(canonicalizer.group(reordered))


def test_russian_lemmas_case_and_yo():
    assert normalize_mention("ОБЪЁМНЫМИ  ДАТЧИКАМИ") == normalize_mention(
        "объемный датчик"
    )
    assert normalize_mention("холодной листовой штамповки") == normalize_mention(
        "Холодная листовая штамповка"
    )
    assert normalize_mention("DIGITAL Twin") == "digital twin"


def test_ru_en_shared_span_canonical_and_most_frequent_surface():
    canonicalizer = Canonicalizer(DeterministicEmbeddingClient())
    first = span("Digital twin", "цифровой двойник", in_title=True)
    repeated = span("Digital twin", "цифровой двойник", start=30)
    russian = span("цифровые двойники", "цифрового двойника")
    result = canonicalizer.group({"a": [first, repeated, first], "b": [russian]})
    assert len(result) == 1
    candidate = result[0]
    assert candidate.name == "Digital twin"
    assert candidate.canonical_ru == "цифровой двойник"
    assert len(candidate.evidence) == 3
    assert candidate.doc_ids == ["a", "b"]


def test_cross_language_dense_similarity_with_different_canonical_names():
    embeddings = Vectors({"digital twin": [1, 0, 0], "цифровой двойник": vector(0.9)})
    result = Canonicalizer(embeddings).group(
        {
            "en": [span("digital twin", "цифровая модель объекта")],
            "ru": [span("цифровой двойник")],
        }
    )
    assert len(result) == 1
    assert result[0].doc_ids == ["en", "ru"]


def test_parenthesis_abbreviation_from_sentence_and_separate_document():
    full = "высокоэффективная жидкостная хроматография"
    declaration = f"Разработана {full} (ВЭЖХ)."
    result = Canonicalizer(DeterministicEmbeddingClient()).group(
        {
            "a": [span(full, sentence=declaration)],
            "b": [span("ВЭЖХ")],
        }
    )
    assert len(result) == 1
    assert result[0].doc_ids == ["a", "b"]


def test_whole_name_expands_non_initial_abbreviation_and_no_state_leak():
    canonicalizer = Canonicalizer(DeterministicEmbeddingClient())
    full = "беспилотный летательный аппарат"
    result = canonicalizer.group({"a": [span(f"{full} (БПЛА)")], "b": [span("БПЛА")]})
    assert len(result) == 1
    assert len(canonicalizer.group({"a": [span(full)], "b": [span("БПЛА")]})) == 2


def test_ambiguous_abbreviations_remain_document_local():
    result = Canonicalizer(DeterministicEmbeddingClient()).group(
        {
            "a": [span("alpha beta (AB)"), span("AB", start=50)],
            "b": [span("another body (AB)"), span("AB", start=50)],
        }
    )
    assert len(result) == 2
    assert [len(item.evidence) for item in result] == [2, 2]


def test_negative_only_bucket_never_becomes_candidate_even_in_title():
    canonicalizer = Canonicalizer(DeterministicEmbeddingClient())
    assert (
        canonicalizer.group(
            {"a": [span("Quantum computing", in_title=True, is_candidate=False)]}
        )
        == []
    )


def test_candidate_seed_keeps_background_alias_sources_and_evidence():
    seed = span("Digital twin", "цифровой двойник")
    background = span("цифровые двойники", "цифровой двойник", is_candidate=False)
    background.role = SpanRole.BACKGROUND
    result = Canonicalizer(DeterministicEmbeddingClient()).group(
        {"a": [seed], "b": [background]}
    )
    assert len(result) == 1
    assert result[0].doc_ids == ["a", "b"]
    assert result[0].surface_forms == ["Digital twin", "цифровые двойники"]
    assert {item.role for item in result[0].evidence} == {
        SpanRole.BACKGROUND,
        SpanRole.CONTRIBUTION,
    }


def test_record_ids_are_derived_only_from_supplied_metadata():
    docs = {
        key: Document(doc_id=key, source="test", source_type=kind)
        for key, kind in [("a", "news"), ("b", "journal_article")]
    }
    result = Canonicalizer(DeterministicEmbeddingClient(), documents=docs).group(
        {
            "a": [span("датчик")],
            "b": [span("датчик")],
        }
    )
    assert result[0].record_ids == ["a"]


def test_stoplist_matches_english_surfaces_and_aliases_but_not_allowlisted_names():
    canonicalizer = Canonicalizer(DeterministicEmbeddingClient())
    notes = []
    # Английская поверхность при русском каноническом имени тоже сверяется со списком.
    broad = [span("agentic AI", "агентный ИИ"), span("artificial intelligence", "искусственный интеллект"),
             span("model", "модель обработки"), span("networks", "сети доверия")]
    assert canonicalizer.with_notes(notes).group({str(i): [s] for i, s in enumerate(broad)}) == []
    assert len(notes) == len(broad) and all("стоп-лист" in note or "широкая" in note for note in notes)
    for name in ("RepoRift", "LoRA", "RAG", "DVQA"):
        result = canonicalizer.group({"doc": [span(name, confidence=0.9, in_title=True)]})
        assert [candidate.name for candidate in result] == [name]


def test_name_filter_preserves_specific_names_and_reports_rejections():
    canonicalizer = Canonicalizer(DeterministicEmbeddingClient())
    notes = []
    invalid = ["ll", "AI", "12345", "12.5", "from", "машинное обучение", "Google Scholar"]
    result = canonicalizer.with_notes(notes).group({
        str(i): [span(name)] for i, name in enumerate(invalid)
    })
    assert result == []
    assert len(notes) == len(invalid)

    class Tagger(SpanTagger):
        def tag(self, docs):
            return [[span(doc.text)] for doc in docs]

    pipeline = ExtractPipeline(Components(tagger=Tagger(), canonicalizer=canonicalizer))
    request = ExtractRequest(documents=[Document(doc_id="filtered", source="test", text="машинное обучение", lang="ru")])
    for _ in range(2):
        response = pipeline.run(request)
        assert response.candidates == []
        assert len(response.coverage.notes) == 1
        assert "машинное обучение" in response.coverage.notes[0]
    assert all(any(repr(name) in note for note in notes) for name in invalid)
    for name in ["BERT", "AI PC", "датчик", "федеративное обучение с гомоморфным шифрованием"]:
        assert canonicalizer.group({"specific": [span(name)]})[0].canonical_ru == name
    assert len(notes) == len(invalid)


def test_short_names_keep_allowlisted_or_confident_title_mentions():
    canonicalizer = Canonicalizer(DeterministicEmbeddingClient())
    for name in ("5G", "RAG", "MCP", "IAM", "LoRA", "ИНС", "BERT", "rag"):
        result = canonicalizer.group({"doc": [span(name, confidence=0.6)]})
        assert len(result) == 1 and result[0].canonical_ru == name
    for confidence, in_title, accepted in ((0.8, True, True), (0.799, True, False), (0.95, False, False)):
        result = canonicalizer.group({"doc": [span("XYZ", confidence=confidence, in_title=in_title)]})
        assert bool(result) is accepted
        if accepted:
            assert result[0].name == result[0].canonical_ru == "XYZ"
    assert canonicalizer.group({"doc": [span("for", confidence=0.99, in_title=True)]}) == []


def test_allowlisted_abbreviation_is_not_filtered_through_its_stoplisted_expansion():
    text = "Post quantum cryptography (PQC) protects bank keys. PQC was piloted."
    start = text.index("PQC was")
    documents = {"doc": Document(doc_id="doc", source="test", text=text, lang="en")}
    notes = []
    result = Canonicalizer(DeterministicEmbeddingClient(), documents=documents).with_notes(notes).group(
        {"doc": [span("PQC", start=start)]}
    )
    assert [candidate.name for candidate in result] == ["PQC"] and notes == []
    # Само раскрытие всё равно широкая область из стоп-листа.
    assert Canonicalizer(DeterministicEmbeddingClient()).group(
        {"doc": [span("post quantum cryptography")]}
    ) == []
