<script lang="ts">
  import { onMount } from "svelte";
  import { app } from "./lib/store.svelte";
  import TopBar from "./components/TopBar.svelte";
  import Sidebar from "./components/Sidebar.svelte";
  import ResourceTable from "./components/ResourceTable.svelte";
  import DetailDrawer from "./components/DetailDrawer.svelte";
  import BottomDock from "./components/BottomDock.svelte";
  import PortForwardModal from "./components/PortForwardModal.svelte";
  import ApplyModal from "./components/ApplyModal.svelte";

  onMount(() => {
    app.init();
  });

  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") {
      if (app.confirmRequest) app.answerConfirm(false);
      else if (app.showApply) app.showApply = false;
      else if (app.showForwards || app.forwardTarget) {
        app.showForwards = false;
        app.forwardTarget = null;
      } else if (app.selection) app.selection = null;
    }
  }
</script>

<svelte:window onkeydown={onKey} />

<div class="shell">
  <TopBar />

  {#if app.connectionError}
    <div class="banner error conn">
      <span>Could not connect to <strong>{app.context || "kubeconfig"}</strong>: {app.connectionError}</span>
      <button onclick={() => app.selectContext(app.context)}>Retry</button>
    </div>
  {/if}

  <div class="workspace">
    <Sidebar />
    <div class="center">
      <div class="upper">
        {#if app.current}
          <ResourceTable />
        {:else if !app.resourcesLoading && !app.connectionError}
          <p class="placeholder">Select a context to browse resources.</p>
        {/if}
        {#if app.selection}
          <DetailDrawer sel={app.selection} />
        {/if}
      </div>
      {#if app.dockOpen && app.dock.length > 0}
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

  {#if app.confirmRequest}
    <div class="backdrop" role="presentation">
      <div class="modal confirm" role="alertdialog" aria-modal="true">
        <h3>{app.confirmRequest.title}</h3>
        <p>{app.confirmRequest.message}</p>
        <div class="row">
          <button onclick={() => app.answerConfirm(false)}>Cancel</button>
          <button class={app.confirmRequest.danger ? "danger" : "primary"} onclick={() => app.answerConfirm(true)}>
            Confirm
          </button>
        </div>
      </div>
    </div>
  {/if}

  <div class="toasts">
    {#each app.toasts as t (t.id)}
      <div class={`toast ${t.tone}`} role="status">
        <span>{t.text}</span>
        <button class="ghost" onclick={() => app.dismissToast(t.id)}>✕</button>
      </div>
    {/each}
  </div>
</div>

<style>
  .shell {
    display: flex;
    flex-direction: column;
    height: 100%;
  }
  .conn {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin: 8px 12px 0;
  }
  .workspace {
    display: grid;
    grid-template-columns: 210px 1fr;
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
  .confirm {
    min-width: 360px;
  }
  .confirm .row {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 14px;
  }
  .toasts {
    position: fixed;
    right: 14px;
    bottom: 14px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    z-index: 30;
    max-width: 420px;
  }
  .toast {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 10px;
    border-radius: 6px;
    background: var(--panel);
    border: 1px solid var(--border);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.35);
  }
  .toast.ok {
    border-color: var(--ok);
  }
  .toast.error {
    border-color: var(--bad);
    color: var(--bad);
  }
</style>
