use std::future::Future;
use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use tokio::sync::Semaphore;

use tonic::transport::Server;
use tonic::{Request, Response, Status};

use crate::application::{
    BridgeInput, ClusterService, DocumentRef, PathInput, RankInput, RunInput, VectorSource,
};
use crate::domain::bridge as dombridge;
use crate::domain::cluster as dom;
use crate::domain::config::{Algorithm, Granularity};
use crate::domain::pagerank::{self, RankConfig, RankOverrides};
use crate::domain::paths::{self, PathConfig, PathOverrides};
use crate::domain::vector::DocVector;
use crate::domain::{CommunityDetector, ConfigOverrides, DomainConfig};
use crate::interfaces::metrics::{self, Rpc};
use crate::proto::graph_compute_server::{GraphCompute, GraphComputeServer};
use crate::proto::{
    Bridge, BridgeConfig, BridgeRequest, BridgeResponse, BridgeScores, Cluster, ClusterConfig,
    ClusterMetrics, ClusterRequest, ClusterResponse, ComputeStats, GraphPath, Lineage, Mediator,
    PathRequest, PathResponse, RankRequest, RankResponse, RankedNode, Representative,
    SnapshotClusterRequest,
};

#[derive(Clone, Debug)]
pub struct GraphComputeHandler<V, D> {
    service: Arc<ClusterService<V, D>>,
    slots: Arc<Semaphore>,
}

impl<V: Send + Sync, D: Send + Sync> GraphComputeHandler<V, D> {
    pub fn new(service: Arc<ClusterService<V, D>>) -> Self {
        Self { service, slots: Arc::new(Semaphore::new(2)) }
    }

    // Разрешение живёт внутри вычисления: отмена RPC не запускает новую работу
    // поверх ещё не остановившейся задачи в блокирующем пуле.
    async fn compute<T: Send + 'static>(
        &self,
        work: impl Future<Output = anyhow::Result<T>> + Send + 'static,
    ) -> Result<T, Status> {
        let permit =
            tokio::time::timeout(Duration::from_secs(2), self.slots.clone().acquire_owned())
                .await
                .map_err(|_| Status::resource_exhausted("graph worker queue full"))?
                .map_err(|_| Status::unavailable("graph worker stopped"))?;
        let runtime = tokio::runtime::Handle::current();
        tokio::task::spawn_blocking(move || {
            let _permit = permit;
            runtime.block_on(work)
        })
        .await
        .map_err(|_| Status::internal("graph computation interrupted"))?
        .map_err(|err| {
            tracing::warn!(error = %err, "graph computation failed");
            Status::failed_precondition("graph input or dependency is unavailable")
        })
    }
}

#[tonic::async_trait]
impl<V, D> GraphCompute for GraphComputeHandler<V, D>
where
    V: VectorSource + 'static,
    D: CommunityDetector + 'static,
{
    async fn cluster_snapshot(
        &self,
        request: Request<SnapshotClusterRequest>,
    ) -> Result<Response<ClusterResponse>, Status> {
        let input = snapshot_request_to_input(request.into_inner())?;
        let service = self.service.clone();
        let output =
            metrics::timed(Rpc::Cluster, self.compute(async move { service.run_snapshot(input) }))
                .await?;
        Ok(Response::new(output_to_response(output)))
    }

    async fn cluster(
        &self,
        request: Request<ClusterRequest>,
    ) -> Result<Response<ClusterResponse>, Status> {
        let input = request_to_input(request.into_inner());
        let service = self.service.clone();
        let output =
            metrics::timed(Rpc::Cluster, self.compute(async move { service.run(input).await }))
                .await?;
        Ok(Response::new(output_to_response(output)))
    }

    async fn score_bridges(
        &self,
        request: Request<BridgeRequest>,
    ) -> Result<Response<BridgeResponse>, Status> {
        let input = bridge_request_to_input(request.into_inner());
        let service = self.service.clone();
        let output = metrics::timed(
            Rpc::Bridges,
            self.compute(async move { service.score_bridges(input).await }),
        )
        .await?;
        Ok(Response::new(bridge_output_to_response(output)))
    }

    async fn rank(&self, request: Request<RankRequest>) -> Result<Response<RankResponse>, Status> {
        let input = rank_request_to_input(request.into_inner());
        let service = self.service.clone();
        let output =
            metrics::timed(Rpc::Rank, self.compute(async move { service.rank(input).await }))
                .await?;
        Ok(Response::new(rank_output_to_response(output)))
    }

    async fn paths(&self, request: Request<PathRequest>) -> Result<Response<PathResponse>, Status> {
        let input = path_request_to_input(request.into_inner());
        let service = self.service.clone();
        let output =
            metrics::timed(Rpc::Paths, self.compute(async move { service.paths(input).await }))
                .await?;
        Ok(Response::new(path_output_to_response(output)))
    }
}

pub async fn serve<V, D>(
    service: Arc<ClusterService<V, D>>,
    addr: SocketAddr,
    shutdown: impl Future<Output = ()> + Send + 'static,
) -> anyhow::Result<()>
where
    V: VectorSource + 'static,
    D: CommunityDetector + 'static,
{
    let handler = GraphComputeHandler::new(service);
    let tls = super::security::tls_config()?;
    let enabled = tls.is_some();
    let mut server = Server::builder();
    if let Some(config) = tls {
        server = server.tls_config(config)?;
    }
    let rpc = GraphComputeServer::new(handler).max_decoding_message_size(32 * 1024 * 1024);
    let secured = tonic::service::interceptor::InterceptedService::new(rpc, move |request| {
        if enabled {
            super::security::authorize(request)
        } else {
            Ok(request)
        }
    });
    server.add_service(secured).serve_with_shutdown(addr, shutdown).await?;
    Ok(())
}

fn snapshot_request_to_input(
    request: SnapshotClusterRequest,
) -> Result<crate::application::snapshot::SnapshotInput, Status> {
    let Some(config) = request.config else {
        return Err(Status::invalid_argument("explicit snapshot config required"));
    };
    if config.seed.is_none()
        || config.algorithm != "leiden"
        || config.granularity != "document"
        || !(1..=64).contains(&config.knn_k)
        || !(2..=2000).contains(&config.min_size)
        || !config.resolution.is_finite()
        || !(0.01..=10.0).contains(&config.resolution)
        || !config.sim_threshold.is_finite()
        || !(0.0..=1.0).contains(&config.sim_threshold)
        || config.max_iterations.is_none_or(|v| !(1..=100).contains(&v))
    {
        return Err(Status::invalid_argument("invalid bounded snapshot clustering config"));
    }
    Ok(crate::application::snapshot::SnapshotInput {
        manifest_sha256: request.manifest_sha256,
        model_revision: request.model_revision,
        dimension: request.dimension as usize,
        config: DomainConfig::resolve(overrides(&config)),
        works: request
            .works
            .into_iter()
            .map(|work| DocVector {
                id: work.canonical_id.into(),
                vector: work.vector,
                rep_text: work.representative_text.into(),
                chunk_count: 1,
                filename: Some(work.title.into()),
            })
            .collect(),
    })
}

fn request_to_input(request: ClusterRequest) -> RunInput {
    let config = request
        .config
        .map_or_else(DomainConfig::default, |proto| DomainConfig::resolve(overrides(&proto)));
    let collection =
        if request.collection.is_empty() { "documents".to_owned() } else { request.collection };
    RunInput {
        owner_id: request.owner_id,
        collection,
        config,
        documents: request
            .documents
            .into_iter()
            .map(|doc| DocumentRef { id: doc.id, filename: doc.filename })
            .collect(),
        previous_clusters: request
            .previous_clusters
            .into_iter()
            .map(|prev| dom::PreviousCluster { id: prev.id, members: prev.members })
            .collect(),
    }
}

fn overrides(proto: &ClusterConfig) -> ConfigOverrides {
    ConfigOverrides {
        knn_k: (proto.knn_k != 0).then_some(proto.knn_k as usize),
        knn_block_size: (proto.knn_block_size != 0).then_some(proto.knn_block_size as usize),
        resolution: (proto.resolution != 0.0).then_some(proto.resolution),
        min_size: (proto.min_size != 0).then_some(proto.min_size as usize),
        sim_threshold: Some(proto.sim_threshold),
        mutual_knn: Some(proto.mutual_knn),
        chunk_weighting: Some(proto.chunk_weighting),
        chunk_lead_count: (proto.chunk_lead_count != 0).then_some(proto.chunk_lead_count as usize),
        chunk_w_lead: (proto.chunk_w_lead != 0.0).then_some(proto.chunk_w_lead),
        chunk_w_results: (proto.chunk_w_results != 0.0).then_some(proto.chunk_w_results),
        chunk_w_refs: (proto.chunk_w_refs != 0.0).then_some(proto.chunk_w_refs),
        lineage_overlap_min: (proto.lineage_overlap_min != 0.0)
            .then_some(proto.lineage_overlap_min),
        algorithm: (!proto.algorithm.is_empty()).then(|| Algorithm::parse(&proto.algorithm)),
        seed: proto.seed,
        max_iterations: proto.max_iterations.map(|value| value as usize),
        granularity: (!proto.granularity.is_empty())
            .then(|| Granularity::parse(&proto.granularity)),
    }
}

fn output_to_response(output: dom::ClusterOutput) -> ClusterResponse {
    ClusterResponse {
        clusters: output.clusters.into_iter().map(cluster_to_proto).collect(),
        stats: Some(ComputeStats {
            document_count: output.stats.document_count,
            edge_count: output.stats.edge_count,
            modularity: output.stats.modularity,
            compute_ms: output.stats.compute_ms,
        }),
    }
}

fn cluster_to_proto(cluster: dom::Cluster) -> Cluster {
    Cluster {
        members: cluster.members,
        fingerprint: cluster.fingerprint,
        signals: cluster.signals,
        chunk_count: cluster.chunk_count,
        metrics: Some(ClusterMetrics {
            size: cluster.metrics.size,
            avg_similarity: cluster.metrics.avg_similarity,
            modularity: cluster.metrics.modularity,
            modularity_contribution: cluster.metrics.modularity_contribution,
        }),
        representatives: cluster
            .representatives
            .into_iter()
            .map(|rep| Representative {
                document_id: rep.document_id,
                filename: rep.filename,
                snippet: rep.snippet,
            })
            .collect(),
        lineage: cluster.lineage.map(|lineage| Lineage {
            previous_cluster_id: lineage.previous_cluster_id,
            jaccard: lineage.jaccard,
            stability: lineage.stability,
            merged_from: lineage.merged_from,
            split_from: lineage.split_from,
        }),
    }
}

fn bridge_request_to_input(request: BridgeRequest) -> BridgeInput {
    let (config, bridge) = request.config.map_or_else(
        || (DomainConfig::default(), dombridge::BridgeConfig::default()),
        |proto| {
            let cluster = proto
                .cluster
                .as_ref()
                .map_or_else(DomainConfig::default, |cfg| DomainConfig::resolve(overrides(cfg)));
            (cluster, dombridge::BridgeConfig::resolve(bridge_overrides(&proto)))
        },
    );
    let collection =
        if request.collection.is_empty() { "documents".to_owned() } else { request.collection };
    BridgeInput {
        owner_id: request.owner_id,
        collection,
        config,
        bridge,
        documents: request
            .documents
            .into_iter()
            .map(|doc| DocumentRef { id: doc.id, filename: doc.filename })
            .collect(),
    }
}

fn bridge_overrides(proto: &BridgeConfig) -> dombridge::BridgeOverrides {
    dombridge::BridgeOverrides {
        top_n: (proto.top_n != 0).then_some(proto.top_n as usize),
        min_affinity: (proto.min_affinity != 0.0).then_some(proto.min_affinity),
        max_mediators: (proto.max_mediators != 0).then_some(proto.max_mediators as usize),
        min_convergence: (proto.min_convergence != 0).then_some(proto.min_convergence as usize),
        w_maverick: (proto.w_maverick != 0.0).then_some(proto.w_maverick),
        w_bridging: (proto.w_bridging != 0.0).then_some(proto.w_bridging),
        w_vanguard: (proto.w_vanguard != 0.0).then_some(proto.w_vanguard),
    }
}

fn bridge_output_to_response(output: dombridge::BridgeOutput) -> BridgeResponse {
    BridgeResponse {
        bridges: output.bridges.into_iter().map(bridge_to_proto).collect(),
        stats: Some(ComputeStats {
            document_count: output.stats.document_count,
            edge_count: output.stats.edge_count,
            modularity: output.stats.modularity,
            compute_ms: output.stats.compute_ms,
        }),
    }
}

fn bridge_to_proto(bridge: dombridge::Bridge) -> Bridge {
    Bridge {
        fingerprint: bridge.fingerprint,
        community_a: bridge.community_a,
        community_b: bridge.community_b,
        members_a: bridge.members_a,
        members_b: bridge.members_b,
        endpoint_a: bridge.endpoint_a,
        endpoint_b: bridge.endpoint_b,
        mediators: bridge
            .mediators
            .into_iter()
            .map(|mediator| Mediator {
                document_id: mediator.document_id,
                filename: mediator.filename,
                snippet: mediator.snippet,
            })
            .collect(),
        scores: Some(BridgeScores {
            affinity: bridge.scores.affinity,
            link_density: bridge.scores.link_density,
            maverick: bridge.scores.maverick,
            vanguard: bridge.scores.vanguard,
            bridging_centrality: bridge.scores.bridging_centrality,
            convergence: bridge.scores.convergence,
            composite: bridge.scores.composite,
        }),
    }
}

fn rank_request_to_input(request: RankRequest) -> RankInput {
    let config = request
        .config
        .map_or_else(DomainConfig::default, |proto| DomainConfig::resolve(overrides(&proto)));
    let collection =
        if request.collection.is_empty() { "documents".to_owned() } else { request.collection };
    let rank = RankConfig::resolve(RankOverrides {
        damping: (request.damping != 0.0).then_some(request.damping),
        max_iterations: (request.max_iterations != 0).then_some(request.max_iterations as usize),
        top_n: (request.top_n != 0).then_some(request.top_n as usize),
    });
    RankInput { owner_id: request.owner_id, collection, config, seed_ids: request.seed_ids, rank }
}

fn rank_output_to_response(output: pagerank::RankOutput) -> RankResponse {
    RankResponse {
        nodes: output
            .nodes
            .into_iter()
            .map(|node| RankedNode { id: node.id, score: node.score })
            .collect(),
        stats: Some(ComputeStats {
            document_count: output.stats.document_count,
            edge_count: output.stats.edge_count,
            modularity: output.stats.modularity,
            compute_ms: output.stats.compute_ms,
        }),
    }
}

fn path_request_to_input(request: PathRequest) -> PathInput {
    let config = request
        .config
        .map_or_else(DomainConfig::default, |proto| DomainConfig::resolve(overrides(&proto)));
    let collection =
        if request.collection.is_empty() { "documents".to_owned() } else { request.collection };
    let paths = PathConfig::resolve(PathOverrides {
        max_paths: (request.max_paths != 0).then_some(request.max_paths as usize),
        max_hops: (request.max_hops != 0).then_some(request.max_hops as usize),
    });
    PathInput {
        owner_id: request.owner_id,
        collection,
        config,
        source_ids: request.source_ids,
        target_ids: request.target_ids,
        paths,
    }
}

fn path_output_to_response(output: paths::PathOutput) -> PathResponse {
    PathResponse {
        paths: output
            .paths
            .into_iter()
            .map(|path| GraphPath { node_ids: path.node_ids, score: path.score })
            .collect(),
        stats: Some(ComputeStats {
            document_count: output.stats.document_count,
            edge_count: output.stats.edge_count,
            modularity: output.stats.modularity,
            compute_ms: output.stats.compute_ms,
        }),
    }
}
