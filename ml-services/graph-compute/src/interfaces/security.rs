//! Внутренний RPC требует проверенный сертификат и разрешённую сервисную роль.

use anyhow::{bail, Result};
use tonic::transport::{Certificate, Identity, ServerTlsConfig};
use tonic::{Request, Status};
use x509_parser::extensions::GeneralName;
use x509_parser::parse_x509_certificate;

pub(super) fn tls_config() -> Result<Option<ServerTlsConfig>> {
    let mode = std::env::var("GRPC_TLS_MODE").unwrap_or_default();
    if mode.is_empty() || mode == "disabled" {
        if std::env::var("APP_ENV").as_deref() == Ok("production") {
            bail!("production requires GRPC_TLS_MODE=mtls");
        }
        return Ok(None);
    }
    if mode != "mtls" {
        bail!("unsupported GRPC_TLS_MODE");
    }
    // HTTP-клиент и сервер включают разные реализации; выбираем одну явно.
    let _ = rustls::crypto::ring::default_provider().install_default();
    let ca = std::fs::read(std::env::var("GRPC_TLS_CA")?)?;
    let cert = std::fs::read(std::env::var("GRPC_TLS_CERT")?)?;
    let key = std::fs::read(std::env::var("GRPC_TLS_KEY")?)?;
    Ok(Some(
        ServerTlsConfig::new()
            .identity(Identity::from_pem(cert, key))
            .client_ca_root(Certificate::from_pem(ca)),
    ))
}

pub(super) fn authorize(request: Request<()>) -> Result<Request<()>, Status> {
    let certificates = request
        .peer_certs()
        .ok_or_else(|| Status::unauthenticated("service certificate required"))?;
    let leaf = certificates
        .first()
        .ok_or_else(|| Status::unauthenticated("service certificate required"))?;
    let (_, certificate) = parse_x509_certificate(leaf.as_ref())
        .map_err(|_| Status::unauthenticated("invalid certificate"))?;
    let san = certificate
        .subject_alternative_name()
        .map_err(|_| Status::permission_denied("invalid identity"))?
        .ok_or_else(|| Status::permission_denied("identity required"))?;
    let identities: Vec<&str> = san
        .value
        .general_names
        .iter()
        .filter_map(|name| if let GeneralName::URI(uri) = name { Some(*uri) } else { None })
        .collect();
    if identities.len() != 1
        || !matches!(
            identities[0],
            "spiffe://rag.local/service/main-service"
                | "spiffe://rag.local/service/llm-service"
                | "spiffe://rag.local/service/cluster-worker"
                | "spiffe://rag.local/service/discovery-worker"
                | "spiffe://rag.local/service/raptor-worker"
                | "spiffe://rag.local/service/signal-worker"
        )
    {
        return Err(Status::permission_denied("graph RPC denied for service"));
    }
    Ok(request)
}
