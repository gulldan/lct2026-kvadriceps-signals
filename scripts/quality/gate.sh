#!/usr/bin/env bash
# Выпуск проверяется локально; пропуск интеграций требует явного профиля unit.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
profile=${1:-all}
case "$profile" in unit|integration|all) ;; *) echo 'usage: gate.sh [all|unit|integration]' >&2; exit 2 ;; esac
if [[ "$profile" != unit ]]; then
  for name in DB_TEST_DSN RABBITMQ_TEST_URL S3_TEST_ENDPOINT QDRANT_TEST_URL OPENSEARCH_TEST_URL VALKEY_TEST_URL; do
    [[ -n "${!name:-}" ]] || { echo "required isolated test dependency is unset: $name" >&2; exit 1; }
  done
fi
if [[ "$profile" != integration ]]; then
  make build test vet lint
  (cd frontend && bun run verify)
  bash scripts/quality/python.sh
  if [[ -d /opt/homebrew/opt/libarchive/lib/pkgconfig ]]; then
    export PKG_CONFIG_PATH="/opt/homebrew/opt/libarchive/lib/pkgconfig:${PKG_CONFIG_PATH:-}"
  fi
  pkg-config --exists libarchive
  for crate in backend/archive-scan ml-services/graph-compute ml-services/workbook-parser-service; do
    features=()
    [[ "$crate" == ml-services/workbook-parser-service ]] || features=(--features service)
    (cd "$crate" && cargo test --locked "${features[@]}" && cargo clippy --all-targets --locked "${features[@]}" -- -D warnings)
  done
fi
if [[ "$profile" != unit ]]; then
  integration_tmp=$(mktemp -d)
  manifest="$integration_tmp/manifest.json"
  trap 'rm -rf "$integration_tmp"' EXIT
  python3 - "$manifest" <<'PYCODE'
import json,sys,uuid
from pathlib import Path
Path(sys.argv[1]).write_text(json.dumps({"test_run_id":"tr-test-"+str(uuid.uuid4()),"ids":[str(uuid.uuid4()) for _ in range(8)]}))
PYCODE
  export TR_INDEX_MANIFEST="$manifest"
  db_required=(
    TestSessionsRotateRevokeRefreshRolesAndRejectConcurrentReplay
    TestHypothesisJobsRecoverLeaseWithoutDuplicatingEffects
    TestChatTurnRetryConcurrencyAndFencing TestChatHistoryCursor
    TestIngestionOutboxRecoversAmbiguousPublishAndFencesGenerations
    TestHypothesisVersionFencesStaleWritersAndSerializesRevisions
    TestKPIDocumentAccessAndAtomicBatch TestClusterPublicationAtomicIdempotentAndFenced
    TestCatalogPagesAndAggregates TestSettingsBatchRollbackCASAndAudit TestSourceIndexLive
    TestAnalysisCollectionScopeLive TestHarvestIncrementalLive TestExtractionLanguagesLive
  )
  db_tests=$(IFS='|'; echo "^(${db_required[*]})$")
  # Старые проверки читают адрес из общего окружения; подменяем его только тестовой БД.
  (cd backend/db-service && POSTGRES_DSN="$DB_TEST_DSN" \
    TR_COLLECTION_SCOPE_LIVE=1 TR_HARVEST_INCREMENTAL_LIVE=1 TR_MULTILINGUAL_INTAKE_LIVE=1 \
    go test -count=1 -json ./internal/infrastructure/postgres -run "$db_tests") | python3 scripts/quality/check-go-results.py "${db_required[@]}"
  (cd backend/platform && go test -count=1 -json ./pkg/messaging ./pkg/storage ./pkg/vectorstore ./pkg/searchstore ./pkg/grpcx \
    -run 'Test(Broker|S3Live|QdrantSchemaLive|OpenSearchSchemaLive|MTLS)') | python3 scripts/quality/check-go-results.py \
    TestBrokerDelayedRetryAndMandatoryRouting TestBrokerParkingWaitsForBrokerAcceptance \
    TestBrokerCancelPreservesPrefetchedDeliveries TestS3LiveConditionalWritesAndReadBudgets \
    TestQdrantSchemaLive TestOpenSearchSchemaLive TestMTLSChecksPeerIdentityAndRPCPrivilege
  (cd ml-services/graph-compute && cargo test --locked --features service live_qdrant_selection -- --ignored)
  bash scripts/quality/dev-pki.sh "$integration_tmp/pki"
  (cd ml-services/graph-compute && cargo build --locked --features service --bin graph_compute_server)
  PYTHONPATH="$root/ml-services/runtime:$root/backend/platform/pkg/genproto" \
    uv run --no-project --with-requirements scripts/proto-requirements.txt python scripts/quality/rpc-live.py \
    --pki "$integration_tmp/pki" --binary "$root/ml-services/graph-compute/target/debug/graph_compute_server"
  PYTHONPATH="$root/ml-services/runtime" uv run --no-project --python 3.14 --with pytest --with redis \
    python -m pytest -q ml-services/tests/test_worker_lease.py
  uv run --no-project --python 3.14 --with pytest --with redis --with "psycopg[binary]" \
    python -m pytest -q scripts/quality/test_legacy_migration.py
fi
