import { ref, watch } from "vue";

/**
 * useStableRowKeys mints a stable key per editor row so appending or removing a
 * row never reuses another row's DOM identity (C4-14). Editors call `insertAt`
 * / `removeAt` alongside their model update; a length change from outside
 * (loading a stored collection) is reconciled by the count watcher.
 */
export function useStableRowKeys(count: () => number) {
  const keys = ref<string[]>([]);
  let sequence = 0;
  const mint = (): string => `row-${(sequence += 1)}`;

  watch(
    count,
    (next) => {
      while (keys.value.length < next) {
        keys.value.push(mint());
      }
      if (keys.value.length > next) {
        keys.value.splice(next);
      }
    },
    { immediate: true, flush: "sync" },
  );

  /** insertAt reserves a key for a row inserted at `index`. */
  function insertAt(index: number): void {
    keys.value.splice(index, 0, mint());
  }

  /** removeAt drops the key of the row removed at `index`. */
  function removeAt(index: number): void {
    keys.value.splice(index, 1);
  }

  return { keys, insertAt, removeAt };
}
