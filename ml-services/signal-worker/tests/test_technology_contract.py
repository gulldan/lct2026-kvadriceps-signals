from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from genproto.technology.v1 import technology_pb2 as technology


class TechnologyContractTests(unittest.TestCase):
    def result(self):
        mention = technology.TechnologyMention(
            mention_id='mention-1',
            technology=technology.TechnologyRef(technology_id='tech-1'),
            source_revision_id='revision-1',
            extractor_version='extractor-v1',
            schema_version='technology-extraction-v1',
            name='Адаптивная маршрутизация поиска',
            name_kind='descriptive',
            role='proposed_contribution',
            problem='Выбор способа поиска',
            mechanism='Маршрутизация по характеристикам запроса',
            inputs=['Запрос'],
            outputs=['Выбранный способ поиска'],
            extraction_status='complete',
        )
        for i in range(3):
            evidence_id = f'evidence-{i}'
            mention.evidence.append(technology.Evidence(
                evidence_id=evidence_id, mention_id='mention-1', source_revision_id='revision-1',
                locator=technology.Locator(section='3', page=i + 1, start_char=1000 + i, end_char=1100 + i),
                quote=f'Точная подстрока сохранённого источника {evidence_id}',
            ))
            mention.field_evidence.append(
                technology.FieldEvidence(field='mechanism', evidence_ids=[evidence_id]))
        return technology.TechnologyExtractionResult(
            schema_version='technology-extraction-v1', source_revision_id='revision-1',
            extraction_version='extractor-v1',
            coverage=technology.ExtractionCoverage(status='complete', processed_sections=12, total_sections=12),
            technologies=[mention],
        )

    def test_extraction_result_survives_the_wire(self):
        restored = technology.TechnologyExtractionResult()
        restored.ParseFromString(self.result().SerializeToString())
        mention = restored.technologies[0]
        self.assertEqual(len(mention.evidence), 3)
        self.assertEqual(mention.technology.technology_id, 'tech-1')
        self.assertEqual(mention.evidence[2].quote, 'Точная подстрока сохранённого источника evidence-2')
        self.assertEqual(mention.evidence[0].locator.page, 1)
        self.assertEqual(restored.coverage.status, 'complete')

    def test_technology_and_work_references_are_separate_types(self):
        direction = technology.TrendDirection(direction_id='direction-1')
        direction.technologies.append(technology.TechnologyRef(technology_id='tech-1'))
        direction.supporting_works.append(technology.WorkRef(canonical_work_id='work-1'))
        with self.assertRaises(TypeError):
            direction.technologies.append(technology.WorkRef(canonical_work_id='work-1'))

    def test_unavailable_metric_stays_absent(self):
        metric = technology.Metric(name='share', status='unavailable', reason_codes=['denominator_unknown'])
        self.assertFalse(metric.HasField('value'))
        restored = technology.Metric()
        restored.ParseFromString(metric.SerializeToString())
        self.assertFalse(restored.HasField('value'))


if __name__ == '__main__':
    unittest.main()
