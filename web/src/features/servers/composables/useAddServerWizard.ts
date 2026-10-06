// Wizard state machine for AddServerWizard (extracted from the component).
//
// Owns the connection form, its rules, the validation probes and the
// create/reset flow. The shell keeps the modal chrome, the step rail, the
// footer and the install step (the jus20 script check reads installCommand and
// its copy wiring from AddServerWizard.vue source, so those stay there).
// Step components read this context through WizardKey instead of prop
// drilling the whole form.

import type { FormInst, FormRules } from "naive-ui";
import { useMessage } from "naive-ui";
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import type { ComputedRef, InjectionKey, Ref } from "vue";
import { useI18n } from "vue-i18n";

import {
  createPrivateKey,
  describeServerError,
} from "@/features/servers/api/servers";
import type { CheckResult, Server, ServerCheckName } from "@/features/servers/api/servers";
import {
  connectionRules,
} from "@/features/servers/schemas/servers";
import { useServersStore } from "@/features/servers/stores/servers";
import { useInFlightGuard } from "@/shared/composables/useInFlightGuard";
import { onLocaleChange } from "@/shared/i18n";
import { formatBytes } from "@/shared/utils/format";

export interface ConnectionForm {
  name: string;
  ip: string;
  port: number | null;
  sshUser: string;
  authMode: "key" | "password";
  keyMode: "new" | "existing";
  keyName: string;
  privateKey: string;
  passphrase: string;
  keyId: string;
  password: string;
  trustHostKey: boolean;
}

/** Display state of one fixed validation check. */
export type CheckState = "idle" | "running" | "ok" | "fail";

export interface FixedCheck {
  name: ServerCheckName;
  label: string;
  state: CheckState;
  detail: string;
}

export interface AddServerWizardContext {
  step: Ref<number>;
  stepNames: ComputedRef<string[]>;
  form: ConnectionForm;
  formRef: Ref<FormInst | null>;
  setFormRef: (_instance: FormInst | null) => void;
  rules: ComputedRef<FormRules>;
  errorMessage: Ref<string>;
  validateMessage: Ref<string>;
  validationPassed: Ref<boolean>;
  fixedChecks: Ref<FixedCheck[]>;
  createdServer: Ref<Server | null>;
  creating: Ref<boolean>;
  validating: Ref<boolean>;
  hasCreatedServer: ComputedRef<boolean>;
  currentServer: ComputedRef<Server | null>;
  isReady: ComputedRef<boolean>;
  sshStatusLine: ComputedRef<string>;
  passedCount: ComputedRef<number>;
  checkSummary: ComputedRef<string>;
  hasRunChecks: ComputedRef<boolean>;
  nodeInitials: ComputedRef<string>;
  resourceSummary: ComputedRef<string>;
  handleCreate: () => Promise<void>;
  handleValidate: () => Promise<void>;
  closeWizard: () => void;
  handleShowChange: (_value: boolean) => void;
}

export const WizardKey: InjectionKey<AddServerWizardContext> = Symbol("add-server-wizard");

/** Fixed probe list — exactly the checks the validate API reports. */
const FIXED_CHECK_NAMES: ServerCheckName[] = ["docker", "cpu", "ram", "disk"];

type WizardEmit = {
  (_event: "update:show", _value: boolean): void;
  (_event: "created", _server: Server): void;
};

export function useAddServerWizard(emit: WizardEmit): AddServerWizardContext {
  const serversStore = useServersStore();
  const message = useMessage();
  const { t } = useI18n();

  const stepNames = computed<string[]>(() => [
    t("servers.wizard.stepConnect"),
    t("servers.wizard.stepValidate"),
    t("servers.wizard.stepInstall"),
    t("servers.wizard.stepFinish"),
  ]);

  const step = ref(0);
  const formRef = ref<FormInst | null>(null);
  const errorMessage = ref("");
  const validateMessage = ref("");
  /**
   * validationAttempted records that the connection form has been validated
   * at least once, so a language switch can refresh already-visible feedback
   * without ever surfacing errors on a pristine form.
   */
  const validationAttempted = ref(false);
  const validationPassed = ref(false);
  const fixedChecks = ref<FixedCheck[]>(makeIdleChecks());
  const createdServer = ref<Server | null>(null);

  // Invalidates in-flight create/validate responses when the wizard is reset or
  // closed, and clears their loading flags, so a late answer cannot repopulate a
  // wizard the user already left or leave it stuck loading.
  const inFlight = useInFlightGuard();
  const creating = inFlight.creating;
  const validating = inFlight.validating;

  const form = reactive<ConnectionForm>({
    name: "",
    ip: "",
    port: 22,
    sshUser: "root",
    authMode: "key",
    keyMode: "new",
    keyName: "",
    privateKey: "",
    passphrase: "",
    keyId: "",
    password: "",
    trustHostKey: false,
  });

  const rules = computed<FormRules>(() => connectionRules(form));

  /** hasCreatedServer reports whether the connection step already registered a node. */
  const hasCreatedServer = computed<boolean>(() => createdServer.value !== null);

  /** currentServer prefers the polled store copy so status flips live. */
  const currentServer = computed<Server | null>(() => {
    const created = createdServer.value;
    if (!created) {
      return null;
    }
    return serversStore.servers.find((item) => item.id === created.id) ?? created;
  });

  const isReady = computed<boolean>(() => currentServer.value?.status === "ready");

  /** SSH status line for the validate step (connection facts only). */
  const sshStatusLine = computed<string>(() => {
    const server = currentServer.value;
    if (!server) {
      return "";
    }
    return `SSH ${server.ip}:${server.port} · user ${server.ssh_user}`;
  });

  const passedCount = computed<number>(
    () => fixedChecks.value.filter((check) => check.state === "ok").length,
  );

  /** checkLabel renders one fixed probe name in the current locale. */
  function checkLabel(name: ServerCheckName): string {
    switch (name) {
      case "docker":
        return t("servers.wizard.checkDocker");
      case "cpu":
        return t("servers.wizard.checkCpu");
      case "ram":
        return t("servers.wizard.checkRam");
      case "disk":
        return t("servers.wizard.checkDisk");
    }
  }

  /** checkSummary reports the real outcome — never a fabricated duration. */
  const checkSummary = computed<string>(() => {
    if (validating.value) {
      return t("servers.wizard.summaryRunning");
    }
    if (!hasRunChecks.value) {
      return t("servers.wizard.summaryIdle");
    }
    const total = fixedChecks.value.length;
    const passed = passedCount.value;
    if (validationPassed.value) {
      return t("servers.wizard.summaryPassed", { passed, total });
    }
    return t("servers.wizard.summaryFailed", { passed, total });
  });

  const hasRunChecks = computed<boolean>(
    () => fixedChecks.value.some((check) => check.state === "ok" || check.state === "fail"),
  );

  /** nodeInitials renders the avatar on the Finish step. */
  const nodeInitials = computed<string>(() => {
    const name = (currentServer.value?.name ?? form.name).trim();
    if (name === "") {
      return "··";
    }
    const compact = name.replace(/[^a-zA-Z0-9]/g, "");
    return (compact.slice(0, 2) || name.slice(0, 2)).toUpperCase();
  });

  const resourceSummary = computed<string>(() => {
    const server = currentServer.value;
    if (!server) {
      return "";
    }
    const parts: string[] = [];
    if (server.total_mem !== null) {
      parts.push(formatBytes(server.total_mem));
    }
    if (server.total_disk !== null) {
      parts.push(formatBytes(server.total_disk));
    }
    if (server.arch !== null && server.arch !== "") {
      parts.push(server.arch);
    }
    return parts.join(" · ");
  });

  // Entering the validate step kicks off the first probe automatically; the
  // Retry button repeats it on demand.
  watch(step, (value) => {
    if (value === 1 && createdServer.value && !hasRunChecks.value && !validating.value) {
      void handleValidate();
    }
  });

  // Editing the connection details invalidates a previous validation: a pass for
  // the old values must never unlock install for the new ones.
  const connectionSnapshot = computed<string>(() =>
    JSON.stringify([
      form.name,
      form.ip,
      form.port,
      form.sshUser,
      form.authMode,
      form.keyMode,
      form.keyName,
      form.privateKey,
      form.keyId,
      form.password !== "",
    ]),
  );
  watch(connectionSnapshot, () => {
    if (!createdServer.value) {
      return;
    }
    validationPassed.value = false;
    validateMessage.value = "";
    fixedChecks.value = makeIdleChecks();
  });

  /** makeIdleChecks returns the fixed probe list in the idle state. */
  function makeIdleChecks(): FixedCheck[] {
    return FIXED_CHECK_NAMES.map((name) => ({
      name,
      label: checkLabel(name),
      state: "idle" as CheckState,
      detail: t("servers.wizard.checkPending"),
    }));
  }

  /** applyCheckResults maps the API outcome onto the fixed check list. */
  function applyCheckResults(results: CheckResult[]): void {
    const byName = new Map(results.map((item) => [item.name, item]));
    fixedChecks.value = FIXED_CHECK_NAMES.map((name) => {
      const result = byName.get(name);
      if (!result) {
        return { name, label: checkLabel(name), state: "idle" as CheckState, detail: t("servers.wizard.checkPending") };
      }
      return {
        name,
        label: checkLabel(name),
        state: (result.ok ? "ok" : "fail") as CheckState,
        detail: result.detail,
      };
    });
  }

  /**
   * relabelChecks refreshes the fixed probe labels after a language switch.
   * States and server-reported details are preserved; only the static probe
   * names and the idle/running placeholders are re-rendered.
   */
  function relabelChecks(): void {
    fixedChecks.value = fixedChecks.value.map((check) => {
      let detail = check.detail;
      if (check.state === "idle") {
        detail = t("servers.wizard.checkPending");
      } else if (check.state === "running") {
        detail = t("servers.wizard.checkRunning");
      }
      return { ...check, label: checkLabel(check.name), detail };
    });
  }

  /** handleCreate optionally stores a key, then registers the server. */
  async function handleCreate(): Promise<void> {
    // Back from the validate step must not register the node twice: an already
    // created server just advances to validation again.
    if (createdServer.value) {
      step.value = 1;
      return;
    }

    errorMessage.value = "";
    validationAttempted.value = true;
    try {
      await formRef.value?.validate();
    } catch {
      return;
    }

    const token = inFlight.begin();
    creating.value = true;
    try {
      let keyId: string | null = null;
      let password: string | undefined;
      if (form.authMode === "password") {
        password = form.password;
      } else if (form.keyMode === "new") {
        const key = await createPrivateKey({
          name: form.keyName.trim(),
          private_key: form.privateKey,
        });
        keyId = key.id;
      } else {
        keyId = form.keyId.trim() || null;
      }

      const server = await serversStore.addServer({
        name: form.name.trim(),
        ip: form.ip.trim(),
        port: form.port ?? 22,
        ssh_user: form.sshUser.trim(),
        ssh_key_id: keyId,
        ...(password ? { password } : {}),
      });

      if (!inFlight.isCurrent(token)) {
        return; // the wizard was closed while the create was in flight
      }
      createdServer.value = server;
      emit("created", server);
      step.value = 1;
    } catch (error) {
      if (!inFlight.isCurrent(token)) {
        return;
      }
      errorMessage.value = describeServerError(error);
    } finally {
      if (inFlight.isCurrent(token)) {
        creating.value = false;
      }
    }
  }

  /** handleValidate runs the probe and stores the per-check results. */
  async function handleValidate(): Promise<void> {
    const server = createdServer.value;
    if (!server) {
      return;
    }

    const token = inFlight.begin();
    validating.value = true;
    validateMessage.value = "";
    // A retry starts from "not passed": a previous pass must never remain
    // visible (or unlock Continue) while the new probe runs or after it fails.
    validationPassed.value = false;
    fixedChecks.value = FIXED_CHECK_NAMES.map((name) => ({
      name,
      label: checkLabel(name),
      state: "running" as CheckState,
      detail: t("servers.wizard.checkRunning"),
    }));
    try {
      const outcome = await serversStore.validate(server.id, form.passphrase || undefined, {
        trustHostKey: form.trustHostKey,
      });
      if (!inFlight.isCurrent(token)) {
        return; // the wizard was closed while the probe was in flight
      }
      applyCheckResults(outcome.checks);
      validateMessage.value = outcome.message;
      validationPassed.value = outcome.ok;
      if (outcome.ok) {
        message.success(t("servers.toasts.validationOk"));
      }
    } catch (error) {
      if (!inFlight.isCurrent(token)) {
        return;
      }
      fixedChecks.value = makeIdleChecks();
      validateMessage.value = describeServerError(error);
      validationPassed.value = false;
    } finally {
      if (inFlight.isCurrent(token)) {
        validating.value = false;
      }
    }
  }

  /** closeWizard closes the modal and resets the wizard state. */
  function closeWizard(): void {
    emit("update:show", false);
    resetWizard();
  }

  /** handleShowChange mirrors the modal visibility and resets when closing. */
  function handleShowChange(value: boolean): void {
    emit("update:show", value);
    if (!value) {
      resetWizard();
    }
  }

  /** resetWizard returns every field to its initial value. */
  function resetWizard(): void {
    // Invalidate any in-flight create/validate and clear its loading flags, so a
    // reopen never inherits a stuck button from the request it abandoned.
    inFlight.reset();
    step.value = 0;
    form.name = "";
    form.ip = "";
    form.port = 22;
    form.sshUser = "root";
    form.authMode = "key";
    form.keyMode = "new";
    form.keyName = "";
    form.privateKey = "";
    form.passphrase = "";
    form.keyId = "";
    form.password = "";
    form.trustHostKey = false;
    errorMessage.value = "";
    validateMessage.value = "";
    validationPassed.value = false;
    validationAttempted.value = false;
    fixedChecks.value = makeIdleChecks();
    createdServer.value = null;
    formRef.value?.restoreValidation();
  }

  /** setFormRef binds the connection NForm instance owned by the step. */
  function setFormRef(instance: FormInst | null): void {
    formRef.value = instance;
  }

  /**
   * A language switch refreshes already-visible connection feedback without
   * touching the draft: probe labels re-render, and a previously validated
   * form re-validates (same visible set, translated). A pristine form is
   * never validated, so no errors surface on untouched fields.
   */
  const stopLocaleWatch = onLocaleChange(() => {
    relabelChecks();
    if (validationAttempted.value && formRef.value) {
      void formRef.value.validate().catch(() => {});
    }
  });
  onBeforeUnmount(stopLocaleWatch);

  return {
    step,
    stepNames,
    form,
    formRef,
    setFormRef,
    rules,
    errorMessage,
    validateMessage,
    validationPassed,
    fixedChecks,
    createdServer,
    creating,
    validating,
    hasCreatedServer,
    currentServer,
    isReady,
    sshStatusLine,
    passedCount,
    checkSummary,
    hasRunChecks,
    nodeInitials,
    resourceSummary,
    handleCreate,
    handleValidate,
    closeWizard,
    handleShowChange,
  };
}
