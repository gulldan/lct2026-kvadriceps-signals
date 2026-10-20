#!/usr/bin/env bash
# Единый источник контрактов: Go- и Python-стабы из proto/ ложатся в backend/platform/pkg/genproto,
# откуда vendor-platform.sh разносит их по сервисам. Нужны protoc, protoc-gen-go,
# protoc-gen-go-grpc и grpcio-tools; интерпретатор задаёт PYTHON (по умолчанию python3).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROTO_DIR="$ROOT/backend/proto"
PLATFORM="$ROOT/backend/platform"
PYTHON="${PYTHON:-python3}"
export PATH="$(go env GOPATH)/bin:$PATH"

# Проверяем инструменты до замены файлов: неудачная генерация сохраняет старые стабы.
"$PYTHON" - "$ROOT/scripts/proto-requirements.txt" <<'PY'
from importlib.metadata import PackageNotFoundError, version
from pathlib import Path
import sys

for line in Path(sys.argv[1]).read_text().splitlines():
    if not line or line.startswith("#"):
        continue
    package, expected = line.split("==", 1)
    try:
        actual = version(package)
    except PackageNotFoundError:
        actual = "missing"
    if actual != expected:
        raise SystemExit(f"{package}: expected {expected}, found {actual}; install {sys.argv[1]} with {sys.executable}")
PY
OUTPUT="$(mktemp -d "$PLATFORM/.genproto.XXXXXX")"
trap 'rm -rf "$OUTPUT"' EXIT
mkdir -p "$OUTPUT/pkg/genproto"

cd "$PLATFORM"
protoc -I "$PROTO_DIR" \
  --go_out="$OUTPUT" --go_opt=module=github.com/example/rag-mvp \
  --go-grpc_out="$OUTPUT" --go-grpc_opt=module=github.com/example/rag-mvp \
  $(find "$PROTO_DIR" -name '*.proto')

# Межпакетные импорты Python-стабов переписывает vendor-platform.sh, как и Go-импорты.
"$PYTHON" -m grpc_tools.protoc -I "$PROTO_DIR" \
  --python_out="$OUTPUT/pkg/genproto" --grpc_python_out="$OUTPUT/pkg/genproto" \
  $(find "$PROTO_DIR" -name '*.proto')
find "$OUTPUT/pkg/genproto" -mindepth 1 -type d -exec touch {}/__init__.py \;
rm -rf "$PLATFORM/pkg/genproto"
mv "$OUTPUT/pkg/genproto" "$PLATFORM/pkg/genproto"

echo "generated (go):"
find "$PLATFORM/pkg/genproto" -name '*.go' | sed "s#$ROOT/##" | sort
echo "generated (python):"
find "$PLATFORM/pkg/genproto" -name '*.py' | sed "s#$ROOT/##" | sort
