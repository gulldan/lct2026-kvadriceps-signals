use std::io::{Read, Write};
use std::process::Stdio;
use std::time::Duration;

use anyhow::{bail, Context, Result};
use tokio::io::{AsyncReadExt, AsyncWriteExt};

use crate::model::ParseResponse;

const INPUT_LIMIT: u64 = 128 << 20;
const OUTPUT_LIMIT: u64 = 64 << 20;

// Разреженные XLS/XLSX и повторения ODS могут раздувать память внутри библиотеки
// до проверки Range. Ограничение отдельного процесса действует и на этот этап.
pub fn child() -> Result<()> {
    rlimit::Resource::CPU.set(60, 60)?;
    rlimit::Resource::CORE.set(0, 0)?;
    #[cfg(target_os = "linux")]
    rlimit::Resource::AS.set(512 << 20, 512 << 20)?;
    let name = std::env::args().nth(2).context("missing workbook name")?;
    let mut data = Vec::new();
    std::io::stdin()
        .take(INPUT_LIMIT + 1)
        .read_to_end(&mut data)?;
    if data.len() as u64 > INPUT_LIMIT {
        bail!("workbook input exceeds byte budget");
    }
    let workbook = crate::parser::parse_workbook(&data, &name)?;
    let response = crate::render::render_response(workbook)?;
    serde_json::to_writer(
        LimitedWriter {
            remaining: OUTPUT_LIMIT,
        },
        &response,
    )?;
    Ok(())
}

struct LimitedWriter {
    remaining: u64,
}

impl Write for LimitedWriter {
    fn write(&mut self, data: &[u8]) -> std::io::Result<usize> {
        if data.len() as u64 > self.remaining {
            return Err(std::io::Error::other("workbook output exceeds byte budget"));
        }
        let written = std::io::stdout().write(data)?;
        self.remaining -= written as u64;
        Ok(written)
    }

    fn flush(&mut self) -> std::io::Result<()> {
        std::io::stdout().flush()
    }
}

pub async fn parse(data: &[u8], file_name: &str) -> Result<ParseResponse> {
    let mut child = tokio::process::Command::new(std::env::current_exe()?)
        .args(["--parse-child", file_name])
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .kill_on_drop(true)
        .spawn()
        .context("start workbook parser")?;
    let mut input = child.stdin.take().context("parser stdin unavailable")?;
    let output = child.stdout.take().context("parser stdout unavailable")?;
    let run = async {
        let send = async move {
            input.write_all(data).await?;
            drop(input);
            Ok::<(), std::io::Error>(())
        };
        let receive = async {
            let mut bytes = Vec::new();
            output
                .take(OUTPUT_LIMIT + 1)
                .read_to_end(&mut bytes)
                .await?;
            if bytes.len() as u64 > OUTPUT_LIMIT {
                return Err(std::io::Error::other("workbook output exceeds byte budget"));
            }
            Ok(bytes)
        };
        let (_, bytes, status) = tokio::try_join!(send, receive, child.wait())?;
        if !status.success() {
            bail!("invalid workbook or parser resource budget exceeded");
        }
        serde_json::from_slice(&bytes).context("read workbook parser result")
    };
    match tokio::time::timeout(Duration::from_secs(90), run).await {
        Ok(Ok(result)) => Ok(result),
        result => {
            // Дожидаемся завершения перед освобождением слота, включая таймаут.
            let _ = child.kill().await;
            match result {
                Ok(Err(err)) => Err(err),
                _ => bail!("workbook parser deadline exceeded"),
            }
        }
    }
}
