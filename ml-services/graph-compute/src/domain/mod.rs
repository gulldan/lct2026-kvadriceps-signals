//! Без I/O и proto; при фиксированном seed детерминирован: один и тот же запрос
//! всегда даёт одну и ту же доску.

pub mod bridge;
pub mod cluster;
pub mod config;
pub mod fingerprint;
pub mod graph;
pub mod knn;
pub mod lineage;
pub mod metrics;
pub mod pagerank;
pub mod paths;
pub mod vector;

pub use bridge::{
    score_bridges, Bridge, BridgeConfig, BridgeOutput, BridgeOverrides, BridgeScores, Mediator,
};
pub use cluster::{
    cluster_documents, Cluster, ClusterMetrics, ClusterOutput, CommunityDetector, ComputeStats,
    Lineage, Representative,
};
pub use config::{Algorithm, ConfigOverrides, DomainConfig, Granularity, EDGE_EPS};
pub use graph::Graph;
pub use pagerank::{rank_nodes, RankConfig, RankOutput, RankOverrides, RankedNode};
pub use paths::{connecting_paths, GraphPath, PathConfig, PathOutput, PathOverrides};
pub use vector::{ChunkPoint, DocVector};
