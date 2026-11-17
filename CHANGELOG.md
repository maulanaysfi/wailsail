# Changelog

All notable changes to **`wailsail`** are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- **Object Overview & Events in DetailDrawer**:
  - Added **Overview** and **YAML** tabs when inspecting a selected resource (`GetDetail` and `ObjectEvents` on `kube.Service`).
  - Displays structured **Metadata** (name, namespace, API version/kind, creation age & timestamp, UID, resourceVersion/generation, ownerReferences, finalizers, labels, and annotations), resource-specific **Specification & status** summaries, **Containers**, **Conditions**, and related Kubernetes **Events**.
- **Container Resource Usage Progress Bars**:
  - Fetches live container CPU (`m`) and memory (`bytes`) usage via `metrics.k8s.io/v1beta1` for Pods and workload controllers (`GetDetail`, `PodMetrics`).
  - Added **CPU Usage** and **Memory Usage** columns to the Pod table (placed right after the `Status` column) displaying the main container's resource usage with the same progress bar as `DetailDrawer`.
  - Renders rounded-edge progress bars on a theme-matched grey track representing resource limits, with a vertical line marker for resource requests (white at `request / limit` when both request and limit are defined, or yellow at `25%` when only a request is defined so `100%` equals `4 × request`).
  - Falls back to plain-text usage (`N/A` when metrics are unavailable) when neither request nor limit is defined, omits the vertical marker when only a limit is defined, colors the bar indicator by usage threshold (`<80%` green, `80%–94.99%` yellow, `>=95%` red), and shows `Used / Request / Limit` on hover.
- **Jump to Owner Navigation**:
  - Added **Jump to Owner** button in the resource table and `DetailDrawer` for owned objects (`Pod`, `ReplicaSet`, `Job`, etc.), navigating directly to the controlling owner resource and disabling the button when no owner reference exists (such as standalone pods).
- **Persistent Column Order & Widths**:
  - Custom column orders (`columnOrders`) and resized column widths (`columnWidths`) are now persisted per resource type in `settings.json` across restarts.
- **Row Selection Toggle**:
  - Clicking an already-selected object row in `ResourceTable` now toggles `DetailDrawer` closed.
- **Settings Modal (`SettingsModal.svelte`)**:
  - Added **Screen scale** selector (`50%`, `75%`, `100%`, `125%`, `150%`, default `100%`).
  - Added removable **Kubeconfig** file and folder list (defaulting to `$HOME/.kube/config` on Linux/macOS and `%USERPROFILE%\.kube\config` on Windows) with `~` and environment variable expansion, non-recursive folder scanning for valid kubeconfigs, native file/folder picker dialogs, and a **Reset to default** action.
- **Interactive Resource Table Columns**:
  - Made table columns static (`table-layout: fixed` with explicit `<col>` widths), resizable via header edge handles (double-click to reset), and draggable to reorder columns per resource type.
  - Long resource names and cell values no longer wrap and are clipped cleanly at the next column boundary.
- **Typography & Spacing System**:
  - Bundled the **Google Sans Flex** variable font (`frontend/public/fonts/GoogleSansFlex.ttf`, SIL OFL 1.1) for offline desktop use.
  - Standardized UI layout and component spacing on an 8-point grid (`--space-1`..`--space-6`).

### Changed
- **Wide Resource Columns by Default**: `ResourceTable` now displays all Kubernetes table columns (`kubectl get <resource> -o wide` equivalent) by default and removes the `Wide` toolbar checkbox.
- **DetailDrawer Ledger Layout**: Redesigned `DetailDrawer` sections and `Events` into flat horizontal ruled sections and a structured tabular ledger (`Reason`, `Message`, `Age`) without nested cards or decorative left-border stripes.
- **TopBar Controls & Branding**:
  - Removed the `wailsail` text label from the TopBar while keeping the app icon.
  - Replaced the text theme toggle with SVG icons (System monitor, Light sun, Dark moon) and added a Settings gear icon button.
- **Hover Tooltips**: Removed hover `title` tooltips from non-interactive elements (table cells, column headers, pills, chips, and static text) and added descriptive action tooltips to clickable buttons (and container resource usage progress bars).

### Fixed
- **Sub-100% Screen Scaling**: Fixed `50%` and `75%` screen scale options by applying CSS `zoom` with a fixed-inset root container where Wails v3's native `Window.SetZoom` clamps values below `1.0`.
- **Reload Preserves Current Page**: Clicking `Reload` in the TopBar (or updating kubeconfig paths) now keeps the currently viewed resource type and open selection instead of resetting to the `Pod` page.
- **App Icon Transparency**: Synced the transparent root `icon.png` across `frontend/public/icon.png`, `build/appicon.png`, `build/ios/icon.png`, and platform icon bundles.

## [0.1.0] - 2026-11-16

### Added
- Initial release of **`wailsail`** built with Go, Wails v3, and Svelte 5 + TypeScript.
- Kubernetes context & namespace switching, dynamic API resource discovery, and live table watches (`meta.k8s.io/v1` `Table` with 150ms batching).
- Server-Side Apply (`fieldManager: "wailsail"`), resource deletion, scaling, and rollout restarts.
- Container log streaming (`wailsail:logs:<id>`) and interactive container shell via WebSocket/SPDY (`wailsail:exec:<id>`) in the bottom dock.
- Local TCP port forwarding for Pods and Services (`wailsail:portforwards`).
- Atomic user settings persistence at `<UserConfigDir>/wailsail/settings.json`.
