use tracing_subscriber::EnvFilter;

fn main() -> anyhow::Result<()> {
    if std::env::args().nth(1).as_deref() == Some("--parse-child") {
        return workbook_parser_service::sandbox::child();
    }
    tracing_subscriber::fmt()
        .json()
        .with_env_filter(EnvFilter::from_default_env())
        .init();
    tokio::runtime::Runtime::new()?.block_on(workbook_parser_service::http::serve())
}
