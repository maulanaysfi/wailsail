<script lang="ts">
  import { onMount, tick } from "svelte";
  import { fade } from "svelte/transition";
  import { cubicInOut } from "svelte/easing";
  import { app, type Toast } from "./lib/store.svelte";
  import TopBar from "./components/TopBar.svelte";
  import Sidebar from "./components/Sidebar.svelte";
  import ResourceTable from "./components/ResourceTable.svelte";
  import DetailDrawer from "./components/DetailDrawer.svelte";
  import BottomDock from "./components/BottomDock.svelte";
  import PortForwardModal from "./components/PortForwardModal.svelte";
  import ApplyModal from "./components/ApplyModal.svelte";
  import SettingsModal from "./components/SettingsModal.svelte";

  let copiedToastId = $state(0);
  let scaleInput: HTMLInputElement | undefined = $state();
  let scaleValue = $state(1);
  let scaleBusy = $state(false);

  $effect(() => {
    const target = app.scaleTarget;
    if (!target) return;
    scaleValue = target.replicas;
    scaleBusy = false;
    tick().then(() => {
      scaleInput?.focus();
      scaleInput?.select();
    });
  });

  async function submitScale() {
    const target = app.scaleTarget;
    if (!target || scaleBusy) return;
    scaleBusy = true;
    const ok = await app.scaleResource(target.sel, scaleValue);
    scaleBusy = false;
    if (ok) app.scaleTarget = null;
  }

  function toastFadeDuration(): number {
    if (typeof window !== "undefined" && window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) {
      return 0;
    }
    return 200;
  }

  async function copyToast(t: Toast) {
    try {
      await navigator.clipboard.writeText(t.text);
      copiedToastId = t.id;
      setTimeout(() => {
        if (copiedToastId === t.id) copiedToastId = 0;
      }, 1200);
    } catch {
      // Clipboard write unavailable or denied.
    }
  }

  onMount(() => {
    app.init();
    const blockPinchWheel = (e: WheelEvent) => {
      if (e.ctrlKey || e.metaKey) e.preventDefault();
    };
    const blockGesture = (e: Event) => e.preventDefault();
    const blockKeyZoom = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && (e.key === "+" || e.key === "-" || e.key === "=" || e.key === "0")) {
        e.preventDefault();
      }
    };
    window.addEventListener("wheel", blockPinchWheel, { passive: false });
    window.addEventListener("gesturestart", blockGesture, { passive: false });
    window.addEventListener("gesturechange", blockGesture, { passive: false });
    window.addEventListener("keydown", blockKeyZoom);
    return () => {
      window.removeEventListener("wheel", blockPinchWheel);
      window.removeEventListener("gesturestart", blockGesture);
      window.removeEventListener("gesturechange", blockGesture);
      window.removeEventListener("keydown", blockKeyZoom);
    };
  });

  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") {
      if (app.confirmRequest) app.answerConfirm(false);
      else if (app.scaleTarget) app.scaleTarget = null;
      else if (app.showSettings) app.showSettings = false;
      else if (app.showApply) app.showApply = false;
      else if (app.showForwards || app.forwardTarget) {
        app.showForwards = false;
        app.forwardTarget = null;
      } else if (app.cancelYamlEdit) {
        app.cancelYamlEdit();
      } else if (app.selection) app.closeSelection();
    }
  }
</script>

<svelte:window onkeydown={onKey} />

<div class="shell">
  <TopBar />

  <div class="workspace">
    <Sidebar />
    <div class="center">
      <div class="upper">
        {#if app.current}
          <ResourceTable />
        {:else if !app.resourcesLoading}
          <p class="placeholder">
            {app.connectionError ? `Disconnected from ${app.context || "cluster"}.` : "Select a context to browse resources."}
          </p>
        {/if}
        {#if app.selection}
          <DetailDrawer sel={app.selection} />
        {/if}
      </div>
      {#if app.dock.length > 0}
        <BottomDock />
      {/if}
    </div>
  </div>

  {#if app.showForwards || app.forwardTarget}
    <PortForwardModal />
  {/if}
  {#if app.showApply}
    <ApplyModal />
  {/if}
  {#if app.showSettings}
    <SettingsModal />
  {/if}

  {#if app.scaleTarget}
    <div class="backdrop" role="presentation" onclick={(e) => e.target === e.currentTarget && (app.scaleTarget = null)}>
      <div class="modal scale-modal" role="dialog" aria-modal="true" aria-labelledby="scale-title">
        <h3 id="scale-title">Scale {app.scaleTarget.sel.type.kind} {app.scaleTarget.sel.name}</h3>
        <label class="scale-field">
          <span>Replicas</span>
          <input
            bind:this={scaleInput}
            type="number"
            min="0"
            bind:value={scaleValue}
            onkeydown={(e) => e.key === "Enter" && submitScale()}
          />
        </label>
        <div class="row">
          <button onclick={() => (app.scaleTarget = null)} disabled={scaleBusy} title="Cancel scaling (Esc)">Cancel</button>
          <button class="primary" onclick={submitScale} disabled={scaleBusy} title={`Set replicas to ${scaleValue ?? 0} (Enter)`}>
            {scaleBusy ? "Scaling…" : "Set"}
          </button>
        </div>
      </div>
    </div>
  {/if}

  {#if app.confirmRequest}
    <div class="backdrop" role="presentation">
      <div class="modal confirm" role="alertdialog" aria-modal="true">
        <h3>{app.confirmRequest.title}</h3>
        <p>{app.confirmRequest.message}</p>
        <div class="row">
          <button onclick={() => app.answerConfirm(false)} title="Cancel action (Esc)">Cancel</button>
          <button
            class={app.confirmRequest.danger ? "danger" : "primary"}
            onclick={() => app.answerConfirm(true)}
            title="Confirm and proceed"
          >
            Confirm
          </button>
        </div>
      </div>
    </div>
  {/if}

  <div class="toasts" aria-live="polite">
    {#each app.toasts as t (t.id)}
      <div
        class={`toast ${t.tone}`}
        role={t.tone === "error" || t.tone === "warn" ? "alert" : "status"}
        transition:fade={{ duration: toastFadeDuration(), easing: cubicInOut }}
        onmouseenter={() => app.pauseToast(t.id)}
        onmouseleave={() => app.resumeToast(t.id)}
        onfocusin={() => app.pauseToast(t.id)}
        onfocusout={() => app.resumeToast(t.id)}
      >
        <span class="toast-icon" aria-hidden="true">
          {#if t.tone === "ok"}
            <svg viewBox="0 0 24 24">
              <circle cx="12" cy="12" r="10" fill="currentColor" />
              <polyline points="8 12.5 10.8 15.3 16.5 9.2" fill="none" stroke="var(--panel)" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          {:else if t.tone === "warn"}
            <svg viewBox="0 0 24 24">
              <path d="M12 2.5L1.5 20.5h21L12 2.5z" fill="currentColor" />
              <line x1="12" y1="9" x2="12" y2="14" stroke="var(--panel)" stroke-width="2.2" stroke-linecap="round" />
              <circle cx="12" cy="17.2" r="1.15" fill="var(--panel)" />
            </svg>
          {:else if t.tone === "error"}
            <svg viewBox="0 0 24 24">
              <circle cx="12" cy="12" r="10" fill="currentColor" />
              <line x1="8.8" y1="8.8" x2="15.2" y2="15.2" stroke="var(--panel)" stroke-width="2.2" stroke-linecap="round" />
              <line x1="15.2" y1="8.8" x2="8.8" y2="15.2" stroke="var(--panel)" stroke-width="2.2" stroke-linecap="round" />
            </svg>
          {:else}
            <svg viewBox="0 0 24 24">
              <circle cx="12" cy="12" r="10" fill="currentColor" />
              <line x1="12" y1="16" x2="12" y2="11" stroke="var(--panel)" stroke-width="2.2" stroke-linecap="round" />
              <circle cx="12" cy="7.5" r="1.2" fill="var(--panel)" />
            </svg>
          {/if}
        </span>
        <div class="toast-body">
          <div class="toast-msg">{t.text}</div>
          {#if t.tone === "warn" || t.tone === "error" || (t.retryable && app.context)}
            <div class="toast-actions">
              {#if t.retryable && app.context}
                <button class="toast-link" onclick={() => app.selectContext(app.context)} title="Retry connecting to cluster">
                  Retry
                </button>
              {/if}
              {#if t.tone === "warn" || t.tone === "error"}
                <button class="toast-link" onclick={() => copyToast(t)} title="Copy notification message">
                  {copiedToastId === t.id ? "Copied" : "Copy"}
                </button>
              {/if}
            </div>
          {/if}
        </div>
        <button class="toast-close" onclick={() => app.dismissToast(t.id)} title="Dismiss notification" aria-label="Dismiss notification">
          ✕
        </button>
      </div>
    {/each}
  </div>

  <footer class="bottom-bar">
    {#if app.context}
      <span class="cluster-pill">
        <span class={`conn-dot ${app.clusterStatus}`}></span>
        {#if app.serverVersion}
          <span class="pill">{app.serverVersion}</span>
        {/if}
      </span>
    {/if}
    <span class="grow"></span>
    <button
      class="ghost mini"
      onclick={() => (app.showApply = true)}
      disabled={!app.context}
      title="Apply YAML or JSON manifest to current cluster"
    >
      Apply
    </button>
    <button class="ghost mini" onclick={() => (app.showForwards = true)} title="Manage active local port forwards">
      Forwards
    </button>
    <button
      class="ghost mini"
      onclick={() => (app.dockOpen = !app.dockOpen)}
      disabled={app.dock.length === 0}
      title={app.dockOpen ? "Minimize bottom dock" : "Show bottom dock"}
    >
      Dock{app.dock.length ? ` (${app.dock.length})` : ""}
    </button>
  </footer>
</div>

<style>
  .shell {
    display: flex;
    flex-direction: column;
    height: 100%;
  }
  .workspace {
    display: grid;
    grid-template-columns: auto 1fr;
    flex: 1;
    min-height: 0;
  }
  .center {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }
  .upper {
    display: grid;
    grid-template-columns: 1fr auto;
    flex: 1;
    min-width: 0;
    min-height: 0;
  }
  .placeholder {
    margin: auto;
    color: var(--muted);
  }
  .confirm,
  .scale-modal {
    min-width: 360px;
  }
  .scale-field {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin-top: var(--space-3);
  }
  .scale-field input {
    width: 112px;
  }
  .confirm .row,
  .scale-modal .row {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    margin-top: var(--space-3);
  }
  .toasts {
    position: fixed;
    right: var(--space-3);
    bottom: calc(var(--control-h) + var(--space-3));
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: var(--space-2);
    z-index: 30;
    max-width: min(440px, calc(100vw - var(--space-4)));
    pointer-events: none;
  }
  .toast {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
    max-width: 100%;
    min-width: 0;
    padding: var(--space-2) var(--space-2) var(--space-2) var(--space-3);
    border-radius: var(--radius);
    background: var(--panel);
    border: 1px solid var(--border);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.35);
    pointer-events: auto;
  }
  .toast.info {
    border-color: var(--accent);
  }
  .toast.ok {
    border-color: var(--ok);
  }
  .toast.warn {
    border-color: var(--warn);
  }
  .toast.error {
    border-color: var(--bad);
  }
  .toast-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    margin-top: 1px;
  }
  .toast-icon svg {
    width: 16px;
    height: 16px;
    display: block;
  }
  .toast.info .toast-icon {
    color: var(--accent);
  }
  .toast.ok .toast-icon {
    color: var(--ok);
  }
  .toast.warn .toast-icon {
    color: var(--warn);
  }
  .toast.error .toast-icon {
    color: var(--bad);
  }
  .toast-body {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  .toast-msg {
    min-width: 0;
    color: var(--text-strong);
    line-height: 18px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    word-break: break-word;
    max-height: 36vh;
    overflow-y: auto;
    user-select: text;
    -webkit-user-select: text;
  }
  .toast-actions {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }
  .toast-link {
    all: unset;
    cursor: pointer;
    font-size: 12px;
    line-height: 16px;
    text-decoration: underline;
    text-underline-offset: 2px;
    color: var(--muted);
  }
  .toast.warn .toast-link {
    color: var(--warn);
  }
  .toast.error .toast-link {
    color: var(--bad);
  }
  .toast-link:hover {
    color: var(--text-strong);
  }
  .toast-link:focus-visible,
  .toast-close:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
    border-radius: var(--radius);
  }
  .toast-close {
    flex-shrink: 0;
    min-height: 0;
    padding: 2px var(--space-1);
    border-color: transparent;
    background: transparent;
    color: var(--muted);
    line-height: 1;
  }
  .bottom-bar {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--control-h);
    padding: 0 var(--space-3);
    border-top: 1px solid var(--border);
    background: var(--panel);
    flex-shrink: 0;
  }
  .cluster-pill {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }
  .conn-dot {
    width: var(--space-2);
    height: var(--space-2);
    border-radius: 999px;
    background: var(--muted);
  }
  .conn-dot.connected {
    background: var(--ok);
  }
  .conn-dot.slow {
    background: var(--warn);
  }
  .conn-dot.disconnected {
    background: var(--bad);
  }
  .bottom-bar .grow {
    flex: 1;
  }
</style>
