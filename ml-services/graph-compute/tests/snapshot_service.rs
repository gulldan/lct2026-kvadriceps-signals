use graph_compute::application::snapshot::{cluster_snapshot, SnapshotInput};
use graph_compute::domain::cluster::CommunityDetector;
use graph_compute::domain::vector::DocVector;
use graph_compute::domain::{ConfigOverrides, DomainConfig, Graph};
use graph_compute::infrastructure::leiden::LeidenDetector;
use std::time::Duration;

fn fixture() -> SnapshotInput {
    let works = [
        ("paper:a", vec![1.0, 0.0, 0.01]),
        ("paper:b", vec![0.99, 0.01, 0.0]),
        ("paper:c", vec![1.0, -0.01, 0.0]),
        ("paper:d", vec![0.0, 1.0, 0.01]),
        ("paper:e", vec![0.01, 0.99, 0.0]),
        ("paper:f", vec![-0.01, 1.0, 0.0]),
    ]
    .into_iter()
    .map(|(id, vector)| DocVector {
        id: id.into(),
        vector,
        rep_text: "Pinned primary evidence".into(),
        chunk_count: 999,
        filename: Some(id.into()),
    })
    .collect();
    SnapshotInput {
        manifest_sha256: "a".repeat(64),
        model_revision: "fixed-model".into(),
        dimension: 3,
        config: DomainConfig::resolve(ConfigOverrides {
            seed: Some(42),
            knn_k: Some(2),
            min_size: Some(2),
            sim_threshold: Some(0.9),
            mutual_knn: Some(true),
            ..ConfigOverrides::default()
        }),
        works,
    }
}

#[test]
fn canonical_snapshot_is_permutation_invariant_and_counts_works() {
    let input = fixture();
    let expected = cluster_snapshot(input.clone(), &LeidenDetector).expect("valid snapshot");
    assert_eq!(expected.clusters.len(), 2);
    for shift in 0..6 {
        let mut permuted = input.clone();
        permuted.works.rotate_left(shift);
        if shift % 2 == 0 {
            permuted.works.reverse();
        }
        let output = cluster_snapshot(permuted, &LeidenDetector).expect("valid permutation");
        let membership: Vec<_> = output.clusters.iter().map(|c| c.members.clone()).collect();
        let original: Vec<_> = expected.clusters.iter().map(|c| c.members.clone()).collect();
        assert_eq!(membership, original);
        assert!(output.clusters.iter().all(|c| c.chunk_count == 3 && c.metrics.size == 3));
    }
}

#[test]
fn invalid_vectors_and_duplicate_work_ids_are_rejected() {
    let mut invalid = fixture();
    invalid.works[0].vector[0] = f32::NAN;
    assert!(cluster_snapshot(invalid, &LeidenDetector).is_err());
    let mut duplicate = fixture();
    duplicate.works[1].id = duplicate.works[0].id.clone();
    assert!(cluster_snapshot(duplicate, &LeidenDetector).is_err());
    let mut mismatch = fixture();
    mismatch.dimension = 2;
    assert!(cluster_snapshot(mismatch, &LeidenDetector).is_err());
    let mut zero = fixture();
    zero.works[0].vector.fill(0.0);
    assert!(cluster_snapshot(zero, &LeidenDetector).is_err());
}

#[test]
fn empty_and_singleton_graphs_do_not_invent_trends() {
    let mut input = fixture();
    input.works.truncate(1);
    let single = cluster_snapshot(input.clone(), &LeidenDetector).expect("diagnostic singleton");
    assert!(single.clusters.is_empty());
    assert_eq!(single.stats.document_count, 1);
    input.works.clear();
    assert!(cluster_snapshot(input, &LeidenDetector)
        .expect("empty diagnostic")
        .clusters
        .is_empty());
}

#[test]
fn isolated_work_is_not_attached_below_similarity_threshold() {
    let mut input = fixture();
    input.works.push(DocVector {
        id: "paper:unrelated-physics".into(),
        vector: vec![0.0, 0.0, 1.0],
        rep_text: "An unrelated physical process".into(),
        chunk_count: 1,
        filename: None,
    });
    let output = cluster_snapshot(input, &LeidenDetector).expect("bounded graph");
    assert_eq!(output.stats.document_count, 7);
    assert_eq!(output.clusters.len(), 2);
    assert!(output.clusters.iter().all(|cluster| cluster.members.len() == 3));
    assert!(output
        .clusters
        .iter()
        .flat_map(|cluster| &cluster.members)
        .all(|id| id != "paper:unrelated-physics"));
}

#[test]
fn two_unrelated_works_are_not_forced_into_a_pair() {
    let mut input = fixture();
    input.works = vec![input.works[0].clone(), input.works[3].clone()];
    let output = cluster_snapshot(input, &LeidenDetector).expect("two independent works");
    assert!(output.clusters.is_empty());
    assert_eq!(output.stats.edge_count, 0);
    assert_eq!(output.stats.document_count, 2);
}

#[test]
fn snapshot_compute_time_includes_community_detection() {
    struct DelayedDetector;
    impl CommunityDetector for DelayedDetector {
        fn detect(&self, graph: &Graph, config: &DomainConfig) -> Vec<Vec<usize>> {
            std::thread::sleep(Duration::from_millis(20));
            LeidenDetector.detect(graph, config)
        }
    }

    let output = cluster_snapshot(fixture(), &DelayedDetector).expect("timed snapshot");
    assert!(output.stats.compute_ms >= 20, "the complete compute stage must be timed");
}
