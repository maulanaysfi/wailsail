# Changelog

All notable changes to **`wailsail`** are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- **CRD Structured Spec Rendering (`specs.ts`, `DetailDrawer`)**: Replaced generic `(N) item(s)` fallbacks with bounded structured `Specification & status` rendering for Cilium and other CRDs (flattening nested fields, summarizing object arrays and Envoy `@type` resources, summarizing `CustomResourceDefinition` without dumping `openAPIV3Schema`, rendering labeled selector `.chips` rows, and capping long lists at 5 items with an inline expand toggle).
- **Aggregated Workload Logs (`logs.StartWorkload`, `LogViewer`, `DetailDrawer`)**: Added an aggregated `Logs` button in `DetailDrawer` for workloads (`Deployment`, `ReplicaSet`, `StatefulSet`, `DaemonSet`, `Job`, `CronJob`) streaming all main containers with a 15-pod safety cap, evenly split `TailLines`, automatic live following of newly created rollout Pods without reconnecting, and uniquely colored Pod name prefixes before each log line.
- **Controlled Pods `Logs` & `Shell` Buttons (`DetailDrawer`)**: Added compact `Logs` and `Shell` text action buttons between `Name` and `Ready` on each row in `DetailDrawer`'s controlled `Pods` table.
- **Log Viewer `Wrap` Toggle (`LogViewer`)**: Added a `Wrap` checkbox to toggle between single-line horizontal scrolling (`white-space: pre`) and word-wrapped lines (`white-space: pre-wrap; word-break: break-all`).

### Changed
- **Status Toasts & Connectivity Notifications (`App.svelte`, `store.svelte.ts`, `format.ts`)**: Replaced global top connectivity error banners with bottom-right status toasts (`info` blue, `ok` green, `warn` yellow, `error` red) featuring filled status icons, ease-in-out fade transitions, word-count-scaled conditional timeouts (`toastTimeoutMs`) that pause on hover/focus, wrapping for long unformatted messages without overflow, underlined `Copy` links on `warn`/`error` toasts, and a pinned connectivity toast with `Retry` until reconnected or dismissed (keeping inline modal/panel `.banner.error` blocks unchanged).
- **Sibling CRD Spec Module (`crdSpecs.ts`, `specs.ts`)**: Split CRD spec flattening, acronym humanization, `CustomResourceDefinition` summary, and `buildSelectorGroups` out of `specs.ts` into `frontend/src/lib/crdSpecs.ts`.
- **Controlled Pods Layout & Drawer Width (`DetailDrawer`)**: Moved the `Logs` / `Shell` action column (empty header) between `Name` and `Ready`, increased button gap (`8px`) and column gutters (`16px`) with middle alignment and `Restarts` flush-right, and widened default `DetailDrawer` width from `40vw` to `45vw`.

### Fixed
- **YAML Edits Preserving Attribute Deletions (`internal/kube/service.go`, `DetailDrawer`)**: Switched `Service.Update` from Server-Side Apply to full replacement (`PUT`, matching `kubectl edit`) with `metadata.resourceVersion` preserved (`PrepareForUpdate`), and ensured **Edit** loads full raw YAML instead of neated YAML so deleting foreign-owned fields (e.g., terminating PV `finalizers`, Node `node-role` labels) takes effect.
- **Auto-Reload After Reconnect (`store.svelte.ts`, `ResourceTable`, `DetailDrawer`, `watch.go`)**: Automatically hydrated `APIResources`, `Namespaces`, and active watches in `#probeCluster()` when recovering from an offline startup or disconnect (`reconnectNonce`), and lowered max watch backoff to `10s`.
- **Log Filter Box Overflow (`LogViewer`, `BottomDock`)**: Added `min-width: 0` across `.dock`, `.body`, `.logs`, `.bar`, and `.out` (with `overflow: hidden` on `.logs`) so long unwrapped log lines scroll horizontally inside `.out` instead of pushing the `Filter lines…` box off-screen.
- **CRD Label Formatting & Policy Rule Rendering (`specs.ts`)**: Preserved plural acronyms (`Destination CIDRs`, `External IPs`, `Load balancer IPs`, `Allow first last IPs`) and `BootID` on `CiliumNode`, labeled IP pool `blocks` as `CIDR Blocks`, unpacked `CiliumClusterwideNetworkPolicy`/`CiliumNetworkPolicy` `specs[]` rule lists cleanly, and raised summary depth/key limits so nested `ingress`/`egress` rules and ports render completely.
- **Log Viewer Scroll & Virtualization (`LogViewer`, `logWindow.ts`)**: Replaced fixed `16px` virtualizer math for wrapped lines with per-row height estimation/DOM measurement and viewport scroll anchoring (`computeLogWindow`), guarded `stickBottom` against async batch bursts and `onWheel` upward scrolling, and preserved scroll position when `MAX_LINES` trims or `Wrap` toggles.
- **Helm Template Sprig & Capabilities Support (`internal/kube/helm.go`)**: Switched Helm template functions to `github.com/Masterminds/sprig/v3` (fixing missing `semver` and related Sprig functions on charts like Cilium), added `.Capabilities.APIVersions.Has`, `fromYamlArray`/`fromJsonArray`, and wired live cluster `lookup`.
- **Resource Table Header Overlap (`ResourceTable.svelte`)**: Raised sticky `thead th` `z-index` above `.meter-req-mark` so scrolling Pod rows no longer float vertical request lines over column headers.
- **Pod Usage Text Cropping & Bar Alignment (`format.ts`, `ResourceTable.svelte`)**: Fixed `measureTextWidth(..., true)` applying `font-variant-numeric: tabular-nums` after `style.font` (which otherwise reset `tabular-nums`) and normalizing by CSS `zoom`, plus kept `.cell-meter` locked to `var(--text-w)` so trailing units (`173m`, `815Mi`) are never cropped and all usage bars stay aligned across rows.
- **Resource Meter Fill Clipping**: Added an inner rounded `.meter-clip` container inside `.meter-track` (`style.css`, `ResourceTable.svelte`, `DetailDrawer.svelte`) so low-percentage usage fills render as a partial circle clipped to the pill track instead of a flat vertical line while keeping `.meter-req-mark` extending 2px above and below the bar.

## [0.2.0-beta] - 2026-11-21

### Added
- **Helm Release Support (`internal/kube/helm.go`, `DetailDrawer`, `Sidebar`)**:
  - Added a `Helm` (`Release`) menu section placed right after `Cluster` in the sidebar, streaming live deduplicated latest-revision releases (`Name`, `Revision`, `Status`, `Chart`, `App Version`, `Age`) from in-cluster `sh.helm.release.v1` Secrets without adding external Helm SDK dependencies.
  - Added `DetailDrawer` support for Helm releases with `Overview` (release metadata, chart/app versions, deployment timestamps, notes, and a paginated 5-row `Revisions` history table with one-click **Rollback**), `Values` (`User values` checkbox toggle matching the `Neat` YAML toggle—unchecked shows merged values, checked shows user-only values—plus live YAML editing & **Apply** to re-render templates and upgrade the release to revision `v<N+1>`), `Manifest` (syntax-highlighted rendered Kubernetes manifest), and **Uninstall** (deleting managed resources and release history).
- **Zero-Dependency YAML Syntax Highlighting (`YamlEditor`, `yamlHighlight.ts`)**: Added lightweight YAML syntax highlighting for read-only viewing and live overlay editing in both `DetailDrawer` and `ApplyModal`, with matching Dark and Light theme tokens in `frontend/public/style.css`.
- **Controlled Pods List in `DetailDrawer`**: Added a live-watching, paginated (`5` rows per page) `Pods` summary table (`Name`, `Ready`, `Status`, `Age`, `Restarts`, click a row to inspect that Pod) above `Events` for workload controllers (`Deployment` tracking both old and new `ReplicaSet`s during rollouts until old `ReplicaSet` pods terminate, `ReplicaSet`, `StatefulSet`, `DaemonSet`, `Job`, `CronJob`), `Service` (via `spec.selector`), `Node` (via `spec.nodeName` field selector), and `Namespace` (Pods in that namespace).
- **Filtered Table Watches, Immutable Edit Guard & Owner UID (`internal/kube`)**: Added `Service.WatchFiltered` (`labelSelector` / `fieldSelector` query support), `Service.Update` (blocking changes to immutable identity fields `kind`, `apiVersion` group, `metadata.name`, and `metadata.namespace` when editing from `DetailDrawer`), and `OwnerRef.UID` on `Row` metadata.

### Changed
- **Lazy-Loaded Terminal Bundle (`BottomDock`)**: Dynamically imported `TerminalView` (`@xterm/xterm`) when opening a container shell tab, splitting it out of the main JS chunk to keep initial startup bundle well under Vite's 500 kB limit.
- **Resource Meter & Drawer Layout**: Set the Pod table's CPU/Memory usage text-to-bar gap to `8px` (`--space-2`) starting from the exact longest usage text width, extended the vertical request marker line 2px above and 2px below all usage bars (`z-index: 2`, removing track clipping), and initialized `DetailDrawer` width to 40% of the app viewport width when opened.
- **Unready Workload Highlighting**: Highlighted unready workload rows in yellow (`--warn`) while leaving `0/0` scaled-to-zero and `Complete`/`Completed`/`Succeeded` workloads normal and highlighting unready error states (`Name` + `Ready` for `Pod`, `Deployment`, `StatefulSet`; `Name` for `ReplicaSet`, `DaemonSet`; `Name` + `Completions` for `Job`; `Ready` only inside `DetailDrawer`'s `Pods` table).
- **CI Workflow Actions & Release Flag (`.github/workflows/ci.yml`)**: Updated GitHub Actions steps (`checkout@v6`, `setup-go@v6`, `setup-node@v6`, `upload-artifact@v7`, `download-artifact@v8`) to Node 24 majors and passed `--latest` on `gh release create`.

### Fixed
- **DetailDrawer Reload Guard**: Scoped `DetailDrawer`'s selection reload `$effect` with an explicit context/resource/namespace/name key and `untrack` so unrelated UI state toggles (such as toggling the bottom dock) cannot clear the loaded object or flash `Loading…`.

## [0.1.3-beta] - 2026-11-19

### Added
- **GitHub Actions CI & Auto-Release (`.github/workflows/ci.yml`)**: Added automated validation/testing on PRs and pushes to `master`, plus 4-target release builds (`linux/amd64`, `windows/amd64`, `darwin/arm64`, `darwin/amd64`) with combined `SHA256SUMS` when `build/config.yml` `info.version` is bumped.
- **Dock Tab Icons, Status Colors & Close All**:
  - Replaced text badges in `BottomDock` tabs with Logs and Shell SVG icons colored by live session status (`green` = connected, `yellow` = connecting/reconnecting, `red` = disconnected via `connected` events on `logs.Event` and `exec.Event`).
  - Made the dock tab strip horizontally scrollable when many tabs are open (with hidden scrollbar and overflow mouse-wheel scrolling) and added a **Close all** button (with confirmation prompt) next to the hide dock button.
- **Minimalist Scrollbars**: Added lightweight `6px` custom scrollbars across the app while hiding the scrollbar on the `BottomDock` tab strip.
- **ConfigMap & Secret Data Viewer in DetailDrawer**:
  - Added a borderless paired `Data` table for `ConfigMap` and `Secret` resources showing keys and values side by side, truncating values longer than 100 characters with expand/collapse toggles and placing copy buttons before each value.
  - Masked `Secret` keys after the first 4 characters (`****` for `<= 4` chars) with an eye toggle before each key, and masked `Secret` values with a constant 20-asterisk placeholder alongside eye and copy buttons before each value (base64-decoding UTF-8 text on reveal and copy, with base64 fallback for binary data).
- **Log Text Selection & High-Contrast Timestamps**: Made container log text in `LogViewer` selectable while coloring Kubernetes timestamps in high contrast (`--accent`) and keeping timestamps non-selectable.
- **Service Cluster FQDN in DetailDrawer**: Added a copyable `Cluster FQDN` (`<name>.<namespace>`) on the last line of `Service` `Specification & status` and removed the copy button from `Ports`.
- **Unified Build, Test & Checksum Script (`scripts/build.sh`)**: Added `scripts/build.sh` to validate and test frontend/Go, build OS/arch-named release binaries into `bin/dist/`, and generate/verify deterministic `SHA256SUMS` (supporting `--test-only`, `--skip-tests`, and `--checksums-only` modes for GitHub Actions).

### Changed
- **DetailDrawer Spec Mapper Extraction**: Extracted pure resource spec, selector, and container summary builders out of `DetailDrawer.svelte` into `frontend/src/lib/specs.ts`.
- **LogViewer Windowing & Pre-Split Ingest**: Pre-parsed Kubernetes log timestamps and search text once on ingest and windowed `.out` rendering so 10,000-line buffers avoid per-batch array cloning and DOM bloat.
- **Pod Meter Text Width Caching**: Added bounded memoization (with font-load invalidation) to `measureTextWidth` in `frontend/src/lib/format.ts`.
- **Backend & Store Cleanup**: Removed unused Wails bindings (`GetYAML`, `ObjectEvents`, `Service.KubeconfigFiles`), unused `ShowWide` setting, unused `stream.Registry.Len`, dead branches in `internal/kube/neat.go`, and `internal/kube/aliases.go`; limited `GetDetail` workload metrics lookups to workload kinds and deduplicated `AppState.reload()` via `reloadContexts()`.
- **Unit Test Coverage**: Added unit tests for `internal/exec`, `internal/stream` (`Stop`/`StopAll`), and frontend `format.ts`/`specs.ts` (`npm test` via `node --test`).
- **Bottom-Right Dock Button**: Moved the **Dock** toggle button out of `TopBar` into a bottom bar anchored at the bottom-right corner.
- **YAML Edit Escape Behavior**: Pressing `Esc` (or clicking `Cancel`) while editing YAML in `DetailDrawer` now discards edits without a confirmation prompt and exits edit mode without closing the drawer.

### Fixed
- **LogViewer Empty Output Regression**: Combined `LogViewer` filtering and windowing into a single `view` `$derived.by` tracking `version` so in-place log ingestion always updates the UI when no search filter is active.
- **Dock Tab Error Status**: Marked dock tabs as `disconnected` when `Kube.PodContainers` fails in `LogViewer` or `TerminalView` instead of leaving them stuck on `connecting`.
- **Stream Registry Reused-ID Race**: Returned a per-stream `done` callback from `stream.Registry.Start` so an exiting stream cannot remove or cancel a newer stream registered under the same ID.
- **Kubeconfig Reset Cache Race**: Added a generation counter in `kube.Manager` so in-flight `For` client builds cannot cache stale clients after `Reset` or `SetKubeconfigPaths`.
- **Manifest `List` Validation**: Applied `apiVersion`, `kind`, and `metadata.name` checks to every item expanded from `kind: List` manifests in `DecodeManifest`.
- **Settings Save Ordering**: Serialized and coalesced `Settings.Save` calls in `AppState` so rapid UI preference changes cannot overwrite newer state out of order.
- **Table Watch `resourceVersion` Handling**: Preserved the outer `Table` list `resourceVersion` in `parseTable` (falling back to row `resourceVersion` only on watch events where outer RV is empty) to avoid immediate `410 Gone` relists.
- **Port Forward Dual-Protocol Crash**: Keyed `PortForwardModal` port options by port/protocol/name/index (preventing Svelte duplicate-key crashes on Services like `kube-dns` exposing `53/TCP` and `53/UDP`) and guarded port lookups against stale async updates.
- **DetailDrawer Stale Load & Scale Reset**: Added request sequencing in `DetailDrawer.load()` so slow responses from a previously selected resource cannot overwrite the current selection, and reset inline `replicas` scale input when switching resources.
- **Log Streaming Lines Over 1 MiB**: Replaced `bufio.Scanner` with a chunked `bufio.Reader` loop in `logs.Pump` so log lines exceeding 1 MiB split cleanly instead of aborting the stream with `bufio.ErrTooLong`.
- **Dock Logs & Shell Reconnect Loop**: Kept dock tab connection statuses in a separate `dockStatuses` map outside `app.dock` tab objects so status transitions no longer replace keyed tab items and restart `LogViewer` / `TerminalView` streams.
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
