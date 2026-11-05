//! Resolve Markdown image targets the way the editor does, as filesystem paths.
use std::path::{Path, PathBuf};
use gpui::{ImageSource, Resource, SharedUri};

pub(super) fn resolver(
    note_dir: Option<PathBuf>,
) -> impl Fn(&SharedUri) -> ImageSource + Send + Sync + 'static {
    move |uri| source_for(uri.as_ref(), note_dir.as_deref())
}

fn source_for(raw: &str, note_dir: Option<&Path>) -> ImageSource {
    let src = raw.trim();
    if src.is_empty() || is_remote(src) || is_data(src) || (src.contains("://") && !is_file_url(src)) {
        return ImageSource::Resource(Resource::Uri(src.to_string().into()));
    }
    let path = local_path(src, note_dir);
    ImageSource::Resource(Resource::from(path))
}

fn local_path(src: &str, note_dir: Option<&Path>) -> PathBuf {
    let path = if is_file_url(src) {
        let mut rest = &src[7..];
        if rest.len() >= 9 && rest.as_bytes()[..9].eq_ignore_ascii_case(b"localhost") {
            rest = &rest[9..];
        }
        PathBuf::from(rest.replace("%20", " "))
    } else {
        PathBuf::from(src.replace("%20", " "))
    };
    if path.is_absolute() {
        path
    } else if let Some(dir) = note_dir {
        dir.join(path)
    } else {
        path
    }
}

fn is_file_url(src: &str) -> bool {
    src.len() >= 7 && src.as_bytes()[..7].eq_ignore_ascii_case(b"file://")
}

fn is_remote(src: &str) -> bool {
    let bytes = src.as_bytes();
    (bytes.len() > 7 && bytes[..7].eq_ignore_ascii_case(b"http://"))
        || (bytes.len() > 8 && bytes[..8].eq_ignore_ascii_case(b"https://"))
}

fn is_data(src: &str) -> bool {
    src.len() > 5 && src.as_bytes()[..5].eq_ignore_ascii_case(b"data:")
}
