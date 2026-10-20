"""Обязательная интеграционная проверка не может завершиться пропуском всех тестов."""
import json
import sys

passed = set()
for line in sys.stdin:
    event = json.loads(line)
    if event.get("Output"):
        print(event["Output"], end="")
    if event.get("Action") == "skip" and event.get("Test"):
        raise SystemExit(f"required integration skipped: {event['Test']}")
    if event.get("Action") == "fail":
        raise SystemExit("required integration failed")
    if event.get("Action") == "pass" and event.get("Test"):
        passed.add(event["Test"])
missing = set(sys.argv[1:]) - passed
if missing:
    raise SystemExit("required integration tests did not run: " + ", ".join(sorted(missing)))
if not passed:
    raise SystemExit("no required integration tests ran")
print(f"Required integrations passed: {len(passed)}")
