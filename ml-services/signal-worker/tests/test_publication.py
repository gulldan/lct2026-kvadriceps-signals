"""Пробы отказов управляющего контура — отдельно от приёмки качества на эталоне."""
from pathlib import Path
import sys
import threading
import unittest
from unittest.mock import patch

import grpc

sys.path.insert(0, str(Path(__file__).parents[1]))
import worker
from signalgraph.manifest import canonical, digest


class Deadline(grpc.RpcError):
    def code(self):
        return grpc.StatusCode.DEADLINE_EXCEEDED


class PublicationTests(unittest.TestCase):
    def test_timeout_cancel_and_changed_input_never_publish(self):
        method = canonical({'method': 'controlled boundary fixture'})
        raw = canonical({'frozen': 'input'})
        claim = {'job': {'id': 'job', 'status': 'prepared', 'method_sha256': digest(method),
                         'method_json': method.decode()}, 'lease': {'job_id': 'job', 'token': 'lease'},
                 'input': {'bytes': len(raw), 'sha256': digest(raw), 'artifact': 'input'}}
        result = {'ranking': {'ordered_ids': []}, 'candidates': []}
        for scenario in ('timeout','cancel','stale-input'):
            calls = []
            stop = threading.Event()
            class Database:
                checks = 0
                def recheck(self, _):
                    self.checks += 1
                    if scenario == 'stale-input' and self.checks == 2:
                        raise ValueError('reader access changed after computation')
                def call(self, name, *_args, **_kwargs):
                    calls.append(name)
                    return {'status':'published','active':False}
            class Objects:
                def read(self, *_):
                    return raw
                def write_verified(self, *_):
                    calls.append('write')
            def compute(*_):
                if scenario == 'timeout':
                    raise Deadline()
                if scenario == 'cancel':
                    stop.set()
                return result
            with self.subTest(scenario=scenario), patch.object(worker,'load_input',return_value={}), \
                    patch.object(worker,'compute',side_effect=compute):
                with self.assertRaises((grpc.RpcError, InterruptedError, ValueError)):
                    worker.process(Database(),Objects(),claim,method,stop)
                self.assertNotIn('Publish',calls)
                self.assertNotIn('Validate',calls)
                self.assertNotIn('write',calls)
        self.assertEqual(worker.failure(Deadline()), ('compute_unavailable',True))


if __name__ == '__main__':
    unittest.main()
