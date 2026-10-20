#!/usr/bin/env bash
# Замер каталога технологий на одноразовой базе. Только там — см. README.
# Запрос берётся из исходника (catalogue-sql.py), объём — из technology-catalogue-load.sql.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
MIGRATIONS="$HERE/../../../backend/db-service/internal/infrastructure/postgres/migrations"
PGC="${PGC:-rag-ci-pg}"
DB="${DB:-ops07}"
SCHEMA="${SCHEMA:-/tmp/schema.sql}"
CLIENTS="${CLIENTS:-10}"
SECONDS_PER_RUN="${SECONDS_PER_RUN:-30}"

# Формы запроса: общий термин, узкий термин, попадание только в синоним, весь
# каталог и худший случай — термин из описания каждой технологии.
TERMS=("сепарация" "запись 4." "alias routing 5" "" "метод решает задачу")
LABELS=("общий" "узкий" "только синоним" "весь каталог" "худший случай")

psql() { docker exec -i "$PGC" psql -U rag -v ON_ERROR_STOP=1 "$@"; }
in_db() { psql -d "$DB" -q "$@"; }

prepare() { # prepare <techs> <mentions>
    psql -d postgres -q -c "DROP DATABASE IF EXISTS $DB WITH (FORCE);" -c "CREATE DATABASE $DB;"
    in_db -f - < "$SCHEMA" >/dev/null
    # Индексы создаются после загрузки: порядок на замер не влияет, а загрузка
    # миллиона строк в готовый GIN длится втрое дольше.
    in_db -v techs="$1" -v mentions="$2" -f - < "$HERE/technology-catalogue-load.sql" | tail -1
    for migration in 00037_catalogue_search_index.sql 00038_catalogue_epoch.sql; do
        sed -n '/+goose Up/,/+goose Down/p' "$MIGRATIONS/$migration" | grep -v '^-- +goose' | in_db -f - >/dev/null
    done
    in_db -c 'ANALYZE technologies; ANALYZE technology_aliases; ANALYZE technology_mentions; ANALYZE documents;'
    in_db -t -c "SELECT 'объём: ' || (SELECT count(*) FROM technologies) || ' технологий, ' ||
        (SELECT count(*) FROM technology_mentions) || ' упоминаний, ' ||
        (SELECT count(*) FROM technology_aliases) || ' синонимов, ' ||
        (SELECT count(*) FROM documents) || ' документов';"
}

single() { # один пользователь, каждая форма запроса
    for i in "${!TERMS[@]}"; do
        local sql rows ms
        sql="$(python3 "$HERE/catalogue-sql.py" --term "${TERMS[$i]}")"
        # Два прогона: второй — тёплый, он и идёт в протокол.
        printf '%s' "$sql" | in_db -f - >/dev/null
        ms="$( { printf '\\timing on\n%s' "$sql" | in_db -f - ; } | sed -n 's/^Time: \([0-9.]*\) ms.*/\1/p')"
        rows="$(printf '%s' "$sql" | in_db -t -f - | grep -c . || true)"
        printf '  %-16s %9s мс  строк: %s\n' "${LABELS[$i]}" "$ms" "$rows"
    done
}

p95() { # CLIENTS параллельных пользователей на каждой форме
    for i in "${!TERMS[@]}"; do
        python3 "$HERE/catalogue-sql.py" --term "${TERMS[$i]}" | docker exec -i "$PGC" sh -c 'cat > /tmp/bench.sql'
        docker exec "$PGC" sh -c "rm -f /tmp/ops07log*; pgbench -U rag -d $DB -n -f /tmp/bench.sql \
            -c $CLIENTS -j $CLIENTS -T $SECONDS_PER_RUN -l --log-prefix=/tmp/ops07log" >/dev/null
        docker exec "$PGC" sh -c 'cat /tmp/ops07log*' | awk '{print $3}' | sort -n | awk -v l="${LABELS[$i]}" '
            {a[NR]=$1} END {
                if (NR == 0) { printf "  %-16s нет транзакций\n", l; exit }
                printf "  %-16s p50 %.3f с  p95 %.3f с  p99 %.3f с  запросов %d\n", l,
                    a[int(NR*0.50+0.5)]/1e6, a[int(NR*0.95+0.5)]/1e6, a[int(NR*0.99+0.5)]/1e6, NR }'
    done
}

explain() { python3 "$HERE/catalogue-sql.py" --term "${1:-}" --explain 'ANALYZE, BUFFERS' | in_db -f -; }

prepared() { # тот же запрос подготовленным оператором — так его гоняет сервис
    for mode in force_custom_plan force_generic_plan; do
        echo "  $mode"
        for i in "${!TERMS[@]}"; do
            local ms
            ms="$(python3 "$HERE/catalogue-sql.py" --term "${TERMS[$i]}" --prepared "$mode" \
                | in_db -f - | sed -n 's/^Time: \([0-9.]*\) ms.*/\1/p' | sort -n | head -1)"
            printf '    %-16s %9s мс\n' "${LABELS[$i]}" "$ms"
        done
    done
}

sweep() { # таблица объёмов одним пользователем, затем порог на целевом
    for volume in "1000 10000" "3000 30000" "10000 100000" "30000 300000" "100000 1000000"; do
        # shellcheck disable=SC2086
        echo "--- $(prepare $volume | grep -o 'объём:.*')"
        single
    done
    echo "--- подготовленный оператор на целевом объёме"
    prepared
    echo "--- $CLIENTS параллельных пользователей на целевом объёме"
    p95
}

"${1:?usage: measure.sh prepare <techs> <mentions> | single | prepared | p95 | explain [term] | sweep}" "${@:2}"
