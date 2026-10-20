#!/usr/bin/env python3
"""Печатает запрос каталога дословно из technology_catalogue.go, подставив
литералы вместо $1..$7. Источник один — исходник: копия запроса разошлась бы с
кодом и замер перестал бы что-либо значить.

Без --term печатается запрос «весь каталог», с --term — поисковый. С --prepared
печатается скрипт, который гоняет тот же запрос подготовленным оператором, как
это делает сам сервис (pgx держит именованные операторы), в заданном режиме
плана: generic-план не знает значения параметров и может выбрать другой план.
"""
import argparse
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[3]
GO = ROOT / "backend/db-service/internal/infrastructure/postgres/technology_catalogue.go"


def go_consts(src):
    """Собирает строковые const'ы: конкатенацию `литералов` и ссылок на другие."""
    found = {}
    for head in re.finditer(r"^const (\w+) = (?=[`\w])", src, re.M):
        parts, i = [], head.end()
        while True:
            if src[i] == "`":
                end = src.index("`", i + 1)
                parts.append(src[i + 1:end])
                i = end + 1
            else:
                ref = re.match(r"\w+", src[i:])
                parts.append(("ref", ref.group(0)))
                i += ref.end()
            plus = re.match(r"[ \t\n]*\+[ \t\n]*", src[i:])
            if not plus:
                break
            i += plus.end()
        found[head.group(1)] = parts
    return found


def resolve(name, consts):
    return "".join(resolve(p[1], consts) if isinstance(p, tuple) else p for p in consts[name])


def emit_prepared(args, sql):
    types = "text,boolean,text[],text[],int,int" + (",text" if args.term else "")
    values = [f"'{args.reader}'", "true" if args.shared else "false", "NULL", "NULL",
              str(args.limit), str(args.offset)] + ([f"'{args.term}'"] if args.term else [])
    call = f"EXECUTE catalogue({', '.join(values)});"
    warm = "\\o /dev/null\n" + call + "\n" + call + "\n\\o"
    timed = "\\o /dev/null\n" + "\n".join([call] * 3) + "\n\\o"
    print(f"SET plan_cache_mode = {args.prepared};")
    print(f"PREPARE catalogue({types}) AS {sql.strip()};")
    print(warm)
    print("\\timing on")
    print(timed)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--term", default="")
    ap.add_argument("--limit", type=int, default=21)
    ap.add_argument("--offset", type=int, default=0)
    ap.add_argument("--reader", default="00000000-0000-0000-0000-0000000000ff")
    ap.add_argument("--shared", action="store_true")
    ap.add_argument("--explain", default="", help="например 'ANALYZE, BUFFERS'")
    ap.add_argument("--prepared", default="", choices=["", "force_custom_plan", "force_generic_plan"],
                    help="гонять подготовленным оператором в этом режиме плана")
    args = ap.parse_args()

    consts = go_consts(GO.read_text(encoding="utf-8"))
    name = "catalogueSearchSQL" if args.term else "catalogueAllSQL"
    if name not in consts:
        sys.exit(f"{GO}: не найден const {name}")
    sql = resolve(name, consts)
    if "'" in args.term:
        sys.exit("терм с кавычкой: замер не подставляет её безопасно")
    if args.prepared:
        return emit_prepared(args, sql)
    for placeholder, literal in (
        ("$7", f"'{args.term}'"),
        ("$1", f"'{args.reader}'"),
        ("$2", "true" if args.shared else "false"),
        ("$3", "NULL::text[]"),
        ("$4", "NULL::text[]"),
        ("$5", str(args.limit)),
        ("$6", str(args.offset)),
    ):
        sql = sql.replace(placeholder, literal)
    prefix = f"EXPLAIN ({args.explain}) " if args.explain else ""
    print(prefix + sql.strip() + ";")


if __name__ == "__main__":
    main()
