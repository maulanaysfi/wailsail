import { cssZoom } from "./dom.ts";

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
  deployed: "ok",
  succeeded: "muted",
  completed: "muted",
  complete: "muted",
  superseded: "muted",
  uninstalled: "muted",
  pending: "warn",
  pendinginstall: "warn",
  pendingupgrade: "warn",
  pendingrollback: "warn",
  uninstalling: "warn",
  unknown: "warn",
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
let measureSpan: HTMLSpanElement | null | undefined;
const measureCache = new Map<string, number>();
const MAX_MEASURE_CACHE = 500;

/** Clear cached text measurements (e.g. once web fonts finish loading). */
export function clearMeasureTextCache(): void {
  measureCache.clear();
}

/** Measure rendered text width in pixels for a CSS font string. */
export function measureTextWidth(text: string, font: string, tabular = false): number {
  if (!text) return 0;
  const key = `${font}\0${tabular ? "t" : "p"}\0${text}`;
  const cached = measureCache.get(key);
  if (cached !== undefined) return cached;

  let width: number;
  if (tabular && typeof document !== "undefined" && document.body) {
    if (!measureSpan) {
      measureSpan = document.createElement("span");
      measureSpan.style.cssText =
        "position:fixed;left:-9999px;top:-9999px;visibility:hidden;pointer-events:none;white-space:nowrap;";
      document.body.appendChild(measureSpan);
    }
    measureSpan.style.font = font;
    // Set after `style.font` because the CSS `font` shorthand resets font-variant-numeric.
    measureSpan.style.fontVariantNumeric = "tabular-nums";
    measureSpan.textContent = text;
    width = Math.ceil(measureSpan.getBoundingClientRect().width / cssZoom());
  } else {
    if (measureCtx === undefined && typeof document !== "undefined") {
      measureCtx = document.createElement("canvas").getContext("2d");
    }
    if (measureCtx) {
      measureCtx.font = font;
      width = Math.ceil(measureCtx.measureText(text).width);
    } else {
      width = text.length * 8;
    }
  }
  if (measureCache.size >= MAX_MEASURE_CACHE) measureCache.clear();
  measureCache.set(key, width);
  return width;
}

export const SECRET_VALUE_MASK = "*".repeat(20);

/** Mask a Secret key, keeping at most the first 4 characters and asterisks for the rest. */
export function maskSecretKey(key: string): string {
  if (key.length <= 4) return "****";
  return key.slice(0, 4) + "*".repeat(key.length - 4);
}

/** Decode base64 Secret data as UTF-8 text, falling back to raw base64 for binary content. */
export function decodeBase64Text(b64: string): { text: string; binary: boolean } {
  if (!b64) return { text: "", binary: false };
  try {
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    const text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
    if (/[\x00-\x08\x0b\x0c\x0e-\x1f]/.test(text)) return { text: b64, binary: true };
    return { text, binary: false };
  } catch {
    return { text: b64, binary: true };
  }
}

export interface ConfigDataEntry {
  id: string;
  key: string;
  maskedKey: string;
  value: string;
  binary: boolean;
  source: "data" | "stringData" | "binaryData";
}

/** Collect ConfigMap or Secret key/value pairs in deterministic key order. */
export function buildConfigDataEntries(obj: Record<string, any> | null, isSecret: boolean): ConfigDataEntry[] {
  if (!obj) return [];
  const out: ConfigDataEntry[] = [];
  const data = isPlainObject(obj.data) ? obj.data : {};
  for (const key of Object.keys(data).sort()) {
    const raw = data[key] == null ? "" : String(data[key]);
    const decoded = isSecret ? decodeBase64Text(raw) : { text: raw, binary: false };
    out.push({
      id: `data:${key}`,
      key,
      maskedKey: maskSecretKey(key),
      value: decoded.text,
      binary: decoded.binary,
      source: "data",
    });
  }
  if (isSecret && isPlainObject(obj.stringData)) {
    for (const key of Object.keys(obj.stringData).sort()) {
      const text = obj.stringData[key] == null ? "" : String(obj.stringData[key]);
      out.push({
        id: `stringData:${key}`,
        key,
        maskedKey: maskSecretKey(key),
        value: text,
        binary: false,
        source: "stringData",
      });
    }
  }
  if (!isSecret && isPlainObject(obj.binaryData)) {
    for (const key of Object.keys(obj.binaryData).sort()) {
      const text = obj.binaryData[key] == null ? "" : String(obj.binaryData[key]);
      out.push({
        id: `binaryData:${key}`,
        key,
        maskedKey: maskSecretKey(key),
        value: text,
        binary: true,
        source: "binaryData",
      });
    }
  }
  return out;
}

export function isPlainObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

export type ToastTone = "info" | "ok" | "warn" | "error";

const TOAST_BASE_MS: Record<ToastTone, number> = {
  info: 3500,
  ok: 3500,
  warn: 5500,
  error: 8000,
};

/** Compute conditional auto-dismiss timeout (ms), extending past 10 words up to 18s. */
export function toastTimeoutMs(text: string, tone: ToastTone = "info"): number {
  const base = TOAST_BASE_MS[tone];
  const words = text.trim() ? text.trim().split(/\s+/).length : 0;
  if (words <= 10) return base;
  return Math.min(18_000, base + (words - 10) * 250);
}

const logTimeFormatters = new Map<string, Intl.DateTimeFormat>();

function getLogTimeFormatter(timeZone: string | undefined): Intl.DateTimeFormat {
  const key = timeZone ?? "__local__";
  let fmt = logTimeFormatters.get(key);
  if (!fmt) {
    fmt = new Intl.DateTimeFormat("en-GB", {
      timeZone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hour12: false,
    });
    logTimeFormatters.set(key, fmt);
  }
  return fmt;
}

/**
 * Format a parsed Kubernetes log timestamp (`2026-...Z `) in the configured
 * timezone ("local", "UTC", or IANA zone) as `YYYY-MM-DD HH:mm:ss` while
 * preserving trailing separator whitespace.
 */
export function formatLogTimestamp(rawTs: string, timezone = "local"): string {
  const m = /^(\S+)(\s*)$/.exec(rawTs);
  if (!m) return rawTs;
  const ms = Date.parse(m[1]);
  if (Number.isNaN(ms)) return rawTs;
  const tz = !timezone || timezone === "local" ? undefined : timezone;
  try {
    const parts: Record<string, string> = {};
    for (const p of getLogTimeFormatter(tz).formatToParts(ms)) {
      if (p.type !== "literal") parts[p.type] = p.value;
    }
    const hour = parts.hour === "24" ? "00" : parts.hour;
    return `${parts.year}-${parts.month}-${parts.day} ${hour}:${parts.minute}:${parts.second}${m[2]}`;
  } catch {
    return rawTs;
  }
}

/** Return selectable timezone options: Local (System), UTC, and supported IANA zones. */
export function timezoneOptions(): Array<{ value: string; label: string }> {
  const out: Array<{ value: string; label: string }> = [
    { value: "local", label: "Local (System)" },
    { value: "UTC", label: "UTC" },
  ];
  const intlAny = Intl as typeof Intl & { supportedValuesOf?: (key: string) => string[] };
  if (typeof intlAny.supportedValuesOf === "function") {
    for (const tz of intlAny.supportedValuesOf("timeZone")) {
      if (tz !== "UTC") out.push({ value: tz, label: tz });
    }
  }
  return out;
}
