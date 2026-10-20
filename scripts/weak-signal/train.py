"""Обучение модели этапа 1: слабый сигнал против зрелого, хайпа и провала.

Воспроизводится без стенда: все признаки лежат в data/. Команда пишет веса
и отчёт оценки туда, откуда их встраивает радар, и эталонные векторы для
сверки расчёта в Go с расчётом здесь.

    python train.py            # обучить, оценить, записать артефакты
    python train.py --check    # обучить заново и сверить с записанными
"""
import argparse
import base64
import json
import math
import struct
import sys
from pathlib import Path

import numpy as np
from sklearn.compose import ColumnTransformer
from sklearn.decomposition import PCA
from sklearn.impute import SimpleImputer
from sklearn.linear_model import LogisticRegression
from sklearn.metrics import f1_score, precision_score, recall_score, roc_auc_score
from sklearn.model_selection import StratifiedGroupKFold
from sklearn.pipeline import make_pipeline
from sklearn.preprocessing import StandardScaler

HERE = Path(__file__).resolve().parent
APP = HERE.parent.parent / "backend/main-service/internal/application"
VERSION = "weak-signal-2026-10-18.2"
MONTH = "2026-09"
YEAR = 2026
SEED = 20260924
C_GRID = (0.03, 0.1, 0.3, 1.0)
# Коридор ТЗ — 75–80 %. Полнота закрепляется на верхней границе, остальное отдаётся точности: порог по
# максимуму F1 зависит от доли отрицательных в выборке, а она у закрытого датасета неизвестна.
RECALL_TARGET = 0.8
THRESHOLDS = np.round(np.arange(0.05, 0.951, 0.01), 2)
PCA_K = 16
SINGLE_WEIGHT = 0.5
EMBEDDING_MODEL = "codefuse-ai/F2LLM-v2-0.6B"
# Входы llm_* собраны один раз (data/llm.json); радар спрашивает ту же модель тем же промптом. Хеш — от
# liveRatingSystem в Go, тест сверяет его: другой промпт или другая модель — уже другой признак.
LLM_RATING = {"model": "corp/deepseek-v4-flash-abliterated", "prompt_sha256":
              "be8e6a1e5bd141c514f3657c921131c314e089e4cd6d1ce28c0913da0e4f6125", "temperature": 0,
              "reasoning_effort": "none", "max_tokens": 80, "collected": "2026-09-24"}
# Классово-условный (Mondrian) конформный прогноз поверх тех же вероятностей: истинная метка попадает в
# множество не реже чем в 90 % случаев для сигналов и для не сигналов отдельно. Одна метка в множестве —
# решение, обе — «не могу решить»: так модель сама уводит в третье состояние то, что путает с хайпом.
CONFORMAL_ALPHA = 0.1
ABLATION_REPEATS = 5
# Абляции: какие группы входов остаются. Вопрос жюри — что модель добавляет к одной оценке языковой модели.
ABLATIONS = {"full": ("window", "history", "hn", "llm", "embedding"), "llm_only": ("llm",),
             "no_llm": ("window", "history", "hn", "embedding"), "no_embedding": ("window", "history", "hn", "llm"),
             "no_hn": ("window", "history", "llm", "embedding")}
SLICES = {"etalon_team": ("etalon", "team"), "etalon_hype": ("etalon", "hype"),
          "all": ("etalon", "team", "mullany", "cosmos", "hype")}

# Порядок входов одной фразы. Технология описывается двумя фразами шлюза —
# самой устоявшейся и самой узкой: одна обычно называет общее понятие, другая
# саму новинку, и модели нужны обе.
PHRASE_INPUTS = [
    "log_documents", "active_periods", "groups_per_document", "top_group_share", "growth_per_document",
    "months_observed", "window_empty",
    "history_log_works", "history_share_before", "history_growth", "history_log_recent", "history_age_years",
    "history_slope", "history_share_last3",
    "hn_log_total", "hn_share_before", "hn_growth", "hn_log_recent", "hn_log_recent_story", "hn_story_growth",
    "llm_stage", "llm_failed", "llm_confidence",
]
GROUPS = (7, 7, 6, 3)


def load(name):
    return json.loads((HERE / "data" / name).read_text())


def months_between(a, b):
    try:
        y1, m1 = map(int, a.split("-"))
        y2, m2 = map(int, b.split("-"))
        return float((y2 - y1) * 12 + m2 - m1)
    except (AttributeError, ValueError):
        return math.nan


def window_inputs(f):
    if f is None:
        return [math.nan] * 7
    d = f["documents"]
    per = (lambda v: v / d) if d else (lambda v: math.nan)
    share, growth = f.get("top_group_share"), f.get("share_growth_pp")
    return [math.log1p(d), float(f["active_periods"]), per(f["author_groups"]),
            share if share is not None else math.nan, per(growth * 1000) if growth is not None else math.nan,
            months_between(f.get("first_observed") or "", MONTH), 1.0 if d == 0 else 0.0]


def history_inputs(h):
    if h is None:
        return [math.nan] * 7
    h = {int(k): v for k, v in h.items()}
    total = sum(v for k, v in h.items() if k <= YEAR - 1)
    if total == 0:
        return [0.0] * 7
    before = sum(v for k, v in h.items() if k <= YEAR - 7)
    mid = sum(h.get(y, 0) for y in range(YEAR - 6, YEAR - 2)) / 4
    recent = sum(h.get(y, 0) for y in (YEAR - 2, YEAR - 1)) / 2
    years = [y for y in range(YEAR - 30, YEAR) if h.get(y, 0) >= max(3, 0.02 * total)]
    xs = np.arange(YEAR - 8, YEAR, dtype=float)
    ys = np.log1p([h.get(int(y), 0) for y in xs])
    slope = float(np.sum((xs - xs.mean()) * (ys - ys.mean())) / np.sum((xs - xs.mean()) ** 2))
    return [math.log1p(total), before / total, math.log(recent + 1) - math.log(mid + 1), math.log1p(recent),
            float(YEAR - years[0]) if years else 0.0, slope,
            sum(h.get(y, 0) for y in (YEAR - 3, YEAR - 2, YEAR - 1)) / total]


def hn_inputs(v):
    if v is None:
        return [math.nan] * 6
    total = v["before"] + v["mid"] + v["recent"]
    return [math.log1p(total), v["before"] / total if total else 0.0,
            math.log(v["recent"] / 2 + 1) - math.log(v["mid"] / 4 + 1), math.log1p(v["recent"]),
            math.log1p(v["recent_story"]), math.log(v["recent_story"] / 2 + 1) - math.log(v["mid_story"] / 4 + 1)]


def llm_inputs(v):
    if v is None:
        return [math.nan] * 3
    return [float(v["stage"]), float(v["failed"]), float(v["confidence"])]


def decode_f16(s):
    raw = base64.b64decode(s)
    return [struct.unpack_from("<e", raw, 2 * i)[0] for i in range(len(raw) // 2)]


class Data:
    def __init__(self):
        self.tech = load("technologies.json")
        self.window, self.history, self.hn, self.llm = load("window.json"), load("history.json"), load("hn.json"), load("llm.json")
        self.emb = {p: decode_f16(v) for p, v in load("embeddings_f16.json").items()}

    def works(self, phrase):
        return sum(self.history.get(phrase, {}).values())

    def narrowest(self, phrases):
        return sorted(phrases, key=self.works, reverse=True)[-1]

    def phrase_inputs(self, p):
        return (window_inputs(self.window.get(p)) + history_inputs(self.history.get(p)) + hn_inputs(self.hn.get(p))
                + llm_inputs(self.llm.get(p)))

    def row(self, phrases):
        """Входы самой устоявшейся и самой узкой фразы и средний эмбеддинг."""
        ph = sorted([p for p in phrases if p in self.emb], key=self.works, reverse=True)
        lo, hi = self.phrase_inputs(ph[0]), self.phrase_inputs(ph[-1])
        # Порядок как при исследовании: окно, история, HN, LLM — сначала обе фразы по группе.
        out, i = [], 0
        for k in GROUPS:
            out += lo[i:i + k] + hi[i:i + k]
            i += k
        return out, list(np.mean([self.emb[p] for p in ph], axis=0)), ph

    def gap(self, phrase):
        """Правило «мало данных» радара (liveThinEvidence и liveDataGap в Go) для одной фразы."""
        win, hist, hn = self.window.get(phrase), self.history.get(phrase), self.hn.get(phrase)
        if win is not None and hist is not None and hn is not None:
            works, mentions = max(win.get("total", 0), win["documents"]), hn["before"] + hn["mid"] + hn["recent"]
            if works == 0 and self.works(phrase) == 0 and mentions == 0:
                return "not_found"
            if works == 1 and self.works(phrase) <= 1 and mentions == 0:
                return "too_early"
        sources = (win, hist, hn, self.llm.get(phrase), self.emb.get(phrase))
        return "not_collected" if sum(v is None for v in sources) >= 2 else ""


def input_names():
    names, i = [], 0
    for k in GROUPS:
        group = PHRASE_INPUTS[i:i + k]
        names += [f"broad_{n}" for n in group] + [f"narrow_{n}" for n in group]
        i += k
    return names


def input_group(name):
    kind = name.split("_", 1)[1]
    return next((g for g in ("history", "hn", "llm") if kind.startswith(g + "_")), "window")


def model(nt, c, emb=True):
    parts = [("t", make_pipeline(SimpleImputer(strategy="median"), StandardScaler()), list(range(nt)))]
    if emb:
        parts.append(("e", make_pipeline(StandardScaler(), PCA(PCA_K, random_state=7), StandardScaler()),
                      list(range(nt, nt + 1024))))
    return make_pipeline(ColumnTransformer(parts), LogisticRegression(C=c, max_iter=5000))


def fit(X, y, g, w, nt, emb=True):
    """C и порог выбираются только по обучающей части, её групповой проверкой. Порог — самый строгий, при
    котором внутренняя полнота не ниже RECALL_TARGET; из C берётся тот, где на этом пороге точнее всего по
    всем источникам отрицательных, включая хайп. Веса строк те же, что при обучении: строки по одной фразе
    вдвое легче, иначе самые трудные из них тянули бы порог."""
    folds = list(StratifiedGroupKFold(5, shuffle=True, random_state=7).split(X, y, g))
    best = None
    for c in C_GRID:
        p = np.zeros(len(y))
        for tr, te in folds:
            p[te] = model(nt, c, emb).fit(X[tr], y[tr], logisticregression__sample_weight=w[tr]).predict_proba(X[te])[:, 1]
        th = max((q for q in THRESHOLDS if recall_score(y, p >= q, sample_weight=w) >= RECALL_TARGET),
                 default=THRESHOLDS[0])
        precision = precision_score(y, p >= th, sample_weight=w, zero_division=0)
        if best is None or precision > best[0]:
            best = (precision, c, float(th))
    _, c, th = best
    return model(nt, c, emb).fit(X, y, logisticregression__sample_weight=w), th, c


def scores(y, pred):
    tp, fp = int(np.sum(pred & (y == 1))), int(np.sum(pred & (y == 0)))
    fn, tn = int(np.sum(~pred & (y == 1))), int(np.sum(~pred & (y == 0)))
    return {"tp": tp, "fp": fp, "fn": fn, "tn": tn, "precision": precision_score(y, pred, zero_division=0),
            "recall": recall_score(y, pred), "f1": f1_score(y, pred)}


def cross_validate(X, y, g, w, n, nt, reps, emb=True, X1=None, blank=None):
    """Внешний цикл только отчётный: его строки не участвуют ни в весах, ни в выборе C и порога. Первые n
    строк X — технологии, дальше их варианты по одной фразе, которые идут только в обучение. blank —
    группы входов, которые на проверяемой части гасятся: так выглядит отказ источника в радаре."""
    runs = []
    for r in range(reps):
        run = {"prob": np.zeros(n), "pred": np.zeros(n, bool), "pred1": np.zeros(n, bool), "chosen": [],
               "fold": np.zeros(n, int), "outage": {k: np.zeros(n, bool) for k in blank or {}}}
        folds = StratifiedGroupKFold(5, shuffle=True, random_state=SEED + r).split(X[:n], y[:n], g[:n])
        for f, (tr0, te) in enumerate(folds):
            tr = np.isin(g, g[tr0])
            run["fold"][te] = f
            m, th, c = fit(X[tr], y[tr], g[tr], w[tr], nt, emb)
            run["chosen"].append((c, th))
            run["prob"][te] = m.predict_proba(X[te])[:, 1]
            run["pred"][te] = run["prob"][te] >= th
            if X1 is not None:
                run["pred1"][te] = m.predict_proba(X1[te])[:, 1] >= th
            for k, cols in (blank or {}).items():
                Xt = X[te].copy()
                Xt[:, cols] = np.nan
                run["outage"][k][te] = m.predict_proba(Xt)[:, 1] >= th
        runs.append(run)
    return runs


def slice_scores(runs, y, src, sources, key="pred", keep=None):
    """Средние за повтор: меры и матрица ошибок. keep убирает строки, на которых радар воздержался бы."""
    sel = np.isin(src, sources) if keep is None else np.isin(src, sources) & keep
    per = [scores(y[sel], (r[key] if key == "pred" else r["outage"][key])[sel]) for r in runs]
    out = {k: float(np.mean([p[k] for p in per])) for k in per[0]}
    out["f1_sd"] = float(np.std([p["f1"] for p in per]))
    if key == "pred" and len(set(y[sel])) == 2:
        out["auc"] = float(np.mean([roc_auc_score(y[sel], r["prob"][sel]) for r in runs]))
    return out


def false_signal(runs, src, key="pred"):
    """Доля строк каждого источника отрицательных, названных сигналом, когда источник был в обучении."""
    return {s: float(np.mean([(r[key] if key == "pred" else r["outage"][key])[src == s].mean() for r in runs]))
            for s in SLICES["all"] if s != "etalon"}


def leave_source_out(X, y, g, w, src, n, nt, emb=True, sources=None):
    out = {}
    for s in sources or sorted(set(src) - {"etalon"}):
        keep = src != s
        m, th, _ = fit(X[keep], y[keep], g[keep], w[keep], nt, emb)
        out[str(s)] = float(np.mean(m.predict_proba(X[:n][src[:n] == s])[:, 1] >= th))
    return out


def conformal_quantiles(probs, y):
    """Квантили несоответствия по классам: у сигнала несоответствие 1 − p, у не сигнала p. Повторы внешнего
    цикла складываются в одну выборку, а поправка конечной выборки (n + 1) берётся по числу строк класса:
    повтор не добавляет новых технологий."""
    out = {}
    for c, key in ((1, "weak"), (0, "not_weak")):
        s = np.concatenate([1 - p[y == 1] if c else p[y == 0] for p in probs])
        k = int((y == c).sum())
        out[key] = float(np.quantile(s, min(1.0, math.ceil((k + 1) * (1 - CONFORMAL_ALPHA)) / k), method="higher"))
    return out


def conformal_report(runs, y, src):
    """Квантили считаются по четырём папкам внешнего цикла и применяются к пятой: строка, к которой применён
    квантиль, в его расчёт не входит, а папки не делят групп технологий. Ни одной метки в множестве — тоже
    «не могу решить»: обе метки неправдоподобны, и решать нечем."""
    sets = []
    for r in runs:
        weak, not_weak = np.zeros(len(y), bool), np.zeros(len(y), bool)
        for f in np.unique(r["fold"]):
            te = r["fold"] == f
            q = conformal_quantiles([r["prob"][~te]], y[~te])
            weak[te], not_weak[te] = r["prob"][te] >= 1 - q["weak"], r["prob"][te] <= q["not_weak"]
        sets.append((weak, not_weak))
    # Частоты по строке за все повторы: на постоянном наборе строк их среднее равно среднему долей за повтор.
    freq = lambda rule: np.mean([rule(w, nw) for w, nw in sets], axis=0)
    weak, not_weak = freq(lambda w, nw: w), freq(lambda w, nw: nw)
    decided = {"weak": freq(lambda w, nw: w & ~nw), "undecided": freq(lambda w, nw: w == nw),
               "not_weak": freq(lambda w, nw: nw & ~w)}
    return {"alpha": CONFORMAL_ALPHA,
            "coverage": {"weak": float(weak[y == 1].mean()), "not_weak": float(not_weak[y == 0].mean())},
            "undecided": float(decided["undecided"].mean()),
            "weak_precision": float(decided["weak"][y == 1].sum() / decided["weak"].sum()),
            "by_source": {s: {k: float(v[src == s].mean()) for k, v in decided.items()} for s in SLICES["all"]}}


def evaluate(data, X, X1, y, g, w, src, area, n, nt, reps):
    names = input_names()
    cols = {k: [i for i, name in enumerate(names) if input_group(name) == k] for k in ("window", "history", "hn", "llm")}
    runs = cross_validate(X, y, g, w, n, nt, reps, X1=X1, blank={k: cols[k] for k in ("llm", "history", "hn")})
    yn, sn = y[:n], src[:n]
    core = np.isin(sn, SLICES["etalon_team"])
    f1s = [f1_score(yn[core], r["pred"][core]) for r in runs]
    chosen = [c for r in runs for c in r["chosen"]]
    main = slice_scores(runs, yn, sn, SLICES["etalon_team"])
    report = {
        "selection": {"c_grid": list(C_GRID), "recall_target": RECALL_TARGET,
                      "c_chosen": {str(c): sum(1 for x, _ in chosen if x == c) for c in C_GRID},
                      "thresholds": [min(t for _, t in chosen), float(np.median([t for _, t in chosen])),
                                     max(t for _, t in chosen)]},
        "nested_cv": {"repeats": reps, "folds": 5, "scope": list(SLICES["etalon_team"]),
                      "f1_mean": main["f1"], "f1_sd": main["f1_sd"], "f1_min": float(min(f1s)),
                      "precision_mean": main["precision"], "recall_mean": main["recall"], "auc_mean": main["auc"],
                      "single_phrase_f1_mean": float(np.mean([f1_score(yn[core], r["pred1"][core]) for r in runs])),
                      "first_repeat": scores(yn[core], runs[0]["pred"][core])},
        # Нижняя граница: назвать сигналом всё. F1 модели сравнивается с ней, а не с нулём.
        "baseline_all_weak": scores(yn[core], np.ones(int(core.sum()), bool)),
        "slices": {k: slice_scores(runs, yn, sn, v) for k, v in SLICES.items()},
        "false_signal_by_source": false_signal(runs, sn),
        "leave_source_out_false_signal": leave_source_out(X, y, g, w, src, n, nt),
        "conformal": conformal_report(runs, yn, sn),
    }
    lao = {}
    for a in sorted(set(area[:n][yn == 1])):
        keep = ~((area == a) & (y == 1))
        m, th, _ = fit(X[keep], y[keep], g[keep], w[keep], nt)
        lao[str(a)] = float(np.mean(m.predict_proba(X[:n][(area[:n] == a) & (yn == 1)])[:, 1] >= th))
    report["leave_area_out_recall"] = lao
    # Отказ источника: модель обучена со всеми входами, а на проверке группа не измерена и заменена медианой.
    report["outage"] = {k: {**slice_scores(runs, yn, sn, SLICES["etalon_team"], key=k),
                            "all_f1": slice_scores(runs, yn, sn, SLICES["all"], key=k)["f1"],
                            "false_signal": false_signal(runs, sn, key=k)} for k in ("llm", "history", "hn")}
    ablations = {}
    for name, groups in ABLATIONS.items():
        keep = [i for k in ("window", "history", "hn", "llm") if k in groups for i in cols[k]]
        emb = "embedding" in groups
        Xa = np.hstack([X[:, keep], X[:, nt:]]) if emb else X[:, keep]
        ar = runs[:ABLATION_REPEATS] if name == "full" else cross_validate(Xa, y, g, w, n, len(keep), ABLATION_REPEATS, emb)
        # Сравнение по F1 зависит от правила порога; различение эталона с каждым видом отрицательных — нет.
        auc_vs = {s: float(np.mean([roc_auc_score(yn[np.isin(sn, sel)], r["prob"][np.isin(sn, sel)]) for r in ar]))
                  for s, sel in [(s, ("etalon", s)) for s in false_signal(ar, sn)] + [("all", SLICES["all"])]}
        ablations[name] = {"inputs": len(keep), "embedding": emb, "auc_vs": auc_vs,
                           **{k: v for k, v in slice_scores(ar, yn, sn, SLICES["etalon_team"]).items() if k != "f1_sd"},
                           "all_f1": slice_scores(ar, yn, sn, SLICES["all"])["f1"],
                           "false_signal": false_signal(ar, sn),
                           "unseen_hype": leave_source_out(Xa, y, g, w, src, n, len(keep), emb, ["hype"])["hype"]}
    report["ablations"] = ablations
    # Радар не выносит решения по фразе, которой почти нет в данных (liveThinEvidence в Go). Здесь признаки
    # собраны для всех строк и оценены все; сколько из них радар отложил бы — отдельно, не как ошибки.
    gaps = [data.gap(data.narrowest(t["gateway_phrases"])) for t in data.tech]
    kept = np.array([not x for x in gaps])
    report["abstention"] = {"rule": "narrowest_phrase_thin_evidence",
                            "by_source": {s: {"total": int((sn == s).sum()),
                                              "abstained": int(sum(1 for x, z in zip(gaps, sn) if x and z == s))}
                                          for s in SLICES["all"]},
                            "etalon_team_without": slice_scores(runs, yn, sn, SLICES["etalon_team"], keep=kept)}
    return report, runs[0]["prob"], runs[0]["pred"], gaps, conformal_quantiles([r["prob"] for r in runs], yn)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", action="store_true")
    ap.add_argument("--repeats", type=int, default=10)
    args = ap.parse_args()
    data = Data()
    n = len(data.tech)
    # Кандидат радара приходит с одной фразой, а строки эталона — с двумя. Технология с двумя фразами
    # идёт в обучение ещё и по самой узкой фразе, с половинным весом, чтобы модель видела оба вида
    # входа. Широкая фраза отдельно не добавляется: у сигнала она часто мейнстрим («prompt
    # injection»), и одна она сигналом не является — такая метка учила модель пропускать хайп.
    items = [(i, t["gateway_phrases"], 1.0) for i, t in enumerate(data.tech)]
    items += [(i, [data.narrowest(t["gateway_phrases"])], SINGLE_WEIGHT) for i, t in enumerate(data.tech)
              if len(t["gateway_phrases"]) > 1]
    rows = [data.row(phrases) for _, phrases, _ in items]
    tab = np.array([r[0] for r in rows], float)
    emb = np.array([r[1] for r in rows], float)
    X = np.hstack([tab, emb])
    nt = tab.shape[1]
    single = [data.row([data.narrowest(t["gateway_phrases"])]) for t in data.tech]
    X1 = np.hstack([np.array([r[0] for r in single], float), np.array([r[1] for r in single], float)])
    tech = [data.tech[i] for i, _, _ in items]
    y = np.array([t["label"] for t in tech])
    g = np.array([t["group"] for t in tech])
    src = np.array([t["source"] for t in tech])
    area = np.array([t["area"] for t in tech])
    w = np.array([weight for _, _, weight in items])
    final, th, c = fit(X, y, g, w, nt)
    report, oof, weak, gaps, conformal = evaluate(data, X, X1, y, g, w, src, area, n, nt, args.repeats)
    # Радар применяет квантили к новым кандидатам, которых нет в выборке, поэтому здесь берутся все строки.
    # Порог внутри полосы «не могу решить» — тогда вне полосы решение множества совпадает с решением порога.
    assert 1 - conformal["weak"] <= th <= conformal["not_weak"], (th, conformal)

    ct, lr = final.steps[0][1], final.steps[1][1]
    imp, sc = ct.named_transformers_["t"].steps[0][1], ct.named_transformers_["t"].steps[1][1]
    esc, pca, psc = (ct.named_transformers_["e"].steps[i][1] for i in range(3))
    names = input_names()
    artifact = {
        "model": "weak-signal-logreg", "version": VERSION, "feature_schema": "weak-signal-features-v1",
        "threshold": th, "intercept": float(lr.intercept_[0]), "inputs": names,
        "conformal": {"alpha": CONFORMAL_ALPHA, **conformal},
        "impute": imp.statistics_.tolist(), "mean": sc.mean_.tolist(), "scale": sc.scale_.tolist(),
        "coef": lr.coef_[0][:nt].tolist(),
        # Диапазон входов на обучающих строках: за его краем радар прижимает вход и сигналом не называет.
        "min": np.nanmin(tab, axis=0).tolist(), "max": np.nanmax(tab, axis=0).tolist(),
        "embedding": {"model": EMBEDDING_MODEL, "mean": esc.mean_.tolist(), "scale": esc.scale_.tolist(),
                      "pca_mean": pca.mean_.tolist(), "components": pca.components_.tolist(),
                      "post_mean": psc.mean_.tolist(), "post_scale": psc.scale_.tolist(),
                      "coef": lr.coef_[0][nt:].tolist()},
        "training": {"seed": SEED, "month": MONTH, "c": c, "c_grid": list(C_GRID), "pca": PCA_K, "rows": n,
                     "single_phrase_rows": len(y) - n, "recall_target": RECALL_TARGET, "llm_rating": LLM_RATING,
                     "method": "L2-логистическая регрессия; вход — две фразы шлюза и средний эмбеддинг F2LLM",
                     "threshold_basis": "самый строгий порог с полнотой не ниже 80 % на групповой кросс-валидации"},
    }
    counts = {s: int((src[:n] == s).sum()) for s in dict.fromkeys(src[:n])}
    evaluation = {"version": VERSION, "month": MONTH, "dataset": counts, "threshold": th, "llm_rating": LLM_RATING,
                  **report,
                  "items": [{"id": t["id"], "name": t["name"], "class": t["class"], "area": t["area"], "source": t["source"],
                             "phrases": r[2], "validation": float(oof[i]), "validation_weak": bool(weak[i]),
                             "shipped": float(final.predict_proba(X[i:i + 1])[0, 1]), **({"data_gap": gaps[i]} if gaps[i] else {})}
                            for i, (t, r) in enumerate(zip(data.tech, rows))]}
    # Эталонные векторы для сверки Go: сырые входы и эмбеддинг в том же float16.
    pick = [0, 1, 5, 150, 300, 480]  # две фразы, одна, пусто в окне, Cosmos, хайп
    parity = []
    for i in pick:
        packed = data_emb_f16(data, rows[i][2])
        x = np.hstack([tab[i], decode_f16(packed)])[None, :]
        evidence = {p: {"window": data.window.get(p), "history": data.history.get(p), "hn": data.hn.get(p),
                        "llm": data.llm.get(p)} for p in rows[i][2]}
        parity.append({"phrases": data.tech[i]["gateway_phrases"], "evidence": evidence,
                       "inputs": [None if math.isnan(v) else v for v in tab[i]], "embedding": packed,
                       "probability": float(final.predict_proba(x)[0, 1])})
    out = {APP / "trend_live_classifier_weights.json": artifact, APP / "trend_live_classifier_evaluation.json": evaluation,
           APP / "testdata/weak_signal_parity.json": parity}
    text = {p: json.dumps(v, ensure_ascii=False, separators=(",", ":"), sort_keys=True) + "\n" for p, v in out.items()}
    if args.check:
        bad = [p.name for p, t in text.items() if not p.exists() or p.read_text() != t]
        print("совпадает" if not bad else f"расходится: {bad}")
        sys.exit(1 if bad else 0)
    for p, t in text.items():
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(t)
    cv, pct = report["nested_cv"], lambda v: f"{v:.1%}"
    print(f"порог {th:.2f}, C {c}; F1 {cv['f1_mean']:.3f}±{cv['f1_sd']:.3f} (мин {cv['f1_min']:.3f}), "
          f"P {cv['precision_mean']:.3f}, R {cv['recall_mean']:.3f}, AUC {cv['auc_mean']:.3f}")
    print("срезы F1:", {k: pct(v["f1"]) for k, v in report["slices"].items()})
    print("ложные по источникам:", {k: pct(v) for k, v in report["false_signal_by_source"].items()})
    print("без источника, ложные:", {k: pct(v) for k, v in report["leave_source_out_false_signal"].items()})
    print("без области, полнота:", {k: pct(v) for k, v in report["leave_area_out_recall"].items()})
    for k, v in report["ablations"].items():
        print(f"абляция {k}: P {pct(v['precision'])} R {pct(v['recall'])} F1 {pct(v['f1'])} AUC {v['auc']:.3f} "
              f"все {pct(v['all_f1'])} незнакомый хайп {pct(v['unseen_hype'])}")
    for k, v in report["outage"].items():
        print(f"отказ {k}: P {pct(v['precision'])} R {pct(v['recall'])} F1 {pct(v['f1'])} хайп {pct(v['false_signal']['hype'])}")
    print("воздержание:", report["abstention"]["by_source"], "F1 без них", pct(report["abstention"]["etalon_team_without"]["f1"]))
    cf = report["conformal"]
    print(f"конформное {1 - CONFORMAL_ALPHA:.0%}: полоса {1 - conformal['weak']:.3f}–{conformal['not_weak']:.3f}, "
          f"покрытие {pct(cf['coverage']['weak'])} / {pct(cf['coverage']['not_weak'])}, не могу решить "
          f"{pct(cf['undecided'])}, точность уверенного сигнала {cf['weak_precision']:.3f}; по источникам:",
          {k: pct(v["undecided"]) for k, v in cf["by_source"].items()})


def data_emb_f16(data, phrases):
    """Средний эмбеддинг фраз строки в float16 — ровно то, что прочитает Go."""
    vec = np.mean([data.emb[p] for p in phrases], axis=0)
    return base64.b64encode(b"".join(struct.pack("<e", float(x)) for x in vec)).decode()


if __name__ == "__main__":
    main()
