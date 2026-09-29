//! Метрики Prometheus вручную: среднее `sum/count` заменяет перцентили, поэтому
//! нет ни гистограмм, ни крейта метрик.

use std::future::Future;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::OnceLock;
use std::time::{Duration, Instant};

#[derive(Clone, Copy, Debug)]
pub enum Rpc {
    Cluster,
    Bridges,
    Rank,
    Paths,
}

impl Rpc {
    const ALL: [Self; 4] = [Self::Cluster, Self::Bridges, Self::Rank, Self::Paths];

    const fn name(self) -> &'static str {
        match self {
            Self::Cluster => "cluster",
            Self::Bridges => "bridges",
            Self::Rank => "rank",
            Self::Paths => "paths",
        }
    }
}

/// Длительности в целых наносекундах, чтобы хватало голых атомиков.
#[derive(Debug, Default)]
struct RpcSlot {
    ok: AtomicU64,
    error: AtomicU64,
    duration_sum_nanos: AtomicU64,
    duration_count: AtomicU64,
    last_duration_nanos: AtomicU64,
}

#[derive(Debug, Default)]
pub struct Metrics {
    slots: [RpcSlot; 4],
}

pub fn registry() -> &'static Metrics {
    static REGISTRY: OnceLock<Metrics> = OnceLock::new();
    REGISTRY.get_or_init(Metrics::default)
}

pub async fn timed<T, E>(rpc: Rpc, call: impl Future<Output = Result<T, E>>) -> Result<T, E> {
    let started = Instant::now();
    let result = call.await;
    registry().observe(rpc, started.elapsed(), result.is_ok());
    result
}

impl Metrics {
    pub fn observe(&self, rpc: Rpc, elapsed: Duration, ok: bool) {
        let slot = &self.slots[rpc as usize];
        let outcome = if ok { &slot.ok } else { &slot.error };
        outcome.fetch_add(1, Ordering::Relaxed);
        let nanos = u64::try_from(elapsed.as_nanos()).unwrap_or(u64::MAX);
        slot.duration_sum_nanos.fetch_add(nanos, Ordering::Relaxed);
        slot.duration_count.fetch_add(1, Ordering::Relaxed);
        slot.last_duration_nanos.store(nanos, Ordering::Relaxed);
    }

    pub fn render(&self) -> String {
        use std::fmt::Write as _;

        let mut out = String::with_capacity(2048);
        out.push_str(
            "# HELP graph_compute_up Service liveness.\n\
             # TYPE graph_compute_up gauge\n\
             graph_compute_up 1\n",
        );
        out.push_str(
            "# HELP graph_compute_requests_total Finished RPCs by outcome.\n\
             # TYPE graph_compute_requests_total counter\n",
        );
        for rpc in Rpc::ALL {
            let slot = &self.slots[rpc as usize];
            let _ = writeln!(
                out,
                "graph_compute_requests_total{{rpc=\"{}\",outcome=\"ok\"}} {}",
                rpc.name(),
                slot.ok.load(Ordering::Relaxed),
            );
            let _ = writeln!(
                out,
                "graph_compute_requests_total{{rpc=\"{}\",outcome=\"error\"}} {}",
                rpc.name(),
                slot.error.load(Ordering::Relaxed),
            );
        }
        out.push_str(
            "# HELP graph_compute_duration_seconds RPC wall time.\n\
             # TYPE graph_compute_duration_seconds summary\n",
        );
        for rpc in Rpc::ALL {
            let slot = &self.slots[rpc as usize];
            let _ = writeln!(
                out,
                "graph_compute_duration_seconds_sum{{rpc=\"{}\"}} {}",
                rpc.name(),
                secs(slot.duration_sum_nanos.load(Ordering::Relaxed)),
            );
            let _ = writeln!(
                out,
                "graph_compute_duration_seconds_count{{rpc=\"{}\"}} {}",
                rpc.name(),
                slot.duration_count.load(Ordering::Relaxed),
            );
        }
        out.push_str(
            "# HELP graph_compute_last_duration_seconds Wall time of the most recent call.\n\
             # TYPE graph_compute_last_duration_seconds gauge\n",
        );
        for rpc in Rpc::ALL {
            let slot = &self.slots[rpc as usize];
            let _ = writeln!(
                out,
                "graph_compute_last_duration_seconds{{rpc=\"{}\"}} {}",
                rpc.name(),
                secs(slot.last_duration_nanos.load(Ordering::Relaxed)),
            );
        }
        out
    }
}

fn secs(nanos: u64) -> f64 {
    nanos as f64 / 1e9
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn observe_and_render() {
        let metrics = Metrics::default();
        metrics.observe(Rpc::Cluster, Duration::from_millis(250), true);
        metrics.observe(Rpc::Cluster, Duration::from_millis(750), false);
        let text = metrics.render();
        assert!(text.contains("graph_compute_up 1"));
        assert!(text.contains("graph_compute_requests_total{rpc=\"cluster\",outcome=\"ok\"} 1"));
        assert!(text.contains("graph_compute_requests_total{rpc=\"cluster\",outcome=\"error\"} 1"));
        assert!(text.contains("graph_compute_duration_seconds_sum{rpc=\"cluster\"} 1"));
        assert!(text.contains("graph_compute_duration_seconds_count{rpc=\"cluster\"} 2"));
        assert!(text.contains("graph_compute_last_duration_seconds{rpc=\"cluster\"} 0.75"));
        assert!(text.contains("graph_compute_requests_total{rpc=\"paths\",outcome=\"ok\"} 0"));
    }
}
