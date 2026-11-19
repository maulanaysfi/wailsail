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
import { kindKey, resourceKey } from "./resources";

export interface Selection {
  type: ResourceType;
  namespace: string;
  name: string;
}

export type DockStatus = "connecting" | "connected" | "disconnected";

export type DockTab =
  | { id: string; kind: "logs"; title: string; context: string; namespace: string; pod: string; container?: string }
  | { id: string; kind: "shell"; title: string; context: string; namespace: string; pod: string; container?: string };

export interface Toast {
  id: number;
  tone: "info" | "error" | "ok";
  text: string;
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
  showWide: true,
  pinned: [],
  columnOrders: {},
  columnWidths: {},
};

export const scaleOptions = [50, 75, 100, 125, 150] as const;
export const APP_VERSION = "v0.1.2 (beta)";

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
  cancelYamlEdit: (() => void) | null = null;
  now = $state(Date.now());

  #toastSeq = 0;
  #lastReplyAt = 0;
  #unansweredSince = 0;
  #probing = false;
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
      this.connectionError = `Could not load kubeconfig: ${errorMessage(e)}`;
      return;
    }
    const names = this.contexts.map((c) => c.name);
    let pick = this.settings.lastContext;
    if (!names.includes(pick)) pick = this.contexts.find((c) => c.current)?.name ?? names[0] ?? "";
    if (pick) await this.selectContext(pick);
  }

  async reload() {
    const prevCtx = this.context;
    const prevResKey = this.current ? resourceKey(this.current) : "";
    const prevSelection = this.selection;
    await Kube.Reload();
    this.context = "";
    this.contexts = (await Kube.Contexts().catch(() => [])) ?? [];
    const names = this.contexts.map((c) => c.name);
    const pick = names.includes(prevCtx) ? prevCtx : (this.contexts.find((c) => c.current)?.name ?? names[0] ?? "");
    if (pick) {
      await this.selectContext(pick, {
        preserveResourceKey: prevResKey,
        preserveSelection: pick === prevCtx ? prevSelection : null,
      });
    }
  }

  async selectContext(
    name: string,
    opts?: { preserveResourceKey?: string; preserveSelection?: Selection | null },
  ) {
    const wantResKey = opts?.preserveResourceKey ?? (this.current ? resourceKey(this.current) : "");
    const wantSelection = opts?.preserveSelection !== undefined ? opts.preserveSelection : (this.context === name ? this.selection : null);

    this.context = name;
    this.selection = null;
    this.current = null;
    this.resources = [];
    this.namespaces = [];
    this.serverVersion = "";
    this.connectionError = "";
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
      const [version, resources, namespaces] = await Promise.all([
        Kube.ServerVersion(name),
        Kube.APIResources(name),
        Kube.Namespaces(name),
      ]);
      if (this.context !== name) return; // switched meanwhile
      this.#lastReplyAt = Date.now();
      this.#unansweredSince = 0;
      this.serverVersion = version;
      this.resources = resources ?? [];
      this.namespaces = namespaces ?? [];
      const preserved = wantResKey ? this.resources.find((r) => resourceKey(r) === wantResKey) : undefined;
      const pods = this.resources.find((r) => kindKey(r) === "/Pod");
      this.current = preserved ?? pods ?? this.resources[0] ?? null;
      if (wantSelection && this.current && resourceKey(wantSelection.type) === resourceKey(this.current)) {
        this.selection = { ...wantSelection, type: this.current };
      }
    } catch (e) {
      if (this.context === name) this.connectionError = errorMessage(e);
    } finally {
      if (this.context === name) {
        this.resourcesLoading = false;
        this.#refreshClusterStatus();
      }
    }
  }

  #probeCluster() {
    const ctx = this.context;
    if (!ctx || this.#probing || this.resourcesLoading) return;
    this.#probing = true;
    if (this.#unansweredSince === 0) this.#unansweredSince = Date.now();
    Kube.ServerVersion(ctx)
      .then((v) => {
        if (this.context !== ctx) return;
        this.#lastReplyAt = Date.now();
        this.#unansweredSince = 0;
        if (v) this.serverVersion = v;
        this.connectionError = "";
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
        return;
      }
      if (elapsed >= 5_000) {
        this.clusterStatus = "slow";
        return;
      }
    }
    this.clusterStatus = this.#lastReplyAt > 0 ? "connected" : "slow";
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
  }

  jumpToOwner(owner: { apiVersion?: string; kind: string; name: string } | undefined | null, namespace: string) {
    if (!owner?.kind || !owner?.name) return;
    const parts = (owner.apiVersion ?? "").split("/");
    const group = parts.length > 1 ? parts[0] : "";
    const target =
      this.resources.find((r) => r.kind === owner.kind && (!owner.apiVersion || r.group === group)) ??
      this.resources.find((r) => r.kind === owner.kind);
    if (!target) {
      this.toast(`Owner resource kind ${owner.kind} not found`, "error");
      return;
    }
    const ns = target.namespaced ? namespace : "";
    if (target.namespaced && this.namespace !== "" && this.namespace !== ns) {
      this.namespace = ns;
    }
    this.current = target;
    this.selection = { type: target, namespace: ns, name: owner.name };
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
    this.connectionError = "";
    try {
      this.contexts = (await Kube.Contexts()) ?? [];
    } catch (e) {
      this.contexts = [];
      this.connectionError = `Could not load kubeconfig: ${errorMessage(e)}`;
      return;
    }
    const names = this.contexts.map((c) => c.name);
    const pick = names.includes(prev) ? prev : (this.contexts.find((c) => c.current)?.name ?? names[0] ?? "");
    if (pick) {
      await this.selectContext(pick, {
        preserveResourceKey: prevResKey,
        preserveSelection: pick === prev ? prevSelection : null,
      });
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

  openDock(tab: Omit<DockTab, "id">) {
    const existing = this.dock.find(
      (t) =>
        t.kind === tab.kind &&
        t.context === tab.context &&
        t.namespace === tab.namespace &&
        t.pod === tab.pod &&
        (!tab.container || !t.container || t.container === tab.container),
    );
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

  toast(text: string, tone: Toast["tone"] = "info") {
    const id = ++this.#toastSeq;
    this.toasts = [...this.toasts, { id, tone, text }];
    setTimeout(() => this.dismissToast(id), tone === "error" ? 8000 : 3500);
  }

  dismissToast(id: number) {
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
