//! Здесь ждём I/O, чтобы домен оставался чистым и синхронным.

pub mod service;
pub mod snapshot;

pub use service::{
    BridgeInput, ClusterService, DocumentRef, PathInput, RankInput, RunInput, VectorSource,
};
