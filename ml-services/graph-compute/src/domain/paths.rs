//! Связующие пути (PathRAG): перечисляем по числу рёбер без учёта весов, а
//! оцениваем по косинусным весам рёбер.

use std::cmp::Ordering;
use std::convert::Infallible;

use ahash::{AHashMap, AHashSet};
use petgraph::graph::{NodeIndex, UnGraph};
use rustworkx_core::shortest_path::all_shortest_paths;
use serde::{Deserialize, Serialize};

use crate::domain::cluster::ComputeStats;
use crate::domain::vector::DocVector;
use crate::domain::Graph;

/// Штраф за каждое ребро сверх первого: средний вес умножается на
/// `LENGTH_DECAY^(hops - 1)`.
const LENGTH_DECAY: f64 = 0.8;

#[derive(Clone, Debug, Default)]
pub struct PathOverrides {
    pub max_paths: Option<usize>,
    pub max_hops: Option<usize>,
}

/// Умолчания совпадают с описанными в proto.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PathConfig {
    pub max_paths: usize,
    pub max_hops: usize,
}

impl PathConfig {
    #[must_use]
    pub fn resolve(overrides: PathOverrides) -> Self {
        Self {
            max_paths: overrides.max_paths.unwrap_or(10),
            max_hops: overrides.max_hops.unwrap_or(4).max(1),
        }
    }
}

impl Default for PathConfig {
    fn default() -> Self {
        Self::resolve(PathOverrides::default())
    }
}

#[derive(Clone, Debug, Default, Deserialize, PartialEq, Serialize)]
pub struct GraphPath {
    pub node_ids: Vec<String>,
    pub score: f64,
}

#[derive(Clone, Debug, Default, Deserialize, PartialEq, Serialize)]
pub struct PathOutput {
    pub paths: Vec<GraphPath>,
    pub stats: ComputeStats,
}

/// `modularity` и `compute_ms` остаются нулевыми: разбиения здесь нет, а время
/// замеряет вызывающий.
#[must_use]
pub fn connecting_paths(
    docs: &[DocVector],
    graph: &Graph,
    source_ids: &[String],
    target_ids: &[String],
    config: &PathConfig,
) -> PathOutput {
    let stats = ComputeStats {
        document_count: docs.len() as u32,
        edge_count: graph.edge_count() as u32,
        modularity: 0.0,
        compute_ms: 0,
    };
    let index: AHashMap<&str, usize> =
        docs.iter().enumerate().map(|(node, doc)| (doc.id.as_ref(), node)).collect();
    let sources = resolve_nodes(source_ids, &index);
    let targets = resolve_nodes(target_ids, &index);
    if sources.is_empty() || targets.is_empty() {
        return PathOutput { paths: Vec::new(), stats };
    }

    let pet = graph.to_petgraph();
    let mut seen: AHashSet<Vec<usize>> = AHashSet::new();
    let mut paths: Vec<GraphPath> = Vec::new();
    for &source in &sources {
        for &target in &targets {
            if source == target {
                continue;
            }
            for indices in shortest_index_paths(&pet, source, target) {
                let hops = indices.len().saturating_sub(1);
                if hops == 0 || hops > config.max_hops || !seen.insert(canonical_key(&indices)) {
                    continue;
                }
                if let Some(score) = path_score(graph, &indices) {
                    let node_ids = indices.iter().map(|&node| docs[node].id.to_string()).collect();
                    paths.push(GraphPath { node_ids, score });
                }
            }
        }
    }

    paths.sort_by(|a, b| {
        b.score
            .partial_cmp(&a.score)
            .unwrap_or(Ordering::Equal)
            .then_with(|| a.node_ids.cmp(&b.node_ids))
    });
    paths.truncate(config.max_paths);
    PathOutput { paths, stats }
}

fn resolve_nodes(ids: &[String], index: &AHashMap<&str, usize>) -> Vec<usize> {
    let mut nodes: Vec<usize> =
        ids.iter().filter_map(|id| index.get(id.as_str()).copied()).collect();
    nodes.sort_unstable();
    nodes.dedup();
    nodes
}

fn shortest_index_paths(pet: &UnGraph<(), f64>, source: usize, target: usize) -> Vec<Vec<usize>> {
    all_shortest_paths(pet, NodeIndex::new(source), NodeIndex::new(target), |_| {
        Ok::<usize, Infallible>(1)
    })
    .unwrap_or_default()
    .into_iter()
    .map(|path| path.iter().map(|node| node.index()).collect())
    .collect()
}

/// Путь и его разворот — одна цепочка: граф неориентированный.
fn canonical_key(seq: &[usize]) -> Vec<usize> {
    let reversed: Vec<usize> = seq.iter().rev().copied().collect();
    if reversed.as_slice() < seq {
        reversed
    } else {
        seq.to_vec()
    }
}

/// `None` — только если соседние узлы пути не смежны: у кратчайших путей так не
/// бывает, это страховка.
fn path_score(graph: &Graph, path: &[usize]) -> Option<f64> {
    if path.len() < 2 {
        return None;
    }
    let weight_sum: f64 =
        path.windows(2).map(|pair| edge_weight(graph, pair[0], pair[1])).sum::<Option<f64>>()?;
    let hops = (path.len() - 1) as f64;
    let decay = LENGTH_DECAY.powi((path.len() - 2) as i32);
    Some((weight_sum / hops) * decay)
}

fn edge_weight(graph: &Graph, a: usize, b: usize) -> Option<f64> {
    graph.neighbors(a).iter().find_map(|&(node, weight)| (node == b).then_some(weight))
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

    fn ids(values: &[&str]) -> Vec<String> {
        values.iter().map(|value| (*value).to_owned()).collect()
    }

    fn barbell() -> (Vec<DocVector>, Graph) {
        let edges = vec![
            (0, 1, 1.0),
            (0, 2, 1.0),
            (1, 2, 1.0),
            (3, 4, 1.0),
            (3, 5, 1.0),
            (4, 5, 1.0),
            (2, 3, 1.0),
        ];
        (docs(&["n0", "n1", "n2", "n3", "n4", "n5"]), Graph::from_edges(6, edges))
    }

    fn diamond() -> (Vec<DocVector>, Graph) {
        let edges = vec![(0, 1, 1.0), (0, 2, 1.0), (1, 3, 1.0), (2, 3, 1.0)];
        (docs(&["n0", "n1", "n2", "n3"]), Graph::from_edges(4, edges))
    }

    #[test]
    fn finds_the_connecting_path_between_two_clusters() {
        let (documents, graph) = barbell();
        let out = connecting_paths(
            &documents,
            &graph,
            &ids(&["n0"]),
            &ids(&["n5"]),
            &PathConfig::default(),
        );

        assert_eq!(out.stats.document_count, 6);
        assert_eq!(out.paths.len(), 1, "one shortest chain crosses the single bridge");
        let path = &out.paths[0];
        assert_eq!(path.node_ids, vec!["n0", "n2", "n3", "n5"]);
        // Три ребра с весом 1,0: 1,0 · 0,8² = 0,64.
        assert!((path.score - 0.64).abs() < 1e-9);
    }

    #[test]
    fn max_hops_prunes_long_paths() {
        let (documents, graph) = barbell();
        let config = PathConfig { max_hops: 2, ..PathConfig::default() };
        let out = connecting_paths(&documents, &graph, &ids(&["n0"]), &ids(&["n5"]), &config);
        assert!(out.paths.is_empty(), "the only chain is 3 hops, beyond the 2-hop cap");
    }

    #[test]
    fn unreachable_targets_yield_no_paths() {
        let documents = docs(&["n0", "n1", "n2", "n3"]);
        let graph = Graph::from_edges(4, vec![(0, 1, 1.0), (2, 3, 1.0)]);
        let out = connecting_paths(
            &documents,
            &graph,
            &ids(&["n0"]),
            &ids(&["n3"]),
            &PathConfig::default(),
        );
        assert!(out.paths.is_empty());
        assert_eq!(out.stats.edge_count, 2);
    }

    #[test]
    fn enumerates_all_shortest_paths_and_breaks_ties_by_id() {
        let (documents, graph) = diamond();
        let out = connecting_paths(
            &documents,
            &graph,
            &ids(&["n0"]),
            &ids(&["n3"]),
            &PathConfig::default(),
        );

        assert_eq!(out.paths.len(), 2, "both equal-length chains are returned");
        assert_eq!(out.paths[0].node_ids, vec!["n0", "n1", "n3"]);
        assert_eq!(out.paths[1].node_ids, vec!["n0", "n2", "n3"]);
        assert!((out.paths[0].score - 0.8).abs() < 1e-9);
    }

    #[test]
    fn max_paths_truncates_to_top_k() {
        let (documents, graph) = diamond();
        let config = PathConfig { max_paths: 1, ..PathConfig::default() };
        let out = connecting_paths(&documents, &graph, &ids(&["n0"]), &ids(&["n3"]), &config);
        assert_eq!(out.paths.len(), 1);
        assert_eq!(out.paths[0].node_ids, vec!["n0", "n1", "n3"]);
    }

    #[test]
    fn overlapping_source_target_sets_dedup_reversed_paths() {
        let (documents, graph) = diamond();
        let both = ids(&["n0", "n3"]);
        let out = connecting_paths(&documents, &graph, &both, &both, &PathConfig::default());
        // n0→n3 и n3→n0 — те же две цепочки, а не четыре.
        assert_eq!(out.paths.len(), 2);
    }

    #[test]
    fn paths_are_deterministic() {
        let (documents, graph) = diamond();
        let a = connecting_paths(
            &documents,
            &graph,
            &ids(&["n0"]),
            &ids(&["n3"]),
            &PathConfig::default(),
        );
        let b = connecting_paths(
            &documents,
            &graph,
            &ids(&["n0"]),
            &ids(&["n3"]),
            &PathConfig::default(),
        );
        assert_eq!(a, b);
    }
}
