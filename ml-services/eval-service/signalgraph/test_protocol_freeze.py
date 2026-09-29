"""Переписывание протокола не стирает историю: вскрытый holdout остаётся вскрытым."""
import hashlib
import json
import unittest
from pathlib import Path

ROOT = Path(__file__).parent.resolve()

# Хеш текста протокола, под которым holdout уже вскрывался. Значение записано здесь
# намеренно: если история из freeze исчезнет, тест обязан это заметить, а не
# прочитать отсутствие записи как отсутствие проблемы.
OPENED = '4fcb4e881da2c79c218445f69e0097a9c3f73bd9f5619e90323183235ca35662'


class ProtocolFreezeTests(unittest.TestCase):
    def setUp(self):
        self.freeze = json.loads((ROOT / 'protocol-freeze.json').read_text())

    def test_hash_matches_the_current_text(self):
        actual = hashlib.sha256((ROOT / 'protocol-freeze.json').parent.joinpath(
            self.freeze['file']).read_bytes()).hexdigest()
        self.assertEqual(self.freeze['sha256'], actual,
                         'freeze закрепляет не тот текст, который лежит в файле')

    def test_version_stays_v1(self):
        self.assertEqual(self.freeze['version'], 'signalgraph-benchmark-v1')

    def test_opened_holdout_is_still_recorded(self):
        history = self.freeze.get('history') or []
        opened = [entry for entry in history if entry.get('sha256') == OPENED]
        self.assertTrue(opened, 'из freeze исчезла запись о прежней фиксации протокола')
        entry = opened[0]
        self.assertEqual(entry['holdout_status'], 'opened',
                         'вскрытый holdout перестал числиться вскрытым')
        self.assertEqual(entry['frozen_at'], '2026-09-09')
        evaluation = ROOT / entry['holdout_evaluation']
        self.assertTrue(evaluation.is_file(),
                        f'ссылка на вскрытую оценку не открывается: {entry["holdout_evaluation"]}')

    def test_current_holdout_is_not_claimed_sealed(self):
        # Новый holdout ещё не запечатан; объявить его запечатанным без набора
        # значило бы принять приёмку на раскрытых данных.
        self.assertEqual(self.freeze['holdout_status'], 'not_sealed')
