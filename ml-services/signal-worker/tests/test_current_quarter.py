"""Регрессия календаря и системы, а не размеченные данные о качестве метода."""
import copy
from pathlib import Path
import sys
import unittest
from unittest.mock import patch
import uuid

sys.path.insert(0, str(Path(__file__).parents[1]))
import test_manifest as manifest_tests
from signalgraph import method
from signalgraph.emergence import assess
from signalgraph.manifest import canonical, digest, load_input
from signalgraph.pipeline import compute
from signalgraph.temporal import measure


def calendar_input(dates=('2026-09-01', '2026-09-08')):
    manifest, _ = manifest_tests.ManifestTests().fixture()
    owner = str(uuid.uuid5(uuid.NAMESPACE_URL, 'calendar-regression-owner'))
    manifest.update(run_id=str(uuid.uuid5(uuid.NAMESPACE_URL, 'calendar-regression-input')),
                    created_at='2026-09-11T00:00:00Z', access_epoch=1,
                    limitations=['synthetic_calendar_regression_not_scientific_evidence'], excluded=[])
    manifest['request'].update(owner_id=owner,
                               request_id=str(uuid.uuid5(uuid.NAMESPACE_URL, 'calendar-regression-request')),
                               query='retrieval augmented generation', start='2023-01-01',
                               cutoff='2026-09-11T00:00:00Z')
    records, objects = [], {}
    for number, published in enumerate(dates):
        record = copy.deepcopy(manifest['records'][0])
        identifier = str(uuid.uuid5(uuid.NAMESPACE_URL, f'calendar-regression-work-{number}'))
        snapshot_id = str(uuid.uuid5(uuid.NAMESPACE_URL, f'calendar-regression-snapshot-{number}'))
        text = (f'Synthetic calendar fixture {number}. Current retrieval augmented generation methods fail to '
                'track calendar boundaries. We propose a retrieval augmented generation calendar '
                'check. The method improves boundary handling. We evaluate the method on a '
                'controlled calendar task. This is an engineering fixture, not a research result.')
        source = record['source']
        source.update(version=1, native_id=identifier, canonical_id=identifier, content_hash=digest(text.encode()),
                      observed_at='2026-09-10T00:00:00Z',
                      publication={'value':published, 'precision':'day',
                                   'basis':'synthetic calendar regression, not benchmark evidence'})
        snapshot = {'version':1, 'document_id':identifier, 'owner_id':owner, 'snapshot_id':snapshot_id,
                    'model':manifest['model'], 'parser_revision':'parser-v1', 'pipeline_revision':'pipeline-v1',
                    'source':source, 'text_hash':digest(text.encode()),
                    'chunks':[{'id':f'calendar-chunk-{number}', 'index':0, 'text':text,
                               'char_start':0, 'char_end':len(text), 'vector':[1.0,0.5],
                               'metadata':{}, 'lexical_text':text}]}
        snapshot_bytes = canonical(snapshot)
        prefix = f'source-index/v1/{identifier}/{snapshot_id}/'
        snapshot_key, text_key = prefix+digest(snapshot_bytes)+'.json', prefix+digest(text.encode())+'.txt'
        objects[snapshot_key], objects[text_key] = snapshot_bytes, text.encode()
        record.update(id=identifier, title='Synthetic retrieval augmented generation calendar fixture',
                      mime_type='text/plain', source_hash=digest(canonical(source)))
        record['index'].update(version=1, document_id=identifier, owner_id=owner, snapshot_id=snapshot_id,
                               raw_hash=source['content_hash'], raw_key=text_key,
                               data={'key':snapshot_key,'sha256':digest(objects[snapshot_key])},
                               text={'key':text_key,'sha256':digest(objects[text_key])})
        records.append(record)
    manifest['records'] = records
    return manifest, objects


def load_calendar(manifest, objects):
    raw = canonical(manifest)
    return load_input(raw, digest(raw), lambda key, maximum: objects[key])


class CurrentQuarterTests(unittest.TestCase):
    def test_current_quarter_full_compute_preserves_counts_without_ranking(self):
        manifest, objects = calendar_input()
        corpus = load_calendar(manifest, objects)
        members = [work['id'] for work in corpus['works']]
        partition = {'candidates':[{'candidate_id':'calendar-group', 'work_ids':members,
                                    'representative_work_ids':members}], 'unassigned':[]}
        definition = canonical(method.definition())
        job = {'id':'calendar-engineering-job', 'method_sha256':digest(definition)}
        # Подменены только внешние транспорты, все стадии расчёта боевые; настоящий граф вызывает
        # отдельная интеграционная проба.
        with patch('signalgraph.candidates.cluster', return_value=partition), \
                patch('signalgraph.relevance.query_vector', side_effect=OSError('controlled unavailable transport')):
            result = compute(corpus, job, 'unused-unit-transport')
        candidate = result['candidates'][0]
        assessment = candidate['assessment']
        self.assertEqual(assessment['features']['first_age_quarters'], 0)
        self.assertFalse(assessment['eligible'])
        self.assertIsNone(assessment['order'])
        self.assertIn('too_few_scientific_periods', assessment['reasons'])
        self.assertIn('no_recent_scientific_support', assessment['reasons'])
        self.assertIn('comparable_coverage_missing', assessment['reasons'])
        self.assertIn('temporal_basis', candidate['missing_fields'])
        last = candidate['measurements']['series'][-1]
        self.assertEqual(last['period'], '2026-Q3')
        self.assertEqual(last['found']['scientific_works'], 2)
        self.assertFalse(last['complete_period'])
        self.assertIsNone(last['comparable_scientific_works'])
        self.assertFalse(candidate['complete_for_brief'])
        self.assertEqual(result['ranking']['ordered_ids'], [])
        self.assertEqual(result['ranking']['completeness'], 'insufficient_coverage')

    def test_future_publication_is_rejected_before_source_reads(self):
        for published in ('2026-09-12', '2026-10-01'):
            manifest, _ = calendar_input((published, published))
            raw = canonical(manifest)
            with self.subTest(publication=published), self.assertRaisesRegex(ValueError, 'future publication interval'):
                load_input(raw, digest(raw), lambda *_: self.fail('future source reached storage'))

    def test_completed_quarter_and_future_quarter_keep_existing_boundaries(self):
        manifest, objects = calendar_input()
        corpus = load_calendar(manifest, objects)
        members = [work['id'] for work in corpus['works']]
        corpus['input']['request']['cutoff'] = '2026-09-30T23:59:59Z'
        result = assess(measure(corpus, members, []))
        self.assertEqual(result['features']['first_age_quarters'], 1)
        self.assertEqual(result['features']['dated_scientific_works'], 2)
        self.assertFalse(result['eligible'])
        self.assertIn('too_few_scientific_periods', result['reasons'])
        # Даже испорченное вручную измерение не ставит первый источник в квартал позже охваченного периода.
        measurements = measure(corpus, members, [])
        measurements['earliest_scientific']['date']['value'] = '2026-10-01'
        with self.assertRaisesRegex(ValueError, 'follows measured cutoff'):
            assess(measurements)


if __name__ == '__main__':
    unittest.main()
