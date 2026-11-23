export const LOG_LINE_HEIGHT = 16;
export const LOG_OVERSCAN = 60;
const MONO_CHAR_WIDTH = 7.25;
const OUT_HORIZONTAL_PADDING = 24;

export interface LogEntryItem {
  id: number;
  lower: string;
  pod: string;
  podColor: number;
  ts: string;
  body: string;
}

export interface LogWindowOptions {
  wrap: boolean;
  stickBottom: boolean;
  scrollTop: number;
  viewportHeight: number;
  viewportWidth: number;
  measured: ReadonlyMap<number, number>;
  lineHeight?: number;
  overscan?: number;
}

export interface LogWindowResult<T extends LogEntryItem> {
  total: number;
  first: number;
  last: number;
  topPad: number;
  bottomPad: number;
  totalHeight: number;
  items: T[];
}

/** Estimate monospace characters per wrapped log line for a scroll viewport width. */
export function estimateCharsPerLine(viewportWidth: number): number {
  const usable = Math.max(120, viewportWidth - OUT_HORIZONTAL_PADDING);
  return Math.max(16, Math.floor(usable / MONO_CHAR_WIDTH));
}

/** Return the rendered or estimated pixel height for a single log entry. */
export function entryHeight(
  entry: LogEntryItem,
  wrap: boolean,
  measured: ReadonlyMap<number, number>,
  charsPerLine: number,
  lineHeight = LOG_LINE_HEIGHT,
): number {
  if (!wrap) return lineHeight;
  const cached = measured.get(entry.id);
  if (cached !== undefined && cached > 0) return cached;
  const len = (entry.pod ? entry.pod.length + 1 : 0) + entry.ts.length + entry.body.length;
  const lines = Math.max(1, Math.ceil(len / Math.max(1, charsPerLine)));
  return lines * lineHeight;
}

/** Return the cumulative Y offset of an entry ID within list, or -1 if absent. */
export function entryOffset(
  list: readonly LogEntryItem[],
  entryId: number,
  wrap: boolean,
  measured: ReadonlyMap<number, number>,
  viewportWidth: number,
  lineHeight = LOG_LINE_HEIGHT,
): number {
  const charsPerLine = estimateCharsPerLine(viewportWidth);
  let y = 0;
  for (let i = 0; i < list.length; i++) {
    if (list[i].id === entryId) return y;
    y += entryHeight(list[i], wrap, measured, charsPerLine, lineHeight);
  }
  return -1;
}

/** Compute the virtualized log slice and spacer heights for wrapped or unwrapped rows. */
export function computeLogWindow<T extends LogEntryItem>(
  list: readonly T[],
  options: LogWindowOptions,
): LogWindowResult<T> {
  const lineHeight = options.lineHeight ?? LOG_LINE_HEIGHT;
  const overscan = options.overscan ?? LOG_OVERSCAN;
  const total = list.length;
  if (total === 0) {
    return { total: 0, first: 0, last: 0, topPad: 0, bottomPad: 0, totalHeight: 0, items: [] };
  }

  if (!options.wrap) {
    const totalHeight = total * lineHeight;
    const page = Math.max(1, Math.ceil(options.viewportHeight / lineHeight));
    let first: number;
    let last: number;
    if (options.stickBottom) {
      last = total;
      first = Math.max(0, last - page - overscan * 2);
    } else {
      const maxScroll = Math.max(0, totalHeight - options.viewportHeight);
      const viewTop = Math.min(Math.max(0, options.scrollTop), maxScroll);
      first = Math.max(0, Math.floor(viewTop / lineHeight) - overscan);
      last = Math.min(total, Math.ceil((viewTop + options.viewportHeight) / lineHeight) + overscan);
    }
    return {
      total,
      first,
      last,
      topPad: first * lineHeight,
      bottomPad: (total - last) * lineHeight,
      totalHeight,
      items: list.slice(first, last),
    };
  }

  const charsPerLine = estimateCharsPerLine(options.viewportWidth);
  const heights = new Array<number>(total);
  let totalHeight = 0;
  for (let i = 0; i < total; i++) {
    const h = entryHeight(list[i], true, options.measured, charsPerLine, lineHeight);
    heights[i] = h;
    totalHeight += h;
  }

  if (options.stickBottom) {
    const minWindowH = options.viewportHeight + overscan * lineHeight;
    let first = total - 1;
    let acc = heights[first];
    while (first > 0 && (acc < minWindowH || total - first < overscan * 2)) {
      first--;
      acc += heights[first];
    }
    return {
      total,
      first,
      last: total,
      topPad: totalHeight - acc,
      bottomPad: 0,
      totalHeight,
      items: list.slice(first, total),
    };
  }

  const maxScroll = Math.max(0, totalHeight - options.viewportHeight);
  const viewTop = Math.min(Math.max(0, options.scrollTop), maxScroll);
  const viewBottom = viewTop + options.viewportHeight;

  let y = 0;
  let visStart = total - 1;
  let visEnd = total;
  let foundStart = false;
  for (let i = 0; i < total; i++) {
    const nextY = y + heights[i];
    if (!foundStart && nextY > viewTop) {
      visStart = i;
      foundStart = true;
    }
    if (foundStart && nextY >= viewBottom) {
      visEnd = i + 1;
      break;
    }
    y = nextY;
  }

  const first = Math.max(0, visStart - overscan);
  const last = Math.min(total, visEnd + overscan);

  let topPad = 0;
  for (let i = 0; i < first; i++) topPad += heights[i];
  let bottomPad = 0;
  for (let i = last; i < total; i++) bottomPad += heights[i];

  return {
    total,
    first,
    last,
    topPad,
    bottomPad,
    totalHeight,
    items: list.slice(first, last),
  };
}
