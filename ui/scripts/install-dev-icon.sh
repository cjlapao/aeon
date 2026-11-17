#!/usr/bin/env bash
set -euo pipefail

# GNOME/Wayland resolves the dock icon from a desktop file matching the
# window's app ID. Tauri's bundle icons alone do not register a dev process.
ui_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
data_dir="${XDG_DATA_HOME:-${HOME}/.local/share}"
entry_dir="${data_dir}/applications"
entry="${entry_dir}/dev.cjlapao.aeon.Dev.desktop"
icon="${ui_dir}/src-tauri/icons/icon.png"
binary="${ui_dir}/src-tauri/target/debug/aeon-ui"

if [[ -e "$entry" ]] && ! grep -Fxq 'X-Aeon-Dev-Managed=true' "$entry"; then
  printf 'refusing to replace unmanaged desktop entry: %s\n' "$entry" >&2
  exit 1
fi

mkdir -p "$entry_dir"
temporary="$(mktemp "${entry}.XXXXXX")"
trap 'rm -f "$temporary"' EXIT
cat > "$temporary" <<EOF
[Desktop Entry]
Type=Application
Name=Aeon (dev)
Comment=Aeon development window
Exec=$binary
Icon=$icon
Terminal=false
NoDisplay=true
StartupNotify=true
StartupWMClass=dev.cjlapao.aeon.Dev
X-Aeon-Dev-Managed=true
EOF
chmod 644 "$temporary"
mv "$temporary" "$entry"
printf 'Aeon dev desktop icon registered: %s\n' "$entry"
