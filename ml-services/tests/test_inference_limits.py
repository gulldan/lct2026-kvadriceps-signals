import asyncio
import io
import threading
import unittest

from fastapi import HTTPException
from inference_limits import InferenceGate, inference_slot, validate_image
from PIL import Image


class InputBudgetTest(unittest.TestCase):
    def test_streamed_body_is_bounded_before_application_and_capacity_is_released(self):
        calls = []

        async def app(scope, receive, send):
            calls.append((await receive())["body"])

        gate = InferenceGate(app, max_bytes=4, capacity=1)
        scope = {"type": "http", "method": "POST", "headers": []}

        async def run(chunks):
            sent = []

            async def receive():
                chunk = chunks.pop(0)
                return {"type": "http.request", "body": chunk, "more_body": bool(chunks)}

            async def send(value):
                sent.append(value)

            await gate(scope, receive, send)
            return sent

        response = asyncio.run(run([b"123", b"45"]))
        self.assertEqual(response[0]["status"], 413)
        self.assertFalse(calls)
        asyncio.run(run([b"1234"]))
        self.assertEqual(calls, [b"1234"])
        gate.slots.acquire()
        self.assertEqual(asyncio.run(run([b"x"]))[0]["status"], 503)
        gate.slots.release()

    def test_image_headers_reject_amplification_and_busy_model_has_deadline(self):
        buffer = io.BytesIO()
        Image.new("1", (10000, 5000)).save(buffer, format="PNG")
        with self.assertRaises(HTTPException) as rejected:
            validate_image(buffer.getvalue())
        self.assertEqual(rejected.exception.status_code, 413)
        lock = threading.Lock()
        lock.acquire()
        with self.assertRaises(HTTPException) as busy, inference_slot(lock):
            self.fail("busy model entered")
        self.assertEqual(busy.exception.status_code, 503)
        lock.release()


if __name__ == "__main__":
    unittest.main()
