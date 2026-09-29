"""Explainable temporal eligibility and ordering, independent of descriptions.

This initial configuration is not a quality-frozen release. All measurements,
exclusions and parameters are retained; a missing period cannot become a zero.
"""
from __future__ import annotations

from dataclasses import asdict, dataclass
import math

from .temporal import date_interval

VERSION = 'temporal-emergence-v5-development-query-corpus-share'


@dataclass(frozen=True)
class Config:
    recent_quarters: int = 4
    prior_quarters: int = 4
    max_first_age_quarters: int = 20
    min_scientific_works: int = 2
    min_active_quarters: int = 2
    min_recent_works: int = 1
    smoothing: float = .5

    def validate(self):
        if (any(type(v) is not int or not 1 <= v <= 40 for v in (
                self.recent_quarters, self.prior_quarters, self.max_first_age_quarters,
                self.min_scientific_works, self.min_active_quarters, self.min_recent_works))
                or self.min_scientific_works < 2 or self.min_active_quarters < 2
                or not math.isfinite(self.smoothing) or not 0 < self.smoothing <= 10):
            raise ValueError('invalid bounded emergence parameters')


def _quarter(value: str) -> int:
    year, quarter = value.split('-Q')
    if len(year) != 4 or quarter not in ('1', '2', '3', '4'):
        raise ValueError('invalid measured quarter')
    return int(year)*4 + int(quarter)-1


def assess(measurements: dict, config: Config = Config()) -> dict:
    config.validate()
    series = measurements['series']
    numbers = [_quarter(row['period']) for row in series]
    if any(right != left + 1 for left, right in zip(numbers, numbers[1:])):
        raise ValueError('measured periods are not consecutive')
    for row in series:
        count, exposure = row['comparable_scientific_works'], row['comparable_exposure']
        if count is None and exposure is None:
            continue
        if type(count) is not int or type(exposure) is not int or not 0 <= count <= exposure <= 2000:
            raise ValueError('invalid comparable scientific counts')
        if not row['complete_period'] or row['coverage'] not in ('examined_bounded_population', 'query_corpus'):
            raise ValueError('unexamined period carries comparable counts')
        rate = row.get('normalized_activity')
        if rate is not None and (not math.isfinite(rate) or exposure == 0 or abs(rate-count/exposure) > 1e-12):
            raise ValueError('normalized activity differs from measured counts')
    full = [row for row in series if row['complete_period']]
    if any(_quarter(right['period']) != _quarter(left['period']) + 1 for left, right in zip(full, full[1:])):
        raise ValueError('complete measurement window has a missing quarter')
    window_size = config.recent_quarters + config.prior_quarters
    window = full[-window_size:]
    reasons = []
    if len(window) != window_size or any(row['comparable_exposure'] is None for row in window):
        reasons.append('comparable_coverage_missing')
    prior = window[:-config.recent_quarters]
    recent = window[-config.recent_quarters:]
    recent_found = sum(row['found']['scientific_works'] for row in recent)
    total_found = sum(row['found']['scientific_works'] for row in full)
    active_periods = sum(row['found']['scientific_works'] > 0 for row in full)
    if total_found < config.min_scientific_works:
        reasons.append('too_few_dated_scientific_works')
    if active_periods < config.min_active_quarters:
        reasons.append('too_few_scientific_periods')
    if recent_found < config.min_recent_works:
        reasons.append('no_recent_scientific_support')
    earliest = measurements.get('earliest_scientific')
    age = None
    if earliest is None:
        reasons.append('scientific_first_mention_unknown')
    elif full:
        start, _ = date_interval(earliest['date'])
        first_quarter = start.year*4 + (start.month-1)//3
        age = _quarter(full[-1]['period']) - first_quarter + 1
        # A first source in the current partial quarter has zero completed
        # quarters of history. It stays outside the full-period support counts.
        current_partial = (age == 0 and not series[-1]['complete_period']
                           and first_quarter == numbers[-1])
        if age < 1 and not current_partial:
            raise ValueError('first scientific source follows measured cutoff')
        if age > config.max_first_age_quarters:
            reasons.append('established_scientific_history')
    uncertain = len(measurements.get('unallocated_scientific_work_ids') or [])
    # A work dated only by year cannot be placed in a quarter. A few such works
    # leave the series usable; when they are a quarter of the topic or more,
    # the series itself is in doubt.
    if uncertain and uncertain * 4 > uncertain + total_found:
        reasons.append('scientific_date_uncertainty')
    prior_found = sum(row['found']['scientific_works'] for row in prior)
    features = {'dated_scientific_works': total_found, 'active_scientific_quarters': active_periods,
                'recent_found_works': recent_found, 'prior_found_works': prior_found, 'first_age_quarters': age,
                'uncertain_dated_works': uncertain,
                'prior_count': None, 'prior_exposure': None, 'recent_count': None, 'recent_exposure': None,
                'prior_rate': None, 'recent_rate': None, 'log_rate_ratio': None, 'log_count_ratio': None}
    comparable = 'comparable_coverage_missing' not in reasons
    if not comparable:
        # Without a verified population the share cannot be normalized; the
        # found counts still order topics, and the answer says so (basis).
        features['log_count_ratio'] = math.log((recent_found + config.smoothing) / (prior_found + config.smoothing))
        if features['log_count_ratio'] <= 0:
            reasons.append('scientific_count_not_increasing')
    if comparable:
        for name, rows in (('prior', prior), ('recent', recent)):
            features[name + '_count'] = sum(row['comparable_scientific_works'] for row in rows)
            features[name + '_exposure'] = sum(row['comparable_exposure'] for row in rows)
        if features['prior_exposure'] == 0 or features['recent_exposure'] == 0:
            reasons.append('scientific_exposure_absent')
        else:
            for name in ('prior', 'recent'):
                features[name + '_rate'] = ((features[name + '_count'] + config.smoothing)
                                            / (features[name + '_exposure'] + 2*config.smoothing))
            features['log_rate_ratio'] = math.log(features['recent_rate']/features['prior_rate'])
            if features['log_rate_ratio'] <= 0:
                reasons.append('scientific_share_not_increasing')
    eligible = not [reason for reason in reasons if reason != 'comparable_coverage_missing']
    if not eligible:
        order = None
    elif comparable:
        order = [features['log_rate_ratio'], features['recent_rate'], active_periods, total_found]
    else:
        order = [features['log_count_ratio'], recent_found, active_periods, total_found]
    if not comparable:
        basis = 'observed_counts'
    elif all(row['coverage'] == 'examined_bounded_population' for row in window):
        basis = 'normalized_rates'
    else:
        basis = 'query_corpus_share'
    return {'version': VERSION, 'quality_status': 'not_frozen', 'parameters': asdict(config),
            'eligible': eligible, 'basis': basis,
            'status': 'temporal_candidate' if eligible else 'insufficient_or_established',
            'reasons': reasons, 'features': features, 'order': order,
            'support_unit': 'distinct_canonical_papers_not_independent_experimental_replications',
            'growth_meaning': ('change_in_smoothed_share_of_examined_scientific_population'
                               if basis == 'normalized_rates' else
                               'change_in_smoothed_share_of_query_corpus_works' if comparable else
                               'change_in_smoothed_count_of_found_scientific_works_coverage_unverified'),
            'ordering': (['log_rate_ratio', 'recent_rate'] if comparable else ['log_count_ratio', 'recent_found_works'])
            + ['active_scientific_quarters', 'dated_scientific_works', 'candidate_id_ascending_for_equal_features']}
