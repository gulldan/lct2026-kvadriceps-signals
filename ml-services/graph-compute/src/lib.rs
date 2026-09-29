//! Кластеризация — точный порт Python `cluster-worker`: формат `clusters` обязан
//! остаться совместимым с доской тем. Состояния нет, прогон целиком задан запросом.

pub mod application;
pub mod config;
pub mod domain;
pub mod infrastructure;

#[cfg(feature = "service")]
pub mod interfaces;

#[cfg(feature = "service")]
#[allow(
    clippy::all,
    clippy::pedantic,
    clippy::nursery,
    clippy::restriction,
    unreachable_pub,
    unused_qualifications,
    unused_lifetimes,
    rust_2018_idioms
)]
#[rustfmt::skip]
pub mod proto {
    tonic::include_proto!("rag.graph.v1");
}
