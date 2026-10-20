#!/usr/bin/env bash
# Перенос контура на другую машину: выгрузка данных и связанных с ними секретов,
# обратная загрузка и проверка целостности.
#
# Тома копируются архивом как есть, а не логическим дампом: версии Postgres,
# Qdrant и OpenSearch закреплены в том же compose, который переезжает вместе с
# данными, поэтому один механизм покрывает все тома и не расходится с ними по
# версиям. Цена — выгрузку делаем на остановленном контуре.
set -euo pipefail

PROJECT=rag-platform
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
ENV_FILE=${ENV_FILE:-$ROOT/infra/.env}
MODELS_DIR=${MODELS_DIR:-$ROOT/models}

CORE_VOLUMES="pgdata qdrantdata osdata seaweedfsdata"
CACHE_VOLUMES="arxivdata patentdata cnpatentdata webdata rabbitmqdata valkeydata"
OPS_VOLUMES="promdata grafanadata"

# Ключи, которые обязаны переехать вместе с данными: пароль Postgres лежит
# внутри pgdata, ключи S3 — внутри seaweedfsdata, хеш администратора — в базе,
# а имя модели эмбеддингов определяет, чем посчитаны векторы в Qdrant. Новое
# значение любого из них делает восстановленные данные недоступными.
LINKED_KEYS="POSTGRES_PASSWORD RABBITMQ_USER RABBITMQ_PASSWORD S3_ACCESS_KEY S3_SECRET_KEY JWT_SECRET ADMIN_USERNAME ADMIN_PASSWORD EMBEDDINGS_MODEL F2LLM_MODEL EMBEDDINGS_REVISION"

die() { printf 'ошибка: %s\n' "$*" >&2; exit 1; }
say() { printf '%s\n' "$*" >&2; }

command -v docker >/dev/null 2>&1 || die "docker не установлен"

if command -v zstd >/dev/null 2>&1; then
  COMPRESS="zstd -T0 -3 -q"
  DECOMPRESS="zstd -dcq"
  EXT="tar.zst"
else
  COMPRESS="gzip -1"
  DECOMPRESS="gzip -dc"
  EXT="tar.gz"
fi

sha_of() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
  else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

volume_exists() { docker volume inspect "${PROJECT}_$1" >/dev/null 2>&1; }

volume_empty() {
  [ "$(docker run --rm -v "${PROJECT}_$1":/v busybox sh -c 'ls -A /v | head -1' 2>/dev/null)" = "" ]
}

usage() {
  cat >&2 <<'USAGE'
Использование:
  bundle.sh export КАТАЛОГ [--with-cache] [--with-ops] [--with-models] [--keep-running]
  bundle.sh import КАТАЛОГ [--force]
  bundle.sh verify КАТАЛОГ

export  — остановить контур и выгрузить тома, секреты и опись в КАТАЛОГ.
          По умолчанию берутся только данные продукта: pgdata, qdrantdata,
          osdata, seaweedfsdata. Кеши сборщиков и метрики — флагами.
import  — восстановить тома из КАТАЛОГА и внести связанные ключи в infra/.env.
verify  — сверить контрольные суммы архивов с описью.
USAGE
  exit 2
}

cmd_export() {
  local out=$1; shift
  local volumes="$CORE_VOLUMES" with_models=0 keep_running=0
  while [ $# -gt 0 ]; do
    case $1 in
      --with-cache) volumes="$volumes $CACHE_VOLUMES" ;;
      --with-ops) volumes="$volumes $OPS_VOLUMES" ;;
      --with-models) with_models=1 ;;
      --keep-running) keep_running=1 ;;
      *) die "неизвестный флаг $1" ;;
    esac
    shift
  done
  mkdir -p "$out"
  out=$(cd "$out" && pwd)

  if [ "$keep_running" = 0 ]; then
    local running
    running=$(docker ps -q --filter "label=com.docker.compose.project=$PROJECT")
    if [ -n "$running" ]; then
      say "останавливаю контур: копия работающих томов непоследовательна"
      docker stop $running >/dev/null
    fi
  else
    say "ВНИМАНИЕ: контур не остановлен, копия может оказаться непоследовательной"
  fi

  : > "$out/checksums.txt"
  local manifest="$out/manifest.txt"
  {
    echo "снято: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "хост: $(hostname)"
    echo "проект compose: $PROJECT"
    echo "коммит: $(git -C "$ROOT" rev-parse HEAD 2>/dev/null || echo '(вне git)')"
    echo "ветка: $(git -C "$ROOT" rev-parse --abbrev-ref HEAD 2>/dev/null || echo '-')"
    echo "рабочее дерево чистое: $([ -z "$(git -C "$ROOT" status --porcelain 2>/dev/null)" ] && echo да || echo нет)"
    echo "сжатие: $EXT"
    echo
    echo "тома:"
  } > "$manifest"

  local vol archive size
  for vol in $volumes; do
    volume_exists "$vol" || { say "пропускаю $vol: тома нет"; continue; }
    archive="$out/$vol.$EXT"
    say "выгружаю $vol"
    docker run --rm -v "${PROJECT}_$vol":/from:ro busybox tar cf - -C /from . | $COMPRESS > "$archive"
    size=$(du -h "$archive" | cut -f1)
    printf '  %-16s %8s  %s\n' "$vol" "$size" "$(sha_of "$archive")" >> "$manifest"
    printf '%s  %s\n' "$(sha_of "$archive")" "$(basename "$archive")" >> "$out/checksums.txt"
  done

  if [ "$with_models" = 1 ]; then
    [ -d "$MODELS_DIR" ] || die "каталога весов нет: $MODELS_DIR"
    say "выгружаю веса моделей (это десятки гигабайт)"
    tar cf - -C "$MODELS_DIR" . | $COMPRESS > "$out/models.$EXT"
    printf '  %-16s %8s  %s\n' "models" "$(du -h "$out/models.$EXT" | cut -f1)" "$(sha_of "$out/models.$EXT")" >> "$manifest"
    printf '%s  %s\n' "$(sha_of "$out/models.$EXT")" "models.$EXT" >> "$out/checksums.txt"
  else
    echo "  models           —        не включены: скачаются при первом старте" >> "$manifest"
  fi

  if [ -f "$ENV_FILE" ]; then
    local key value
    : > "$out/linked.env"
    chmod 600 "$out/linked.env"
    for key in $LINKED_KEYS; do
      value=$(awk -F= -v k="$key" '$1 == k { sub(/^[^=]*=/, ""); print; exit }' "$ENV_FILE" || true)
      [ -n "${value:-}" ] && printf '%s=%s\n' "$key" "$value" >> "$out/linked.env"
    done
    echo >> "$manifest"
    echo "связанные ключи: linked.env ($(wc -l < "$out/linked.env" | tr -d ' ') шт., режим 600, содержит секреты)" >> "$manifest"
  else
    say "ВНИМАНИЕ: $ENV_FILE не найден, связанные ключи не выгружены"
  fi

  {
    echo
    echo "образы на момент снятия:"
    docker images --format '  {{.Repository}}:{{.Tag}} {{.ID}}' | grep "^  $PROJECT" | sort || true
  } >> "$manifest"

  say ""
  say "готово: $out ($(du -sh "$out" | cut -f1))"
  say "перенесите каталог вместе с копией репозитория, затем на новой машине:"
  say "  make bootstrap BUNDLE=<каталог>"
}

cmd_import() {
  local from=$1; shift
  local force=0
  while [ $# -gt 0 ]; do
    case $1 in
      --force) force=1 ;;
      *) die "неизвестный флаг $1" ;;
    esac
    shift
  done
  [ -d "$from" ] || die "каталога нет: $from"
  from=$(cd "$from" && pwd)
  [ -f "$from/manifest.txt" ] || die "в каталоге нет manifest.txt — это не бандл"

  local archive vol
  for archive in "$from"/*.tar.zst "$from"/*.tar.gz; do
    [ -e "$archive" ] || continue
    vol=$(basename "$archive"); vol=${vol%%.tar.*}
    [ "$vol" = "models" ] && continue
    if volume_exists "$vol" && ! volume_empty "$vol" && [ "$force" = 0 ]; then
      die "том ${PROJECT}_$vol не пуст. Это перезапишет данные — повторите с --force"
    fi
  done

  for archive in "$from"/*.tar.zst "$from"/*.tar.gz; do
    [ -e "$archive" ] || continue
    vol=$(basename "$archive"); vol=${vol%%.tar.*}
    if [ "$vol" = "models" ]; then
      mkdir -p "$MODELS_DIR"
      say "восстанавливаю веса моделей в $MODELS_DIR"
      $DECOMPRESS "$archive" | tar xf - -C "$MODELS_DIR"
      continue
    fi
    say "восстанавливаю $vol"
    docker volume create "${PROJECT}_$vol" >/dev/null
    docker run --rm -i -v "${PROJECT}_$vol":/to busybox sh -c 'rm -rf /to/* /to/.[!.]* 2>/dev/null; tar xf - -C /to'  < <($DECOMPRESS "$archive")
  done

  if [ -f "$from/linked.env" ]; then
    [ -f "$ENV_FILE" ] || die "$ENV_FILE не создан: сначала make env"
    local key value tmp
    tmp=$(mktemp)
    cp "$ENV_FILE" "$tmp"
    while IFS='=' read -r key value; do
      [ -n "$key" ] || continue
      if grep -q "^$key=" "$tmp"; then
        awk -v k="$key" -v v="$value" '$0 ~ "^" k "=" { print k "=" v; next } { print }' "$tmp" > "$tmp.new"
        mv "$tmp.new" "$tmp"
      else
        printf '%s=%s\n' "$key" "$value" >> "$tmp"
      fi
    done < "$from/linked.env"
    mv "$tmp" "$ENV_FILE"
    chmod 600 "$ENV_FILE"
    say "связанные ключи внесены в $ENV_FILE"
  fi

  say ""
  say "данные на месте. Дальше: make up (или make bootstrap завершит сам)"
}

cmd_verify() {
  local from=$1
  [ -f "$from/checksums.txt" ] || die "нет checksums.txt в $from"
  local sum name actual bad=0
  while read -r sum name; do
    [ -n "$sum" ] || continue
    actual=$(sha_of "$from/$name")
    if [ "$actual" = "$sum" ]; then printf 'ок   %s\n' "$name"
    else printf 'БИТ  %s\n' "$name"; bad=1; fi
  done < "$from/checksums.txt"
  [ "$bad" = 0 ] || die "контрольные суммы не сошлись"
  say "все архивы целы"
}

[ $# -ge 2 ] || usage
case $1 in
  export) shift; cmd_export "$@" ;;
  import) shift; cmd_import "$@" ;;
  verify) shift; cmd_verify "$@" ;;
  *) usage ;;
esac
