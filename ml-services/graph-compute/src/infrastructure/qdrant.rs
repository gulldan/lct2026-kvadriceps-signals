use std::future::Future;
use std::time::{Duration, Instant};

use anyhow::{ensure, Context, Result};
use serde_json::{json, Value};

use crate::application::VectorSource;
use crate::domain::vector::{chunk_index_of, extract_vector, ChunkPoint};

const SCROLL_LIMIT: u32 = 256;
const MAX_PAGE_BYTES: usize = 16 << 20;
// Общего потолка по байтам нет: память держат MAX_POINTS и страница, время — срок
// сбора. Вектор в JSON весит ~13 КБ, и потолок в гигабайт на 48 тыс. документов
// ронял доску тем.
const MAX_POINTS: usize = 200_000;

/// Без границы бюджет точек съедают документы с полным текстом (50–150 чанков), и
/// собранные роботом статьи в граф не попадают. Два — столько, сколько весит
/// `chunk_lead_count`.
const MAX_CHUNKS_PER_DOCUMENT: i64 = 2;

/// По умолчанию снимки источников входят только через ClusterSnapshot: у старого
/// пути нет манифеста доступа. Доске тем флаг нужен — её список документов API уже
/// проверил на доступ, а без снимков она видит лишь загруженный вручную корпус.
static ALLOW_SNAPSHOT_SOURCES: std::sync::LazyLock<bool> = std::sync::LazyLock::new(|| {
    matches!(
        std::env::var("LEGACY_ALLOW_SNAPSHOT_SOURCES").as_deref(),
        Ok("1" | "true" | "yes" | "on")
    )
});

#[derive(Clone, Debug)]
pub struct QdrantVectorSource {
    client: reqwest::Client,
    base_url: String,
}

impl QdrantVectorSource {
    pub fn new(qdrant_url: impl Into<String>) -> Result<Self> {
        let client = reqwest::Client::builder()
            .connect_timeout(Duration::from_secs(5))
            .timeout(Duration::from_secs(30))
            .build()
            .context("build qdrant http client")?;
        let base_url = qdrant_url.into().trim_end_matches('/').to_owned();
        Ok(Self { client, base_url })
    }
}

impl VectorSource for QdrantVectorSource {
    fn fetch(
        &self,
        owner_id: &str,
        collection: &str,
        document_ids: &[String],
    ) -> impl Future<Output = Result<Vec<ChunkPoint>>> + Send {
        let url = format!("{}/collections/{collection}/points/scroll", self.base_url);
        let owner = owner_id.to_owned();
        let documents = document_ids.to_vec();
        let client = self.client.clone();
        async move {
            ensure!(
                !collection.is_empty()
                    && collection.bytes().all(|b| b.is_ascii_alphanumeric() || b"_-".contains(&b)),
                "invalid collection name"
            );
            let mut points = Vec::new();
            let mut offset: Option<Value> = None;
            let deadline = Instant::now() + Duration::from_secs(120);
            let filter = legacy_filter(&owner, &documents);
            loop {
                ensure!(Instant::now() < deadline, "graph source time budget exceeded");
                let mut body = json!({
                    "limit": SCROLL_LIMIT,
                    "with_vector": true,
                    "with_payload": ["document_id", "owner_id", "chunk_index", "text", "filename", "source_index_snapshot", "node_type"],
                    "filter": filter,
                });
                if let Some(cursor) = &offset {
                    body["offset"] = cursor.clone();
                }

                let mut response = client
                    .post(&url)
                    .json(&body)
                    .timeout(
                        deadline
                            .saturating_duration_since(Instant::now())
                            .min(Duration::from_secs(30)),
                    )
                    .send()
                    .await
                    .context("qdrant scroll request failed")?
                    .error_for_status()
                    .context("qdrant scroll returned an error status")?;
                ensure!(
                    response.content_length().is_none_or(|n| n <= MAX_PAGE_BYTES as u64),
                    "graph source page exceeds byte budget"
                );
                let mut raw = Vec::new();
                while let Some(chunk) = response.chunk().await.context("read qdrant page")? {
                    ensure!(
                        raw.len() + chunk.len() <= MAX_PAGE_BYTES,
                        "graph source page exceeds byte budget"
                    );
                    raw.extend_from_slice(&chunk);
                }
                let envelope: Value =
                    serde_json::from_slice(&raw).context("decode qdrant scroll response")?;
                let result =
                    envelope.get("result").context("qdrant scroll response missing `result`")?;

                if let Some(batch) = result.get("points").and_then(Value::as_array) {
                    ensure!(
                        batch.len() <= SCROLL_LIMIT as usize
                            && points.len() + batch.len() <= MAX_POINTS,
                        "graph source point budget exceeded"
                    );
                    points.extend(batch.iter().filter_map(parse_point));
                }

                match result.get("next_page_offset") {
                    Some(next) if !next.is_null() => {
                        ensure!(offset.as_ref() != Some(next), "qdrant repeated pagination cursor");
                        offset = Some(next.clone());
                    }
                    _ => break,
                }
            }
            Ok(points)
        }
    }
}

fn legacy_filter(owner: &str, documents: &[String]) -> Value {
    let mut must = vec![
        json!({"should": [{"is_empty": {"key": "node_type"}},
            {"key": "node_type", "match": {"value": "chunk"}}]}),
        // Документ без chunk_index не отбрасывается: у старых точек поля нет.
        json!({"should": [{"is_empty": {"key": "chunk_index"}},
            {"key": "chunk_index", "range": {"lt": MAX_CHUNKS_PER_DOCUMENT}}]}),
    ];
    if !*ALLOW_SNAPSHOT_SOURCES {
        must.push(json!({"should": [{"is_empty": {"key": "source_index_snapshot"}},
            {"key": "source_index_snapshot", "match": {"value": ""}}]}));
    }
    if !owner.is_empty() {
        must.push(json!({"key": "owner_id", "match": {"value": owner}}));
    }
    if !documents.is_empty() {
        must.push(json!({"key": "document_id", "match": {"any": documents}}));
    }
    json!({"must": must})
}

fn parse_point(point: &Value) -> Option<ChunkPoint> {
    let payload = point.get("payload")?;
    // Проверка страхует фильтр запроса и потому открывается тем же флагом: иначе
    // запрос уходит расширенным, а разбор молча выбрасывает всё расширение.
    if !*ALLOW_SNAPSHOT_SOURCES
        && payload.get("source_index_snapshot").and_then(Value::as_str).is_some_and(|v| !v.is_empty())
    {
        return None;
    }
    let document_id = payload.get("document_id").and_then(Value::as_str)?;
    if document_id.is_empty() {
        return None;
    }
    let vector = extract_vector(point.get("vector")?)?;
    if vector.is_empty() {
        return None;
    }
    if vector.len() > 4096 || vector.iter().any(|v| !v.is_finite()) {
        return None;
    }
    let text = payload.get("text").and_then(Value::as_str).unwrap_or_default();
    let filename = payload.get("filename").and_then(Value::as_str).filter(|name| !name.is_empty());
    Some(ChunkPoint {
        id: point_id(point),
        document_id: document_id.into(),
        vector,
        chunk_index: chunk_index_of(payload),
        text: text.into(),
        filename: filename.map(Into::into),
    })
}

fn point_id(point: &Value) -> Box<str> {
    match point.get("id") {
        Some(Value::String(text)) => text.as_str().into(),
        Some(Value::Number(number)) => number.to_string().into(),
        _ => Box::default(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    #[ignore = "requires an isolated QDRANT_TEST_URL"]
    async fn live_qdrant_selection() {
        let base = std::env::var("QDRANT_TEST_URL").expect("QDRANT_TEST_URL required");
        let nonce =
            std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap().as_nanos();
        let collection = format!("quality_graph_{nonce}");
        let url = format!("{base}/collections/{collection}");
        let client = reqwest::Client::new();
        client
            .put(&url)
            .json(&json!({"vectors":{"size":3,"distance":"Cosine"}}))
            .send()
            .await
            .unwrap()
            .error_for_status()
            .unwrap();
        let payloads = [
            json!({"owner_id":"reader","document_id":"selected","node_type":"chunk"}),
            json!({"owner_id":"foreign","document_id":"selected","node_type":"chunk"}),
            json!({"owner_id":"reader","document_id":"other","node_type":"chunk"}),
            json!({"owner_id":"reader","document_id":"selected","source_index_snapshot":"immutable"}),
            json!({"owner_id":"reader","document_id":"selected","node_type":"summary"}),
            json!({"owner_id":"reader","document_id":"selected"}),
        ];
        let points: Vec<_> = payloads
            .into_iter()
            .enumerate()
            .map(|(id, payload)| json!({"id":id+1,"vector":[1.0,0.0,0.0],"payload":payload}))
            .collect();
        client
            .put(format!("{url}/points?wait=true"))
            .json(&json!({"points":points}))
            .send()
            .await
            .unwrap()
            .error_for_status()
            .unwrap();
        let source = QdrantVectorSource::new(&base).unwrap();
        let result = source.fetch("reader", &collection, &["selected".to_owned()]).await;
        client.delete(&url).send().await.unwrap().error_for_status().unwrap();
        let ids: Vec<_> = result.unwrap().into_iter().map(|point| point.id.to_string()).collect();
        assert_eq!(ids, ["1", "6"]);
    }

    #[tokio::test]
    async fn fetch_applies_selection_and_rejects_oversized_page_before_decode() {
        use axum::{extract::State, routing::post, Json, Router};
        use std::sync::{Arc, Mutex};

        async fn page(
            State(seen): State<Arc<Mutex<Option<Value>>>>,
            Json(body): Json<Value>,
        ) -> Json<Value> {
            *seen.lock().unwrap() = Some(body);
            Json(json!({"result": {"points": [], "next_page_offset": null}}))
        }
        let seen = Arc::new(Mutex::new(None));
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let address = listener.local_addr().unwrap();
        let app = Router::new()
            .route("/collections/documents/points/scroll", post(page))
            .with_state(seen.clone());
        let server = tokio::spawn(async move { axum::serve(listener, app).await.unwrap() });
        let source = QdrantVectorSource::new(format!("http://{address}")).unwrap();
        source.fetch("reader", "documents", &["selected-doc".to_string()]).await.unwrap();
        let body = seen.lock().unwrap().take().unwrap();
        let must = body["filter"]["must"].as_array().unwrap();
        assert!(must.contains(&json!({"key":"document_id","match":{"any":["selected-doc"]}})));
        assert!(must.contains(&json!({"key":"owner_id","match":{"value":"reader"}})));
        assert!(body["with_payload"].is_array());
        server.abort();

        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let address = listener.local_addr().unwrap();
        let server = tokio::spawn(async move {
            use tokio::io::{AsyncReadExt, AsyncWriteExt};
            let (mut stream, _) = listener.accept().await.unwrap();
            let mut request = [0; 4096];
            assert!(stream.read(&mut request).await.unwrap() > 0);
            stream
                .write_all(
                    b"HTTP/1.1 200 OK\r\nContent-Length: 999999999\r\nConnection: close\r\n\r\n",
                )
                .await
                .unwrap();
        });
        let source = QdrantVectorSource::new(format!("http://{address}")).unwrap();
        let err = source.fetch("reader", "documents", &[]).await.unwrap_err();
        assert!(err.to_string().contains("byte budget"));
        server.await.unwrap();
    }

    #[test]
    fn parse_point_extracts_fields() {
        let point = json!({
            "id": "point-9",
            "vector": [0.1, 0.2, 0.3],
            "payload": {"document_id": "doc-1", "chunk_index": 2, "text": "hello", "filename": "a.pdf"}
        });
        let parsed = parse_point(&point).expect("valid point");
        assert_eq!(parsed.id.as_ref(), "point-9");
        assert_eq!(parsed.document_id.as_ref(), "doc-1");
        assert_eq!(parsed.chunk_index, 2);
        assert_eq!(parsed.vector, vec![0.1, 0.2, 0.3]);
        assert_eq!(parsed.filename.as_deref(), Some("a.pdf"));
        let numeric = json!({"id": 42, "vector": [1.0], "payload": {"document_id": "d"}});
        assert_eq!(parse_point(&numeric).expect("valid").id.as_ref(), "42");
    }

    #[test]
    fn parse_point_rejects_missing_vector_or_id() {
        assert!(parse_point(&json!({"payload": {"document_id": "x"}})).is_none());
        assert!(parse_point(&json!({"vector": [1.0], "payload": {}})).is_none());
        assert!(parse_point(&json!({"vector": [1.0], "payload": {"document_id": ""}})).is_none());
        assert!(parse_point(&json!({"vector": [1.0], "payload": {
            "document_id": "private-source", "source_index_snapshot": "immutable-version"
        }}))
        .is_none());
    }
}
