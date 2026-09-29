"""One versioned explanation/parameter artifact, shared with the Go API."""
from __future__ import annotations

from dataclasses import asdict
from pathlib import Path

from . import brief, candidates, coverage, emergence, manifest, ranking, relevance


def implementation_hash() -> str:
    root = Path(__file__).resolve().parent
    files = {path.name: manifest.digest(path.read_bytes()) for path in sorted(root.glob('*.py'))}
    files['../worker.py'] = manifest.digest((root.parent / 'worker.py').read_bytes())
    files['../requirements.txt'] = manifest.digest((root.parent / 'requirements.txt').read_bytes())
    return manifest.digest(manifest.canonical(files))


def definition() -> dict:
    return {
        'version': 'signalgraph-method-v6-development-brief-context', 'quality_status': 'not_frozen',
        'code_sha256': implementation_hash(),
        'parameters': {'versions': manifest.VERSIONS, 'candidates_version': candidates.VERSION,
                       'coverage_version': coverage.VERSION,
                       'coverage_page_limit': coverage.MAX_COVERAGE_PAGES,
                       'coverage_object_byte_limit': coverage.MAX_COVERAGE_OBJECT,
                       'clustering': candidates.CONFIG, 'emergence_version': emergence.VERSION,
                       'emergence': asdict(emergence.Config()), 'brief_version': brief.VERSION,
                       'ranking_limit': 15, 'ranking_version': ranking.VERSION, 'brief_works_examined': brief.MAX_BRIEF_WORKS, 'ranking_min_brief_fields': ranking.MIN_BRIEF_FIELDS,
                       'ranking_lambda': ranking.LAMBDA, 'ranking_duplicate_threshold': ranking.DUPLICATE_THRESHOLD,
                       'search_version': relevance.VERSION,
                       'search_match': 'all_required_query_parts_on_one_work; explicit_known_domain_for_or_dlya_only; '
                                       'modifier_literal_phrase_or_semantics',
                       'semantic_relevance_threshold': relevance.SIMILARITY_THRESHOLD},
        'explanation': {
            'title': 'Как определяется зарождающаяся тема',
            'status': 'Экспериментальная методика. Качество на отложенной выборке еще не принято.',
            'definition': 'Тема объединяет несколько разных научных работ. Временное основание требует '
                          'нескольких полных периодов, сопоставимого обследованного корпуса и роста доли публикаций.',
            'counts': 'Научная работа считается один раз; версии и копии не добавляют поддержку. '
                      'Репозитории и релизы считаются отдельно. Stars не влияют на ранг.',
            'earliest': 'Первое упоминание — самое раннее найденное в обследованном наборе. '
                        'Это не утверждение о мировом первенстве.',
            'history': 'Ряд показывает ретроспективное распределение работ текущей темы. '
                       'Неизвестное покрытие остается пропуском, а не нулем.',
            'formula': 'rate = (papers + smoothing) / (exposure + 2 × smoothing); '
                       'change = ln(recent_rate / prior_rate). Окна и пороги находятся в parameters.emergence.',
            'order': 'Исходный порядок: изменение доли, недавняя доля, активные кварталы и работы. '
                     'MMR сочетает этот порядок с отличием эмбеддингов от уже выбранных тем (вес 0,75). '
                     'Косинусная близость от 0,92 исключает повтор; при равенстве — стабильный ID. '
                     'Пороги экспериментальные. Релевантность запросу проверяется отдельно.',
            'brief': 'Проблема, преимущество и пример содержат точные цитаты одного исследования. '
                     'Неполное численное сравнение допускает только самостоятельную качественную часть '
                     'исходной фразы; иначе поле остается неизвестным. Это заявления авторов '
                     'в пределах исследования, а не доказательство применения в производстве. '
                     'Ссылки на предыдущую фразу сохраняют необходимый контекст. '
                     'Сравнительные преимущества требуют указания на полученный результат.',
            'limitations': ['Разметка benchmark выполнена Codex по первичным источникам.',
                            'Качество кластеризации и извлечения утверждений еще оценивается.',
                            f'История сбора проверяется целиком в пределах {coverage.MAX_COVERAGE_PAGES} страниц '
                            'и остатка бюджета чтения. '
                            'При превышении предела полнота покрытия не подтверждается; '
                            'проверенные источники остаются в анализе.',
                            'Неполный сбор не позволяет заявить рост всей области.',
                            'В рейтинг входят темы хотя бы с двумя из трёх полей обоснования; '
                            'недостающее поле показывается как не найденное.',
                            'При недоборе показывается фактическое число полных тем.'],
        },
    }


def verify(raw: bytes) -> dict:
    import json
    method = json.loads(raw)
    if raw != manifest.canonical(definition()):
        raise ValueError('method artifact differs from installed code or configuration')
    return method
