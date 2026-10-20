import unittest
from contextlib import ExitStack
from unittest.mock import patch

import worker


class Signatures:
    def __init__(self):
        self.hashes = {
            worker.DOC_SIG_KEY: {b"b": b"sig-b", b"d": b"sig-d"},
            worker.DOC_META_KEY: {b"b": b"meta-b", b"d": b"meta-d"},
        }
        self.reads = []

    def hgetall(self, key):
        self.reads.append("all")
        return dict(self.hashes[key])

    def hget(self, key, field):
        self.reads.append("one")
        return self.hashes[key].get(field.encode())

    def hset(self, key, field, value):
        self.hashes[key][field.encode()] = value.encode()


class IncrementalBacklogTest(unittest.TestCase):
    def test_completed_documents_after_batch_limit_are_not_pending(self):
        documents = [
            {"id": name, "status": "indexed", "updated_at": str(4 - index)}
            for index, name in enumerate("abcd")
        ]
        store = Signatures()
        with ExitStack() as stack:
            for name, value in {
                "VLLM_URL": "test-only", "RAPTOR_INCREMENTAL_BATCH": 1,
                "RAPTOR_INCREMENTAL_WORKERS": 1,
            }.items():
                stack.enter_context(patch.object(worker, name, value))
            stack.enter_context(patch.object(worker, "login"))
            stack.enter_context(patch.object(worker, "gw", return_value=documents))
            stack.enter_context(patch.object(worker, "doc_meta_signature", side_effect=lambda d: "meta-" + d["id"]))
            stack.enter_context(patch.object(worker, "doc_signature", side_effect=lambda d, _: "sig-" + d["id"]))
            chunks = stack.enter_context(patch.object(worker, "fetch_doc_chunks", return_value=[]))
            status = stack.enter_context(patch.object(worker, "report_status"))
            build = stack.enter_context(patch.object(
                worker, "build_doc_summary_candidate",
                side_effect=lambda d, _, sig, meta, epoch: (d["id"], sig, meta, 1, None),
            ))
            self.assertFalse(worker.incremental_raptor(store, 7))
            self.assertEqual(status.call_args.kwargs["progress_total"], 2)
            self.assertTrue(worker.incremental_raptor(store, 7))
            self.assertEqual(status.call_args.kwargs["progress_total"], 1)
            self.assertEqual([call.args[0] for call in chunks.call_args_list], ["a", "c"])
            self.assertEqual(build.call_count, 2)
            self.assertEqual(store.reads, ["all"] * 4)


if __name__ == "__main__":
    unittest.main()
