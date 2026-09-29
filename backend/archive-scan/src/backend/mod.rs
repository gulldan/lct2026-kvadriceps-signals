mod libarchive;

use crate::row::EntryKind;
use anyhow::Result;
use std::{io::Read, path::Path};

pub use libarchive::LibarchiveBackend;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct BackendOptions {
    pub block_size: usize,
}

#[derive(Debug, Eq, PartialEq)]
pub struct EntryMetadata {
    pub name: Box<str>,
    pub kind: EntryKind,
}

pub trait ArchiveBackend: Sync {
    fn count_entries(&self, path: &Path, options: BackendOptions) -> Result<usize>;

    fn for_each_entry(
        &self,
        path: &Path,
        options: BackendOptions,
        visitor: &mut dyn FnMut(EntryMetadata, &mut dyn Read) -> Result<()>,
    ) -> Result<()>;
}
