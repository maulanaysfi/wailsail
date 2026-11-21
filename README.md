<p align="center">
  <img src="icon.png" alt="wailsail icon" width="112" />
</p>

<h1 align="center">wailsail</h1>

<p align="center">
  <strong>Lightweight Kubernetes desktop client built with Go, Wails v3, and Svelte 5</strong><br />
  <em>Sail as far as you can, Captain! These nodes are in your control!</em>
</p>

<p align="center">
  <a href="#highlights">Highlights</a> ·
  <a href="#installation">Installation</a> ·
  <a href="#development">Development</a> ·
  <a href="#keyboard-shortcuts">Shortcuts</a> ·
  <a href="CHANGELOG.md">Changelog</a>
</p>

---

**`wailsail`** connects straight to your Kubernetes clusters via `k8s.io/client-go` using your existing kubeconfigs—no proxy server or Electron bundle required.

## Highlights

- **Multi-Cluster & Custom Kubeconfigs** — Merges `~/.kube/config`, `$KUBECONFIG`, and custom files/folders, remembers per-context namespaces, and monitors live cluster health.
- **Discovery-Driven Browser & Helm Releases** — Automatically discovers built-in and custom resources (`CRDs`) plus in-cluster **Helm Releases** (revision history, User/Merged values inspection, live Values edit → Upgrade, Rollback, rendered Manifest, and Uninstall) in a resizable, pinnable sidebar.
- **Live Watch Tables** — Streams `meta.k8s.io/v1` tables with batched updates, draggable/resizable columns, live Pod CPU & memory usage meters, and one-click `Jump to Owner`.
- **Detail Drawer & Neat YAML** — Inspect metadata, resource specs, masked/uncensorable `Secret` & `ConfigMap` data pairs, container meters, conditions, live paginated workload/Service/Node `Pods` tables, and events—or view/edit syntax-highlighted YAML with optional **Neat** cleanup and Server-Side Apply.
- **Persistent Logs & Container Shell Dock** — Follow selectable container logs (with high-contrast timestamps and filtering) and open interactive `@xterm/xterm` shells side-by-side in a persistent bottom dock.
- **Port Forwarding & Preferences** — Forward Pod or Service ports to `127.0.0.1`, switch Light/Dark/System themes, and scale the UI (`50%`–`150%`).

## Installation

### Prebuilt Binaries

Download the latest binary and `SHA256SUMS` from [GitHub Releases](https://github.com/maulanaysfi/wailsail/releases), then verify the checksum before running:

```bash
# Linux
sha256sum -c --ignore-missing SHA256SUMS

# macOS
shasum -a 256 -c --ignore-missing SHA256SUMS
```

```powershell
# Windows (PowerShell)
Get-FileHash .\wailsail-<version>-windows-amd64.exe -Algorithm SHA256
```

### Build from Source

Requires **Go 1.26+**, **Node.js & npm**, **[Wails v3 CLI](https://v3.wails.io/)** (`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0`), and on Linux **GTK4 + WebKitGTK 6.0** (`libgtk-4-dev libwebkitgtk-6.0-dev` on Debian/Ubuntu, `gtk4 webkitgtk-6.0` on Arch, `gtk4-devel webkitgtk6.0-devel` on Fedora).

```bash
./scripts/build.sh
```

Validated binaries and `SHA256SUMS` are written to `bin/dist/`.

## Development

```bash
# Live development mode
wails3 dev

# Validate and run frontend + Go tests only
./scripts/build.sh --test-only

# Full validate, test, desktop build, and SHA256SUMS
./scripts/build.sh
```

See [AGENTS.md](AGENTS.md) for architecture and service contracts, and [RELEASE.md](RELEASE.md) for versioning and release steps.

## Keyboard Shortcuts

- `Esc` — Discard active YAML edits, or close open dialogs and the detail drawer
- `Ctrl/Cmd + S` — Apply changes while editing resource YAML
- `Tab` — Insert 2 spaces while editing resource YAML
