import copy
import math
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).parents[1]))
from signalgraph.ranking import centroid, select


def candidate(identifier, rank=1, complete=True):
    return {'id': identifier, 'work_ids': [identifier+'-1', identifier+'-2'],
            'complete_for_brief': complete, 'relevance': {'relevant': True},
            'brief': {'problem': {'text': 'p'} if complete else None, 'advantage': {'text': 'a'}, 'example': None},
            'assessment': {'eligible': True, 'order': [rank, .1, 2, 2]}}


def representations(candidates, vectors):
    return [{'canonical_id': work, 'vector': vector}
            for c, vector in zip(candidates, vectors) for work in c['work_ids']]


class RankingTests(unittest.TestCase):
    def test_synonyms_use_embeddings_and_cannot_fill_two_positions(self):
        pool = [candidate('a', 3), candidate('unrelated-name', 2), candidate('c', 1)]
        vectors = representations(pool, [[1., 0.], [.999, .01], [0., 1.]])
        result, proof = select(pool, vectors, 'a'*64)
        self.assertEqual([c['id'] for c in result], ['a', 'c'])
        self.assertEqual(proof['suppressed_ids'], ['unrelated-name'])
        changed = copy.deepcopy(pool)
        changed.reverse()
        self.assertEqual(select(changed, list(reversed(vectors)), 'a'*64), (result, proof))

    def test_mmr_promotes_a_distinct_candidate_without_dropping_the_temporal_order(self):
        pool = [candidate(str(i), 20-i) for i in range(20)]
        # Кандидат 1 близок, но ниже порога дубля; кандидат 2 ортогонален.
        vectors = [[1.]+[0.]*20, [.85, math.sqrt(1-.85**2)]+[0.]*19]
        vectors += [[float(i == j) for j in range(21)] for i in range(2, 20)]
        ranked, _ = select(pool, representations(pool, vectors), 'a'*64)
        self.assertEqual([c['id'] for c in ranked[:2]], ['0', '2'])
        self.assertGreater([c['id'] for c in ranked].index('1'), 1)
        self.assertEqual(len(ranked), 15)

    def test_zero_seven_and_incomplete_candidates_are_not_padded(self):
        for count in (0, 7, 15):
            pool = [candidate(str(i)) for i in range(count)]
            vectors = [[float(i == j) for j in range(max(1, count))] for i in range(count)]
            ranked, _ = select(pool, representations(pool, vectors), 'a'*64)
            self.assertEqual(len(ranked), count)
        pool = [candidate('missing-date', complete=False)]
        self.assertEqual(select(pool, [], 'a'*64)[0], [])

    def test_changed_or_missing_vectors_fail_closed(self):
        for values in ([], [[0., 0.]], [[1., 0.], [1.]], [[float('nan')]]):
            with self.assertRaises(ValueError):
                centroid(values)
        with self.assertRaises(KeyError):
            select([candidate('a')], [], 'a'*64)


if __name__ == '__main__':
    unittest.main()
