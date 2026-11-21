export type YamlTokenType = "plain" | "key" | "str" | "num" | "bool" | "null" | "comment" | "punct" | "meta";

export interface YamlToken {
  type: YamlTokenType;
  text: string;
}

const NUM_RE = /^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?$/;
const HEX_RE = /^0x[0-9a-fA-F]+$/;
const BOOL_RE = /^(?:true|false)$/i;
const NULL_RE = /^(?:null|~)$/i;
const META_RE = /^(?:[&*][^\s,[\]{}]+|![^\s,[\]{}]*)$/;

function push(out: YamlToken[], type: YamlTokenType, text: string) {
  if (!text) return;
  const last = out[out.length - 1];
  if (last && last.type === type) {
    last.text += text;
    return;
  }
  out.push({ type, text });
}

function scanQuotedEnd(s: string, start: number): number {
  const q = s[start];
  let i = start + 1;
  while (i < s.length) {
    const ch = s[i];
    if (q === '"' && ch === "\\") {
      i += 2;
      continue;
    }
    if (q === "'" && ch === "'" && s[i + 1] === "'") {
      i += 2;
      continue;
    }
    if (ch === q) return i + 1;
    i++;
  }
  return s.length;
}

// Find the mapping `:` separating key and value in `s[pos..]`, skipping quoted keys.
function findKeyColon(s: string, pos: number): number {
  const first = s[pos];
  if (first === '"' || first === "'") {
    const end = scanQuotedEnd(s, pos);
    let j = end;
    while (j < s.length && (s[j] === " " || s[j] === "\t")) j++;
    return s[j] === ":" && (j + 1 === s.length || s[j + 1] === " " || s[j + 1] === "\t") ? j : -1;
  }
  for (let i = pos; i < s.length; i++) {
    const ch = s[i];
    if (ch === "#") {
      if (i === pos || s[i - 1] === " " || s[i - 1] === "\t") return -1;
    }
    if (ch === ":" && (i + 1 === s.length || s[i + 1] === " " || s[i + 1] === "\t")) {
      return i;
    }
    if (ch === "[" || ch === "{" || ch === '"' || ch === "'") return -1;
  }
  return -1;
}

function tokenizeScalar(s: string, out: YamlToken[]) {
  let i = 0;
  while (i < s.length) {
    const ch = s[i];
    if (ch === " " || ch === "\t") {
      let j = i;
      while (j < s.length && (s[j] === " " || s[j] === "\t")) j++;
      push(out, "plain", s.slice(i, j));
      i = j;
      continue;
    }
    if (ch === "#" && (i === 0 || s[i - 1] === " " || s[i - 1] === "\t")) {
      push(out, "comment", s.slice(i));
      return;
    }
    if (ch === '"' || ch === "'") {
      const end = scanQuotedEnd(s, i);
      push(out, "str", s.slice(i, end));
      i = end;
      continue;
    }
    if (ch === "[" || ch === "]" || ch === "{" || ch === "}" || ch === ",") {
      push(out, "punct", ch);
      i++;
      continue;
    }
    // Flow/inline mapping separator or block scalar header (|, >, |-, >-...)
    if (i === 0 && (ch === "|" || ch === ">")) {
      let j = 1;
      while (j < s.length && (s[j] === "-" || s[j] === "+" || (s[j] >= "0" && s[j] <= "9"))) j++;
      if (j === s.length || s[j] === " " || s[j] === "\t") {
        push(out, "punct", s.slice(0, j));
        i = j;
        continue;
      }
    }
    // Single word/token up to flow punctuation or inline comment.
    let j = i;
    while (j < s.length) {
      const c = s[j];
      if (c === "[" || c === "]" || c === "{" || c === "}" || c === ",") break;
      if (c === "#" && j > i && (s[j - 1] === " " || s[j - 1] === "\t")) break;
      if ((c === " " || c === "\t") && (s.includes("{") || s.includes("[") || s.includes("#") || s[j + 1] === "#")) break;
      j++;
    }
    const seg = s.slice(i, j);
    const trimmed = seg.trimEnd();
    const trail = seg.slice(trimmed.length);
    if (META_RE.test(trimmed)) push(out, "meta", trimmed);
    else if (NULL_RE.test(trimmed)) push(out, "null", trimmed);
    else if (BOOL_RE.test(trimmed)) push(out, "bool", trimmed);
    else if (NUM_RE.test(trimmed) || HEX_RE.test(trimmed)) push(out, "num", trimmed);
    else push(out, "str", trimmed);
    if (trail) push(out, "plain", trail);
    i = j;
  }
}

/** Tokenize a single YAML line while preserving every character verbatim. */
export function tokenizeYamlLine(line: string): YamlToken[] {
  const out: YamlToken[] = [];
  if (!line) return out;

  let pos = 0;
  while (pos < line.length && (line[pos] === " " || line[pos] === "\t")) pos++;
  if (pos > 0) push(out, "plain", line.slice(0, pos));
  if (pos >= line.length) return out;

  if (line[pos] === "#") {
    push(out, "comment", line.slice(pos));
    return out;
  }

  if (pos === 0 && (line.startsWith("---") || line.startsWith("..."))) {
    const end = 3;
    if (end === line.length || line[end] === " " || line[end] === "\t") {
      push(out, "punct", line.slice(0, end));
      pos = end;
      if (pos < line.length) tokenizeScalar(line.slice(pos), out);
      return out;
    }
  }

  // Handle leading list item dashes (`- ` or nested `- - `).
  while (line[pos] === "-" && (pos + 1 === line.length || line[pos + 1] === " " || line[pos + 1] === "\t")) {
    push(out, "punct", "-");
    pos++;
    let ws = pos;
    while (ws < line.length && (line[ws] === " " || line[ws] === "\t")) ws++;
    if (ws > pos) {
      push(out, "plain", line.slice(pos, ws));
      pos = ws;
    }
    if (pos >= line.length) return out;
    if (line[pos] === "#") {
      push(out, "comment", line.slice(pos));
      return out;
    }
  }

  const colon = findKeyColon(line, pos);
  if (colon !== -1) {
    push(out, "key", line.slice(pos, colon));
    push(out, "punct", ":");
    pos = colon + 1;
    if (pos < line.length) tokenizeScalar(line.slice(pos), out);
    return out;
  }

  tokenizeScalar(line.slice(pos), out);
  return out;
}

/** Tokenize full YAML text into per-line token arrays. */
export function tokenizeYaml(text: string): YamlToken[][] {
  return text.split("\n").map(tokenizeYamlLine);
}
