<script setup lang="ts">
import { Plus } from "@lucide/vue";
import { NButton, NModal } from "naive-ui";
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import type { ComponentPublicInstance } from "vue";
import { useI18n } from "vue-i18n";

import QuickCommandEditor from "@/features/terminal/components/QuickCommandEditor.vue";
import QuickCommandRow from "@/features/terminal/components/QuickCommandRow.vue";
import { QUICK_COMMANDS_MAX } from "@/features/terminal/schemas/quickCommands";
import type { QuickCommand, QuickCommandDraft } from "@/features/terminal/schemas/quickCommands";
import { useQuickCommandsStore } from "@/features/terminal/stores/quickCommands";
import { useTerminalStore } from "@/features/terminal/stores/terminal";
import type { TerminalCommandFailure } from "@/features/terminal/transport/terminalRuntime";

/**
 * Quick-command manager modal (JUS-264). The list and the editor are mutually
 * exclusive views inside one bounded Naive UI modal: the header and footer stay
 * fixed and only the body scrolls (shared `.app-modal` contract). Add and Edit
 * swap the list for the editor, so the form is always the first thing in the
 * scrollable body and can never be appended below the fold (JUS-267). Inserting
 * writes the command to the active PTY without Enter, closes the modal and lets
 * the surface return focus to the terminal.
 */
interface Props {
  show: boolean;
  tabId: string;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
  inserted: [label: string];
  closed: [];
}>();
const { t } = useI18n();
const store = useQuickCommandsStore();
const terminal = useTerminalStore();

const showForm = ref(false);
const editingId = ref("");
const formError = ref("");
const insertError = ref("");
/** bodyRef is the modal's content root; the editor replaces the list there. */
const bodyRef = ref<HTMLElement | null>(null);
const addButtonRef = ref<ComponentPublicInstance | null>(null);
const editorRef = ref<InstanceType<typeof QuickCommandEditor> | null>(null);

const editing = computed<QuickCommand | null>(
  () => store.commands.find((command) => command.id === editingId.value) ?? null,
);
const atLimit = computed<boolean>(() => store.commands.length >= QUICK_COMMANDS_MAX);

/** failureText maps an insertion refusal onto localized guidance. */
function failureText(failure?: TerminalCommandFailure): string {
  switch (failure) {
    case "multiline-no-paste":
      return t("terminal.quickCommands.insertMultiline");
    case "control":
      return t("terminal.quickCommands.insertControl");
    case "empty":
      return t("terminal.quickCommands.insertEmpty");
    default:
      return t("terminal.quickCommands.insertUnavailable");
  }
}

/** close requests the modal to hide; after-leave owns focus restoration. */
function close(): void {
  emit("update:show", false);
}

/**
 * onWindowKeydown owns Escape while the modal is open. The editor gets the
 * first Escape (the half-typed draft is discarded explicitly); a second
 * Escape closes the modal. NModal's own close-on-esc is off so it cannot
 * discard an in-progress form in one keystroke.
 */
function onWindowKeydown(event: KeyboardEvent): void {
  if (!props.show || event.key !== "Escape") {
    return;
  }
  event.preventDefault();
  event.stopPropagation();
  if (showForm.value) {
    resetForm({ restoreFocus: true });
    return;
  }
  close();
}

/** buttonElement unwraps a Naive button instance to its real <button>. */
function buttonElement(instance: ComponentPublicInstance | null): HTMLElement | null {
  const element = instance?.$el;
  return element instanceof HTMLElement ? element : null;
}

/**
 * restoreListFocus puts the keyboard back where the editor was opened from:
 * Add for a new command, the edited row's Edit button otherwise. Focus is
 * restored after the list renders (JUS-267 fix round 1).
 */
async function restoreListFocus(commandId: string): Promise<void> {
  await nextTick();
  if (commandId !== "") {
    const row = [
      ...(bodyRef.value?.querySelectorAll<HTMLElement>("[data-qc-edit]") ?? []),
    ].find((element) => element.dataset.qcEdit === commandId);
    if (row) {
      row.focus();
      return;
    }
  }
  buttonElement(addButtonRef.value)?.focus();
}

/**
 * openEditor swaps the list for the editor. The editor is the modal's only
 * body view (never appended below the list), so it is visible without
 * scrolling whatever the list length or viewport height; nextTick resets the
 * shared scroll container so the Label field starts at the top (JUS-267).
 */
async function openEditor(): Promise<void> {
  showForm.value = true;
  await nextTick();
  const scroller = bodyRef.value?.closest(".n-card-content");
  if (scroller instanceof HTMLElement) {
    scroller.scrollTop = 0;
  }
}

/** startAdd opens a blank editor. */
function startAdd(): void {
  if (atLimit.value) {
    return;
  }
  editingId.value = "";
  formError.value = "";
  insertError.value = "";
  void openEditor();
}

/** startEdit opens the editor on one saved command. */
function startEdit(command: QuickCommand): void {
  editingId.value = command.id;
  formError.value = "";
  insertError.value = "";
  void openEditor();
}

/**
 * resetForm closes the editor without saving. Only the in-modal exits
 * (Cancel, Save, Escape-in-form) restore keyboard focus; closing the dialog
 * or reopening it must leave focus to the surface's own after-leave logic.
 */
function resetForm(options: { restoreFocus?: boolean } = {}): void {
  const restoreId = editingId.value;
  showForm.value = false;
  editingId.value = "";
  formError.value = "";
  if (options.restoreFocus) {
    void restoreListFocus(restoreId);
  }
}

/** saveFromFooter submits the pinned footer Save through the editor. */
function saveFromFooter(): void {
  void editorRef.value?.submit();
}

/** saveCommand persists a validated draft and updates the list immediately. */
function saveCommand(draft: QuickCommandDraft): void {
  if (editingId.value === "" && atLimit.value) {
    formError.value = t("terminal.quickCommands.atLimit");
    return;
  }
  const saved =
    editingId.value === ""
      ? store.add(draft) !== null
      : store.update(editingId.value, draft);
  if (!saved) {
    formError.value = t("terminal.quickCommands.saveRejected");
    return;
  }
  resetForm({ restoreFocus: true });
}

/** insertCommand puts the command on the prompt without running it. */
function insertCommand(command: QuickCommand): void {
  const result = terminal.insertCommand(props.tabId, command.command);
  if (!result.ok) {
    insertError.value = failureText(result.failure);
    return;
  }
  insertError.value = "";
  emit("inserted", command.label);
  close();
}

/** removeCommand deletes one saved command from this browser. */
function removeCommand(id: string): void {
  store.remove(id);
  if (editingId.value === id) {
    resetForm();
  }
}

// A reopened modal starts on the list, never on a stale editor.
watch(
  () => props.show,
  (visible) => {
    if (!visible) {
      resetForm();
      insertError.value = "";
    }
  },
);

// The capture listener only exists while the dialog is open, so Escape never
// leaks into the shell or another surface after the modal closes.
watch(
  () => props.show,
  (visible) => {
    if (typeof window === "undefined") {
      return;
    }
    if (visible) {
      window.addEventListener("keydown", onWindowKeydown, true);
      return;
    }
    window.removeEventListener("keydown", onWindowKeydown, true);
  },
  { immediate: true },
);

onBeforeUnmount(() => {
  if (typeof window !== "undefined") {
    window.removeEventListener("keydown", onWindowKeydown, true);
  }
});
</script>

<template>
  <NModal
    class="app-modal term-quick-commands"
    :show="props.show"
    preset="card"
    style="width: min(36rem, 94vw)"
    :title="t('terminal.quickCommands.title')"
    :mask-closable="false"
    :auto-focus="true"
    :trap-focus="true"
    :close-on-esc="false"
    @update:show="(value: boolean) => emit('update:show', value)"
    @after-leave="emit('closed')"
  >
    <div
      ref="bodyRef"
      class="qc"
      role="group"
      :aria-label="t('terminal.quickCommands.title')"
    >
      <template v-if="!showForm">
        <div class="qc-summary">
          <span class="qc-count">
            {{ t("terminal.quickCommands.count", { count: store.commands.length }) }}
          </span>
          <span class="qc-note">{{ t("terminal.quickCommands.storageNote") }}</span>
        </div>

        <p v-if="store.saveFailed" class="qc-alert" role="alert">
          {{ t("terminal.quickCommands.saveFailed") }}
        </p>

        <div v-if="store.commands.length === 0" class="qc-empty">
          <p>{{ t("terminal.quickCommands.empty") }}</p>
          <p>
            {{
              t("terminal.quickCommands.emptyExample", {
                shortcut: "dcup",
              })
            }}
          </p>
        </div>
        <ul v-else class="qc-list">
          <QuickCommandRow
            v-for="command in store.commands"
            :key="command.id"
            :command="command"
            @insert="insertCommand(command)"
            @edit="startEdit(command)"
            @delete="removeCommand(command.id)"
          />
        </ul>

        <p v-if="insertError" class="qc-alert" role="alert">{{ insertError }}</p>

        <p v-if="atLimit" class="qc-note">
          {{ t("terminal.quickCommands.atLimit") }}
        </p>
      </template>

      <QuickCommandEditor
        v-else
        ref="editorRef"
        :key="editingId === '' ? 'new' : editingId"
        :commands="store.commands"
        :editing="editing"
        :error="formError"
        @save="saveCommand"
      />
    </div>

    <template #footer>
      <div class="qc-foot">
        <NButton
          v-if="!showForm"
          ref="addButtonRef"
          size="small"
          secondary
          class="qc-add"
          :disabled="atLimit"
          @click="startAdd"
        >
          <template #icon>
            <Plus :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          {{ t("terminal.quickCommands.add") }}
        </NButton>
        <!-- The footer always keeps the mode actions visible: Add on the
             list, Save + Cancel in the editor, so a primary action is never
             below the fold on a short viewport (JUS-267). -->
        <template v-else>
          <NButton
            size="small"
            type="primary"
            class="qc-save"
            @click="saveFromFooter"
          >
            {{ t("terminal.quickCommands.save") }}
          </NButton>
          <NButton
            size="small"
            secondary
            class="qc-cancel"
            @click="resetForm({ restoreFocus: true })"
          >
            {{ t("terminal.quickCommands.cancel") }}
          </NButton>
        </template>
        <span class="qc-foot-spacer" />
        <NButton size="small" class="qc-close" @click="close">
          {{ t("terminal.quickCommands.done") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.qc {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  min-width: 0;
}

.qc-summary {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-1) var(--space-2);
}

.qc-count {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  color: var(--fg-2);
  font-size: var(--text-sm);
  font-weight: 600;
}

.qc-note {
  margin: 0;
  color: var(--muted);
  font-size: var(--text-xs);
  line-height: 1.45;
}

.qc-alert {
  margin: 0;
  color: var(--danger);
  font-size: var(--text-xs);
}

.qc-empty {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  padding: var(--space-3) 0;
  color: var(--meta);
  font-size: var(--text-xs);
  text-align: center;
}

.qc-empty p {
  margin: 0;
}

.qc-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  margin: 0;
  padding: 0;
  list-style: none;
}

.qc-foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.qc-foot-spacer {
  flex: 1 1 auto;
}
</style>
