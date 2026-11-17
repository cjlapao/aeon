//! Read-only previews for a local component's working directory. Paths are
//! relative to a canonical root; symlinks cannot be used to leave that root.
use std::fs;
use std::path::{Component, Path, PathBuf};

use base64::{engine::general_purpose::STANDARD, Engine};
use serde::Serialize;

const MAX_ENTRIES: usize = 500;
const MAX_TEXT_BYTES: u64 = 1024 * 1024;
const MAX_IMAGE_BYTES: u64 = 8 * 1024 * 1024;

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Entry {
    name: String,
    path: String,
    kind: &'static str,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Listing {
    entries: Vec<Entry>,
    truncated: bool,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Preview {
    kind: &'static str,
    content: Option<String>,
    data_url: Option<String>,
    reason: Option<String>,
}

fn resolve(root: &str, relative: &str) -> Result<(PathBuf, PathBuf), String> {
    let root = fs::canonicalize(root).map_err(|e| format!("Component is unavailable: {e}"))?;
    if !root.is_dir() {
        return Err("Component root is not a directory".into());
    }
    let path = Path::new(relative);
    if path
        .components()
        .any(|component| !matches!(component, Component::Normal(_) | Component::CurDir))
    {
        return Err("Path must stay inside the component".into());
    }
    let target =
        fs::canonicalize(root.join(path)).map_err(|e| format!("File is unavailable: {e}"))?;
    if !target.starts_with(&root) {
        return Err("Path must stay inside the component".into());
    }
    Ok((root, target))
}

fn list(root: &str, path: &str) -> Result<Listing, String> {
    let (root, directory) = resolve(root, path)?;
    if !directory.is_dir() {
        return Err("Path is not a directory".into());
    }
    let mut entries = Vec::new();
    for item in fs::read_dir(directory).map_err(|e| format!("Cannot list directory: {e}"))? {
        let item = item.map_err(|e| format!("Cannot list directory: {e}"))?;
        let kind = item
            .file_type()
            .map_err(|e| format!("Cannot inspect file: {e}"))?;
        // A linked directory can escape the selected component; don't expose it.
        if kind.is_symlink() || !(kind.is_dir() || kind.is_file()) {
            continue;
        }
        let relative = item
            .path()
            .strip_prefix(&root)
            .map_err(|e| e.to_string())?
            .to_string_lossy()
            .replace('\\', "/");
        entries.push(Entry {
            name: item.file_name().to_string_lossy().into_owned(),
            path: relative,
            kind: if kind.is_dir() { "directory" } else { "file" },
        });
    }
    entries.sort_by(|a, b| {
        (a.kind != "directory", a.name.to_lowercase())
            .cmp(&(b.kind != "directory", b.name.to_lowercase()))
    });
    let truncated = entries.len() > MAX_ENTRIES;
    entries.truncate(MAX_ENTRIES);
    Ok(Listing { entries, truncated })
}

fn read(root: &str, path: &str) -> Result<Preview, String> {
    let (_, file) = resolve(root, path)?;
    let metadata = fs::metadata(&file).map_err(|e| format!("Cannot inspect file: {e}"))?;
    if !metadata.is_file() {
        return Err("Path is not a file".into());
    }
    let ext = file
        .extension()
        .and_then(|s| s.to_str())
        .unwrap_or("")
        .to_ascii_lowercase();
    let mime = match ext.as_str() {
        "png" => Some("image/png"),
        "jpg" | "jpeg" => Some("image/jpeg"),
        "gif" => Some("image/gif"),
        "webp" => Some("image/webp"),
        "bmp" => Some("image/bmp"),
        _ => None,
    };
    if let Some(mime) = mime {
        if metadata.len() > MAX_IMAGE_BYTES {
            return Ok(unavailable("Image is too large to preview (8 MiB limit)."));
        }
        let bytes = fs::read(file).map_err(|e| format!("Cannot read image: {e}"))?;
        return Ok(Preview {
            kind: "image",
            content: None,
            data_url: Some(format!("data:{mime};base64,{}", STANDARD.encode(bytes))),
            reason: None,
        });
    }
    let file_name = file
        .file_name()
        .and_then(|s| s.to_str())
        .unwrap_or("")
        .to_ascii_lowercase();
    let kind = match ext.as_str() {
        "md" | "markdown" | "mdx" => "markdown",
        "txt" | "log" | "csv" | "go" | "rs" | "ts" | "tsx" | "js" | "jsx" | "mjs" | "cjs"
        | "json" | "jsonc" | "yaml" | "yml" | "toml" | "mod" | "sum" | "lock" | "py" | "sh"
        | "bash" | "zsh" | "css" | "scss" | "html" | "htm" | "xml" | "svg" | "sql" | "c" | "h"
        | "cpp" | "hpp" | "java" | "kt" | "swift" | "rb" | "php" | "vue" | "svelte" | "proto"
        | "ini" | "cfg" | "conf" | "env" | "gitignore" | "dockerfile" | "makefile" => "code",
        _ if matches!(
            file_name.as_str(),
            "makefile" | "dockerfile" | "justfile" | ".gitignore" | ".env" | ".editorconfig"
        ) =>
        {
            "code"
        }
        _ => "binary",
    };
    if kind == "binary" {
        return Ok(unavailable(
            "This file type cannot be opened in the preview.",
        ));
    }
    if metadata.len() > MAX_TEXT_BYTES {
        return Ok(unavailable("File is too large to preview (1 MiB limit)."));
    }
    let bytes = fs::read(file).map_err(|e| format!("Cannot read file: {e}"))?;
    if bytes.contains(&0) {
        return Ok(unavailable(
            "Binary content cannot be opened in the preview.",
        ));
    }
    let content = match String::from_utf8(bytes) {
        Ok(content) => content,
        Err(_) => {
            return Ok(unavailable(
                "This file is not UTF-8 text and cannot be opened in the preview.",
            ))
        }
    };
    Ok(Preview {
        kind,
        content: Some(content),
        data_url: None,
        reason: None,
    })
}

fn unavailable(reason: &str) -> Preview {
    Preview {
        kind: "binary",
        content: None,
        data_url: None,
        reason: Some(reason.into()),
    }
}

/// List one directory of a component's checkout.
///
/// ASYNC on purpose (S72): a `read_dir` plus a metadata stat per entry is disk
/// work on somebody's working tree — a large checkout on a slow mount is
/// seconds — and a synchronous command runs that on the window's thread,
/// which is how opening a file panel ends up freezing the whole app.
#[tauri::command]
pub async fn component_list(root: String, path: String) -> Result<Listing, String> {
    tauri::async_runtime::spawn_blocking(move || list(&root, &path))
        .await
        .map_err(|e| format!("the listing task did not finish: {e}"))?
}

/// Read one file of a component's checkout, as a preview or a data URL.
#[tauri::command]
pub async fn component_read(root: String, path: String) -> Result<Preview, String> {
    tauri::async_runtime::spawn_blocking(move || read(&root, &path))
        .await
        .map_err(|e| format!("the read task did not finish: {e}"))?
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{SystemTime, UNIX_EPOCH};

    #[test]
    fn lists_and_previews_inside_component_only() {
        let root = std::env::temp_dir().join(format!(
            "aeon-files-{}-{}",
            std::process::id(),
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        fs::create_dir_all(root.join("src")).unwrap();
        fs::write(root.join("README.md"), "# Hello").unwrap();
        fs::write(root.join("src/main.rs"), "fn main() {}").unwrap();
        fs::write(root.join("blob.bin"), [0, 1, 2]).unwrap();
        fs::write(root.join("go.mod"), "module example.com/qa\n").unwrap();
        let root_str = root.to_str().unwrap();
        let listed = list(root_str, ".").unwrap();
        assert_eq!(listed.entries[0].name, "src");
        assert_eq!(read(root_str, "README.md").unwrap().kind, "markdown");
        assert_eq!(read(root_str, "src/main.rs").unwrap().kind, "code");
        assert_eq!(read(root_str, "go.mod").unwrap().kind, "code");
        assert_eq!(read(root_str, "blob.bin").unwrap().kind, "binary");
        assert!(read(root_str, "../README.md").is_err());
        assert!(list(root_str, "/etc").is_err());
        #[cfg(unix)]
        {
            std::os::unix::fs::symlink("/etc/passwd", root.join("outside")).unwrap();
            assert!(read(root_str, "outside").is_err());
            assert!(!list(root_str, ".")
                .unwrap()
                .entries
                .iter()
                .any(|entry| entry.name == "outside"));
        }
        fs::remove_dir_all(root).unwrap();
    }
}
