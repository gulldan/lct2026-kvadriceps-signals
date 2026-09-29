from concurrent.futures import ThreadPoolExecutor
import json
import os
from pathlib import Path
import tempfile
import time
import unittest

import grpc
from genproto.graph.v1 import graph_pb2

from signalgraph import health


class ReadinessTest(unittest.TestCase):
    def test_unstarted_stalled_and_invalid_progress_fail(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "progress.json"
            with self.assertRaises(FileNotFoundError):
                health.progress(path)
            health.update(60, path)
            health.progress(path)
            for state in [
                {"pid": os.getpid(), "deadline": time.time() - 1},
                {"pid": 0, "deadline": time.time() + 60},
                {"pid": os.getpid(), "deadline": time.time() + 3600},
            ]:
                path.write_text(json.dumps(state))
                with self.assertRaises(ValueError):
                    health.progress(path)

    def test_graph_probe_requires_the_snapshot_rpc_rejection(self):
        for code in [grpc.StatusCode.INVALID_ARGUMENT, grpc.StatusCode.UNIMPLEMENTED, grpc.StatusCode.UNAVAILABLE]:
            with self.subTest(code=code), ThreadPoolExecutor(max_workers=1) as executor:
                server = grpc.server(executor)

                def reject(_request, context):
                    context.abort(code, "bounded readiness test")

                server.add_generic_rpc_handlers(
                    (
                        grpc.method_handlers_generic_handler(
                            graph_pb2.DESCRIPTOR.services_by_name["GraphCompute"].full_name,
                            {"ClusterSnapshot": grpc.unary_unary_rpc_method_handler(reject)},
                        ),
                    )
                )
                port = server.add_insecure_port("127.0.0.1:0")
                server.start()
                try:
                    if code == grpc.StatusCode.INVALID_ARGUMENT:
                        health.graph_ready("127.0.0.1:" + str(port))
                    else:
                        with self.assertRaises(grpc.RpcError):
                            health.graph_ready("127.0.0.1:" + str(port))
                finally:
                    server.stop(0).wait()


if __name__ == "__main__":
    unittest.main()
