import copy
from dataclasses import asdict
import hashlib
import json
from pathlib import Path
import random
import sys
import unittest

sys.path.insert(0, str(Path(__file__).parents[1]))
from signalgraph.identity import canonical_works, counting_units
from signalgraph.relations import paper_repository_links

FIXTURE = Path(__file__).parents[3]/'backend/platform/pkg/sourcedoc/testdata/v1/corpus.json'
CUTOFF = '2026-08-31T23:59:59Z'


def records():
    return json.loads(FIXTURE.read_text())['records']


class RelationsTests(unittest.TestCase):
    def test_frozen_explicit_link_and_separate_units(self):
        data = records()
        works = canonical_works(data)
        links = paper_repository_links(data, works, CUTOFF)
        self.assertEqual(len(links), 1)
        link = links[0]
        paper = next(w for w in works if 'arxiv-v1' in w.document_ids)
        repository = next(w for w in works if 'repo-original' in w.document_ids)
        self.assertEqual((link.paper_id, link.repository_id), (paper.id, repository.id))
        self.assertEqual(link.repository_root, 'github:101')
        self.assertFalse(link.official)
        self.assertIsNone(link.company)
        self.assertIsNone(link.production_use)
        self.assertEqual({e.direction for e in link.evidence}, {'readme_to_paper', 'paper_to_repository'})
        self.assertTrue({'arxiv-v1', 'repo-original'} <= {e.document_id for e in link.evidence})
        self.assertNotIn('repo-new-readme', {e.document_id for e in link.evidence})
        self.assertNotIn('false-paper-link', {e.document_id for e in link.evidence})
        self.assertEqual(counting_units(works)['canonical_scientific_works'], 14)
        by_id = {r['id']: r for r in data}
        for e in link.evidence:
            text = by_id[e.document_id]['text']
            self.assertEqual(text[e.char_start:e.char_end], e.quote)
            self.assertEqual(hashlib.sha256(text.encode()).hexdigest(), e.text_sha256)
        random.Random(42).shuffle(data)
        self.assertEqual([asdict(x) for x in links], [asdict(x) for x in paper_repository_links(data, canonical_works(data), CUTOFF)])

    def test_ambiguous_url_without_reciprocal_reference_proves_nothing(self):
        data = [r for r in records() if r['id'] not in ('repo-original','repo-renamed','repo-new-readme')]
        self.assertEqual(paper_repository_links(data, canonical_works(data), CUTOFF), [])

    def test_one_repository_citing_two_papers_does_not_merge_papers(self):
        data = [r for r in records() if r['id'] in ('arxiv-v1','repo-original')]
        other = copy.deepcopy(data[0])
        other['id'] = 'separate-paper'
        other['source'].update(native_id='2502.11111',canonical_id='separate')
        other['source']['arxiv'].update(base_id='2502.11111',doi='10.5555/separate')
        other['text'] = 'A separate scientific technique.'
        data.append(other)
        repo = next(r for r in data if r['id']=='repo-original')
        repo['text'] += ' Related work: https://doi.org/10.5555/separate .'
        works = canonical_works(data)
        self.assertEqual(len(paper_repository_links(data, works, CUTOFF)), 2)
        self.assertEqual(counting_units(works)['canonical_scientific_works'], 2)

    def test_future_readme_cannot_supply_historical_link(self):
        data = [r for r in records() if r['id'] in ('arxiv-v1','repo-original')]
        data[0]['text'] = 'No code link in the paper.'
        repo = next(r for r in data if r['id']=='repo-original')
        repo['source']['observed_at'] = '2026-09-02T00:00:00Z'
        self.assertEqual(paper_repository_links(data, canonical_works(data), CUTOFF), [])

    def test_hash_and_malformed_identifier_rejected(self):
        data = [r for r in records() if r['id'] in ('arxiv-v1','repo-original')]
        repo = next(r for r in data if r['id']=='repo-original')
        repo['text_sha256'] = '0'*64
        with self.assertRaises(ValueError):
            paper_repository_links(data, canonical_works(data), CUTOFF)
        repo.pop('text_sha256')
        repo['text'] = 'See doi:invalid and https://arxiv.org/abs/garbage'
        data[0]['text'] = 'No code link.'
        self.assertEqual(paper_repository_links(data, canonical_works(data), CUTOFF), [])


if __name__ == '__main__':
    unittest.main()
