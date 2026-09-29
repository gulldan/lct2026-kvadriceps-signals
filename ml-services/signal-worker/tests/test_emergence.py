import copy
from dataclasses import replace
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).parents[1]))
from signalgraph.emergence import assess, Config


def measurements(counts=(0, 0, 1, 1, 1, 2, 2, 3), exposure=100):
    rows = []
    for i, count in enumerate(counts):
        rows.append({'period': f'{2023+i//4}-Q{i%4+1}', 'complete_period': True,
                     'found': {'scientific_works': count}, 'coverage': 'examined_bounded_population',
                     'comparable_scientific_works': count, 'comparable_exposure': exposure,
                     'normalized_activity': count/exposure})
    return {'series': rows, 'earliest_scientific': {'date': {'value': '2023-07-02', 'precision': 'day',
                                                          'basis': 'controlled dated work'}},
            'unallocated_scientific_work_ids': []}


class EmergenceRulesTests(unittest.TestCase):
    def test_sustained_activity_has_reproducible_measured_explanation(self):
        data = measurements()
        result = assess(data)
        self.assertTrue(result['eligible'])
        self.assertEqual(result['features']['prior_count'], 2)
        self.assertEqual(result['features']['recent_count'], 8)
        self.assertGreater(result['features']['log_rate_ratio'], 0)
        self.assertEqual(result, assess(copy.deepcopy(data)))
        self.assertEqual(result['quality_status'], 'not_frozen')

    def test_zero_to_one_cannot_become_supported_emergence(self):
        result = assess(measurements((0, 0, 0, 0, 0, 0, 0, 1)))
        self.assertFalse(result['eligible'])
        self.assertIn('too_few_dated_scientific_works', result['reasons'])
        self.assertIn('too_few_scientific_periods', result['reasons'])
        self.assertIsNone(result['order'])

    def test_new_collection_cannot_supply_a_missing_baseline(self):
        data = measurements()
        for row in data['series'][:4]:
            row.update(coverage='unknown_or_partial', comparable_scientific_works=None,
                       comparable_exposure=None, normalized_activity=None)
        result = assess(data)
        self.assertTrue(result['eligible'])
        self.assertEqual(result['basis'], 'observed_counts')
        self.assertIsNone(result['features']['log_rate_ratio'])
        self.assertGreater(result['features']['log_count_ratio'], 0)
        self.assertEqual(result['order'][:2], [result['features']['log_count_ratio'], 8])
        self.assertIn('comparable_coverage_missing', result['reasons'])

    def test_query_corpus_share_replaces_missing_sweep(self):
        data = measurements()
        for row in data['series']:
            row.update(coverage='query_corpus')
        result = assess(data)
        self.assertTrue(result['eligible'])
        self.assertEqual(result['basis'], 'query_corpus_share')
        self.assertGreater(result['features']['log_rate_ratio'], 0)
        self.assertNotIn('comparable_coverage_missing', result['reasons'])

    def test_flat_counts_without_coverage_are_not_emergence(self):
        data = measurements((1, 1, 1, 1, 1, 1, 1, 1))
        for row in data['series']:
            row.update(coverage='unknown_or_partial', comparable_scientific_works=None,
                       comparable_exposure=None, normalized_activity=None)
        result = assess(data)
        self.assertFalse(result['eligible'])
        self.assertEqual(result['basis'], 'observed_counts')
        self.assertIn('scientific_count_not_increasing', result['reasons'])
        self.assertIsNone(result['order'])

    def test_growth_of_corpus_is_not_mistaken_for_growth_of_share(self):
        data = measurements((1, 1, 1, 1, 2, 2, 2, 2), 100)
        for row in data['series'][4:]:
            row.update(comparable_exposure=400, normalized_activity=.005)
        result = assess(data)
        self.assertLess(result['features']['log_rate_ratio'], 0)
        self.assertFalse(result['eligible'])
        self.assertIn('scientific_share_not_increasing', result['reasons'])
        self.assertIsNone(result['order'])

    def test_stable_scientific_share_is_not_emergence(self):
        result = assess(measurements((1, 1, 1, 1, 1, 1, 1, 1)))
        self.assertEqual(result['features']['log_rate_ratio'], 0)
        self.assertFalse(result['eligible'])
        self.assertIn('scientific_share_not_increasing', result['reasons'])

    def test_mature_and_uncertain_dates_block_eligibility(self):
        data = measurements()
        data['earliest_scientific']['date']['value'] = '2010-01-01'
        self.assertIn('established_scientific_history', assess(data)['reasons'])
        data = measurements()
        data['unallocated_scientific_work_ids'] = ['undated-paper']
        self.assertNotIn('scientific_date_uncertainty', assess(data)['reasons'])
        data['unallocated_scientific_work_ids'] = ['undated-1', 'undated-2', 'undated-3', 'undated-4']
        self.assertFalse(assess(data)['eligible'])

    def test_nonfinite_measurement_and_parameters_fail_closed(self):
        data = measurements()
        data['series'][0]['normalized_activity'] = float('nan')
        with self.assertRaisesRegex(ValueError, 'normalized activity'):
            assess(data)
        with self.assertRaisesRegex(ValueError, 'parameters'):
            assess(measurements(), replace(Config(), smoothing=float('inf')))
