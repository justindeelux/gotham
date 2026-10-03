/**
 * A monotonic token source used to discard the responses of superseded
 * requests. Capture `current()` before an await, `bump()` when the owner
 * changes (application switch, wizard close), then check `isCurrent(token)`
 * before writing any state back. Stale responses become no-ops instead of
 * overwriting the new owner's draft.
 */
export interface RequestGeneration {
  current(): number;
  bump(): void;
  isCurrent(_token: number): boolean;
}

/** createRequestGeneration returns a fresh generation counter. */
export function createRequestGeneration(): RequestGeneration {
  let value = 0;
  return {
    current: () => value,
    bump: () => {
      value += 1;
    },
    isCurrent: (token: number) => token === value,
  };
}
