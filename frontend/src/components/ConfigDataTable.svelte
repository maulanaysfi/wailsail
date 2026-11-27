<script lang="ts">
  import type { Snippet } from "svelte";
  import { SECRET_VALUE_MASK, type ConfigDataEntry } from "../lib/format";

  let {
    entries,
    isSecret,
    resetKey,
    copyBtn,
    truncText,
  }: {
    entries: ConfigDataEntry[];
    isSecret: boolean;
    resetKey: string;
    copyBtn: Snippet<[string, string, string]>;
    truncText: Snippet<[string, string, boolean?]>;
  } = $props();

  let revealedKeys = $state<Record<string, boolean>>({});
  let revealedVals = $state<Record<string, boolean>>({});

  $effect(() => {
    resetKey;
    revealedKeys = {};
    revealedVals = {};
  });

  function toggleRevealKey(id: string) {
    revealedKeys[id] = !revealedKeys[id];
  }

  function toggleRevealVal(id: string) {
    revealedVals[id] = !revealedVals[id];
  }
</script>

{#snippet eyeBtn(revealed: boolean, onToggle: () => void, targetLabel: string)}
  <button
    class="eye-btn"
    class:revealed
    onclick={(e) => {
      e.stopPropagation();
      onToggle();
    }}
    title={revealed ? `Hide ${targetLabel}` : `Reveal ${targetLabel}`}
    aria-label={revealed ? `Hide ${targetLabel}` : `Reveal ${targetLabel}`}
  >
    {#if revealed}
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94" />
        <path d="M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19" />
        <path d="M14.12 14.12a3 3 0 1 1-4.24-4.24" />
        <line x1="1" y1="1" x2="23" y2="23" />
      </svg>
    {:else}
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
        <circle cx="12" cy="12" r="3" />
      </svg>
    {/if}
  </button>
{/snippet}

<section class="section">
  <h4>Data ({entries.length})</h4>
  {#if entries.length === 0}
    <p class="muted empty-inline">No data entries.</p>
  {:else}
    <table class="data-table">
      <thead>
        <tr>
          <th class="data-key-col">Key</th>
          <th>Value</th>
        </tr>
      </thead>
      <tbody>
        {#each entries as entry (entry.id)}
          {@const keyOpen = !isSecret || !!revealedKeys[entry.id]}
          {@const valOpen = !isSecret || !!revealedVals[entry.id]}
          <tr>
            <td class="data-key-col">
              <div class="val-copy">
                {#if isSecret}
                  {@render eyeBtn(
                    !!revealedKeys[entry.id],
                    () => toggleRevealKey(entry.id),
                    "secret key",
                  )}
                {/if}
                <span class={`mono ${keyOpen ? "val-text" : "muted"}`}>
                  {keyOpen ? entry.key : entry.maskedKey}
                </span>
                {#if entry.source === "binaryData"}
                  <span class="pill">binary</span>
                {/if}
              </div>
            </td>
            <td class="data-val-col">
              <div class="val-copy">
                {#if isSecret}
                  {@render eyeBtn(
                    !!revealedVals[entry.id],
                    () => toggleRevealVal(entry.id),
                    `value for ${keyOpen ? entry.key : entry.maskedKey}`,
                  )}
                {/if}
                {@render copyBtn(
                  entry.value,
                  `data:${entry.id}`,
                  isSecret && !keyOpen ? "secret value" : `value for ${entry.key}`,
                )}
                {#if !valOpen}
                  <span class="mono muted">{SECRET_VALUE_MASK}</span>
                {:else}
                  {#if entry.binary && isSecret}
                    <span class="pill">binary</span>
                  {/if}
                  {#if entry.value === ""}
                    <span class="muted">(empty)</span>
                  {:else}
                    {@render truncText(entry.value, `data:${entry.id}`, true)}
                  {/if}
                {/if}
              </div>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</section>

<style>
  .eye-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 18px;
    height: 18px;
    min-height: 0;
    padding: 0;
    border: 0;
    border-radius: var(--radius);
    background: transparent;
    color: var(--muted);
    align-self: flex-start;
  }
  .eye-btn:hover:not(:disabled) {
    color: var(--text-strong);
    background: var(--hover);
  }
  .eye-btn:focus-visible {
    outline: 1px solid var(--accent);
    outline-offset: 1px;
  }
  .eye-btn.revealed {
    color: var(--accent);
  }
  .eye-btn svg {
    width: 12px;
    height: 12px;
  }
  .data-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
    table-layout: fixed;
  }
  .data-table th,
  .data-table td {
    text-align: left;
    padding: var(--space-1) var(--space-3) var(--space-1) 0;
    vertical-align: top;
  }
  .data-table th {
    padding-top: 0;
    color: var(--muted);
    font-weight: 500;
  }
  .data-table td.data-key-col {
    width: 36%;
    word-break: break-word;
  }
  .data-table td.data-val-col {
    padding-right: 0;
    word-break: break-all;
  }
  .data-table :global(.copy-btn) {
    align-self: flex-start;
  }
  .data-table .val-copy > span:last-child {
    flex: 1;
    min-width: 0;
  }
</style>
