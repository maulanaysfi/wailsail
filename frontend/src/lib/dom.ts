/** Active CSS zoom multiplier (1 when native webview zoom is used). */
export function cssZoom(): number {
  if (typeof document === "undefined") return 1;
  const z = parseFloat(document.documentElement.style.zoom || "");
  return Number.isFinite(z) && z > 0 ? z : 1;
}

/** Track a window-level pointer drag in zoom-normalized pixels with pointercancel cleanup. */
export function startPointerDrag(
  e: PointerEvent,
  onMove: (dx: number, dy: number, ev: PointerEvent) => void,
  onEnd?: () => void,
): void {
  const z = cssZoom();
  const startX = e.clientX;
  const startY = e.clientY;
  const move = (ev: PointerEvent) => {
    onMove((ev.clientX - startX) / z, (ev.clientY - startY) / z, ev);
  };
  const end = () => {
    window.removeEventListener("pointermove", move);
    window.removeEventListener("pointerup", end);
    window.removeEventListener("pointercancel", end);
    onEnd?.();
  };
  window.addEventListener("pointermove", move);
  window.addEventListener("pointerup", end);
  window.addEventListener("pointercancel", end);
}

/** Translate vertical mouse-wheel scrolling into horizontal scroll on overflowed tab strips. */
export function horizontalWheelScroll(e: WheelEvent & { currentTarget: HTMLElement }): void {
  if (e.deltaY && !e.deltaX && e.currentTarget.scrollWidth > e.currentTarget.clientWidth) {
    e.currentTarget.scrollLeft += e.deltaY;
    e.preventDefault();
  }
}
