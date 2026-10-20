#!/usr/bin/env bash
set -euo pipefail

if ! command -v cargo >/dev/null 2>&1; then
  echo "cargo is required" >&2
  exit 1
fi

if ! cargo deny --version >/dev/null 2>&1; then
  echo "cargo-deny is required. Install it with: cargo install cargo-deny --locked" >&2
  exit 1
fi

cargo fmt --all --check
cargo clippy --workspace --all-targets --all-features --no-deps -- -D warnings
RUSTDOCFLAGS="-D warnings" cargo doc --workspace --all-features --no-deps
bash scripts/check_allocations.sh
cargo test --workspace
cargo test --workspace --no-default-features
cargo test --workspace --no-default-features --features service
cargo test --workspace --all-features
cargo deny check advisories bans licenses sources
