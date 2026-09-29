"""Названия тем из содержимого документов — отдельно от сети и цикла воркера, чтобы тестировать без них."""

import re
from collections import Counter

# Служебные слова, аффилиации и география: запасное название из них не собирается.
STOPWORDS = {
    "после", "также", "более", "может", "должны", "которые", "between", "which",
    "the", "and", "for", "from", "with", "without", "into", "onto", "over", "under",
    "that", "this", "these", "those", "there", "where", "when", "what", "how", "why",
    "have", "has", "had", "were", "was", "are", "is", "been", "being", "can", "could",
    "should", "would", "will", "may", "might", "must", "all", "any", "each", "some",
    "such", "than", "then", "also", "via", "towards", "toward", "using", "used",
    "your", "our", "their", "its", "his", "her", "along",
    "their", "based", "using", "results", "research", "paper", "study", "article",
    "abstract", "introduction", "method", "methods", "table", "figure",
    "university", "department", "school", "faculty", "institute", "center", "centre",
    "laboratory", "lab", "hospital", "clinic", "author", "authors", "published",
    "conference", "journal", "china", "germany", "denmark", "taiwan", "arizona",
    "heidelberg", "beijing", "tsinghua", "peking", "medical", "medicine", "image",
    "images", "imaging", "model", "models", "learning", "deep", "network", "networks",
    "framework", "approach", "task", "tasks", "data", "dataset", "datasets",
    "performance", "evaluation", "benchmark", "efficient",
    "название", "аннотация", "ключевые", "слова", "группа", "работ",
    "technion", "israel", "haifa", "stanford", "edu", "dept", "cse", "pes",
    "bengaluru", "india", "rwth", "aachen", "monash", "melbourne", "gmail",
    "com", "labs", "cave", "technology", "hong", "kong", "rio", "janeiro",
    "coppe", "federal", "usa", "united", "states", "massachusetts", "cambridge",
    "atlanta", "emory", "montreal", "canada", "zagreb", "croatia", "zju",
    "zhejiang", "hangzhou", "evanston", "francisco", "london", "king", "college",
    "chengdu", "cityu", "dkfz", "german", "los", "alamos", "aramco", "saudi",
    "concordia", "software", "computer", "science",
    # Поля библиографической карточки и остатки разметки: у документов, чей текст — карточка Crossref,
    # запасная разметка иначе собирает темы вида «Crossref Record Jats Jats».
    "crossref", "jats", "record", "records", "doi", "url", "issn", "isbn",
    "publisher", "venue", "source", "volume", "issue", "pages", "license",
    "copyright", "elsevier", "springer", "wiley", "taylor", "francis", "mdpi",
    "ieee", "acm", "preprint", "arxiv", "pubmed", "scopus",
}
GENERIC_LABELS = {
    "medical", "medicine", "imaging", "image", "images", "learning", "university",
    "department", "china", "germany", "clinical", "healthcare", "multimodal",
    "vision", "model", "models", "framework", "benchmark", "technology", "research",
    "тема", "медицина", "изображения", "обучение", "университет", "модель", "модели",
}


def is_generic_label(label):
    norm = re.sub(r"[^a-zа-я0-9 ]+", " ", (label or "").lower())
    words = [w for w in norm.split() if w]
    if not words:
        return True
    if len(words) == 1 and words[0] in GENERIC_LABELS:
        return True
    generic_hits = sum(1 for w in words if w in GENERIC_LABELS or w in STOPWORDS)
    return generic_hits >= max(1, len(words) - 1)


def phrase_candidates(signals):
    text = " ".join(signals).lower()
    text = re.sub(r"[^a-zа-я0-9+ -]+", " ", text)
    tokens = [
        t for t in text.split()
        if len(t) >= 3
        and not t.isdigit()
        and not re.fullmatch(r"\d{4,}v\d+", t)
        and not re.fullmatch(r"[a-z]+\d{4,}v?\d*", t)
        and t not in STOPWORDS
    ]
    counts = Counter()
    for n in (4, 3, 2):
        for i in range(0, max(0, len(tokens) - n + 1)):
            phrase = tokens[i:i + n]
            if phrase[0] in STOPWORDS or phrase[-1] in STOPWORDS:
                continue
            if len(set(phrase)) < min(2, n):
                continue
            counts[" ".join(phrase)] += 1
    return [p for p, _ in counts.most_common(8)]


def keyword_fallback(signals):
    """Название из частых фраз самого кластера, когда модель недоступна. Доменного словаря нет
    намеренно: подогнанный под один датасет, на другом корпусе он даёт мусорные названия."""
    text = " ".join(signals).lower()
    phrases = phrase_candidates(signals)
    if phrases:
        label = " / ".join(p.title() for p in phrases[:2])
        return label, f"Группа работ объединена вокруг тем: {', '.join(phrases[:3])}.", phrases[:6]
    words = [w for w in re.findall(r"[А-Яа-яA-Za-z]{5,}", text) if w not in STOPWORDS]
    top = [w for w, _ in Counter(words).most_common(6)]
    label = " ".join(w.title() for w in top[:3]) if top else next(iter(signals), "Тематическая группа")[:100]
    return label, "", top


# У карточек Crossref аннотация лежит внутри <jats:p>: без снятия тегов модель и запасная разметка
# принимают их за слова темы.
MARKUP = re.compile(r"</?[A-Za-z][A-Za-z0-9:_.-]*\s*[^>]*>")


def strip_markup(text):
    return MARKUP.sub(" ", text)


def content_signals(signals):
    """Из библиографической записи — только название и аннотация, без полей, тегов и ссылок."""
    signals = [strip_markup(str(signal)) for signal in signals]
    field = r"(?:title|abstract|summary)"
    boundary = r"(?:title|language|source(?: type| venue)?|publication date|doi|url|pdf url|publisher|matched query|authors|subjects|abstract|summary)"
    pattern = rf"(?is)\b{field}\s*:\s*(.*?)(?=\b{boundary}\s*:|$)"
    cleaned = []
    for signal in signals:
        parts = [part.strip() for part in re.findall(pattern, signal) if part.strip()]
        cleaned.extend(parts or [signal])
    return list(dict.fromkeys(cleaned))


