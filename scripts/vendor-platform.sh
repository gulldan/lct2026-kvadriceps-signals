#!/usr/bin/env bash
# Копирует нужную сервису часть backend/platform/pkg в его internal/platform с переписанными
# импортами, чтобы каждый сервис оставался самостоятельным модулем. Перезапускать после правки
# platform/pkg или gen-proto.sh. Запуск: bash scripts/vendor-platform.sh [сервис ...] (по умолчанию все)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PKGSRC="backend/platform/pkg"
PYGENSRC="$PKGSRC/genproto"
OLD_MODULE="github.com/example/rag-mvp"
ORG="github.com/example" # заглушка: перед публикацией заменить на путь реального репозитория

vendor() {
  local dir="$1"; shift
  local pkgs="$*"
  local name; name="$(basename "$dir")"
  local mod="$ORG/$name"

  echo ">>> $name  (module $mod)"
  rm -rf "$dir/internal/platform"
  mkdir -p "$dir/internal/platform"
  for p in $pkgs; do
    cp -R "$PKGSRC/$p" "$dir/internal/platform/$p"
  done
  # Python-стабы из genproto нужны только Python-сервисам.
  find "$dir/internal/platform" -name '*.py' -delete

  # Кавычка в шаблоне ограничивает замену строками импорта: .pb.go хранят путь модуля ещё и в
  # сериализованном FileDescriptorProto (после тега «Z»), и его правка сдвинет префиксы длины —
  # каждый бинарь упадёт при init с «slice bounds out of range».
  find "$dir" -name '*.go' -print0 | xargs -0 perl -pi -e \
    "s|\"\Q$OLD_MODULE/pkg/\E|\"$mod/internal/platform/|g;"

  ( cd "$dir"
    [ -f go.mod ] || go mod init "$mod" >/dev/null 2>&1
    go mod edit -go="$(go -C "$ROOT/backend/platform" list -m -f '{{.GoVersion}}')"
    go mod tidy
  )
}

# Python-сервисам — только .py-стабы в локальный пакет genproto/; межпакетные импорты
# `from <pkg>.v1` переписываются в `from genproto.<pkg>.v1`, как Go-импорты в vendor().
vendor_python() {
  local dir="$1"
  local name; name="$(basename "$dir")"

  echo ">>> $name  (python genproto)"
  rm -rf "$dir/genproto"
  while IFS= read -r f; do
    local rel="${f#"$PYGENSRC"/}"
    mkdir -p "$dir/genproto/$(dirname "$rel")"
    cp "$f" "$dir/genproto/$rel"
  done < <(find "$PYGENSRC" -name '*.py')
  : >"$dir/genproto/__init__.py"

  find "$dir/genproto" -name '*.py' -print0 | xargs -0 perl -pi -e \
    's|^from (\w+)\.v1 import |from genproto.$1.v1 import |;'
}

# Списки уже включают зависимости пакетов platform друг от друга.
pkgs_for() {
  case "$1" in
    source-harvester) echo "sourcedoc harvest config logger contracts genproto grpcx observability storage messaging" ;;
    db-service)     echo "analysisdoc indexdoc extractdoc config logger contracts genproto grpcx jsonx observability sourcedoc harvest storage messaging" ;;
    auth)           echo "config logger contracts genproto grpcx indexdoc sourcedoc jsonx httpx jwt valkey dbclient observability" ;;
    main-service)   echo "analysisdoc indexdoc extractdoc harvest sourcedoc config logger contracts genproto grpcx jsonx httpx jwt storage messaging dbclient ragclient valkey llmusage runtimecfg observability aiclients openalex" ;;
    llm-service)    echo "sourcedoc indexdoc dbclient config logger contracts genproto grpcx jsonx valkey vectorstore searchstore aiclients llmusage runtimecfg observability" ;;
    pdf-parser)     echo "ingestion sourcedoc indexdoc config logger contracts genproto grpcx dbclient messaging storage observability" ;;
    office-parser)  echo "ingestion sourcedoc indexdoc config logger contracts genproto grpcx dbclient messaging storage observability" ;;
    db-parser)      echo "ingestion sourcedoc indexdoc config logger contracts genproto grpcx dbclient messaging storage observability" ;;
    email-parser)   echo "ingestion sourcedoc indexdoc config logger contracts genproto grpcx dbclient messaging storage observability" ;;
    ocr-service)    echo "ingestion sourcedoc indexdoc config logger contracts genproto grpcx jsonx dbclient messaging storage aiclients observability" ;;
    vlm-service)    echo "ingestion sourcedoc indexdoc config logger contracts genproto grpcx jsonx dbclient messaging storage aiclients observability" ;;
    chunk-splitter) echo "sourcedoc indexdoc config logger contracts genproto grpcx jsonx dbclient messaging storage vectorstore searchstore aiclients valkey llmusage observability" ;;
    archive-worker) echo "sourcedoc indexdoc config logger contracts genproto grpcx jsonx dbclient messaging storage observability" ;;
    *) echo "" ;;
  esac
}

GO_SERVICES="backend/db-service backend/auth backend/main-service backend/llm-service backend/pdf-parser backend/office-parser backend/db-parser backend/email-parser backend/ocr-service backend/vlm-service backend/chunk-splitter backend/archive-worker backend/source-harvester"
PY_SERVICES="ml-services/cluster-worker ml-services/discovery-worker ml-services/raptor-worker ml-services/signal-worker"

SERVICES="$GO_SERVICES $PY_SERVICES"
[ "$#" -gt 0 ] && SERVICES="$*"

for s in $SERVICES; do
  case " $PY_SERVICES " in
    *" $s "*) vendor_python "$s"; continue ;;
  esac
  set=$(pkgs_for "$(basename "$s")")
  case " $set " in
    *" dbclient "*)
      case " $set " in
        *" analysisdoc "*) ;;
        *) set="analysisdoc $set" ;;
      esac
      ;;
  esac
  [ -z "$set" ] && { echo "unknown service: $s" >&2; exit 1; }
  vendor "$s" $set
done
echo "=== done ==="
