# Changelog

All notable changes to **`wailsail`** are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- **Dock Tab Icons, Status Colors & Close All**:
  - Replaced text badges in `BottomDock` tabs with Logs and Shell SVG icons colored by live session status (`green` = connected, `yellow` = connecting/reconnecting, `red` = disconnected via `connected` events on `logs.Event` and `exec.Event`).
  - Made the dock tab strip horizontally scrollable when many tabs are open and added a **Close all** button (with confirmation prompt) next to the hide dock button.

### Changed
- **Bottom-Right Dock Button**: Moved the **Dock** toggle button out of `TopBar` into a bottom bar anchored at the bottom-right corner.
- **YAML Edit Escape Behavior**: Pressing `Esc` (or clicking `Cancel`) while editing YAML in `DetailDrawer` now discards edits without a confirmation prompt and exits edit mode without closing the drawer.

### Fixed
- **DetailDrawer Container Image Truncation**: Truncated long container image names/tags with ellipsis so they no longer expand `DetailDrawer` width or cause horizontal scrolling.

## [0.1.2-beta] - 2026-11-19

### Added
- **Neat YAML Mode & YAML Copy in DetailDrawer**:
  - Added `ObjectToNeatYAML` / `NeatObject` in `internal/kube` and `neatYaml` on `ObjectDetail` to strip server-managed state, runtime fields, and defaulted Kubernetes values (`status`, `managedFields`, `uid`, `resourceVersion`, `creationTimestamp`, `last-applied-configuration`, auto-mounted service account token volumes, and Pod/Service/Deployment defaults).
  - Added a session-scoped **Neat** checkbox (default off) and a **Copy** button in the `DetailDrawer` YAML toolbar (copies neated YAML when Neat is enabled, or raw YAML otherwise).
- **Quick Copy Buttons & Expandable Long Values**:
  - Added inline copy icon buttons on the left of key `Metadata`, `Specification & status`, and container image values in `DetailDrawer`.
  - Truncated label and annotation values longer than 100 characters with a clickable ellipsis (`…`) to expand and collapse inline.
- **Resizable & Abbreviated Resource Sidebar**:
  - Abbreviated well-known long resource kinds in the sidebar (`CRD`, `PVC`, `PDB`, `HPA`, `VPA`, `CSR`).
  - Made the resource sidebar horizontally resizable via its right border up to the width required to display the longest resource name in full.

### Changed
- **Release Guide**: Added [RELEASE.md](RELEASE.md) documenting all application version placeholders and step-by-step release instructions.
- **Text Selection Rules**: Disabled text selection globally across the UI while keeping inputs, textareas, and `DetailDrawer` value fields (plus both keys and values on Labels, Annotations, and Selectors) selectable.
- **Container Resource Usage Boxes in DetailDrawer**: Wrapped each container's CPU and memory meters in a center-aligned outlined container matching the drawer background (`--border` outline) with the metric name and usage text placed side-by-side underneath the progress bar for consistent alignment across multi-container pods.
- **Release Metadata**: Bumped application version placeholders and release tag to `v0.1.2-beta` (`v0.1.2 (beta)` in Settings).

### Fixed
- **Pod Table Usage Text Overlap**: Re-measures rendered Google Sans Flex usage text widths in pixels once fonts load (with character-width floor and a `2px` bar gap) so values such as `250m` never overlap the adjacent CPU/Memory usage progress bar.

## [0.1.1-beta] - 2026-11-17

### Added
- **Cluster Connection Health Indicator & Version Tag**:
  - Added connection status dot beside the cluster version pill in `TopBar` (`green` when connected, `yellow` when not replying for 5s, `red` when timed out for 30s or disconnected).
  - Added application version pill (`v0.1.1 (beta)`) in the `SettingsModal` header.
- **Persistent Dock Sessions & Tab Redirect**:
  - Minimizing and reopening `BottomDock` now keeps active container shell and log sessions alive until their tab is explicitly closed.
  - Clicking `Logs` or `Shell` for a pod/container that already has an open dock tab now focuses and un-minimizes the existing tab instead of opening duplicates.
- **Sidebar Accordion Ordering, Default Collapse & CRD Separator**:
  - Ordered native sidebar sections as `Cluster`, `Workloads`, `Network`, `Storage`, `Config`, and `Access`, with only the first 5 expanded by default and all other groups collapsed by default.
  - Added a `CRDs` divider separating built-in Kubernetes resource groups from custom resource definition groups (`HTTPRoute` and `Gateway` moved out of hardcoded `Network` into dynamic CRD groups).
- **Object Overview & Events in DetailDrawer**:
  - Added **Overview** and **YAML** tabs when inspecting a selected resource (`GetDetail` and `ObjectEvents` on `kube.Service`).
  - Displays structured **Metadata** (name, namespace, API version/kind, creation age & timestamp, UID, resourceVersion/generation, ownerReferences, finalizers, labels, and annotations), resource-specific **Specification & status** summaries, **Containers**, **Conditions**, and related Kubernetes **Events**.
- **Container Resource Usage Progress Bars**:
  - Fetches live container CPU (`m`) and memory (`bytes`) usage via `metrics.k8s.io/v1beta1` for Pods and workload controllers (`GetDetail`, `PodMetrics`).
  - Added **CPU Usage** and **Memory Usage** columns to the Pod table (placed right after the `Status` column) with uniform track widths across rows, and side-by-side CPU/Memory meters in `DetailDrawer`, using a `10px` bar height (`--meter-h`).
  - Renders rounded-edge progress bars on a theme-matched grey track representing resource limits, with a vertical line marker for resource requests (white at `request / limit` when both request and limit are defined, or yellow at `25%` when only a request is defined so `100%` equals `4 × request`).
  - Falls back to plain-text usage (`N/A` when metrics are unavailable) when neither request nor limit is defined, omits the vertical marker when only a limit is defined, colors the bar indicator by usage threshold (`<80%` green, `80%–94.99%` yellow, `>=95%` red), and shows `Used / Request / Limit` on hover.
- **Jump to Owner Navigation**:
  - Added **Jump to Owner** button in `DetailDrawer` for owned objects (`Pod`, `ReplicaSet`, `Job`, etc.), navigating directly to the controlling owner resource and disabling the button when no owner reference exists (such as standalone pods).
- **DetailDrawer Action Icons & Search Clear Buttons**:
  - Replaced `DetailDrawer` header action buttons (`Logs`, `Shell`, `Refresh`, `Edit YAML`, `Delete`) with right-aligned icon buttons (keeping text buttons for `Overview`/`YAML`, `Jump to Owner`, `Port forward`, and per-container `Logs`/`Shell` actions).
  - Removed the redundant right-hand action column from the Pod table since all actions are available in `DetailDrawer`.
  - Added clear (`✕`) buttons to the sidebar resource search bar and table filter input when non-empty.
  - Set resource usage meter text to use Google Sans Flex (`--sans`) with tabular numerals.
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
  - Removed the `+` prefix from the `Apply` button and changed the `Reload` button to an SVG refresh icon.
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
