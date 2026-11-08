//! Classify a file when it is opened. Directory listings never read contents.
use std::fs::File;
use std::io::{self, Read};
use std::path::Path;
use crate::theme::tr;

pub const TEXT_LIMIT: u64 = 5 * 1024 * 1024;
pub const HIGHLIGHT_LIMIT: usize = 128 * 1024;
const LINE_LIMIT: usize = 200 * 1024;
const SNIFF: usize = 4096;

#[derive(Clone, Debug)]
pub struct SourceFile {
    pub text: String,
    pub bom: bool,
    pub language: String,
    pub highlight: bool,
    pub html: bool,
    pub svg: bool,
}

#[derive(Clone, Debug)]
pub enum OpenKind {
    Markdown(String),
    Source(SourceFile),
    /// Raster image, or SVG. `svg` carries editable source when the file is UTF-8 text.
    Preview { svg: Option<SourceFile> },
    Blocked { reason: String },
}

pub struct Classified {
    pub kind: OpenKind,
    /// Exact bytes when the whole file was read; empty for images and unread rejects.
    pub bytes: Vec<u8>,
}

pub fn raw_bytes(text: &str, bom: bool) -> Vec<u8> {
    let mut out = Vec::with_capacity(text.len() + usize::from(bom) * 3);
    if bom { out.extend_from_slice(&[0xEF, 0xBB, 0xBF]); }
    out.extend_from_slice(text.as_bytes());
    out
}

pub fn read_file(path: &Path) -> Result<Classified, String> {
    #[cfg(target_family = "wasm")]
    {
        let text = super::vfs::read_to_string(path).unwrap_or_default();
        let bytes = text.into_bytes();
        return Ok(Classified { kind: kind_of(path, &bytes, bytes.len() as u64), bytes });
    }
    #[cfg(not(target_family = "wasm"))]
    {
        let meta = std::fs::metadata(path).map_err(|e| format!("{}: {e}", tr("无法读取", "Cannot read")))?;
        if !meta.is_file() {
            return Ok(Classified { kind: OpenKind::Blocked { reason: tr("此文件类型暂不支持", "This file type is not supported").into() }, bytes: Vec::new() });
        }
        let len = meta.len();
        if crate::ui::file_preview::is_image_path(path) && !crate::ui::file_preview::is_svg_path(path) {
            return Ok(Classified { kind: OpenKind::Preview { svg: None }, bytes: Vec::new() });
        }
        if len > TEXT_LIMIT {
            return Ok(Classified { kind: over_limit(path), bytes: Vec::new() });
        }
        let bytes = match read_bounded(path, TEXT_LIMIT) {
            Err(e) => return Err(format!("{}: {e}", tr("无法读取", "Cannot read"))),
            Ok(None) => return Ok(Classified { kind: over_limit(path), bytes: Vec::new() }),
            Ok(Some(bytes)) => bytes,
        };
        Ok(Classified { kind: kind_of(path, &bytes, len), bytes })
    }
}

/// Read at most `cap + 1` bytes. `None` means the file grew past the text cap.
fn read_bounded(path: &Path, cap: u64) -> io::Result<Option<Vec<u8>>> {
    let file = File::open(path)?;
    let mut bytes = Vec::new();
    file.take(cap.saturating_add(1)).read_to_end(&mut bytes)?;
    Ok(if bytes.len() as u64 > cap { None } else { Some(bytes) })
}

fn over_limit(path: &Path) -> OpenKind {
    let sample = sniff(path).unwrap_or_default();
    let textual = super::scan::is_markdown(&file_name(path)) || known_text(&file_name(path)) || sample_is_text(&sample);
    let reason = if textual { tr("文件超过 5 MB", "File is larger than 5 MB").into() }
        else { tr("此文件类型暂不支持", "This file type is not supported").into() };
    if crate::ui::file_preview::is_svg_path(path) { OpenKind::Preview { svg: None } } else { OpenKind::Blocked { reason } }
}

fn sniff(path: &Path) -> io::Result<Vec<u8>> {
    let mut file = File::open(path)?;
    let mut buf = vec![0; SNIFF];
    let n = file.read(&mut buf)?;
    buf.truncate(n);
    Ok(buf)
}

fn file_name(path: &Path) -> String {
    path.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default()
}

fn kind_of(path: &Path, bytes: &[u8], _len: u64) -> OpenKind {
    let name = file_name(path);
    if crate::ui::file_preview::is_svg_path(path) {
        return match decode_source(path, bytes, true) {
            Ok(file) => OpenKind::Preview { svg: Some(file) },
            Err(_) => OpenKind::Preview { svg: None },
        };
    }
    if super::scan::is_markdown(&name) {
        return match std::str::from_utf8(bytes) {
            Ok(text) if !line_too_long(text) => OpenKind::Markdown(text.to_owned()),
            Ok(_) => OpenKind::Blocked { reason: tr("单行过长，无法预览", "A line is too long to preview").into() },
            Err(_) => OpenKind::Blocked { reason: tr("不是有效的 UTF-8", "Not valid UTF-8").into() },
        };
    }
    if known_text(&name) {
        return match decode_source(path, bytes, false) {
            Ok(file) => OpenKind::Source(file),
            Err(reason) => OpenKind::Blocked { reason },
        };
    }
    match decode_source(path, bytes, false) {
        Ok(file) => OpenKind::Source(file),
        Err(_) => OpenKind::Blocked { reason: tr("此文件类型暂不支持", "This file type is not supported").into() },
    }
}

fn decode_source(path: &Path, bytes: &[u8], svg: bool) -> Result<SourceFile, String> {
    let (bom, rest) = if bytes.starts_with(&[0xEF, 0xBB, 0xBF]) { (true, &bytes[3..]) } else { (false, bytes) };
    if binary_controls(rest) {
        return Err(tr("包含二进制内容", "File contains binary data").into());
    }
    let text = std::str::from_utf8(rest).map_err(|_| tr("不是有效的 UTF-8", "Not valid UTF-8").to_owned())?;
    if line_too_long(text) {
        return Err(tr("单行过长，无法预览", "A line is too long to preview").into());
    }
    let ext = ext_of(path);
    let language = language_tag(&ext).to_owned();
    let html = matches!(ext.as_str(), "html" | "htm" | "xhtml");
    let highlight = text.len() <= HIGHLIGHT_LIMIT;
    Ok(SourceFile { text: text.to_owned(), bom, language, highlight, html, svg })
}

pub fn source_from_draft(path: &Path, text: String, bom: bool) -> SourceFile {
    let ext = ext_of(path);
    let svg = crate::ui::file_preview::is_svg_path(path);
    let html = matches!(ext.as_str(), "html" | "htm" | "xhtml");
    SourceFile {
        highlight: text.len() <= HIGHLIGHT_LIMIT,
        language: language_tag(&ext).to_owned(),
        text, bom, html, svg,
    }
}

fn line_too_long(text: &str) -> bool {
    text.split('\n').any(|line| line.len() > LINE_LIMIT)
}

fn binary_controls(bytes: &[u8]) -> bool {
    bytes.iter().any(|b| *b == 0 || (*b < 0x20 && !matches!(*b, b'\t' | b'\n' | b'\r')))
}

fn sample_is_text(bytes: &[u8]) -> bool {
    let end = bytes.iter().rposition(|b| *b < 0x80).unwrap_or(0);
    let sample = if end == 0 { bytes } else { &bytes[..=end.min(bytes.len().saturating_sub(1))] };
    std::str::from_utf8(sample).is_ok() && !binary_controls(sample)
}

fn ext_of(path: &Path) -> String {
    path.extension().and_then(|e| e.to_str()).unwrap_or("").to_ascii_lowercase()
}

pub fn language_tag(ext: &str) -> &'static str {
    let canon = crate::editor::block_menu::canonical_lang_tag(ext);
    if !canon.is_empty() { return canon; }
    match ext {
        "mjs" | "cjs" => "javascript",
        "mts" | "cts" => "typescript",
        "xhtml" => "html",
        "xsl" => "xml",
        "fish" => "bash",
        _ => "plaintext",
    }
}

fn known_text(name: &str) -> bool {
    let ext = Path::new(name).extension().and_then(|e| e.to_str()).unwrap_or("").to_ascii_lowercase();
    if ext.is_empty() { return false; }
    matches!(ext.as_str(),
        "txt" | "text" | "log" | "csv" | "tsv" | "toml" | "ini" | "cfg" | "conf" | "env" | "properties"
        | "json" | "jsonc" | "xml" | "svg" | "xsl" | "html" | "htm" | "xhtml" | "css" | "scss" | "less"
        | "yml" | "yaml" | "js" | "jsx" | "mjs" | "cjs" | "ts" | "tsx" | "mts" | "cts" | "py" | "rs"
        | "go" | "java" | "c" | "h" | "cc" | "cpp" | "hpp" | "hh" | "cs" | "rb" | "php" | "lua" | "sql"
        | "sh" | "bash" | "zsh" | "fish" | "ps1" | "bat" | "cmd" | "vue" | "svelte" | "astro"
        | "lock" | "gradle" | "mod" | "sum" | "rst" | "tex" | "latex" | "ltx" | "lean" | "mmd" | "mermaid" | "bib" | "graphql" | "proto" | "zig"
        | "kt" | "kts" | "swift" | "dart" | "ex" | "exs" | "erl" | "hs" | "ml" | "clj" | "scala"
        | "diff" | "patch" | "gitignore" | "dockerignore" | "editorconfig" | "npmrc" | "prettierrc"
        | "eslintrc" | "babelrc" | "markdown" | "mdx")
}
