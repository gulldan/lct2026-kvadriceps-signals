use std::env;
use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use axum::extract::{DefaultBodyLimit, Multipart, State};
use axum::http::StatusCode;
use axum::routing::{get, post};
use axum::{Json, Router};
use tokio::sync::Semaphore;

pub fn app() -> Router {
    Router::new()
        .route("/health", get(health))
        .route("/v1/parse", post(parse))
        .layer(DefaultBodyLimit::max(max_body_bytes()))
        .with_state(Arc::new(Semaphore::new(2)))
}

pub async fn serve() -> anyhow::Result<()> {
    let addr: SocketAddr = env::var("HTTP_ADDR")
        .unwrap_or_else(|_| "0.0.0.0:8095".to_string())
        .parse()?;
    let listener = tokio::net::TcpListener::bind(addr).await?;
    tracing::info!(%addr, "workbook parser listening");
    axum::serve(listener, app()).await?;
    Ok(())
}

async fn health() -> &'static str {
    "ok"
}

async fn parse(
    State(slots): State<Arc<Semaphore>>,
    mut multipart: Multipart,
) -> Result<Json<crate::model::ParseResponse>, (StatusCode, String)> {
    let permit = tokio::time::timeout(Duration::from_secs(2), slots.acquire_owned())
        .await
        .map_err(|_| {
            (
                StatusCode::SERVICE_UNAVAILABLE,
                "parser queue full".to_string(),
            )
        })?
        .map_err(|_| {
            (
                StatusCode::SERVICE_UNAVAILABLE,
                "parser stopped".to_string(),
            )
        })?;
    let mut file_name = String::from("workbook.xlsx");
    let mut data = None;

    while let Some(field) = multipart.next_field().await.map_err(bad_request)? {
        if field.name() != Some("file") {
            continue;
        }
        if let Some(name) = field.file_name() {
            file_name = name.to_string();
        }
        data = Some(field.bytes().await.map_err(bad_request)?.to_vec());
        break;
    }

    let data = data.ok_or_else(|| {
        (
            StatusCode::BAD_REQUEST,
            "missing multipart field 'file'".to_string(),
        )
    })?;
    let response = crate::sandbox::parse(&data, &file_name)
        .await
        .map_err(invalid_workbook)?;
    drop(permit);
    Ok(Json(response))
}

fn max_body_bytes() -> usize {
    env::var("WORKBOOK_MAX_UPLOAD_MB")
        .ok()
        .and_then(|value| value.parse::<usize>().ok())
        .unwrap_or(128)
        .clamp(1, 128)
        * 1024
        * 1024
}

fn bad_request(err: axum::extract::multipart::MultipartError) -> (StatusCode, String) {
    (StatusCode::BAD_REQUEST, err.to_string())
}

fn invalid_workbook(err: anyhow::Error) -> (StatusCode, String) {
    (StatusCode::UNPROCESSABLE_ENTITY, err.to_string())
}
