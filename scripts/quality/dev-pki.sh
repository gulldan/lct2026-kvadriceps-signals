#!/usr/bin/env bash
# Локальный CA для проверок production-профиля; выпускающий ключ не монтируется в сервисы.
set -euo pipefail
out=${1:?usage: dev-pki.sh NEW_DIRECTORY}
[[ ! -e "$out" ]] || { echo 'destination already exists' >&2; exit 1; }
umask 077
mkdir -p "$out"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 30 \
  -subj /CN=rag-local-test-ca -keyout "$out/ca-key.pem" -out "$out/ca.pem" >/dev/null 2>&1
for service in db-service auth main-service llm-service chunk-splitter archive-worker pdf-parser office-parser email-parser \
  db-parser ocr-service vlm-service source-harvester signal-worker cluster-worker discovery-worker raptor-worker graph-compute; do
  openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=$service" \
    -keyout "$out/$service.key" -out "$out/request.csr" >/dev/null 2>&1
  cat > "$out/extensions" <<EOF
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature
extendedKeyUsage=serverAuth,clientAuth
subjectAltName=DNS:$service,URI:spiffe://rag.local/service/$service
EOF
  openssl x509 -req -in "$out/request.csr" -CA "$out/ca.pem" -CAkey "$out/ca-key.pem" -CAcreateserial \
    -days 7 -extfile "$out/extensions" -out "$out/$service.pem" >/dev/null 2>&1
 done
rm "$out/request.csr" "$out/extensions"
echo 'Local identities created; CA private key must remain outside container mounts.'
