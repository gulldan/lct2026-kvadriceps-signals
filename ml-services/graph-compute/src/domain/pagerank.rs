//! Персонализированный PageRank (HippoRAG-2). Вектор рангов — распределение с
//! суммой 1, поэтому оценки сравнимы в пределах запроса.

use std::cmp::Ordering;

use ahash::AHashMap;
use serde::{Deserialize, Serialize};

use crate::domain::cluster::ComputeStats;
use crate::domain::vector::DocVector;
use crate::domain::Graph;

const CONVERGENCE_TOL: f64 = 1e-9;

#[derive(Clone, Debug, Default)]
pub struct RankOverrides {
    pub damping: Option<f64>,
    pub max_iterations: Option<usize>,
    pub top_n: Option<usize>,
}

/// Умолчания совпадают с описанными в proto.
#[derive(Clone, Debug, PartialEq)]
pub struct RankConfig {
    pub damping: f64,
    pub max_iterations: usize,
    /// `0` — вернуть все узлы.
    pub top_n: usize,
}

impl RankConfig {
    #[must_use]
    pub fn resolve(overrides: RankOverrides) -> Self {
        Self {
            damping: overrides.damping.unwrap_or(0.85).clamp(0.0, 1.0),
            max_iterations: overrides.max_iterations.unwrap_or(100).max(1),
            top_n: overrides.top_n.unwrap_or(0),
        }
    }
}

impl Default for RankConfig {
    fn default() -> Self {
        Self::resolve(RankOverrides::default())
    }
}

#[derive(Clone, Debug, Default, Deserialize, PartialEq, Serialize)]
pub struct RankedNode {
    pub id: String,
    pub score: f64,
}

#[derive(Clone, Debug, Default, Deserialize, PartialEq, Serialize)]
pub struct RankOutput {
    pub nodes: Vec<RankedNode>,
    pub stats: ComputeStats,
}

/// `modularity` и `compute_ms` остаются нулевыми: разбиения здесь нет, а время
/// замеряет вызывающий.
#[must_use]
pub fn rank_nodes(
    docs: &[DocVector],
    graph: &Graph,
    seed_ids: &[String],
    config: &RankConfig,
) -> RankOutput {
    let n = docs.len();
    let stats = ComputeStats {
        document_count: n as u32,
        edge_count: graph.edge_count() as u32,
        modularity: 0.0,
        compute_ms: 0,
    };
    if n == 0 {
        return RankOutput { nodes: Vec::new(), stats };
    }

    let personalization = personalization_vector(docs, n, seed_ids);
    let ranks =
        personalized_pagerank(graph, &personalization, config.damping, config.max_iterations);

    let mut nodes: Vec<RankedNode> = docs
        .iter()
        .zip(&ranks)
        .map(|(doc, &score)| RankedNode { id: doc.id.to_string(), score })
        .collect();
    nodes.sort_by(|a, b| {
        b.score.partial_cmp(&a.score).unwrap_or(Ordering::Equal).then_with(|| a.id.cmp(&b.id))
    });
    if config.top_n > 0 {
        nodes.truncate(config.top_n);
    }
    RankOutput { nodes, stats }
}

fn personalization_vector(docs: &[DocVector], n: usize, seed_ids: &[String]) -> Vec<f64> {
    let index: AHashMap<&str, usize> =
        docs.iter().enumerate().map(|(node, doc)| (doc.id.as_ref(), node)).collect();
    let mut seeds: Vec<usize> =
        seed_ids.iter().filter_map(|id| index.get(id.as_str()).copied()).collect();
    seeds.sort_unstable();
    seeds.dedup();

    if seeds.is_empty() {
        return vec![1.0 / n as f64; n];
    }
    let share = 1.0 / seeds.len() as f64;
    let mut vector = vec![0.0; n];
    for &node in &seeds {
        vector[node] = share;
    }
    vector
}

/// Масса висячих узлов телепортируется в затравки вместе с `1 - damping`: так
/// масса сохраняется и вектор остаётся распределением.
fn personalized_pagerank(
    graph: &Graph,
    personalization: &[f64],
    damping: f64,
    max_iterations: usize,
) -> Vec<f64> {
    let n = graph.node_count();
    if n == 0 {
        return Vec::new();
    }
    let degrees: Vec<f64> = (0..n).map(|node| graph.degree(node)).collect();
    let mut rank = personalization.to_vec();

    for _ in 0..max_iterations {
        let dangling: f64 = rank
            .iter()
            .zip(&degrees)
            .filter(|(_, &degree)| degree <= 0.0)
            .map(|(&mass, _)| mass)
            .sum();
        let teleport = damping.mul_add(dangling, 1.0 - damping);
        let mut next: Vec<f64> = personalization.iter().map(|&share| teleport * share).collect();

        // У узла на ребре степень > 0, поэтому деление безопасно.
        for &(a, b, weight) in graph.edges() {
            next[b] += damping * rank[a] * weight / degrees[a];
            next[a] += damping * rank[b] * weight / degrees[b];
        }

        let delta: f64 = next.iter().zip(&rank).map(|(&x, &y)| (x - y).abs()).sum();
        rank = next;
        if delta < CONVERGENCE_TOL {
            break;
        }
    }
    rank
}

#[cfg(test)]
mod tests {
    use super::*;

    fn doc(id: &str) -> DocVector {
        DocVector {
            id: id.into(),
            vector: vec![1.0, 0.0],
            rep_text: String::new().into(),
            chunk_count: 1,
            filename: None,
        }
    }

    fn docs(ids: &[&str]) -> Vec<DocVector> {
        ids.iter().map(|id| doc(id)).collect()
    }

    fn score_of(out: &RankOutput, id: &str) -> f64 {
        out.nodes.iter().find(|node| node.id == id).map_or(0.0, |node| node.score)
    }

    #[test]
    fn uniform_pagerank_ranks_the_hub_highest() {
        let documents = docs(&["n0", "n1", "n2", "n3", "n4"]);
        let graph = Graph::from_edges(5, vec![(0, 1, 1.0), (0, 2, 1.0), (0, 3, 1.0), (0, 4, 1.0)]);

        let out = rank_nodes(&documents, &graph, &[], &RankConfig::default());

        assert_eq!(out.nodes.len(), 5);
        assert_eq!(out.nodes[0].id, "n0", "the hub ranks first");
        let hub = score_of(&out, "n0");
        for leaf in ["n1", "n2", "n3", "n4"] {
            assert!(hub > score_of(&out, leaf), "hub outranks every leaf");
        }
        let total: f64 = out.nodes.iter().map(|node| node.score).sum();
        assert!((total - 1.0).abs() < 1e-9);
    }

    #[test]
    fn seeded_ppr_concentrates_in_the_seed_neighborhood() {
        let documents = docs(&["n0", "n1", "n2", "n3", "n4", "n5"]);
        let graph = Graph::from_edges(
            6,
            vec![
                (0, 1, 1.0),
                (0, 2, 1.0),
                (1, 2, 1.0),
                (3, 4, 1.0),
                (3, 5, 1.0),
                (4, 5, 1.0),
                (2, 3, 0.1),
            ],
        );

        let out = rank_nodes(&documents, &graph, &["n0".to_owned()], &RankConfig::default());

        assert_eq!(out.nodes[0].id, "n0", "the seed ranks first");
        let a_min =
            ["n0", "n1", "n2"].into_iter().map(|id| score_of(&out, id)).fold(f64::MAX, f64::min);
        let b_max = ["n3", "n4", "n5"].into_iter().map(|id| score_of(&out, id)).fold(0.0, f64::max);
        assert!(a_min > b_max, "every node in the seed's community outranks the far community");

        let uniform = rank_nodes(&documents, &graph, &[], &RankConfig::default());
        assert!(score_of(&out, "n0") > score_of(&uniform, "n0"));
    }

    #[test]
    fn dangling_seed_keeps_all_mass_on_the_seed() {
        let documents = docs(&["n0", "n1", "n2"]);
        let graph = Graph::from_edges(3, Vec::new());

        let out = rank_nodes(&documents, &graph, &["n1".to_owned()], &RankConfig::default());
        assert!((score_of(&out, "n1") - 1.0).abs() < 1e-9);
        assert!(score_of(&out, "n0") < 1e-9);
        assert!(score_of(&out, "n2") < 1e-9);
    }

    #[test]
    fn unresolved_seeds_fall_back_to_uniform() {
        let documents = docs(&["n0", "n1", "n2", "n3"]);
        let graph = Graph::from_edges(4, vec![(0, 1, 1.0), (1, 2, 1.0), (2, 3, 1.0)]);

        let seeded =
            rank_nodes(&documents, &graph, &["missing".to_owned()], &RankConfig::default());
        let uniform = rank_nodes(&documents, &graph, &[], &RankConfig::default());
        for id in ["n0", "n1", "n2", "n3"] {
            assert!((score_of(&seeded, id) - score_of(&uniform, id)).abs() < 1e-12);
        }
    }

    #[test]
    fn top_n_truncates_and_run_is_deterministic() {
        let documents = docs(&["n0", "n1", "n2", "n3", "n4"]);
        let graph = Graph::from_edges(5, vec![(0, 1, 1.0), (0, 2, 1.0), (0, 3, 1.0), (0, 4, 1.0)]);
        let config = RankConfig { top_n: 2, ..RankConfig::default() };

        let first = rank_nodes(&documents, &graph, &["n0".to_owned()], &config);
        let second = rank_nodes(&documents, &graph, &["n0".to_owned()], &config);
        assert_eq!(first.nodes.len(), 2, "top_n caps the returned nodes");
        assert_eq!(first, second, "ranking is deterministic");
    }
}
