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
