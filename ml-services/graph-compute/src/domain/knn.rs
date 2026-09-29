//! Векторы нормирует вызывающий, поэтому скалярное произведение — косинус.
//! Ничьи уходят к меньшему индексу, иначе граф недетерминирован.

use rayon::prelude::*;
use smallvec::SmallVec;

/// Ёмкость 8 при `k = 6` по умолчанию держит обычный случай без кучи.
pub type Neighbors = SmallVec<[(usize, f64); 8]>;

/// Строки плитки (32 вектора по 4 КБ) живут в L2, и каждый вектор корпуса читается
/// из памяти один раз на плитку, а не на строку: 48 тыс. векторов — это 196 МБ, и
/// построчный обход упирался в пропускную способность памяти.
const ROW_TILE: usize = 32;

/// Матрица `n × n` не строится; блоки и плитки нужны только для локальности
/// памяти, от их размера результат не зависит.
#[must_use]
pub fn build_knn_maps(vectors: &[Vec<f32>], k: usize, block_size: usize) -> Vec<Neighbors> {
    let n = vectors.len();
    let block = block_size.max(1);
    let mut maps: Vec<Neighbors> = vec![SmallVec::new(); n];
    maps.par_chunks_mut(block).enumerate().for_each(|(chunk_idx, chunk)| {
        for (tile_idx, tile) in chunk.chunks_mut(ROW_TILE).enumerate() {
            let first = chunk_idx * block + tile_idx * ROW_TILE;
            let rows = &vectors[first..first + tile.len()];
            for (j, other) in vectors.iter().enumerate() {
                for (i, (best, row)) in (first..).zip(tile.iter_mut().zip(rows)) {
                    if i == j {
                        continue;
                    }
                    let cosine = cosine(row, other).clamp(-1.0, 1.0);
                    if cosine.is_finite() {
                        insert_top_k(best, k, j, cosine);
                    }
                }
            }
        }
    });
    maps
}

fn insert_top_k(best: &mut Neighbors, k: usize, j: usize, cosine: f64) {
    if best.len() >= k {
        let (worst_j, worst_c) = best[best.len() - 1];
        if cosine < worst_c || (cosine == worst_c && j > worst_j) {
            return;
        }
        best.pop();
    }
    let position = best
        .iter()
        .position(|&(ej, ec)| cosine > ec || (cosine == ec && j < ej))
        .unwrap_or(best.len());
    best.insert(position, (j, cosine));
}

/// Восемь независимых сумм векторизуются под базовый x86-64. Прежний `mul_add` в
/// одной цепочке без FMA в сборке звал `fmaf` из libm на каждое число и был в
/// одиннадцать раз медленнее.
#[inline]
fn cosine(a: &[f32], b: &[f32]) -> f64 {
    let n = a.len().min(b.len());
    let ((a8, a_tail), (b8, b_tail)) = (a[..n].as_chunks::<8>(), b[..n].as_chunks::<8>());
    let mut lanes = [0.0_f32; 8];
    for (x, y) in a8.iter().zip(b8) {
        for ((lane, x), y) in lanes.iter_mut().zip(x).zip(y) {
            *lane += x * y;
        }
    }
    let tail: f32 = a_tail.iter().zip(b_tail).map(|(x, y)| x * y).sum();
    f64::from(lanes.iter().sum::<f32>() + tail)
}

#[must_use]
#[inline]
pub fn neighbors_contains(neighbors: &[(usize, f64)], target: usize) -> bool {
    neighbors.iter().any(|&(j, _)| j == target)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn top_k_excludes_self_and_orders_by_cosine() {
        let raw: Vec<Vec<f32>> = vec![vec![1.0, 0.0], vec![0.9, 0.1], vec![0.0, 1.0]];
        let normed: Vec<Vec<f32>> =
            raw.iter().map(|v| crate::domain::vector::l2_normalize(v)).collect();
        let maps = build_knn_maps(&normed, 2, 512);
        assert_eq!(maps.len(), 3);
        assert_eq!(maps[0][0].0, 1);
        assert!(!neighbors_contains(&maps[0], 0));
        assert!(neighbors_contains(&maps[0], 1));
    }

    #[test]
    fn block_size_does_not_change_result() {
        let vectors = vec![
            vec![1.0, 0.0, 0.0],
            vec![0.8, 0.2, 0.0],
            vec![0.0, 1.0, 0.0],
            vec![0.0, 0.9, 0.1],
        ];
        let a = build_knn_maps(&vectors, 2, 512);
        let b = build_knn_maps(&vectors, 2, 1);
        assert_eq!(a, b);
    }
}
