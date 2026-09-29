//! Контракт gRPC собирается только с фичей `service`: библиотеке и тестам
//! `protoc` не нужен.

fn main() -> Result<(), Box<dyn std::error::Error>> {
    if std::env::var_os("CARGO_FEATURE_SERVICE").is_none() {
        return Ok(());
    }

    println!("cargo:rerun-if-changed=../../backend/proto/graph/v1/graph.proto");
    tonic_prost_build::configure()
        .build_server(true)
        .build_client(false)
        .compile_protos(&["../../backend/proto/graph/v1/graph.proto"], &["../../backend/proto"])?;
    Ok(())
}
