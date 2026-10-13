<script setup lang="ts">
import { NButton, NCheckbox, NFormItem, NInput, NModal, useMessage } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { describeFilesError } from "@/features/files/api/files";
import type { FilesEntry } from "@/features/files/schemas/files";
import { filesOctalModeSchema } from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
import {
  formatOctalMode,
  modeFromFlags,
  parseOctalMode,
  permissionFlags,
} from "@/features/files/utils/files";
import { submitOnEnter } from "@/features/projects/utils/submitOnEnter";

interface Props {
  show: boolean;
  entry: FilesEntry | null;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
}>();

const { t } = useI18n();
const message = useMessage();
const files = useFilesStore();

// The octal field is the source of truth; the 3x3 grid is derived from it and
// writes back a fresh octal string, so the two stay in sync in both
// directions. An invalid octal string leaves the grid showing all-false; the
// Apply button stays disabled until the field is valid again.
const octal = ref("644");
const touched = ref(false);

/** The nine rwx flags in owner/group/other x read/write/execute order. */
const flagIndexes = [0, 1, 2, 3, 4, 5, 6, 7, 8] as const;

/** roles labels the three grid rows. */
const rows = computed(() => [
  { label: String(t("files.permissions.owner")), offset: 0 },
  { label: String(t("files.permissions.group")), offset: 3 },
  { label: String(t("files.permissions.other")), offset: 6 },
]);

/** columns labels the three grid columns. */
const columns = computed(() => [
  String(t("files.permissions.read")),
  String(t("files.permissions.write")),
  String(t("files.permissions.execute")),
]);

const octalValid = computed<boolean>(
  () => filesOctalModeSchema.safeParse(octal.value).success,
);

const flags = computed<boolean[]>(() =>
  octalValid.value ? permissionFlags(parseOctalMode(octal.value)) : [false, false, false, false, false, false, false, false, false],
);

/** errorKey is the catalog key shown under the field once it was touched. */
const errorKey = computed<string | null>(() =>
  touched.value && !octalValid.value ? "files.permissions.invalid" : null,
);

/** flagAria builds a screen-reader label for one grid checkbox. */
function flagAria(offset: number, column: number): string {
  return String(
    t("files.permissions.flagAria", {
      who: rows.value[offset / 3]?.label ?? "",
      perm: columns.value[column] ?? "",
    }),
  );
}

/** toggleFlag writes one grid toggle back into the octal field. */
function toggleFlag(index: number, checked: boolean): void {
  const next = [...flags.value];
  next[index] = checked;
  octal.value = formatOctalMode(modeFromFlags(next));
}

watch(
  () => props.show,
  (open) => {
    if (open) {
      octal.value = props.entry ? formatOctalMode(props.entry.mode) : "644";
      touched.value = false;
    }
  },
);

/** submit applies the mode and surfaces any failure in a toast. */
async function submit(): Promise<void> {
  if (files.acting || !props.entry) {
    return;
  }
  touched.value = true;
  if (!octalValid.value) {
    return;
  }
  const mode = parseOctalMode(octal.value);
  try {
    await files.chmod(props.entry, mode);
    message.success(
      String(
        t("files.permissions.saved", {
          name: props.entry.name,
          mode: formatOctalMode(mode),
        }),
      ),
    );
    emit("update:show", false);
  } catch (err) {
    message.error(describeFilesError(err));
  }
}
</script>

<template>
  <NModal
    class="app-modal"
    :show="props.show"
    preset="card"
    style="width: 460px; max-width: 94vw"
    :title="t('files.permissions.title')"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <div class="stack gap-4">
      <p class="small muted path">{{ props.entry?.name ?? "" }}</p>
      <NFormItem
        :label="t('files.permissions.octal')"
        :validation-status="errorKey ? 'error' : undefined"
        :feedback="errorKey ? t('files.permissions.invalid') : ''"
      >
        <NInput
          v-model:value="octal"
          class="mono octal"
          maxlength="3"
          autofocus
          :placeholder="t('files.permissions.octalPlaceholder')"
          @keydown.enter="(event: KeyboardEvent) => submitOnEnter(event, submit)"
        />
      </NFormItem>
      <p class="hint small muted">{{ t("files.permissions.octalHint") }}</p>

      <div class="field">
        <span class="field-label">{{ t("files.permissions.bitsLabel") }}</span>
        <div class="perms-grid" role="group" :aria-label="t('files.permissions.bitsLabel')">
          <span class="perms-head" aria-hidden="true"></span>
          <span v-for="column in columns" :key="column" class="perms-head">{{ column }}</span>
          <template v-for="row in rows" :key="row.label">
            <span class="perms-row">{{ row.label }}</span>
            <label
              v-for="column in flagIndexes.slice(0, 3)"
              :key="`${row.offset}-${column}`"
              class="check"
              :title="flagAria(row.offset, column)"
            >
              <NCheckbox
                size="small"
                :checked="flags[row.offset + column]"
                :aria-label="flagAria(row.offset, column)"
                @update:checked="(checked: boolean) => toggleFlag(row.offset + column, checked)"
              />
            </label>
          </template>
        </div>
      </div>

      <div class="callout">
        <div>
          <h4>{{ t("files.permissions.specialTitle") }}</h4>
          <p class="small">{{ t("files.permissions.specialNote") }}</p>
        </div>
      </div>
    </div>
    <template #footer>
      <div class="dialog-foot">
        <span class="small muted grow">{{ t("files.permissions.keepTypeNote") }}</span>
        <NButton @click="emit('update:show', false)">
          {{ t("common.actions.cancel") }}
        </NButton>
        <NButton
          type="primary"
          :loading="files.acting"
          :disabled="!octalValid || !files.canWrite"
          @click="() => void submit()"
        >
          {{ t("files.permissions.apply") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.stack {
  display: flex;
  flex-direction: column;
}

.gap-4 {
  gap: var(--space-4);
}

.path {
  margin: 0;
  font-family: var(--font-mono);
}

.octal {
  max-width: 120px;
}

.hint {
  margin: 0;
}

.perms-grid {
  display: grid;
  grid-template-columns: max-content repeat(3, 1fr);
  align-items: center;
  gap: var(--space-2) var(--space-3);
  margin-top: var(--space-2);
}

.perms-head {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--muted);
}

.perms-row {
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.perms-grid .check {
  justify-content: center;
}

.dialog-foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.grow {
  flex: 1 1 auto;
}
</style>
