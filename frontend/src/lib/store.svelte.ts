import { Window } from "@wailsio/runtime";
import {
  Kube,
  Settings,
  errorMessage,
  type ContextInfo,
  type ResourceRef,
  type ResourceType,
  type SettingsData,
} from "./api";
import { toastTimeoutMs, type ToastTone } from "./format";
import { pushNavEntry, type NavEntry, type NavState } from "./nav";
import { kindKey, resourceKey } from "./resources";
import {
  closePageTabState,
  navigatePageTab,
  openOrFocusTab,
  selectionKey,
  type PageTab,
  type Selection,
} from "./tabs";

export type { PageTab, Selection } from "./tabs";

export type DockStatus = "connecting" | "connected" | "disconnected";

export type DockTab =
  | {
      id: string;
      kind: "logs";
      title: string;
      context: string;
      namespace: string;
      pod: string;
      container?: string;
      workload?: { kind: string; uid: string; name: string };
    }
  | { id: string; kind: "shell"; title: string; context: string; namespace: string; pod: string; container?: string };

type DistributiveOmit<T, K extends keyof any> = T extends any ? Omit<T, K> : never;
export type DockTabInput = DistributiveOmit<DockTab, "id">;

export interface Toast {
  id: number;
  key?: string;
  tone: ToastTone;
  text: string;
  pinned?: boolean;
  retryable?: boolean;
}

interface ConfirmRequest {
  title: string;
  message: string;
  danger: boolean;
  resolve: (ok: boolean) => void;
}

const defaultSettings: SettingsData = {
  theme: "system",
  scale: 100,
  kubeconfigPaths: [],
  lastContext: "",
  lastNamespace: {},
  logTailLines: 500,
  logTimestamps: false,
  logTimezone: "local",
  pinned: [],
  columnOrders: {},
  columnWidths: {},
};

export const scaleOptions = [50, 75, 100, 125, 150] as const;
export const APP_VERSION = "v0.2.1 (beta)";

class AppState {
  contexts = $state<ContextInfo[]>([]);
  context = $state("");
  serverVersion = $state("");
  connectionError = $state("");
  clusterStatus = $state<"connected" | "slow" | "disconnected">("disconnected");
  namespaces = $state<string[]>([]);
  namespace = $state(""); // "" means all namespaces
  resources = $state<ResourceType[]>([]);
  resourcesLoading = $state(false);
  current = $state<ResourceType | null>(null);
  selection = $state<Selection | null>(null);
  pageTabs = $state<PageTab[]>([]);
  activePageTab = $state<string | null>(null);
  dock = $state<DockTab[]>([]);
  dockStatuses = $state<Record<string, DockStatus>>({});
  activeDock = $state("");
  dockOpen = $state(false);
  settings = $state<SettingsData>({ ...defaultSettings });
  toasts = $state<Toast[]>([]);
  confirmRequest = $state<ConfirmRequest | null>(null);
  showForwards = $state(false);
  showApply = $state(false);
  showSettings = $state(false);
  defaultKubeconfigPath = $state("");
  forwardTarget = $state<Selection | null>(null);
  scaleTarget = $state<{ sel: Selection; replicas: number } | null>(null);
  editOnOpen = $state(false);
  cancelYamlEdit: (() => void) | null = null;
  reconnectNonce = $state(0);
  now = $state(Date.now());
  #nav = $state<NavState>({ entries: [], index: -1 });
  #pageTabSeq = 0;

  get activeTab(): PageTab | null {
    return this.activePageTab ? (this.pageTabs.find((t) => t.id === this.activePageTab) ?? null) : null;
  }

  get canGoBack(): boolean {
    const nav = this.activeTab?.nav ?? this.#nav;
    return nav.index > 0;
  }

  get canGoForward(): boolean {
    const nav = this.activeTab?.nav ?? this.#nav;
    return nav.index >= 0 && nav.index < nav.entries.length - 1;
  }

  #toastSeq = 0;
  #toastTimers = new Map<number, { handle: ReturnType<typeof setTimeout> | null; remainingMs: number; startedAt: number }>();
  #connToastDismissedText = "";
  #lastReplyAt = 0;
  #unansweredSince = 0;
  #probing = false;
  #hydrating = false;
  #ticks = 0;
  #saveChain = Promise.resolve();
  #savePending = false;

  async init() {
    setInterval(() => {
      this.now = Date.now();
      this.#ticks++;
      if (this.#ticks % 5 === 0) this.#probeCluster();
      this.#refreshClusterStatus();
    }, 1000);
    try {
      this.settings = { ...defaultSettings, ...((await Settings.Get()) ?? {}) };
    } catch (e) {
      this.toast(`Could not read settings: ${errorMessage(e)}`, "error");
    }
    this.defaultKubeconfigPath = (await Settings.DefaultKubeconfig().catch(() => "")) ?? "";
    this.applyTheme();
    this.applyScale();
    await this.loadContexts();
  }

  async loadContexts() {
    try {
      this.contexts = (await Kube.Contexts()) ?? [];
    } catch (e) {
      this.contexts = [];
      this.#setConnectionError(`Could not load kubeconfig: ${errorMessage(e)}`);
      return;
    }
    const names = this.contexts.map((c) => c.name);
    let pick = this.settings.lastContext;
    if (!names.includes(pick)) pick = this.contexts.find((c) => c.current)?.name ?? names[0] ?? "";
    if (pick) await this.selectContext(pick);
  }

  async reload() {
    await Kube.Reload();
    await this.reloadContexts();
  }

  async selectContext(
    name: string,
    opts?: { preserveResourceKey?: string; preserveSelection?: Selection | null; preserveTabs?: boolean },
  ) {
    const wantResKey = opts?.preserveResourceKey ?? (this.current ? resourceKey(this.current) : "");
    const wantSelection = opts?.preserveSelection !== undefined ? opts.preserveSelection : (this.context === name ? this.selection : null);

    const isSwitch = this.context !== name;
    this.context = name;
    this.selection = null;
    this.current = null;
    this.resources = [];
    this.namespaces = [];
    this.serverVersion = "";
    if (isSwitch && !opts?.preserveTabs) {
      this.#nav = { entries: [], index: -1 };
      this.pageTabs = [];
      this.activePageTab = null;
    }
    this.#connToastDismissedText = "";
    this.#setConnectionError("");
    this.saveSettings({ lastContext: name });

    const saved = this.settings.lastNamespace?.[name];
    if (saved !== undefined) {
      this.namespace = saved;
    } else {
      this.namespace = (await Kube.DefaultNamespace(name).catch(() => "")) ?? "";
    }

    this.resourcesLoading = true;
    this.#unansweredSince = Date.now();
    this.#lastReplyAt = 0;
    this.#refreshClusterStatus();
    try {
      await this.#hydrateContext(name, wantResKey, wantSelection);
    } catch (e) {
      if (this.context === name) {
        this.#setConnectionError(`Could not connect to ${name || "kubeconfig"}: ${errorMessage(e)}`);
      }
    } finally {
      if (this.context === name) {
        this.resourcesLoading = false;
        this.#refreshClusterStatus();
      }
    }
  }

  async #hydrateContext(name: string, wantResKey: string, wantSelection: Selection | null) {
    if (this.#hydrating) return;
    this.#hydrating = true;
    try {
      if (!this.namespace && !(this.settings.lastNamespace && name in this.settings.lastNamespace)) {
        const defNs = (await Kube.DefaultNamespace(name).catch(() => "")) ?? "";
        if (this.context !== name) return;
        if (defNs) this.namespace = defNs;
      }
      const [version, resources, namespaces] = await Promise.all([
        Kube.ServerVersion(name),
        Kube.APIResources(name),
        Kube.Namespaces(name),
      ]);
      if (this.context !== name) return; // switched meanwhile
      this.#lastReplyAt = Date.now();
      this.#unansweredSince = 0;
      this.#setConnectionError("");
      this.serverVersion = version;
      this.resources = resources ?? [];
      this.namespaces = namespaces ?? [];
      const preserved = wantResKey ? this.resources.find((r) => resourceKey(r) === wantResKey) : undefined;
      const pods = this.resources.find((r) => kindKey(r) === "/Pod");
      this.current = preserved ?? pods ?? this.resources[0] ?? null;
      if (wantSelection && this.current && resourceKey(wantSelection.type) === resourceKey(this.current)) {
        this.selection = { ...wantSelection, type: this.current };
      }
      if (this.current && this.#nav.entries.length === 0) {
        this.#recordNav();
      }
    } finally {
      this.#hydrating = false;
    }
  }

  #probeCluster() {
    const ctx = this.context;
    if (!ctx || this.#probing || this.resourcesLoading || this.#hydrating) return;
    const wasOffline = this.clusterStatus === "disconnected" || !!this.connectionError;
    const needsHydration = this.resources.length === 0 || !this.current;
    this.#probing = true;
    if (this.#unansweredSince === 0) this.#unansweredSince = Date.now();
    Kube.ServerVersion(ctx)
      .then((v) => {
        if (this.context !== ctx) return;
        this.#lastReplyAt = Date.now();
        this.#unansweredSince = 0;
        if (v) this.serverVersion = v;
        this.#setConnectionError("");
        if (wasOffline) this.toast(`Connected to ${ctx}`, "ok");
        if (needsHydration) {
          const wantResKey = this.current ? resourceKey(this.current) : "";
          void this.#hydrateContext(ctx, wantResKey, this.selection)
            .catch(() => {})
            .finally(() => this.#refreshClusterStatus());
          return;
        }
        if (wasOffline) {
          this.reconnectNonce++;
          void Promise.all([Kube.APIResources(ctx), Kube.Namespaces(ctx)])
            .then(([resources, namespaces]) => {
              if (this.context !== ctx) return;
              if (resources?.length) this.resources = resources;
              if (namespaces) this.namespaces = namespaces;
            })
            .catch(() => {});
        }
      })
      .catch(() => {
        // Keep #unansweredSince running so 5s -> yellow and 30s -> red transitions occur.
      })
      .finally(() => {
        this.#probing = false;
        this.#refreshClusterStatus();
      });
  }

  #refreshClusterStatus() {
    if (!this.context || this.connectionError) {
      this.clusterStatus = "disconnected";
      return;
    }
    if (this.#unansweredSince > 0) {
      const elapsed = Date.now() - this.#unansweredSince;
      if (elapsed >= 30_000) {
        this.clusterStatus = "disconnected";
        this.#syncConnToast(`Lost connection to ${this.context} (retrying…)`);
        return;
      }
      if (elapsed >= 5_000) {
        this.clusterStatus = "slow";
        return;
      }
    }
    this.clusterStatus = this.#lastReplyAt > 0 ? "connected" : "slow";
  }

  #setConnectionError(msg: string) {
    this.connectionError = msg;
    this.#syncConnToast(msg);
  }

  #syncConnToast(msg: string) {
    if (!msg) {
      this.#connToastDismissedText = "";
      const existing = this.toasts.find((t) => t.key === "cluster-conn");
      if (existing) this.dismissToast(existing.id);
      return;
    }
    if (this.#connToastDismissedText === msg) return;
    this.toast(msg, "error", { key: "cluster-conn", pinned: true, retryable: !!this.context });
  }

  #recordNav() {
    if (!this.current) return;
    const entry: NavEntry = {
      resourceKey: resourceKey(this.current),
      selection: this.selection
        ? {
            resourceKey: resourceKey(this.selection.type),
            namespace: this.selection.namespace,
            name: this.selection.name,
          }
        : null,
    };
    this.#nav = pushNavEntry(this.#nav, entry);
  }

  #applyNavEntry(entry: NavEntry) {
    const res = this.resources.find((r) => resourceKey(r) === entry.resourceKey);
    if (!res) return;
    this.current = res;
    if (!entry.selection) {
      this.selection = null;
      return;
    }
    const selType = this.resources.find((r) => resourceKey(r) === entry.selection!.resourceKey) ?? res;
    if (selType.namespaced && this.namespace !== "" && this.namespace !== entry.selection.namespace) {
      this.namespace = entry.selection.namespace;
    }
    this.selection = { type: selType, namespace: entry.selection.namespace, name: entry.selection.name };
  }

  #resolveTabSelection(entry: NavEntry): Selection | null {
    if (!entry.selection) return null;
    const selType = this.resources.find((r) => resourceKey(r) === entry.selection!.resourceKey);
    if (!selType) return null;
    if (selType.namespaced && this.namespace !== "" && this.namespace !== entry.selection.namespace) {
      this.namespace = entry.selection.namespace;
    }
    return { type: selType, namespace: entry.selection.namespace, name: entry.selection.name };
  }

  #stepActiveTab(delta: -1 | 1) {
    const tab = this.activeTab;
    if (!tab) return;
    const nextIdx = tab.nav.index + delta;
    if (nextIdx < 0 || nextIdx >= tab.nav.entries.length) return;
    const nextSel = this.#resolveTabSelection(tab.nav.entries[nextIdx]) ?? tab.selection;
    this.pageTabs = this.pageTabs.map((t) =>
      t.id === tab.id ? { ...t, selection: nextSel, nav: { ...t.nav, index: nextIdx } } : t,
    );
  }

  goBack() {
    if (!this.canGoBack) return;
    if (this.activeTab) {
      this.#stepActiveTab(-1);
      return;
    }
    const nextIdx = this.#nav.index - 1;
    this.#nav = { ...this.#nav, index: nextIdx };
    this.#applyNavEntry(this.#nav.entries[nextIdx]);
  }

  goForward() {
    if (!this.canGoForward) return;
    if (this.activeTab) {
      this.#stepActiveTab(1);
      return;
    }
    const nextIdx = this.#nav.index + 1;
    this.#nav = { ...this.#nav, index: nextIdx };
    this.#applyNavEntry(this.#nav.entries[nextIdx]);
  }

  selectNamespace(ns: string) {
    this.namespace = ns;
    this.selection = null;
    const map = { ...(this.settings.lastNamespace ?? {}), [this.context]: ns };
    this.saveSettings({ lastNamespace: map });
  }

  selectResource(r: ResourceType) {
    this.current = r;
    this.selection = null;
    this.activePageTab = null;
    this.#recordNav();
  }

  selectObject(sel: Selection) {
    this.selection = sel;
    this.#recordNav();
  }

  closeSelection() {
    this.selection = null;
  }

  openPageTab(sel: Selection) {
    const next = openOrFocusTab(this.pageTabs, sel, () => `page-${++this.#pageTabSeq}`);
    this.pageTabs = next.tabs;
    this.activePageTab = next.activeId;
  }

  activateMainTab() {
    this.activePageTab = null;
  }

  activatePageTab(id: string) {
    if (this.pageTabs.some((t) => t.id === id)) {
      this.activePageTab = id;
    }
  }

  closePageTab(id: string) {
    const next = closePageTabState(this.pageTabs, this.activePageTab, id);
    this.pageTabs = next.tabs;
    this.activePageTab = next.activeId;
  }

  jumpToResource(ref: { apiVersion?: string; kind: string; name: string } | undefined | null, namespace: string) {
    if (!ref?.kind || !ref?.name) return;
    const parts = (ref.apiVersion ?? "").split("/");
    const group = parts.length > 1 ? parts[0] : "";
    const target =
      this.resources.find((r) => r.kind === ref.kind && (!ref.apiVersion || r.group === group)) ??
      this.resources.find((r) => r.kind === ref.kind);
    if (!target) {
      this.toast(`Resource kind ${ref.kind} not found`, "error");
      return;
    }
    const ns = target.namespaced ? namespace : "";
    if (target.namespaced && this.namespace !== "" && this.namespace !== ns) {
      this.namespace = ns;
    }
    const nextSel: Selection = { type: target, namespace: ns, name: ref.name };
    if (this.activeTab) {
      this.pageTabs = navigatePageTab(this.pageTabs, this.activeTab.id, nextSel);
      return;
    }
    this.current = target;
    this.selection = nextSel;
    this.#recordNav();
  }

  jumpToOwner(owner: { apiVersion?: string; kind: string; name: string } | undefined | null, namespace: string) {
    this.jumpToResource(owner, namespace);
  }

  isPinned(r: ResourceType) {
    return (this.settings.pinned ?? []).includes(resourceKey(r));
  }

  togglePin(r: ResourceType) {
    const k = resourceKey(r);
    const pinned = this.settings.pinned ?? [];
    this.saveSettings({ pinned: pinned.includes(k) ? pinned.filter((p) => p !== k) : [...pinned, k] });
  }

  saveSettings(patch: Partial<SettingsData>) {
    this.settings = { ...this.settings, ...patch };
    if ("theme" in patch) this.applyTheme();
    if ("scale" in patch) this.applyScale();
    if (this.#savePending) return;
    this.#savePending = true;
    this.#saveChain = this.#saveChain.then(async () => {
      this.#savePending = false;
      try {
        await Settings.Save({ ...this.settings });
      } catch (e) {
        this.toast(`Could not save settings: ${errorMessage(e)}`, "error");
      }
    });
  }

  applyScale() {
    const factor = (this.settings.scale || 100) / 100;
    if (factor < 1) {
      // Wails v3 clamps native Window.SetZoom(< 1.0) to 1.0 on all desktop targets.
      // Reset native webview zoom to 1.0 and apply sub-100% scaling via CSS zoom.
      document.documentElement.style.zoom = String(factor);
      Window.SetZoom(1).catch(() => {});
      return;
    }
    Window.SetZoom(factor)
      .then(() => (document.documentElement.style.zoom = ""))
      .catch(() => (document.documentElement.style.zoom = String(factor)));
  }

  async setKubeconfigPaths(paths: string[]) {
    this.saveSettings({ kubeconfigPaths: paths });
    try {
      await Kube.SetKubeconfigPaths(paths);
    } catch (e) {
      this.toast(`Could not apply kubeconfig paths: ${errorMessage(e)}`, "error");
    }
    await this.reloadContexts();
  }

  addKubeconfigPath(path: string) {
    const p = path.trim();
    const paths = this.settings.kubeconfigPaths ?? [];
    if (!p || paths.includes(p)) return;
    return this.setKubeconfigPaths([...paths, p]);
  }

  removeKubeconfigPath(path: string) {
    return this.setKubeconfigPaths((this.settings.kubeconfigPaths ?? []).filter((p) => p !== path));
  }

  resetKubeconfigPaths() {
    return this.setKubeconfigPaths(this.defaultKubeconfigPath ? [this.defaultKubeconfigPath] : []);
  }

  /** Re-read contexts after the kubeconfig sources changed. */
  async reloadContexts() {
    const prev = this.context;
    const prevResKey = this.current ? resourceKey(this.current) : "";
    const prevSelection = this.selection;
    this.context = "";
    this.current = null;
    this.selection = null;
    this.resources = [];
    this.namespaces = [];
    this.serverVersion = "";
    this.#connToastDismissedText = "";
    this.#setConnectionError("");
    try {
      this.contexts = (await Kube.Contexts()) ?? [];
    } catch (e) {
      this.contexts = [];
      this.#setConnectionError(`Could not load kubeconfig: ${errorMessage(e)}`);
      return;
    }
    const names = this.contexts.map((c) => c.name);
    const pick = names.includes(prev) ? prev : (this.contexts.find((c) => c.current)?.name ?? names[0] ?? "");
    if (pick) {
      await this.selectContext(pick, {
        preserveResourceKey: prevResKey,
        preserveSelection: pick === prev ? prevSelection : null,
        preserveTabs: pick === prev,
      });
    } else {
      this.pageTabs = [];
      this.activePageTab = null;
    }
  }

  applyTheme() {
    const t = this.settings.theme;
    const dark = t === "dark" || (t !== "light" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    document.documentElement.dataset.theme = dark ? "dark" : "light";
  }

  cycleTheme() {
    const order = ["system", "light", "dark"];
    const next = order[(order.indexOf(this.settings.theme) + 1) % order.length];
    this.saveSettings({ theme: next });
  }

  openDock(tab: DockTabInput) {
    const existing = this.dock.find((t) => {
      if (t.kind !== tab.kind || t.context !== tab.context || t.namespace !== tab.namespace) return false;
      if (tab.kind === "logs" && tab.workload) {
        return t.kind === "logs" && t.workload?.uid === tab.workload.uid;
      }
      return (
        !(t.kind === "logs" && t.workload) &&
        t.pod === tab.pod &&
        (!tab.container || !t.container || t.container === tab.container)
      );
    });
    if (existing) {
      this.activeDock = existing.id;
      this.dockOpen = true;
      return;
    }
    const id = `${tab.kind}-${Date.now().toString(36)}`;
    // Keep connection status outside dock tab items so updating status never
    // replaces the keyed item object and restarts LogViewer/TerminalView effects.
    this.dockStatuses[id] = "connecting";
    this.dock = [...this.dock, { ...tab, id } as DockTab];
    this.activeDock = id;
    this.dockOpen = true;
  }

  setDockStatus(id: string, status: DockStatus) {
    if (this.dockStatuses[id] !== status) {
      this.dockStatuses[id] = status;
    }
  }

  closeDock(id: string) {
    const idx = this.dock.findIndex((t) => t.id === id);
    this.dock = this.dock.filter((t) => t.id !== id);
    delete this.dockStatuses[id];
    if (this.activeDock === id) {
      this.activeDock = this.dock[Math.max(0, idx - 1)]?.id ?? "";
    }
    if (this.dock.length === 0) this.dockOpen = false;
  }

  closeAllDock() {
    this.dock = [];
    this.dockStatuses = {};
    this.activeDock = "";
    this.dockOpen = false;
  }

  #scheduleToastTimer(id: number, ms: number) {
    this.#clearToastTimer(id);
    const handle = setTimeout(() => {
      this.#toastTimers.delete(id);
      this.dismissToast(id, false);
    }, ms);
    this.#toastTimers.set(id, { handle, remainingMs: ms, startedAt: Date.now() });
  }

  #clearToastTimer(id: number) {
    const t = this.#toastTimers.get(id);
    if (!t) return;
    if (t.handle) clearTimeout(t.handle);
    this.#toastTimers.delete(id);
  }

  pauseToast(id: number) {
    const t = this.#toastTimers.get(id);
    if (!t || !t.handle) return;
    clearTimeout(t.handle);
    const elapsed = Date.now() - t.startedAt;
    this.#toastTimers.set(id, {
      handle: null,
      remainingMs: Math.max(400, t.remainingMs - elapsed),
      startedAt: Date.now(),
    });
  }

  resumeToast(id: number) {
    const entry = this.#toastTimers.get(id);
    if (!entry) return;
    this.#scheduleToastTimer(id, entry.remainingMs);
  }

  toast(
    text: string,
    tone: ToastTone = "info",
    opts?: { key?: string; pinned?: boolean; retryable?: boolean },
  ) {
    const existing = opts?.key ? this.toasts.find((t) => t.key === opts.key) : undefined;
    if (existing) {
      if (
        existing.text === text &&
        existing.tone === tone &&
        !!existing.pinned === !!opts?.pinned &&
        !!existing.retryable === !!opts?.retryable
      ) {
        return;
      }
      this.#clearToastTimer(existing.id);
      this.toasts = this.toasts.map((t) =>
        t.id === existing.id
          ? { ...t, text, tone, pinned: opts?.pinned, retryable: opts?.retryable }
          : t,
      );
      if (!opts?.pinned) this.#scheduleToastTimer(existing.id, toastTimeoutMs(text, tone));
      return;
    }

    const id = ++this.#toastSeq;
    this.toasts = [
      ...this.toasts,
      { id, key: opts?.key, tone, text, pinned: opts?.pinned, retryable: opts?.retryable },
    ];
    if (!opts?.pinned) {
      this.#scheduleToastTimer(id, toastTimeoutMs(text, tone));
    }
  }

  dismissToast(id: number, byUser = true) {
    this.#clearToastTimer(id);
    const target = this.toasts.find((t) => t.id === id);
    if (byUser && target?.key === "cluster-conn") {
      this.#connToastDismissedText = target.text;
    }
    this.toasts = this.toasts.filter((t) => t.id !== id);
  }

  confirm(title: string, message: string, danger = false): Promise<boolean> {
    return new Promise((resolve) => {
      this.confirmRequest = { title, message, danger, resolve };
    });
  }

  answerConfirm(ok: boolean) {
    this.confirmRequest?.resolve(ok);
    this.confirmRequest = null;
  }

  openEdit(sel: Selection) {
    this.editOnOpen = true;
    if (this.activeTab) {
      this.pageTabs = navigatePageTab(this.pageTabs, this.activeTab.id, sel);
      return;
    }
    this.selectObject({ ...sel });
  }

  async scaleResource(sel: Selection, n: number): Promise<boolean> {
    if (!Number.isInteger(n) || n < 0) {
      this.toast("Replicas must be a non-negative integer", "error");
      return false;
    }
    try {
      await Kube.Scale(this.context, refOf(sel.type), sel.namespace, sel.name, n);
      this.toast(`Scaled ${sel.name} to ${n}`, "ok");
      return true;
    } catch (e) {
      this.toast(errorMessage(e), "error");
      return false;
    }
  }

  async restartResource(sel: Selection): Promise<boolean> {
    if (!(await this.confirm(`Restart ${sel.name}?`, "Pods will be replaced following the rollout strategy."))) {
      return false;
    }
    try {
      await Kube.Restart(this.context, refOf(sel.type), sel.namespace, sel.name);
      this.toast(`Restart of ${sel.name} triggered`, "ok");
      return true;
    } catch (e) {
      this.toast(errorMessage(e), "error");
      return false;
    }
  }

  async deleteResource(sel: Selection): Promise<boolean> {
    const isHelm = sel.type.group === "helm.sh" && sel.type.resource === "releases";
    const where = sel.namespace ? ` in namespace ${sel.namespace}` : "";
    const verb = isHelm ? "Uninstall" : "Delete";
    const pastVerb = isHelm ? "uninstalled" : "deleted";
    if (!(await this.confirm(`${verb} ${sel.type.kind}?`, `${sel.name}${where} will be ${pastVerb}.`, true))) {
      return false;
    }
    try {
      await Kube.Delete(this.context, refOf(sel.type), sel.namespace, sel.name);
      this.toast(`${sel.type.kind} ${sel.name} ${pastVerb}`, "ok");
      const deletedKey = selectionKey(sel);
      if (this.selection && selectionKey(this.selection) === deletedKey) {
        this.selection = null;
      }
      for (const tab of this.pageTabs) {
        if (selectionKey(tab.selection) === deletedKey) {
          this.closePageTab(tab.id);
        }
      }
      return true;
    } catch (e) {
      this.toast(errorMessage(e), "error");
      return false;
    }
  }
}

export const app = new AppState();

export function refOf(r: ResourceType): ResourceRef {
  return { group: r.group, version: r.version, resource: r.resource, kind: r.kind, namespaced: r.namespaced };
}

/** Active CSS zoom multiplier (1 when native webview zoom is used). */
export function cssZoom(): number {
  const z = parseFloat(document.documentElement.style.zoom || "");
  return Number.isFinite(z) && z > 0 ? z : 1;
}
