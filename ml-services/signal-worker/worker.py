"""One bounded analysis job at a time, using the existing DB, S3 and graph services."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import signal
import sys
import threading
import time
import uuid

import grpc
import rpc_transport

from genproto.analysis.v1 import analysis_pb2, analysis_pb2_grpc
from signalgraph import health, method
from signalgraph.manifest import MAX_INPUT, canonical, digest, load_input
from signalgraph.pipeline import compute
from signalgraph.storage import Objects


class Database:
    def __init__(self):
        self.channel = rpc_transport.channel(os.environ['DB_SERVICE_ADDR'], options=[
            ('grpc.max_receive_message_length', 64 << 20), ('grpc.max_send_message_length', 32 << 20)])
        self.stub = analysis_pb2_grpc.AnalysisServiceStub(self.channel)

    def call(self, name: str, payload: dict, timeout: int = 20) -> dict:
        response = getattr(self.stub, name)(analysis_pb2.Envelope(json=canonical(payload).decode()), timeout=timeout)
        return json.loads(response.json)

    def recheck(self, frozen: dict):
        current = self.call('Read', {'run_id': frozen['run_id'], 'reader_id': frozen['reader_id'],
                                     'shared_corpus': frozen['shared_corpus']})
        if any(current[key] != frozen[key] for key in ('run_id', 'sha256', 'artifact', 'bytes')):
            raise ValueError('frozen input differs from current access-checked descriptor')


def process(db: Database, objects: Objects, claimed: dict, method_raw: bytes, stopping: threading.Event) -> dict:
    job, lease, frozen = claimed['job'], claimed['lease'], claimed['input']
    if job['method_sha256'] != digest(method_raw) or job['method_json'].encode() != method_raw:
        raise ValueError('claimed method differs from installed artifact')
    renew_done = threading.Event()
    lease_lost = threading.Event()

    def renew():
        while not renew_done.wait(30):
            try:
                db.call('Renew', lease)
            except Exception:
                lease_lost.set()
                return

    heartbeat = threading.Thread(target=renew, name='analysis-lease', daemon=True)
    heartbeat.start()
    started = time.monotonic()
    try:
        db.recheck(frozen)
        size = frozen['bytes']
        if not isinstance(size, int) or not 1 <= size <= MAX_INPUT:
            raise ValueError('invalid frozen input budget')
        raw_input = objects.read(frozen['artifact'], size)
        if len(raw_input) != size or digest(raw_input) != frozen['sha256']:
            raise ValueError('missing or changed frozen input artifact')
        if job['status'] == 'validated':
            result_raw = job['result_json'].encode()
            if digest(result_raw) != job['result_sha256']:
                raise ValueError('recovered result hash mismatch')
        else:
            corpus = load_input(raw_input, frozen['sha256'], objects.read)
            result_raw = canonical(compute(corpus, job, os.getenv('GRAPH_COMPUTE_ADDR', 'graph-compute:9093')))
        if lease_lost.is_set() or stopping.is_set():
            raise InterruptedError('current computation cannot publish')
        db.recheck(frozen)
        result_key = f"signalgraph/v1/{job['id']}/results/{digest(result_raw)}.json"
        objects.write_verified(f"signalgraph/v1/methods/{job['method_sha256']}.json", method_raw)
        objects.write_verified(result_key, result_raw)
        db.call('Validate', {**lease, 'result_json': result_raw.decode()}, timeout=90)
        if lease_lost.is_set() or stopping.is_set():
            raise InterruptedError('current computation cannot publish')
        published = db.call('Publish', lease, timeout=90)
        result = json.loads(result_raw)
        return {'job_id': job['id'], 'status': published['status'], 'active': published['active'],
                'seconds': round(time.monotonic()-started, 3), 'candidates': len(result['candidates']),
                'ranked': len(result['ranking']['ordered_ids']), 'result_sha256': digest(result_raw)}
    finally:
        renew_done.set()
        heartbeat.join(timeout=21)


def failure(exc: Exception) -> tuple[str, bool]:
    if isinstance(exc, grpc.RpcError):
        if exc.code() == grpc.StatusCode.NOT_FOUND:
            return 'access_changed', False
        if exc.code() in (grpc.StatusCode.INVALID_ARGUMENT, grpc.StatusCode.ABORTED):
            return 'invalid_result', False
        return 'compute_unavailable', True
    if isinstance(exc, (ValueError, KeyError, TypeError)):
        return 'invalid_input', False
    return 'worker_failure', True


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--print-method', action='store_true')
    parser.add_argument('--method', type=Path, default=Path(os.getenv('SIGNALGRAPH_METHOD_FILE', 'method.json')))
    parser.add_argument('--once', action='store_true')
    parser.add_argument('--healthcheck', action='store_true')
    args = parser.parse_args()
    if args.print_method:
        sys.stdout.buffer.write(canonical(method.definition()))
        return 0
    method_raw = args.method.read_bytes()
    method.verify(method_raw)
    if args.healthcheck:
        try:
            health.check()
        except Exception as error:
            print(json.dumps({'ready':False,'exception_type':type(error).__name__}),file=sys.stderr)
            return 1
        print(json.dumps({'ready':True}))
        return 0
    for setting in ('DB_SERVICE_ADDR','GRAPH_COMPUTE_ADDR','EMBEDDINGS_URL','S3_BUCKET'):
        if not os.getenv(setting):
            raise ValueError('missing required setting: '+setting)
    if not args.once:
        health.PROGRESS.unlink(missing_ok=True)
    db, objects = Database(), Objects()
    stop = threading.Event()
    def handle_stop(*_):
        stop.set()
        if not args.once:
            health.PROGRESS.unlink(missing_ok=True)
    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, handle_stop)
    worker_id = str(uuid.uuid4())
    try:
        while not stop.is_set():
            if not args.once:
                health.update(60)
            claimed = db.call('Claim', {'worker_id': worker_id, 'method_sha256': digest(method_raw)})
            if claimed.get('job'):
                if not args.once:
                    health.update(600)
                try:
                    print(json.dumps(process(db, objects, claimed, method_raw, stop)), flush=True)
                except Exception as exc:
                    code, retryable = failure(exc)
                    try:
                        db.call('Fail', {**claimed['lease'], 'code': code, 'retryable': retryable})
                    except grpc.RpcError:
                        pass  # Another attempt owns the job, or DB recovery will expire this lease.
                    print(json.dumps({'job_id': claimed['job']['id'], 'status': 'attempt_failed',
                                      'code': code, 'exception_type': type(exc).__name__}), flush=True)
                    if args.once:
                        return 1
            if args.once:
                return 0
            if not stop.is_set():
                health.update(60)
            stop.wait(3)
    finally:
        if not args.once:
            health.PROGRESS.unlink(missing_ok=True)
        db.channel.close()
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
