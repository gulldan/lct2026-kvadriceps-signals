/** Чередуем существующие группы, сохраняя порядок внутри каждой: оценка и фильтры не меняются. */
export function interleaveBy<T>(items: readonly T[], groupOf: (item: T) => string): T[] {
  const groups = new Map<string, T[]>();
  for (const item of items) {
    const key = groupOf(item).trim().toLowerCase();
    const group = groups.get(key);
    if (group) group.push(item);
    else groups.set(key, [item]);
  }
  const buckets = [...groups.values()];
  const result: T[] = [];
  for (let row = 0; result.length < items.length; row++) {
    for (const group of buckets) {
      if (row < group.length) result.push(group[row]!);
    }
  }
  return result;
}

/** Чередуем группы только внутри равных по ключу отрезков уже отсортированного списка: порядок по
 *  ключу (например, росту) не нарушается, разнообразие остаётся среди одинаковых значений. */
export function interleaveTies<T>(
  ranked: readonly T[],
  tieOf: (item: T) => string,
  groupOf: (item: T) => string,
): T[] {
  const runs: T[][] = [];
  ranked.forEach((item, index) => {
    if (index === 0 || tieOf(item) !== tieOf(ranked[index - 1]!)) runs.push([]);
    runs.at(-1)!.push(item);
  });
  return runs.flatMap((run) => interleaveBy(run, groupOf));
}
