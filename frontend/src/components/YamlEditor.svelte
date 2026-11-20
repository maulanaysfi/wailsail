<script lang="ts">
  import { tokenizeYaml } from "../lib/yamlHighlight";

  let {
    value = $bindable(""),
    readonly = false,
    placeholder = "",
    onkeydown,
  }: {
    value: string;
    readonly?: boolean;
    placeholder?: string;
    onkeydown?: (e: KeyboardEvent) => void;
  } = $props();

  let preEl: HTMLPreElement | undefined = $state();
  let taEl: HTMLTextAreaElement | undefined = $state();

  const lines = $derived(tokenizeYaml(value));

  function syncScroll() {
    if (!preEl || !taEl) return;
    preEl.scrollTop = taEl.scrollTop;
    preEl.scrollLeft = taEl.scrollLeft;
  }

  function handleKey(e: KeyboardEvent) {
    onkeydown?.(e);
    if (e.defaultPrevented || readonly) return;
    if (e.key === "Tab") {
      e.preventDefault();
      const ta = e.currentTarget as HTMLTextAreaElement;
      const { selectionStart: s, selectionEnd: en } = ta;
      value = value.slice(0, s) + "  " + value.slice(en);
      requestAnimationFrame(() => {
        ta.setSelectionRange(s + 2, s + 2);
        syncScroll();
      });
    }
  }

  $effect(() => {
    value;
    if (!readonly) requestAnimationFrame(syncScroll);
  });
</script>

{#snippet highlighted()}
  {#if !value && placeholder}
    <span class="yaml-ph">{placeholder}</span>
  {:else}
    {#each lines as toks, li (li)}
      {#if toks.length === 0}
        <span class="tok-plain">{" "}</span>
      {:else}
        {#each toks as t, ti (ti)}
          <span class={`tok-${t.type}`}>{t.text}</span>
        {/each}
      {/if}
      {#if li < lines.length - 1}{"\n"}{/if}
    {/each}
  {/if}
{/snippet}

{#if readonly}
  <div class="yaml-wrap readonly">
    <pre class="yaml-code yaml-view">{@render highlighted()}</pre>
  </div>
{:else}
  <div class="yaml-wrap editing">
    <pre class="yaml-code yaml-under" aria-hidden="true" bind:this={preEl}>{@render highlighted()}</pre>
    <textarea
      class="yaml-input"
      spellcheck="false"
      autocomplete="off"
      autocapitalize="off"
      bind:value
      bind:this={taEl}
      onscroll={syncScroll}
      onkeydown={handleKey}
      {placeholder}
    ></textarea>
  </div>
{/if}

<style>
  .yaml-wrap {
    position: relative;
    flex: 1;
    min-width: 0;
    min-height: 0;
    background: var(--code-bg);
    overflow: hidden;
  }
  .yaml-wrap.editing {
    box-shadow: inset 2px 0 0 var(--accent);
  }
  .yaml-wrap.editing:focus-within {
    outline: 1px solid var(--accent);
    outline-offset: -1px;
  }
  .yaml-code,
  .yaml-input {
    margin: 0;
    width: 100%;
    height: 100%;
    padding: var(--space-3);
    border: 0;
    border-radius: 0;
    font-family: var(--mono);
    font-size: 12px;
    line-height: 16px;
    tab-size: 2;
    letter-spacing: normal;
    word-spacing: normal;
    white-space: pre;
    overflow-wrap: normal;
    word-break: normal;
    box-sizing: border-box;
  }
  .yaml-view {
    overflow: auto;
    user-select: text;
    -webkit-user-select: text;
    cursor: text;
  }
  .yaml-under {
    position: absolute;
    inset: 0;
    overflow: hidden;
    pointer-events: none;
  }
  .yaml-input {
    position: relative;
    display: block;
    resize: none;
    overflow: auto;
    background: transparent;
    color: transparent;
    caret-color: var(--text-strong);
    outline: none;
  }
  .yaml-input:focus {
    outline: none;
    border-color: transparent;
  }
  .yaml-input::selection {
    background: color-mix(in srgb, var(--accent) 28%, transparent);
    color: transparent;
  }
  .yaml-input::placeholder,
  .yaml-ph {
    color: var(--muted);
  }
  .tok-plain {
    color: var(--text);
  }
  .tok-key {
    color: var(--yaml-key);
  }
  .tok-str {
    color: var(--yaml-str);
  }
  .tok-num {
    color: var(--yaml-num);
  }
  .tok-bool {
    color: var(--yaml-bool);
  }
  .tok-null {
    color: var(--yaml-null);
  }
  .tok-comment {
    color: var(--yaml-comment);
    font-style: italic;
  }
  .tok-punct {
    color: var(--yaml-punct);
  }
  .tok-meta {
    color: var(--yaml-meta);
  }
</style>
