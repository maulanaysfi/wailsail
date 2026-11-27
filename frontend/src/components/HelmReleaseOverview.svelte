<script lang="ts">
  import type { Snippet } from "svelte";
  import type { HelmReleaseDetail } from "../lib/api";
  import { age, toneFor } from "../lib/format";
  import { app } from "../lib/store.svelte";

  let {
    detail,
    loading,
    releaseName,
    rollingBackRev,
    onRollback,
    copyBtn,
  }: {
    detail: HelmReleaseDetail | null;
    loading: boolean;
    releaseName: string;
    rollingBackRev: number | null;
    onRollback: (rev: number) => void;
    copyBtn: Snippet<[string, string, string]>;
  } = $props();

  const REV_PAGE_SIZE = 5;
  let revPage = $state(0);

  $effect(() => {
    detail?.namespace;
    detail?.name;
    revPage = 0;
  });

  const helmHistory = $derived(detail?.history ?? []);
  const revTotalPages = $derived(Math.max(1, Math.ceil(helmHistory.length / REV_PAGE_SIZE)));
  const safeRevPage = $derived(Math.min(revPage, revTotalPages - 1));
  const pagedRevisions = $derived(helmHistory.slice(safeRevPage * REV_PAGE_SIZE, (safeRevPage + 1) * REV_PAGE_SIZE));
</script>

{#if loading && !detail}
  <p class="empty">Loading…</p>
{:else if detail}
  {@const fullChart = detail.chartVersion ? `${detail.chart}-${detail.chartVersion}` : detail.chart}
  <section class="section">
    <h4>Release info</h4>
    <dl class="kv-grid">
      <dt>Name</dt>
      <dd class="val-copy">
        {@render copyBtn(detail.name, "helm:name", "release name")}
        <span class="mono val-text">{detail.name}</span>
      </dd>
      <dt>Namespace</dt>
      <dd class="val-copy">
        {@render copyBtn(detail.namespace, "helm:ns", "namespace")}
        <span class="mono val-text">{detail.namespace}</span>
      </dd>
      <dt>Revision</dt>
      <dd class="mono val-text">{detail.revision}</dd>
      <dt>Status</dt>
      <dd class={`val-text tone-${toneFor(detail.status)}`}>{detail.status}</dd>
      <dt>Chart</dt>
      <dd class="val-copy">
        {@render copyBtn(fullChart, "helm:chart", "chart")}
        <span class="mono val-text">{fullChart || "—"}</span>
      </dd>
      {#if detail.appVersion}
        <dt>App version</dt>
        <dd class="val-copy">
          {@render copyBtn(detail.appVersion, "helm:appver", "app version")}
          <span class="mono val-text">{detail.appVersion}</span>
        </dd>
      {/if}
      {#if detail.firstDeployed}
        <dt>First deployed</dt>
        <dd class="val-text">{age(detail.firstDeployed, app.now)} ago <span class="muted">({detail.firstDeployed})</span></dd>
      {/if}
      {#if detail.lastDeployed}
        <dt>Last deployed</dt>
        <dd class="val-text">{age(detail.lastDeployed, app.now)} ago <span class="muted">({detail.lastDeployed})</span></dd>
      {/if}
      {#if detail.description}
        <dt>Description</dt>
        <dd class="val-text">{detail.description}</dd>
      {/if}
    </dl>
  </section>

  <section class="section">
    <div class="section-head">
      <h4>Revisions ({helmHistory.length})</h4>
      {#if revTotalPages > 1}
        <div class="pager">
          <span class="muted pager-range">
            {safeRevPage * REV_PAGE_SIZE + 1}–{Math.min((safeRevPage + 1) * REV_PAGE_SIZE, helmHistory.length)} of {helmHistory.length}
          </span>
          <button
            class="mini"
            disabled={safeRevPage === 0}
            onclick={() => (revPage = Math.max(0, safeRevPage - 1))}
            title="Previous revisions page"
          >
            ‹
          </button>
          <button
            class="mini"
            disabled={safeRevPage >= revTotalPages - 1}
            onclick={() => (revPage = Math.min(revTotalPages - 1, safeRevPage + 1))}
            title="Next revisions page"
          >
            ›
          </button>
        </div>
      {/if}
    </div>
    {#if helmHistory.length === 0}
      <p class="muted empty-inline">No revision history.</p>
    {:else}
      <table class="ledger-table revisions-table">
        <thead>
          <tr>
            <th class="col-rev-num">Rev</th>
            <th class="col-rev-status">Status</th>
            <th>Chart</th>
            <th class="col-rev-age">Updated</th>
            <th>Description</th>
            <th class="col-rev-act"></th>
          </tr>
        </thead>
        <tbody>
          {#each pagedRevisions as h (h.revision)}
            <tr>
              <td class="col-rev-num mono val-text">{h.revision}</td>
              <td class={`col-rev-status val-text tone-${toneFor(h.status)}`}>{h.status}</td>
              <td class="mono val-text">{h.chart || "—"}</td>
              <td class="col-rev-age mono muted val-text">{h.updated ? age(h.updated, app.now) : "—"}</td>
              <td class="val-text">{h.description || "—"}</td>
              <td class="col-rev-act">
                {#if h.revision !== detail.revision}
                  <button
                    class="mini"
                    disabled={rollingBackRev !== null}
                    onclick={() => onRollback(h.revision)}
                    title={`Roll back ${releaseName} to revision ${h.revision}`}
                  >
                    {rollingBackRev === h.revision ? "…" : "Rollback"}
                  </button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </section>

  {#if detail.notes}
    <section class="section">
      <h4>Notes</h4>
      <pre class="helm-notes mono val-text">{detail.notes}</pre>
    </section>
  {/if}
{/if}

<style>
  .empty {
    color: var(--muted);
    text-align: center;
    margin-top: var(--space-4);
  }
  .revisions-table .col-rev-num {
    width: 44px;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .revisions-table .col-rev-status {
    width: 88px;
    white-space: nowrap;
  }
  .revisions-table .col-rev-age {
    width: 60px;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .revisions-table .col-rev-act {
    width: 72px;
    text-align: right;
    padding-right: 0;
    white-space: nowrap;
  }
  .helm-notes {
    margin: 0;
    padding: var(--space-2);
    background: var(--code-bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    white-space: pre-wrap;
    word-break: break-word;
    line-height: 17px;
  }
</style>
