//! Кластеризация зафиксированного снимка работ. Доступ и артефакты проверяет
//! вызывающий; из Qdrant этот путь ничего не добирает.

use ahash::AHashMap;
use anyhow::{bail, Result};
use std::time::Instant;

use crate::domain::cluster::{cluster_from_prepared, ClusterOutput, CommunityDetector};
use crate::domain::graph::build_strict_graph;
use crate::domain::knn::build_knn_maps;
use crate::domain::vector::{l2_normalize, DocVector};
use crate::domain::DomainConfig;

/// Границы проверяют и транспорт, и [`cluster_snapshot`]: вторая проверка — страховка.
#[derive(Clone, Debug)]
pub struct SnapshotInput {
    pub manifest_sha256: String,
    pub model_revision: String,
    pub dimension: usize,
    pub config: DomainConfig,
    pub works: Vec<DocVector>,
}

/// Сортировка по id до построения графа делает результат независимым от порядка
/// входа и от разрешения ничьих.
pub fn cluster_snapshot<D: CommunityDetector>(
    mut input: SnapshotInput,
    detector: &D,
) -> Result<ClusterOutput> {
    let started = Instant::now();
    if input.manifest_sha256.len() != 64
        || !input.manifest_sha256.bytes().all(|v| v.is_ascii_digit() || (b'a'..=b'f').contains(&v))
        || input.model_revision.is_empty()
        || input.model_revision.len() > 512
        || !(1..=8192).contains(&input.dimension)
        || input.works.len() > 2000
    {
        bail!("invalid immutable snapshot identity or bounds");
    }
    input.works.sort_by(|a, b| a.id.cmp(&b.id));
    for (index, work) in input.works.iter().enumerate() {
        if work.id.is_empty()
            || work.id.len() > 1024
            || (index > 0 && input.works[index - 1].id == work.id)
            || work.vector.len() != input.dimension
            || !work.vector.iter().all(|v| v.is_finite())
            || !work.vector.iter().any(|v| *v != 0.0)
            || work.rep_text.len() > 16_384
            || work.filename.as_ref().is_some_and(|v| v.len() > 4096)
        {
            bail!("invalid, duplicated or incompatible canonical work");
        }
    }
    for work in &mut input.works {
        work.vector = l2_normalize(&work.vector);
        // Формат поля прежний, но здесь оно считает узлы-работы: число чанков
        // исходного документа не должно становиться опорой кластера.
        work.chunk_count = 1;
    }
    let names: AHashMap<Box<str>, Box<str>> = input
        .works
        .iter()
        .filter_map(|work| work.filename.clone().map(|name| (work.id.clone(), name)))
        .collect();
    let count = input.works.len();
    let vectors: Vec<Vec<f32>> = input.works.iter().map(|work| work.vector.clone()).collect();
    let k = input.config.knn_k.min(count.saturating_sub(1)).max(1);
    let knn = build_knn_maps(&vectors, k, input.config.knn_block_size);
    let graph = build_strict_graph(&knn, count, &input.config);
    let communities = if graph.edge_count() == 0 {
        (0..count).map(|node| vec![node]).collect()
    } else {
        detector.detect(&graph, &input.config)
    };
    let mut output =
        cluster_from_prepared(&input.works, &names, &graph, communities, &[], &input.config);
    // `cluster_from_prepared` замеряет только сборку, а снимку нужно время вместе
    // с нормировкой, kNN и детектором.
    output.stats.compute_ms = started.elapsed().as_millis() as u64;
    Ok(output)
}
