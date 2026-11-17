use std::fs;
use std::path::Path;

// The Go method table is the source. The three session bootstrap calls are
// protocol mechanics, not an app-side dispatch table.
#[test]
fn no_protocol_dispatch_table_in_rust() {
    let root = Path::new(env!("CARGO_MANIFEST_DIR"));
    let table = fs::read_to_string(root.join("../../internal/protocol/method.go")).unwrap();
    let mut methods = Vec::new();
    for line in table.lines() {
        if !line.contains("MethodName =") {
            continue;
        }
        if let Some(name) = line.split('"').nth(1) {
            methods.push(name.to_string());
        }
    }
    assert!(methods.len() > 20, "the method table parser found too little");
    for source in fs::read_dir(root.join("src")).unwrap() {
        let source = source.unwrap().path();
        if source.extension().and_then(|e| e.to_str()) != Some("rs") {
            continue;
        }
        let text = fs::read_to_string(&source).unwrap();
        for method in &methods {
            // The session bootstrap calls are protocol mechanics, not an
            // app-side dispatch table; `inbox.summary` is the ONE named
            // exception, added by S58 T7 (docs/aeon/S58-supervision/tasks/
            // T7.md, Implementation 18): the tray's count line asks the
            // daemon through the shell's only session while the webview is
            // hidden, so no web-side generic command can carry the call.
            if ["initialize", "version", "ping", "inbox.summary"].contains(&method.as_str()) {
                continue;
            }
            assert!(
                !text.contains(&format!("\"{method}\"")),
                "{} duplicated Go method {method}",
                source.display()
            );
        }
    }
}
