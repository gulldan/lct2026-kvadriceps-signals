#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
export PYTHONPATH="$root/ml-services/runtime"
ruff check ml-services
# Приёмка радара и настройки стенда проверяются без стенда; вне гейта тест приёмки отстал от её контракта.
# Интерпретатор задан явно: системный python3 3.9 не разбирает даты с суффиксом Z, и гейт зависел от машины.
uv run --no-project --python 3.14 python -m unittest discover -s scripts -p 'test_*.py'
# Контракт извлечения проверяется с потребителем на версии интерпретатора из образа сервиса.
PYTHONPATH="$root/ml-services:$PYTHONPATH" uv run --no-project --python 3.12 \
  --with-requirements ml-services/extractor/requirements-dev.txt \
  python -m pytest ml-services/extractor -q -p no:cacheprovider
for service in cluster-worker discovery-worker raptor-worker; do
  (
    cd "ml-services/$service"
    uv run --no-project --python 3.14 --with-requirements requirements.txt python worker.py --selfcheck
  )
done
for service in eval-service itc-worker; do
  (
    cd "ml-services/$service"
    uv run --no-project --python 3.14 --with-requirements requirements.txt python -c 'import worker'
    if [[ "$service" == eval-service ]]; then
      uv run --no-project --python 3.14 --with-requirements requirements.txt python -m unittest discover -s signalgraph
    else
      uv run --no-project --python 3.14 --with-requirements requirements.txt python -m unittest discover
    fi
  )
done
(
 cd ml-services/cluster-worker
 uv run --no-project --python 3.14 --with-requirements requirements.txt python -m unittest discover
)
(
 cd ml-services/signal-worker
 uv run --no-project --python 3.14 --with-requirements requirements.txt python -m unittest discover -s tests
)
model_python=${MODEL_TEST_PYTHON:-"$root/ml-services/f2llm-service/.venv/bin/python"}
diagram_python=${DIAGRAM_TEST_PYTHON:-"$root/ml-services/diagram-parser-service/.venv/bin/python"}
[[ -x "$model_python" ]] || { echo 'MODEL_TEST_PYTHON must point to the prepared inference environment' >&2; exit 1; }
[[ -x "$diagram_python" ]] || { echo 'DIAGRAM_TEST_PYTHON must point to the prepared diagram environment' >&2; exit 1; }
"$model_python" -m unittest discover -s ml-services/tests -p test_model_runtime.py
"$diagram_python" -m unittest discover -s ml-services/tests -p test_inference_limits.py
(
 cd ml-services/diagram-parser-service
 "$diagram_python" -m unittest discover -s tests
)
"$diagram_python" -c 'import runpy; runpy.run_path("ml-services/paddleocr-vl-service/paddleocr_vl_server.py")'
