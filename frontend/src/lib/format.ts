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

/** Parse a Kubernetes CPU quantity string into millicores (1 core = 1000m). */
export function parseCPUMilli(raw?: string | number | null): number {
  if (raw === undefined || raw === null || raw === "") return 0;
  if (typeof raw === "number") return Math.max(0, Math.round(raw * 1000));
  const s = String(raw).trim();
  if (!s) return 0;
  if (s.endsWith("n")) return Math.max(0, Math.round(parseFloat(s.slice(0, -1)) / 1_000_000));
  if (s.endsWith("u")) return Math.max(0, Math.round(parseFloat(s.slice(0, -1)) / 1_000));
  if (s.endsWith("m")) return Math.max(0, Math.round(parseFloat(s.slice(0, -1))));
  const n = parseFloat(s);
  return Number.isFinite(n) && n > 0 ? Math.round(n * 1000) : 0;
}

/** Format millicores as a compact Kubernetes CPU string (e.g. "250m", "1000m"). */
export function formatCPUMilli(milli: number): string {
  const m = Math.max(0, Math.round(milli));
  return `${m}m`;
}

const memMultipliers: Record<string, number> = {
  Ki: 1024,
  Mi: 1024 ** 2,
  Gi: 1024 ** 3,
  Ti: 1024 ** 4,
  Pi: 1024 ** 5,
  Ei: 1024 ** 6,
  k: 1e3,
  K: 1e3,
  M: 1e6,
  G: 1e9,
  T: 1e12,
  P: 1e15,
  E: 1e18,
};

/** Parse a Kubernetes memory quantity string into bytes. */
export function parseMemoryBytes(raw?: string | number | null): number {
  if (raw === undefined || raw === null || raw === "") return 0;
  if (typeof raw === "number") return Math.max(0, Math.round(raw));
  const s = String(raw).trim();
  const m = /^([0-9]+(?:\.[0-9]+)?)\s*([A-Za-z]*)$/.exec(s);
  if (!m) return 0;
  const val = parseFloat(m[1]);
  if (!Number.isFinite(val) || val <= 0) return 0;
  const unit = m[2];
  if (!unit) return Math.round(val);
  const mult = memMultipliers[unit];
  return mult ? Math.round(val * mult) : 0;
}

/** Format bytes into compact binary units (Ki, Mi, Gi, Ti). */
export function formatMemoryBytes(bytes: number): string {
  const b = Math.max(0, Math.round(bytes));
  if (b === 0) return "0Mi";
  const gi = 1024 ** 3;
  const mi = 1024 ** 2;
  const ki = 1024;
  if (b >= gi) {
    const v = b / gi;
    return `${Number.isInteger(v) ? v : v.toFixed(v >= 10 ? 1 : 2).replace(/\.?0+$/, "")}Gi`;
  }
  if (b >= mi) {
    const v = b / mi;
    return `${Number.isInteger(v) ? v : v.toFixed(v >= 10 ? 0 : 1).replace(/\.0$/, "")}Mi`;
  }
  if (b >= ki) {
    return `${Math.round(b / ki)}Ki`;
  }
  return `${b}B`;
}

export interface ResourceMeter {
  label: string;
  showBar: boolean;
  usedText: string;
  requestText: string;
  limitText: string;
  scaleMaxText: string;
  usedPct: number;
  requestMarkPct: number | null;
  requestMarkWarn: boolean;
  tone: "ok" | "warn" | "bad";
  hoverText: string;
}

/**
 * Compute container resource progress bar geometry and colour thresholds:
 * - No request and no limit -> plain text of current usage (no progress bar).
 * - Request defined, no limit -> vertical line mark at 25% (100% track = 4x request).
 * - Limit defined, no request -> track = limit, no vertical line mark.
 * - Both request and limit defined -> track = limit, vertical line mark at (request / limit)%.
 * - Bar colour: green (<80%), yellow (80%–94.99%), red (>=95%).
 */
export function buildResourceMeter(
  kind: "cpu" | "memory",
  usedVal: number | null | undefined,
  reqRaw?: string | null,
  limRaw?: string | null,
): ResourceMeter {
  const parse = kind === "cpu" ? parseCPUMilli : parseMemoryBytes;
  const fmt = kind === "cpu" ? formatCPUMilli : formatMemoryBytes;
  const hasUsed = typeof usedVal === "number" && Number.isFinite(usedVal) && usedVal >= 0;
  const used = hasUsed ? usedVal : 0;
  const req = parse(reqRaw);
  const lim = parse(limRaw);
  const hasReq = req > 0;
  const hasLim = lim > 0;

  const usedText = hasUsed ? fmt(used) : "N/A";
  const requestText = hasReq ? (reqRaw?.trim() || fmt(req)) : "none";
  const limitText = hasLim ? (limRaw?.trim() || fmt(lim)) : "none";

  if (!hasReq && !hasLim) {
    return {
      label: kind === "cpu" ? "CPU" : "Memory",
      showBar: false,
      usedText,
      requestText,
      limitText,
      scaleMaxText: "",
      usedPct: 0,
      requestMarkPct: null,
      requestMarkWarn: false,
      tone: "ok",
      hoverText: `Used: ${usedText} / Request: none / Limit: none`,
    };
  }

  const maxVal = hasLim ? lim : req * 4;
  const scaleMaxText = fmt(maxVal);
  const requestMarkPct = hasReq ? (hasLim ? Math.min(100, (req / lim) * 100) : 25) : null;
  const requestMarkWarn = hasReq && !hasLim;
  const rawPct = hasUsed && maxVal > 0 ? (used / maxVal) * 100 : 0;
  const usedPct = Math.max(0, Math.min(100, rawPct));
  const tone: "ok" | "warn" | "bad" = rawPct >= 95 ? "bad" : rawPct >= 80 ? "warn" : "ok";

  return {
    label: kind === "cpu" ? "CPU" : "Memory",
    showBar: true,
    usedText,
    requestText,
    limitText,
    scaleMaxText,
    usedPct,
    requestMarkPct,
    requestMarkWarn,
    tone,
    hoverText: `Used: ${usedText} / Request: ${requestText} / Limit: ${limitText}`,
  };
}

let measureCtx: CanvasRenderingContext2D | null | undefined;

/** Measure rendered text width in pixels for a CSS font string. */
export function measureTextWidth(text: string, font: string): number {
  if (!text) return 0;
  if (measureCtx === undefined && typeof document !== "undefined") {
    measureCtx = document.createElement("canvas").getContext("2d");
  }
  if (measureCtx) {
    measureCtx.font = font;
    return Math.ceil(measureCtx.measureText(text).width);
  }
  return text.length * 8;
}
