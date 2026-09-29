use crate::domain::graph::Graph;
use crate::domain::vector::l2_normalize;

#[must_use]
#[inline]
pub fn round4(value: f64) -> f64 {
    (value * 10_000.0).round() / 10_000.0
}

/// Модулярность Ньюмана `Q = Σ_c [L_c/m − (k_c/2m)²]`. Как в Python, `Q`
/// округляется от неокруглённой суммы, а каждый вклад — отдельно.
#[must_use]
pub fn modularity_terms(graph: &Graph, communities: &[Vec<usize>]) -> (f64, Vec<f64>) {
    let m = graph.total_weight();
    if m <= 0.0 {
        return (0.0, vec![0.0; communities.len()]);
    }

    let mut member = vec![false; graph.node_count()];
    let mut contributions = Vec::with_capacity(communities.len());
    for nodes in communities {
        for &u in nodes {
            member[u] = true;
        }
        let mut internal = 0.0;
        let mut degree_sum = 0.0;
        for &u in nodes {
            for &(neighbor, weight) in graph.neighbors(u) {
                // Смежность симметрична: внутреннее ребро считаем один раз.
                if neighbor >= u && member[neighbor] {
                    internal += weight;
                }
            }
            degree_sum += graph.degree(u);
        }
        let fraction = degree_sum / (2.0 * m);
        let internal_fraction = internal / m;
        let expected_fraction = fraction * fraction;
        contributions.push(internal_fraction - expected_fraction);
        for &u in nodes {
            member[u] = false;
        }
    }

    let total = round4(contributions.iter().sum());
    let rounded = contributions.iter().map(|&x| round4(x)).collect();
    (total, rounded)
}

#[must_use]
pub fn avg_intra_similarity(member_vectors: &[&[f32]]) -> f64 {
    let count = member_vectors.len();
    if count <= 1 {
        return 1.0;
    }
    // Σ_{i<j} u_i·u_j = (|Σu|² − Σ|u|²) / 2: линейно по числу документов. Перебор
    // пар на теме в тысячу документов стоил секунду, на десяти тысячах — минуты.
    let dim = member_vectors.iter().map(|v| v.len()).max().unwrap_or(0);
    let mut total = vec![0.0_f64; dim];
    let mut self_dots = 0.0;
    for vector in member_vectors {
        for (slot, &x) in total.iter_mut().zip(&l2_normalize(vector)) {
            let x = f64::from(x);
            *slot += x;
            self_dots += x * x;
        }
    }
    let pairs = (count * (count - 1) / 2) as f64;
    let sum = (total.iter().map(|s| s * s).sum::<f64>() - self_dots) / 2.0;
    round4((sum / pairs).clamp(-1.0, 1.0))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round4_rounds_to_four_decimals() {
        assert!((round4(0.123_456) - 0.1235).abs() < f64::EPSILON);
        assert!((round4(1.0) - 1.0).abs() < f64::EPSILON);
    }

    #[test]
    fn modularity_of_two_disjoint_pairs() {
        let graph = Graph::from_edges(4, vec![(0, 1, 1.0), (2, 3, 1.0)]);
        let (q, contribs) = modularity_terms(&graph, &[vec![0, 1], vec![2, 3]]);
        assert_eq!(contribs.len(), 2);
        assert!(q > 0.0);
        assert!((contribs[0] - contribs[1]).abs() < 1e-9);
    }

    #[test]
    fn modularity_edgeless_is_zero() {
        let graph = Graph::from_edges(3, vec![]);
        let (q, contribs) = modularity_terms(&graph, &[vec![0], vec![1], vec![2]]);
        assert!((q - 0.0).abs() < f64::EPSILON);
        assert_eq!(contribs, vec![0.0, 0.0, 0.0]);
    }

    #[test]
    fn avg_intra_similarity_basics() {
        assert!((avg_intra_similarity(&[&[1.0, 0.0]]) - 1.0).abs() < f64::EPSILON);
        let identical: Vec<&[f32]> = vec![&[1.0, 0.0], &[1.0, 0.0]];
        assert!((avg_intra_similarity(&identical) - 1.0).abs() < f64::EPSILON);
        let orthogonal: Vec<&[f32]> = vec![&[1.0, 0.0], &[0.0, 1.0]];
        assert!((avg_intra_similarity(&orthogonal) - 0.0).abs() < f64::EPSILON);
    }
}
