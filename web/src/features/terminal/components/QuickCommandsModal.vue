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
const bodyRef = ref<HTMLElement | null>(null);
const addButtonRef = ref<ComponentPublicInstance | null>(null);
const editorRef = ref<InstanceType<typeof QuickCommandEditor> | null>(null);

const editing = computed<QuickCommand | null>(
  () => store.commands.find((command) => command.id === editingId.value) ?? null,
);
const atLimit = computed<boolean>(() => store.commands.length >= QUICK_COMMANDS_MAX);

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

function close(): void {
  emit("update:show", false);
}

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

function buttonElement(instance: ComponentPublicInstance | null): HTMLElement | null {
  const element = instance?.$el;
  return element instanceof HTMLElement ? element : null;
}

async function restoreListFocus(commandId: string): Promise<void> {
  await nextTick();
  if (commandId !== "") {
    const row = [
      ...(bodyRef.value?.querySelectorAll<HTMLElement>("[data-term-quick-commands-edit]") ?? []),
    ].find((element) => element.dataset.termQuickCommandsEdit === commandId);
    if (row) {
      row.focus();
      return;
    }
  }
  buttonElement(addButtonRef.value)?.focus();
}

async function openEditor(): Promise<void> {
  showForm.value = true;
  await nextTick();
  const scroller = bodyRef.value?.closest(".n-card-content");
  if (scroller instanceof HTMLElement) {
    scroller.scrollTop = 0;
  }
}

function startAdd(): void {
  if (atLimit.value) {
    return;
  }
  editingId.value = "";
  formError.value = "";
  insertError.value = "";
  void openEditor();
}

function startEdit(command: QuickCommand): void {
  editingId.value = command.id;
  formError.value = "";
  insertError.value = "";
  void openEditor();
}

function resetForm(options: { restoreFocus?: boolean } = {}): void {
  const restoreId = editingId.value;
  showForm.value = false;
  editingId.value = "";
  formError.value = "";
  if (options.restoreFocus) {
    void restoreListFocus(restoreId);
  }
}

function saveFromFooter(): void {
  void editorRef.value?.submit();
}

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

function removeCommand(id: string): void {
  store.remove(id);
  if (editingId.value === id) {
    resetForm();
  }
}

watch(
  () => props.show,
  (visible) => {
    if (!visible) {
      resetForm();
      insertError.value = "";
    }
  },
);

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
      class="term-quick-commands-body"
      role="group"
      :aria-label="t('terminal.quickCommands.title')"
    >
      <template v-if="!showForm">
        <div class="term-quick-commands-summary">
          <span class="term-quick-commands-count">
            {{ t("terminal.quickCommands.count", { count: store.commands.length }) }}
          </span>
          <span class="term-quick-commands-note">
            {{ t("terminal.quickCommands.storageNote") }}
          </span>
        </div>

        <p v-if="store.saveFailed" class="term-quick-commands-alert" role="alert">
          {{ t("terminal.quickCommands.saveFailed") }}
        </p>

        <div v-if="store.commands.length === 0" class="term-quick-commands-empty">
          <p>{{ t("terminal.quickCommands.empty") }}</p>
          <p>
            {{
              t("terminal.quickCommands.emptyExample", {
                shortcut: "dcup",
              })
            }}
          </p>
        </div>
        <ul v-else class="term-quick-commands-list">
          <QuickCommandRow
            v-for="command in store.commands"
            :key="command.id"
            :command="command"
            @insert="insertCommand(command)"
            @edit="startEdit(command)"
            @delete="removeCommand(command.id)"
          />
        </ul>

        <p v-if="insertError" class="term-quick-commands-alert" role="alert">{{ insertError }}</p>

        <p v-if="atLimit" class="term-quick-commands-note">
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
      <div class="term-quick-commands-foot">
        <NButton
          v-if="!showForm"
          ref="addButtonRef"
          size="small"
          secondary
          class="term-quick-commands-add"
          :disabled="atLimit"
          @click="startAdd"
        >
          <template #icon>
            <Plus :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          {{ t("terminal.quickCommands.add") }}
        </NButton>
        <template v-else>
          <NButton
            size="small"
            type="primary"
            class="term-quick-commands-save"
            @click="saveFromFooter"
          >
            {{ t("terminal.quickCommands.save") }}
          </NButton>
          <NButton
            size="small"
            secondary
            class="term-quick-commands-cancel"
            @click="resetForm({ restoreFocus: true })"
          >
            {{ t("terminal.quickCommands.cancel") }}
          </NButton>
        </template>
        <span class="term-quick-commands-foot-spacer" />
        <NButton size="small" class="term-quick-commands-close" @click="close">
          {{ t("terminal.quickCommands.done") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.term-quick-commands-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  min-width: 0;
}

.term-quick-commands-summary {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-1) var(--space-2);
}

.term-quick-commands-count {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  color: var(--fg-2);
  font-size: var(--text-sm);
  font-weight: 600;
}

.term-quick-commands-note {
  margin: 0;
  color: var(--muted);
  font-size: var(--text-xs);
  line-height: 1.45;
}

.term-quick-commands-alert {
  margin: 0;
  color: var(--danger);
  font-size: var(--text-xs);
}

.term-quick-commands-empty {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  padding: var(--space-3) 0;
  color: var(--meta);
  font-size: var(--text-xs);
  text-align: center;
}

.term-quick-commands-empty p {
  margin: 0;
}

.term-quick-commands-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  margin: 0;
  padding: 0;
  list-style: none;
}

.term-quick-commands-foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.term-quick-commands-foot-spacer {
  flex: 1 1 auto;
}
</style>
