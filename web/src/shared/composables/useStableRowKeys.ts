import { ref, watch } from "vue";

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

  function insertAt(index: number): void {
    keys.value.splice(index, 0, mint());
  }

  function removeAt(index: number): void {
    keys.value.splice(index, 1);
  }

  return { keys, insertAt, removeAt };
}
