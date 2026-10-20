//! Умолчания и границы — один в один с `worker.py`, это сверяет тест.

/// Рёбра с почти нулевым сходством всё ещё входят в модулярность, а взвешенные
/// средние не делят на ноль.
pub const EDGE_EPS: f64 = 1e-6;

/// Запас над корпусом в 100 тыс. документов ограничен: поиск соседей квадратичен.
pub const MAX_GRAPH_NODES: usize = 125_000;

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub enum Algorithm {
    #[default]
    Leiden,
    Louvain,
}

impl Algorithm {
    #[must_use]
    pub fn parse(value: &str) -> Self {
        if value.trim().eq_ignore_ascii_case("louvain") {
            Self::Louvain
        } else {
            Self::Leiden
        }
    }
}

/// `Chunk` не сводит чанки в документ: узел — точка Qdrant, и члены кластера —
/// id точек (нижний слой иерархии RAPTOR), а не документов.
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub enum Granularity {
    #[default]
    Document,
    Chunk,
}

impl Granularity {
    #[must_use]
    pub fn parse(value: &str) -> Self {
        if value.trim().eq_ignore_ascii_case("chunk") {
            Self::Chunk
        } else {
            Self::Document
        }
    }
}

/// `None` — умолчание: так HTTP различает пропуск поля, а gRPC сводит
/// бессмысленные нули к умолчаниям.
#[derive(Clone, Debug, Default)]
pub struct ConfigOverrides {
    pub knn_k: Option<usize>,
    pub knn_block_size: Option<usize>,
    pub resolution: Option<f64>,
    pub min_size: Option<usize>,
    pub sim_threshold: Option<f64>,
    pub mutual_knn: Option<bool>,
    pub chunk_weighting: Option<bool>,
    pub chunk_lead_count: Option<usize>,
    pub chunk_w_lead: Option<f64>,
    pub chunk_w_results: Option<f64>,
    pub chunk_w_refs: Option<f64>,
    pub lineage_overlap_min: Option<f64>,
    pub algorithm: Option<Algorithm>,
    pub seed: Option<u64>,
    pub max_iterations: Option<usize>,
    pub granularity: Option<Granularity>,
}

#[derive(Clone, Debug, PartialEq)]
pub struct DomainConfig {
    pub knn_k: usize,
    pub knn_block_size: usize,
    pub resolution: f64,
    pub min_size: usize,
    pub sim_threshold: f64,
    pub mutual_knn: bool,
    pub chunk_weighting: bool,
    pub chunk_lead_count: usize,
    pub chunk_w_lead: f64,
    pub chunk_w_results: f64,
    pub chunk_w_refs: f64,
    pub lineage_overlap_min: f64,
    pub algorithm: Algorithm,
    pub seed: u64,
    pub max_iterations: Option<usize>,
    pub granularity: Granularity,
}

impl DomainConfig {
    #[must_use]
    pub fn resolve(overrides: ConfigOverrides) -> Self {
        let resolution = overrides.resolution.unwrap_or(1.0);
        Self {
            knn_k: overrides.knn_k.unwrap_or(6).max(1),
            knn_block_size: overrides.knn_block_size.unwrap_or(512).max(64),
            resolution: if resolution > 0.0 { resolution } else { 1.0 },
            min_size: overrides.min_size.unwrap_or(2).max(2),
            sim_threshold: overrides.sim_threshold.unwrap_or(0.0).clamp(0.0, 1.0),
            mutual_knn: overrides.mutual_knn.unwrap_or(true),
            chunk_weighting: overrides.chunk_weighting.unwrap_or(true),
            chunk_lead_count: overrides.chunk_lead_count.unwrap_or(2),
            chunk_w_lead: overrides.chunk_w_lead.unwrap_or(2.0).max(0.0),
            chunk_w_results: overrides.chunk_w_results.unwrap_or(1.5).max(0.0),
            chunk_w_refs: overrides.chunk_w_refs.unwrap_or(0.05).max(0.0),
            lineage_overlap_min: overrides.lineage_overlap_min.unwrap_or(0.3).clamp(0.0, 1.0),
            algorithm: overrides.algorithm.unwrap_or_default(),
            seed: overrides.seed.unwrap_or(42),
            max_iterations: overrides.max_iterations,
            granularity: overrides.granularity.unwrap_or_default(),
        }
    }

    #[must_use]
    pub fn lead_cutoff(&self) -> i64 {
        i64::try_from(self.chunk_lead_count).unwrap_or(i64::MAX) - 1
    }
}

impl Default for DomainConfig {
    fn default() -> Self {
        Self::resolve(ConfigOverrides::default())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn defaults_match_worker_py() {
        let config = DomainConfig::default();
        assert_eq!(config.knn_k, 6);
        assert_eq!(config.knn_block_size, 512);
        assert!((config.resolution - 1.0).abs() < f64::EPSILON);
        assert_eq!(config.min_size, 2);
        assert!((config.sim_threshold - 0.0).abs() < f64::EPSILON);
        assert!(config.mutual_knn);
        assert!(config.chunk_weighting);
        assert_eq!(config.chunk_lead_count, 2);
        assert_eq!(config.lead_cutoff(), 1);
        assert!((config.chunk_w_lead - 2.0).abs() < f64::EPSILON);
        assert!((config.chunk_w_results - 1.5).abs() < f64::EPSILON);
        assert!((config.chunk_w_refs - 0.05).abs() < f64::EPSILON);
        assert!((config.lineage_overlap_min - 0.3).abs() < f64::EPSILON);
        assert_eq!(config.algorithm, Algorithm::Leiden);
        assert_eq!(config.seed, 42);
        assert_eq!(config.max_iterations, None);
        assert_eq!(config.granularity, Granularity::Document);
    }

    #[test]
    fn bounds_and_parsing() {
        let config = DomainConfig::resolve(ConfigOverrides {
            knn_k: Some(0),
            knn_block_size: Some(10),
            resolution: Some(-2.0),
            min_size: Some(1),
            sim_threshold: Some(5.0),
            algorithm: Some(Algorithm::parse("LOUVAIN")),
            granularity: Some(Granularity::parse("CHUNK")),
            ..ConfigOverrides::default()
        });
        assert_eq!(config.knn_k, 1);
        assert_eq!(config.knn_block_size, 64);
        assert!((config.resolution - 1.0).abs() < f64::EPSILON);
        assert_eq!(config.min_size, 2);
        assert!((config.sim_threshold - 1.0).abs() < f64::EPSILON);
        assert_eq!(config.algorithm, Algorithm::Louvain);
        assert_eq!(config.granularity, Granularity::Chunk);
        assert_eq!(Granularity::parse("document"), Granularity::Document);
        assert_eq!(Granularity::parse(""), Granularity::Document);
    }
}
