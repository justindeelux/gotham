export const ROW_CONTROL_SELECTOR =
  ".fm-select, .fm-actions, .fm-tile-check, .fm-tile-actions, .fm-size";

export function isRowControlTarget(target: EventTarget | null): boolean {
  return (
    target instanceof Element && target.closest(ROW_CONTROL_SELECTOR) !== null
  );
}

export function isRowControlEvent(event: Event): boolean {
  if (typeof event.composedPath === "function") {
    for (const node of event.composedPath()) {
      if (node instanceof Element && node.matches(ROW_CONTROL_SELECTOR)) {
        return true;
      }
    }
  }
  return isRowControlTarget(event.target);
}
