/**
 * Row controls (JUS-162): elements that belong to a row's OWN control rather
 * than the row itself. A press or key on one of them must never reach the row's
 * selection/activation behaviour — `.fm-size` is the folder-size cell, whose
 * Calculate/Cancel/Retry buttons must keep their native Enter/Space.
 *
 * Kept as a small exported selector so the browser's per-row predicates and the
 * unit test share one definition.
 */
export const ROW_CONTROL_SELECTOR =
  ".fm-select, .fm-actions, .fm-tile-check, .fm-tile-actions, .fm-size";

/** isRowControlTarget reports whether target sits inside one of those controls. */
export function isRowControlTarget(target: EventTarget | null): boolean {
  return (
    target instanceof Element && target.closest(ROW_CONTROL_SELECTOR) !== null
  );
}

/**
 * isRowControlEvent reports whether an event started inside a row control
 * (JUS-217). It reads the event's dispatch-time path, not just the live target:
 * a control that replaces itself when activated (Calculate swaps its button for
 * a spinner) detaches the target before the bubbled press reaches the row, so a
 * `target.closest()` check misfires and the row selects. `composedPath()` keeps
 * the dispatch-time ancestors, so the guard holds for every row control.
 */
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
