#!/usr/bin/env bash
# Только чтение живых источников: база, очередь, ключи и GPU не нужны.
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
output_root="${1:-$repo_dir/artifacts/parser-verification}"
mkdir -p "$output_root"
proof_dir="$(mktemp -d "$output_root/run-XXXXXX")"
proof_dir="$(cd "$proof_dir" && pwd)"
status=0

if ! (
  cd "$repo_dir/backend/trend-sources"
  TR_LIVE_SOURCES=1 TR_SOURCE_PROOF_DIR="$proof_dir" \
    go test -run '^TestLive(Web|Patent)Parsers$' -v -count=1 -timeout 8m
) 2>&1 | tee "$proof_dir/parsers.log"; then
  status=1
fi

if ! (
  cd "$repo_dir/backend/source-harvester"
  TR_LIVE_SOURCES=1 TR_SOURCE_PROOF_DIR="$proof_dir" \
    go test ./internal/provider -run '^Test(RecordedCollectorExports|LiveCrossrefParsers|LiveEuropePMCFullText)$' -v -count=1 -timeout 4m
) 2>&1 | tee "$proof_dir/import-contract.log"; then
  status=1
fi

(
  cd "$repo_dir/backend/source-harvester"
  go build -o "$proof_dir/source-harvester" ./cmd/source-harvester
)
through="$(date -u +%F)"
for provider in crossref github arxiv; do
  query='retrieval augmented generation'
  limit=2
  if [[ "$provider" == github ]]; then
    query='repo:HKUDS/LightRAG'
    limit=1
  fi
  if ! "$proof_dir/source-harvester" \
    --provider "$provider" --query "$query" --from 2023-01-01 --through "$through" \
    --page-size "$limit" --limit "$limit" --timeout 3m \
    --output "$proof_dir/$provider" 2>&1 | tee "$proof_dir/$provider.log"; then
    status=1
  fi
done

printf 'Verification artifacts: %s\n' "$proof_dir"
exit "$status"
