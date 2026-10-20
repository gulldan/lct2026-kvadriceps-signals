"""Сравнение моделей без перезаписи весов радара; признаки и группировка совпадают с train.py.

uv run --with-requirements scripts/weak-signal/requirements.txt \
    python scripts/weak-signal/benchmark.py --output weak-signal-benchmark.json
"""

import argparse
import hashlib
import json
import platform
from collections import Counter
from pathlib import Path

import numpy as np
import sklearn
from sklearn.ensemble import HistGradientBoostingClassifier
from sklearn.linear_model import LogisticRegression
from sklearn.metrics import brier_score_loss, precision_score, recall_score, roc_auc_score
from sklearn.model_selection import StratifiedGroupKFold
from threadpoolctl import threadpool_limits

import train


SCENARIOS = {"full": (), "no_arxiv": ("window",), "no_hn": ("hn",),
             "no_arxiv_hn": ("window", "hn")}
IT_AREAS = {"Edge", "Защита ИИ", "Индустриальный ИИ", "Инфраструктура ИИ", "Роботы", "Финтех"}
UNLABELLED_AREAS = {"Cosmos", "Ретроспектива хайп-циклов"}
PARAMETERS = {"logreg": train.C_GRID, "hist_gradient_boosting": (3, 7, 15)}


def leakage_groups(tech):
    """Общая фраза тоже связывает строки: одинаковые измерения не должны попадать по обе стороны проверки."""
    parents = list(range(len(tech)))
    seen = {}

    def root(index):
        while parents[index] != index:
            parents[index] = parents[parents[index]]
            index = parents[index]
        return index

    for i, row in enumerate(tech):
        for key in ["group:" + row["group"], *("phrase:" + p.casefold().strip() for p in row["gateway_phrases"])]:
            if key in seen:
                parents[root(i)] = root(seen[key])
            else:
                seen[key] = i
    return [root(i) for i in range(len(tech))]


def dataset():
    data = train.Data()
    items = [(i, t["gateway_phrases"], 1.0) for i, t in enumerate(data.tech)]
    items += [(i, [data.narrowest(t["gateway_phrases"])], train.SINGLE_WEIGHT)
              for i, t in enumerate(data.tech) if len(t["gateway_phrases"]) > 1]
    rows = [data.row(phrases) for _, phrases, _ in items]
    tab = np.array([r[0] for r in rows], float)
    X = np.hstack([tab, np.array([r[1] for r in rows], float)])
    tech = [data.tech[i] for i, _, _ in items]
    groups = leakage_groups(data.tech)
    return (data, X, np.array([t["label"] for t in tech]), np.array([groups[i] for i, _, _ in items]),
            np.array([w for _, _, w in items]), tab.shape[1])


def grouped_folds(X, y, groups, folds, seed):
    result = list(StratifiedGroupKFold(folds, shuffle=True, random_state=seed).split(X, y, groups))
    for training, heldout in result:
        if set(groups[training]) & set(groups[heldout]):
            raise AssertionError("a technology group crossed the validation boundary")
        if len(set(y[training])) < 2 or len(set(y[heldout])) < 2:
            raise ValueError("each validation fold must contain both classes")
    return result


def estimator(name, parameter):
    if name == "logreg":
        return LogisticRegression(C=parameter, max_iter=5000)
    # Внутренняя случайная валидация разделила бы варианты одной технологии между обучением и проверкой.
    return HistGradientBoostingClassifier(max_leaf_nodes=parameter, max_iter=120, learning_rate=0.05,
                                         min_samples_leaf=15, l2_regularization=1.0,
                                         early_stopping=False, random_state=train.SEED)


def threshold(y, probability, weights):
    return float(max((q for q in train.THRESHOLDS
                      if recall_score(y, probability >= q, sample_weight=weights) >= train.RECALL_TARGET),
                     default=train.THRESHOLDS[0]))


def select(X, y, groups, weights, nt):
    """Преобразования, параметры и пороги видят только внешнюю обучающую часть."""
    inner = []
    for training, heldout in grouped_folds(X, y, groups, 3, 7):
        transform = train.model(nt, 1).steps[0][1]
        inner.append((training, heldout, transform.fit_transform(X[training]), transform.transform(X[heldout])))
    chosen = {}
    for name, parameters in PARAMETERS.items():
        best = None
        for parameter in parameters:
            probability = np.zeros(len(y))
            for training, heldout, Xtrain, Xtest in inner:
                model = estimator(name, parameter).fit(Xtrain, y[training], sample_weight=weights[training])
                probability[heldout] = model.predict_proba(Xtest)[:, 1]
            cutoff = threshold(y, probability, weights)
            precision = precision_score(y, probability >= cutoff, sample_weight=weights, zero_division=0)
            if best is None or precision > best[0]:
                best = (precision, parameter, cutoff)
        chosen[name] = best[1:]
    return chosen


def metrics(y, probability, prediction, selected):
    if not np.any(selected):
        return {"n": 0, "positive": 0, "negative": 0, "reason": "no independently labelled examples"}
    y, probability, prediction = y[selected], probability[selected], prediction[selected]
    result = {"n": len(y), "positive": int(sum(y == 1)), "negative": int(sum(y == 0)),
              **train.scores(y, prediction), "brier": brier_score_loss(y, probability)}
    if len(set(y)) == 2:
        result["auc"] = roc_auc_score(y, probability)
    else:
        result["auc"] = None
        result["reason"] = "one class only; discrimination is not estimable"
    return result


def coverage(data):
    result = {}
    for area in sorted({t["area"] for t in data.tech}):
        rows = [t for t in data.tech if t["area"] == area]
        result[area] = {"n": len(rows), "positive": sum(t["label"] == 1 for t in rows),
                        "negative": sum(t["label"] == 0 for t in rows)}
    return {"areas": result, "it_positive": sum(t["label"] == 1 and t["area"] in IT_AREAS for t in data.tech),
            "non_it_positive": sum(t["label"] == 1 and t["area"] not in IT_AREAS | UNLABELLED_AREAS for t in data.tech),
            "unknown_domain": sum(t["area"] in UNLABELLED_AREAS for t in data.tech),
            "note": "Cosmos and hype-retrospective are dataset origins, not domain annotations; "
                    "they are not counted as a non-IT validation set."}


def run(data, X, y, groups, weights, nt, repeats):
    n = len(data.tech)
    names = train.input_names()
    area = np.array([t["area"] for t in data.tech])
    source = np.array([t["source"] for t in data.tech])
    slices = {"all": np.ones(n, bool), "etalon_team": np.isin(source, ("etalon", "team")),
              "it_labelled": np.isin(area, tuple(IT_AREAS)),
              "non_it_labelled": ~np.isin(area, tuple(IT_AREAS | UNLABELLED_AREAS))}
    report = {}
    for scenario, removed in SCENARIOS.items():
        keep = [i for i, name in enumerate(names) if train.input_group(name) not in removed]
        rows = np.hstack([X[:, keep], X[:, nt:]])
        runs = {name: [] for name in PARAMETERS}
        for repeat in range(repeats):
            current = {name: {"probability": np.zeros(n), "prediction": np.zeros(n, bool), "choices": [],
                              "outage": {k: np.zeros(n) for k in ("window", "hn") if k not in removed}}
                       for name in PARAMETERS}
            for training, heldout in grouped_folds(rows[:n], y[:n], groups[:n], 5, train.SEED + repeat):
                selected = np.isin(groups, groups[training])
                if set(groups[selected]) & set(groups[heldout]):
                    raise AssertionError("an augmented phrase crossed the outer validation boundary")
                choices = select(rows[selected], y[selected], groups[selected], weights[selected], len(keep))
                transform = train.model(len(keep), 1).steps[0][1]
                Xtrain = transform.fit_transform(rows[selected])
                Xtest = transform.transform(rows[heldout])
                for name, (parameter, cutoff) in choices.items():
                    model = estimator(name, parameter).fit(Xtrain, y[selected], sample_weight=weights[selected])
                    probability = model.predict_proba(Xtest)[:, 1]
                    current[name]["probability"][heldout] = probability
                    current[name]["prediction"][heldout] = probability >= cutoff
                    current[name]["choices"].append({"parameter": parameter, "threshold": cutoff})
                    for missing in current[name]["outage"]:
                        outage = rows[heldout].copy()
                        outage[:, [i for i, col in enumerate(keep) if train.input_group(names[col]) == missing]] = np.nan
                        current[name]["outage"][missing][heldout] = model.predict_proba(transform.transform(outage))[:, 1]
            for name, values in current.items():
                p, prediction = values["probability"], values["prediction"]
                runs[name].append({"repeat": repeat, "selection": values["choices"],
                                   "metrics": {key: metrics(y[:n], p, prediction, sel) for key, sel in slices.items()},
                                   "outage_probability_shift": {key: {"mean": float(np.mean(value-p)),
                                                                        "mean_absolute": float(np.mean(abs(value-p)))}
                                                                 for key, value in values["outage"].items()},
                                   "items": [{"id": t["id"], "probability": float(p[i]), "weak": bool(prediction[i])}
                                             for i, t in enumerate(data.tech)]})
            print(f"{scenario}: repeat {repeat + 1}/{repeats} complete", flush=True)
        report[scenario] = runs
    return report


def shipped_source_audit():
    artifact = json.loads((train.APP / "trend_live_classifier_weights.json").read_text())
    mean, scale, impute, coef = (np.array(artifact[k]) for k in ("mean", "scale", "impute", "coef"))
    window = train.window_inputs({"documents": 0, "active_periods": 0, "author_groups": 0})
    values = np.array(window + window + [0.] * 14 + train.hn_inputs(
        {"before": 0, "mid": 0, "recent": 0, "recent_story": 0, "mid_story": 0}) * 2 + [0.] * 6)
    contributions = (np.where(np.isnan(values), impute, values) - mean) / scale * coef
    return {"version": artifact["version"], "empty_arxiv_log_odds": float(sum(contributions[:14])),
            "empty_hn_log_odds": float(sum(contributions[28:40])),
            "note": "Fixed other inputs; two phrase positions. Journal coverage policy neutralizes only an "
                    "observed empty arXiv window when matching journal evidence exists. HN remains unchanged. "
                    "This decomposition is not a domain quality evaluation."}


def paired_comparison(report, y, groups):
    """Парная оценка разницы на одних отложенных группах; не заменяет независимый временной набор."""
    runs = report["full"]
    predictions = [np.array([item["weak"] for item in runs[name][0]["items"]]) for name in PARAMETERS]
    counts = []
    for group in np.unique(groups):
        mask = groups == group
        counts.append([[sum(p[mask] & (y[mask] == 1)), sum(p[mask] & (y[mask] == 0)),
                        sum(~p[mask] & (y[mask] == 1))] for p in predictions])
    counts = np.array(counts)
    rng = np.random.default_rng(train.SEED)
    differences = []
    for _ in range(2000):
        sample = counts[rng.choice(len(counts), len(counts), replace=True)].sum(axis=0)
        f1 = 2 * sample[:, 0] / np.maximum(2 * sample[:, 0] + sample[:, 1] + sample[:, 2], 1)
        differences.append(f1[1] - f1[0])
    lo, hi = np.quantile(differences, [.025, .975])
    return {"comparison": "hist_gradient_boosting minus logreg, full features, first repeat",
            "f1_difference": runs["hist_gradient_boosting"][0]["metrics"]["all"]["f1"] -
                             runs["logreg"][0]["metrics"]["all"]["f1"],
            "group_bootstrap_95_interval": [float(lo), float(hi)], "bootstrap_repeats": 2000,
            "note": "Conditional on these out-of-fold predictions; does not measure retraining variability."}


def selfcheck():
    X = np.arange(48).reshape(24, 2)
    y, groups = np.repeat([0, 1] * 6, 2), np.repeat(np.arange(12), 2)
    folds = grouped_folds(X, y, groups, 3, train.SEED)
    assert sorted(np.concatenate([heldout for _, heldout in folds])) == list(range(24))
    assert estimator("hist_gradient_boosting", 7).early_stopping is False
    linked = leakage_groups([{"group": "a", "gateway_phrases": ["common"]},
                             {"group": "b", "gateway_phrases": ["COMMON", "other"]},
                             {"group": "c", "gateway_phrases": ["other"]}])
    assert len(set(linked)) == 1
    assert metrics(y, y.astype(float), y.astype(bool), np.zeros(24, bool))["n"] == 0
    cutoff = threshold(np.array([0, 1, 1, 1, 1, 1]), np.array([.2, .3, .4, .5, .6, .7]), np.ones(6))
    assert cutoff == .4
    audit = shipped_source_audit()
    assert audit["empty_arxiv_log_odds"] < 0 < audit["empty_hn_log_odds"]
    same = {name: [{"items": [{"weak": bool(label)} for label in y], "metrics": {"all": {"f1": 1.0}}}]
            for name in PARAMETERS}
    assert paired_comparison({"full": same}, y, groups)["group_bootstrap_95_interval"] == [0.0, 0.0]
    print("benchmark selfcheck passed")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path)
    parser.add_argument("--repeats", type=int, default=1)
    parser.add_argument("--selfcheck", action="store_true")
    args = parser.parse_args()
    if args.selfcheck:
        selfcheck()
        return
    if args.output is None or args.repeats < 1:
        parser.error("--output and a positive --repeats value are required")
    data, X, y, groups, weights, nt = dataset()
    with threadpool_limits(limits=1):
        report = run(data, X, y, groups, weights, nt, args.repeats)
    output = {"method": "exploratory-nested-grouped-model-comparison-v1", "seed": train.SEED,
              "month": train.MONTH, "outer_folds": 5, "inner_folds": 3, "repeats": args.repeats,
              "rows": len(data.tech), "augmented_rows": len(y) - len(data.tech),
              "validation_groups": len(set(groups)),
              "grouping": "Connected components of original technology groups and shared gateway phrases",
              "sources": dict(Counter(t["source"] for t in data.tech)), "coverage": coverage(data),
              "parameters": PARAMETERS, "recall_target": train.RECALL_TARGET,
              "versions": {"python": platform.python_version(), "numpy": np.__version__, "sklearn": sklearn.__version__},
              "data_sha256": {p.name: hashlib.sha256(p.read_bytes()).hexdigest()
                              for p in sorted((train.HERE / "data").glob("*.json"))},
              "source_audit": shipped_source_audit(), "experiments": report,
              "paired_comparison": paired_comparison(report, y[:len(data.tech)], groups[:len(data.tech)]),
              "limitations": ["All positive labels cover IT-related domains. Non-IT quality is unmeasured.",
                              "Random grouped validation is not temporal forecasting validation.",
                              "Both models use the same fold-fitted imputation, scaling and 16-component PCA.",
                              "Ablations retrain without a source; outages replace held-out values with training medians.",
                              "Journal evidence is absent from this frozen dataset; coverage policy is not evaluated.",
                              "This report does not replace shipped weights or declare a deployment winner."]}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(output, ensure_ascii=False, indent=2, allow_nan=False) + "\n")
    print(args.output)


if __name__ == "__main__":
    main()
