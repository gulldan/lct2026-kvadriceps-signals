import copy
import json
from pathlib import Path
import random
import sys
import unittest

sys.path.insert(0, str(Path(__file__).parents[1]))
from signalgraph.identity import canonical_works, counting_units, normalize_doi

FIXTURE = Path(__file__).parents[3] / 'backend/platform/pkg/sourcedoc/testdata/v1/corpus.json'


class IdentityTests(unittest.TestCase):
    def test_frozen_oracle_and_permutation(self):
        corpus = json.loads(FIXTURE.read_text())
        records = corpus['records']
        works = canonical_works(records)
        counts = counting_units(works)
        for key in ['canonical_scientific_works', 'independent_repositories', 'patent_publications']:
            self.assertEqual(counts[key], corpus['oracle'][key])
        same = next(work for work in works if 'arxiv-v1' in work.document_ids)
        self.assertEqual(set(same.document_ids), set(corpus['oracle']['same_work']))
        random.Random(41).shuffle(records)
        self.assertEqual(works, canonical_works(records))

    def test_doi_forms_and_copy_representation(self):
        self.assertEqual(normalize_doi(' HTTPS://DX.DOI.ORG/10.5555/WORK '), '10.5555/work')
        self.assertIsNone(normalize_doi('10.5555/malformed doi'))
        records = json.loads(FIXTURE.read_text())['records'][:2]
        records[1]['text'] = records[0]['text']
        records[1]['source']['content_hash'] = records[0]['source']['content_hash']
        # Одинаковые метаданные не склеивают работы с разными исходными ID.
        self.assertEqual(len(canonical_works(records)), 2)
        for record in records:
            record['representation'] = 'full_text'
        self.assertEqual(len(canonical_works(records)), 1)

    def test_patent_country_and_confirmed_family_counts(self):
        source = json.loads(FIXTURE.read_text())['records'][-1]
        records = []
        for country in ['US', 'EP', 'JP']:
            record = copy.deepcopy(source)
            record['id'] = country
            record['source']['native_id'] = country + '123A1'
            record['source']['canonical_id'] = country + '123A1'
            record['confirmed_patent_family'] = {'id': 'F-1', 'provider': 'control', 'basis': 'explicit family record'}
            records.append(record)
        counts = counting_units(canonical_works(records))
        self.assertEqual(counts['patent_publications'], 3)
        self.assertEqual(counts['confirmed_patent_families'], 1)
        records[0]['confirmed_patent_family']['basis'] = ''
        with self.assertRaises(ValueError):
            canonical_works(records)

    def test_unknown_fork_is_not_independent(self):
        record = next(record for record in json.loads(FIXTURE.read_text())['records'] if record['id'] == 'repo-fork-a')
        record['source']['github']['root_id'] = None
        self.assertEqual(counting_units(canonical_works([record]))['independent_repositories'], 0)

    def test_canonical_url_requires_basis(self):
        record = json.loads(FIXTURE.read_text())['records'][0]
        record['canonical_url'] = 'https://example.org/paper?utm_source=news'
        with self.assertRaises(ValueError):
            canonical_works([record])


if __name__ == '__main__':
    unittest.main()
