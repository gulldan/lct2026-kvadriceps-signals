//! Общий префикс всех прогонов вынесен в `prepare`: кластеры, мосты, PageRank и
//! пути обязаны считаться на одном и том же графе.

use std::future::Future;

use ahash::{AHashMap, AHashSet};

use crate::domain::bridge::{self, BridgeConfig, BridgeOutput};
use crate::domain::cluster::{
    build_communities, cluster_from_prepared, ClusterOutput, PreviousCluster,
};
use crate::domain::pagerank::{self, RankConfig, RankOutput};
use crate::domain::paths::{self, PathConfig, PathOutput};
use crate::domain::vector::{aggregate_docs, chunk_nodes, ChunkPoint, DocVector};
use crate::domain::{CommunityDetector, DomainConfig, Granularity, Graph};

pub trait VectorSource: Send + Sync {
    /// Пустой `owner_id` — общий корпус; отбор по списку документов сервис
    /// повторяет поверх источника.
    fn fetch(
        &self,
        owner_id: &str,
        collection: &str,
        document_ids: &[String],
    ) -> impl Future<Output = anyhow::Result<Vec<ChunkPoint>>> + Send;
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct DocumentRef {
    pub id: String,
    pub filename: String,
}

#[derive(Clone, Debug, Default)]
pub struct RunInput {
    pub owner_id: String,
    pub collection: String,
    pub config: DomainConfig,
    pub documents: Vec<DocumentRef>,
    pub previous_clusters: Vec<PreviousCluster>,
}

#[derive(Clone, Debug, Default)]
pub struct BridgeInput {
    pub owner_id: String,
    pub collection: String,
    pub config: DomainConfig,
    pub bridge: BridgeConfig,
    pub documents: Vec<DocumentRef>,
}

/// Списка документов нет: владельца отсекает только [`VectorSource`].
#[derive(Clone, Debug, Default)]
pub struct RankInput {
    pub owner_id: String,
    pub collection: String,
    pub config: DomainConfig,
    pub seed_ids: Vec<String>,
    pub rank: RankConfig,
}

/// Списка документов нет: владельца отсекает только [`VectorSource`].
#[derive(Clone, Debug, Default)]
pub struct PathInput {
    pub owner_id: String,
    pub collection: String,
    pub config: DomainConfig,
    pub source_ids: Vec<String>,
    pub target_ids: Vec<String>,
    pub paths: PathConfig,
}

struct Prepared {
    docs: Vec<DocVector>,
    graph: Graph,
    communities: Vec<Vec<usize>>,
}

#[derive(Clone, Debug)]
pub struct ClusterService<V, D> {
    source: V,
    detector: D,
}

impl<V: VectorSource, D: CommunityDetector> ClusterService<V, D> {
    pub const fn new(source: V, detector: D) -> Self {
        Self { source, detector }
    }

    pub fn run_snapshot(
        &self,
        input: super::snapshot::SnapshotInput,
    ) -> anyhow::Result<ClusterOutput> {
        super::snapshot::cluster_snapshot(input, &self.detector)
    }

    pub async fn run(&self, input: RunInput) -> anyhow::Result<ClusterOutput> {
        let prepared = self
            .prepare(&input.owner_id, &input.collection, &input.config, &input.documents)
            .await?;
        let doc_filenames = filename_map(&input.documents);
        Ok(cluster_from_prepared(
            &prepared.docs,
            &doc_filenames,
            &prepared.graph,
            prepared.communities,
            &input.previous_clusters,
            &input.config,
        ))
    }

    pub async fn score_bridges(&self, input: BridgeInput) -> anyhow::Result<BridgeOutput> {
        let prepared = self
            .prepare(&input.owner_id, &input.collection, &input.config, &input.documents)
            .await?;
        let doc_filenames = filename_map(&input.documents);
        Ok(bridge::score_bridges(
            &prepared.docs,
            &doc_filenames,
            &prepared.graph,
            &prepared.communities,
            &input.bridge,
        ))
    }

    pub async fn rank(&self, input: RankInput) -> anyhow::Result<RankOutput> {
        let prepared = self.prepare(&input.owner_id, &input.collection, &input.config, &[]).await?;
        Ok(pagerank::rank_nodes(&prepared.docs, &prepared.graph, &input.seed_ids, &input.rank))
    }

    pub async fn paths(&self, input: PathInput) -> anyhow::Result<PathOutput> {
        let prepared = self.prepare(&input.owner_id, &input.collection, &input.config, &[]).await?;
        Ok(paths::connecting_paths(
            &prepared.docs,
            &prepared.graph,
            &input.source_ids,
            &input.target_ids,
            &input.paths,
        ))
    }

    async fn prepare(
        &self,
        owner_id: &str,
        collection: &str,
        config: &DomainConfig,
        documents: &[DocumentRef],
    ) -> anyhow::Result<Prepared> {
        // Потолок по документам, а не по точкам (их держит MAX_POINTS): документ
        // даёт графу не больше двух чанков, а корпус стенда — 48 тыс. документов
        // и растёт на несколько тысяч в день.
        anyhow::ensure!(documents.len() <= 100_000, "document selection exceeds graph budget");
        let document_ids: Vec<String> = documents.iter().map(|doc| doc.id.clone()).collect();
        let mut points = self.source.fetch(owner_id, collection, &document_ids).await?;

        // Страховка владения: источник уже отфильтровал, но точку чужого документа
        // не пропускаем, даже если фильтр источника ошибся.
        if !documents.is_empty() {
            let owned: AHashSet<&str> = documents.iter().map(|doc| doc.id.as_str()).collect();
            points.retain(|point| owned.contains(point.document_id.as_ref()));
        }

        let docs = match config.granularity {
            Granularity::Document => aggregate_docs(&points, config),
            Granularity::Chunk => chunk_nodes(&points),
        };
        // Поиск соседей квадратичен: 8 тыс. узлов на 32 ядрах — ~0,4 с, значит
        // 100 тыс. — около минуты, с большим запасом до срока вызова.
        anyhow::ensure!(docs.len() <= 100_000, "graph node budget exceeded");
        let (graph, communities) = build_communities(&docs, config, &self.detector);
        Ok(Prepared { docs, graph, communities })
    }
}

fn filename_map(documents: &[DocumentRef]) -> AHashMap<Box<str>, Box<str>> {
    documents.iter().map(|doc| (doc.id.as_str().into(), doc.filename.as_str().into())).collect()
}
