from copy import deepcopy
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from signalgraph.coverage import enrich, VERSION
from signalgraph.manifest import canonical, digest
import test_coverage


class CoverageSweepTests(unittest.TestCase):
    def fixture(self):
        original, _, template = test_coverage.CoverageTests().fixture()
        corpus = {'input': deepcopy(original['input']), 'records': [], 'works': []}
        corpus['input']['pages'] = []
        objects, stored = {}, []
        for i in range(2):
            source = {**template['page']['records'][0]['source'], 'native_id': '2301.0000'+str(i),
                      'content_hash': str(i)*64}
            doc = 'doc'+str(i)
            corpus['records'].append({**deepcopy(original['records'][0]), 'id': doc, 'source': source})
            corpus['works'].append({'id': 'work'+str(i), 'kind': 'paper', 'document_ids': [doc]})
            raw = ('recorded provider response '+str(i)).encode()
            page = {**deepcopy(template['page']), 'records': [{'source': source}], 'provider_total': 2,
                    'cursor': str(i), 'next_cursor': '1' if i == 0 else '', 'exhausted': i == 1,
                    'observed_at': '2023-12-3'+str(i)+'T00:00:00Z', 'raw_sha256': digest(raw)}
            entry = {'page': page, 'prepared': [{'source': source, 'text_status': 'pdf', 'limitations': []}]}
            ref = {**deepcopy(original['input']['pages'][0]), 'run_id': 'batch'+str(i), 'number': 0,
                   'exhausted': i == 1, 'raw': {'key': 'raw'+str(i), 'sha256': digest(raw)},
                   'manifest': {'key': 'manifest'+str(i), 'sha256': ''}}
            corpus['input']['pages'].append(ref)
            objects[ref['raw']['key']] = raw
            stored.append(entry)
        return corpus, objects, stored

    def encode(self, corpus, objects, stored):
        for ref, entry in zip(corpus['input']['pages'], stored, strict=True):
            objects[ref['manifest']['key']] = canonical(entry)
            ref['manifest']['sha256'] = digest(objects[ref['manifest']['key']])

    def test_resumed_batches_prove_one_complete_sweep_in_cursor_order(self):
        corpus, objects, stored = self.fixture()
        self.encode(corpus, objects, stored)
        corpus['input']['pages'].reverse()
        enrich(corpus, lambda key, maximum: objects[key])
        self.assertEqual(len(corpus['coverage_windows']), 1)
        window = corpus['coverage_windows'][0]
        self.assertEqual(window['work_ids'], ['work0', 'work1'])
        self.assertEqual(window['basis']['run_ids'], ['batch0', 'batch1'])
        self.assertEqual(window['basis']['proof_version'], VERSION)
        self.assertEqual([p['key'] for p in window['basis']['page_manifests']], ['manifest0', 'manifest1'])

    def test_incomplete_or_changed_sweeps_never_gain_coverage(self):
        for case in ('missing_head', 'gap', 'branch', 'loop', 'scope_change', 'total_change',
                     'time_reversal', 'rejected', 'prepared_missing', 'source_revision_change',
                     'source_bytes_change', 'source_repeat', 'incomplete_tail', 'nonempty_terminal_cursor'):
            with self.subTest(case=case):
                corpus, objects, stored = self.fixture()
                first, last = stored[0]['page'], stored[1]['page']
                if case == 'missing_head':
                    corpus['input']['pages'].pop(0)
                    stored.pop(0)
                elif case == 'gap':
                    last['cursor'] = '2'
                elif case == 'branch':
                    last['cursor'] = '0'
                elif case == 'loop':
                    last.update(exhausted=False, next_cursor='0')
                    corpus['input']['pages'][1]['exhausted'] = False
                elif case == 'scope_change':
                    last['scope']['query'] = 'different population'
                    corpus['input']['pages'][1]['scope']['query'] = 'different population'
                elif case == 'total_change':
                    last['provider_total'] = 3
                elif case == 'time_reversal':
                    last['observed_at'] = '2023-12-29T00:00:00Z'
                elif case == 'rejected':
                    corpus['input']['pages'][1]['rejected'] = 1
                elif case == 'prepared_missing':
                    stored[1]['prepared'] = []
                elif case.startswith('source_'):
                    source = last['records'][0]['source']
                    source['native_id'] = first['records'][0]['source']['native_id']
                    source['revision'] = 'v2' if case == 'source_revision_change' else 'v1'
                    if case == 'source_repeat':
                        source['content_hash'] = first['records'][0]['source']['content_hash']
                elif case == 'incomplete_tail':
                    last.update(exhausted=False, next_cursor='2')
                    corpus['input']['pages'][1]['exhausted'] = False
                elif case == 'nonempty_terminal_cursor':
                    last['next_cursor'] = '2'
                self.encode(corpus, objects, stored)
                enrich(corpus, lambda key, maximum: objects[key])
                self.assertEqual(corpus['coverage_windows'], [])


if __name__ == '__main__':
    unittest.main()
