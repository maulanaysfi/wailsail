/** Compact age like "45s", "12m", "3h", "5d", "2y". */
export function age(created: string, now: number): string {
  if (!created) return "";
  const t = Date.parse(created);
  if (Number.isNaN(t)) return "";
  let s = Math.max(0, Math.floor((now - t) / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h}h${m % 60 && h < 10 ? `${m % 60}m` : ""}`;
  const d = Math.floor(h / 24);
  if (d < 730) return `${d}d`;
  return `${Math.floor(d / 365)}y`;
}

/** Compare two cell strings: numbers numerically, everything else naturally. */
export function compareCells(a: string, b: string): number {
  const na = Number(a);
  const nb = Number(b);
  if (a !== "" && b !== "" && !Number.isNaN(na) && !Number.isNaN(nb)) return na - nb;
  return a.localeCompare(b, undefined, { numeric: true, sensitivity: "base" });
}

const statusTone: Record<string, "ok" | "warn" | "bad" | "muted"> = {
  running: "ok",
  ready: "ok",
  active: "ok",
  bound: "ok",
  succeeded: "muted",
  completed: "muted",
  complete: "muted",
  pending: "warn",
  containercreating: "warn",
  podinitializing: "warn",
  terminating: "warn",
  notready: "bad",
  failed: "bad",
  error: "bad",
  crashloopbackoff: "bad",
  imagepullbackoff: "bad",
  errimagepull: "bad",
  oomkilled: "bad",
  evicted: "bad",
  lost: "bad",
};

/** Colour hint for status-like cells. */
export function toneFor(value: string): string {
  const key = value.toLowerCase().replace(/[^a-z]/g, "");
  return statusTone[key] ?? "";
}
