"""Синтетические граничные тесты лексического сравнения, а не разметка качества."""
import unittest

from keyword_baseline import cluster


class KeywordBaselineTests(unittest.TestCase):
    def test_source_only_clusters_are_deterministic_disjoint_and_do_not_pad(self):
        sources = [{'canonical_id':str(i), 'title':title, 'text':text} for i,title,text in [
            (1,'voltage electrode electrolyte','battery cathode charge lithium'),
            (2,'voltage electrode electrolyte','battery cathode charge lithium'),
            (3,'enzyme protein folding','molecular amino sequence binding'),
            (4,'enzyme protein folding','molecular amino sequence binding'),
            (5,'galactic lens telescope','nebula astronomy supernova')]]
        result = cluster(sources)
        self.assertEqual(result,cluster(list(reversed(sources))))
        self.assertEqual([c['work_ids'] for c in result['candidates']],
                         [['paper:1','paper:2'],['paper:3','paper:4']])
        self.assertEqual(result['unassigned'],['paper:5'])
        self.assertEqual(cluster([])['candidates'],[])


if __name__ == '__main__':
    unittest.main()
