
import type { FormInst, FormRules } from "naive-ui";
import { useMessage } from "naive-ui";
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import type { ComputedRef, InjectionKey, Ref } from "vue";
import { useI18n } from "vue-i18n";

import {
  createPrivateKey,
  failureText,
  generatePrivateKey,
  listPrivateKeys,
} from "@/features/servers/api/servers";
import type {
  CheckResult,
  PrivateKeyMeta,
  Server,
  ServerCheckName,
} from "@/features/servers/api/servers";
import {
  connectionRules,
} from "@/features/servers/schemas/servers";
import { useServersStore } from "@/features/servers/stores/servers";
import { useDockerInstall } from "@/features/servers/composables/useDockerInstall";
import type { DockerInstallState } from "@/features/servers/composables/useDockerInstall";
import { generateKeyName } from "@/features/servers/utils/keyName";
import { useInFlightGuard } from "@/shared/composables/useInFlightGuard";
import { onLocaleChange } from "@/shared/i18n";
import { formatBytes } from "@/shared/utils/format";

export interface ConnectionForm {
  name: string;
  ip: string;
  port: number | null;
  sshUser: string;
  authMode: "managed" | "manual";
  keyId: string;
  privateKey: string;
  publicKey: string;
}

export type CheckState = "idle" | "running" | "ok" | "fail";

export interface FixedCheck {
  name: ServerCheckName;
  label: string;
  state: CheckState;
  detail: string;
  installable: boolean;
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
  privateKeys: Ref<PrivateKeyMeta[]>;
  keysLoading: Ref<boolean>;
  generatingKey: Ref<boolean>;
  keyErrorMessage: ComputedRef<string>;
  loadPrivateKeys: () => Promise<void>;
  generateKey: () => Promise<void>;
  keyInstallNeeded: Ref<boolean>;
  installPassword: Ref<string>;
  installingKey: Ref<boolean>;
  keyInstallError: Ref<string>;
  handleCreate: () => Promise<void>;
  handleValidate: () => Promise<void>;
  handleInstallKey: () => Promise<void>;
  closeWizard: () => void;
  handleShowChange: (_value: boolean) => void;
  dockerInstall: DockerInstallState;
  dockerInstallOpen: Ref<boolean>;
  dockerInstallOffer: ComputedRef<boolean>;
  openDockerInstall: () => void;
  closeDockerInstall: () => void;
  recheckAfterDockerInstall: () => Promise<void>;
}

export const WizardKey: InjectionKey<AddServerWizardContext> = Symbol("add-server-wizard");

const FIXED_CHECK_NAMES: ServerCheckName[] = ["docker", "cpu", "ram", "disk"];

type WizardEmit = {
  (_event: "update:show", _value: boolean): void;
  (_event: "created", _server: Server): void;
};

export function useAddServerWizard(emit: WizardEmit): AddServerWizardContext {
  const serversStore = useServersStore();
  const message = useMessage();
  const { locale, t } = useI18n();

  const stepNames = computed<string[]>(() => [
    t("servers.wizard.stepConnect"),
    t("servers.wizard.stepValidate"),
    t("servers.wizard.stepInstall"),
    t("servers.wizard.stepFinish"),
  ]);

  const step = ref(0);
  const formRef = ref<FormInst | null>(null);
  const createFailure = ref<unknown>(null);
  const probeFailure = ref<unknown>(null);
  const errorMessage = computed<string>(() =>
    createFailure.value === null || createFailure.value === undefined
      ? ""
      : failureText(createFailure.value, locale.value),
  );
  const validateMessage = computed<string>(() => {
    const failure = probeFailure.value;
    if (failure === null || failure === undefined || failure === "") {
      return "";
    }
    if (typeof failure === "string") {
      return failure;
    }
    return failureText(failure, locale.value);
  });
  const validationAttempted = ref(false);
  const validationPassed = ref(false);
  const fixedChecks = ref<FixedCheck[]>(makeIdleChecks());
  const createdServer = ref<Server | null>(null);

  const inFlight = useInFlightGuard();
  const creating = inFlight.creating;
  const validating = inFlight.validating;

  const form = reactive<ConnectionForm>({
    name: "",
    ip: "",
    port: 22,
    sshUser: "root",
    authMode: "managed",
    keyId: "",
    privateKey: "",
    publicKey: "",
  });

  const privateKeys = ref<PrivateKeyMeta[]>([]);
  const keysLoading = ref(false);
  const generatingKey = ref(false);
  const keyFailure = ref<unknown>(null);
  const keyErrorMessage = computed<string>(() =>
    keyFailure.value === null || keyFailure.value === undefined
      ? ""
      : failureText(keyFailure.value, locale.value),
  );
  const keyInstallNeeded = ref(false);
  const installPassword = ref("");
  const installingKey = ref(false);
  const keyInstallError = ref("");
  let manualKeyId = "";

  const rules = computed<FormRules>(() => connectionRules({ authMode: form.authMode }));

  async function loadPrivateKeys(): Promise<void> {
    keysLoading.value = true;
    keyFailure.value = null;
    try {
      privateKeys.value = await listPrivateKeys();
      if (form.keyId === "" && privateKeys.value.length > 0) {
        form.keyId = privateKeys.value[0].id;
      }
    } catch (error) {
      keyFailure.value = error;
    } finally {
      keysLoading.value = false;
    }
  }

  async function generateKey(): Promise<void> {
    generatingKey.value = true;
    keyFailure.value = null;
    try {
      const key = await generatePrivateKey({ name: generateKeyName(), type: "ed25519" });
      privateKeys.value = [key, ...privateKeys.value];
      form.authMode = "managed";
      form.keyId = key.id;
    } catch (error) {
      keyFailure.value = error;
    } finally {
      generatingKey.value = false;
    }
  }

  const dockerInstall = useDockerInstall({
    serverId: computed<string>(() => createdServer.value?.id ?? ""),
  });
  const dockerInstallOpen = ref(false);
  const dockerInstallOffer = computed<boolean>(() => {
    if (createdServer.value === null || validating.value) {
      return false;
    }
    const docker = fixedChecks.value.find((check) => check.name === "docker");
    return docker?.state === "fail" && docker.installable;
  });

  function openDockerInstall(): void {
    dockerInstallOpen.value = true;
    void dockerInstall.start();
  }

  function closeDockerInstall(): void {
    dockerInstallOpen.value = false;
    dockerInstall.stop();
  }

  async function recheckAfterDockerInstall(): Promise<void> {
    closeDockerInstall();
    await handleValidate();
  }

  const hasCreatedServer = computed<boolean>(() => createdServer.value !== null);

  const currentServer = computed<Server | null>(() => {
    const created = createdServer.value;
    if (!created) {
      return null;
    }
    return serversStore.servers.find((item) => item.id === created.id) ?? created;
  });

  const isReady = computed<boolean>(() => currentServer.value?.status === "ready");

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

  watch(step, (value) => {
    if (value === 1 && createdServer.value && !hasRunChecks.value && !validating.value) {
      void handleValidate();
    }
  });

  const connectionSnapshot = computed<string>(() =>
    JSON.stringify([
      form.name,
      form.ip,
      form.port,
      form.sshUser,
      form.authMode,
      form.keyId,
      form.privateKey,
      form.publicKey,
    ]),
  );
  watch(connectionSnapshot, () => {
    if (!createdServer.value) {
      return;
    }
    validationPassed.value = false;
    probeFailure.value = null;
    keyInstallNeeded.value = false;
    keyInstallError.value = "";
    fixedChecks.value = makeIdleChecks();
  });

  watch(
    () => [form.privateKey, form.publicKey],
    () => {
      manualKeyId = "";
    },
  );

  function makeIdleChecks(): FixedCheck[] {
    return FIXED_CHECK_NAMES.map((name) => ({
      name,
      label: checkLabel(name),
      state: "idle" as CheckState,
      detail: t("servers.wizard.checkPending"),
      installable: false,
    }));
  }

  function applyCheckResults(results: CheckResult[]): void {
    const byName = new Map(results.map((item) => [item.name, item]));
    fixedChecks.value = FIXED_CHECK_NAMES.map((name) => {
      const result = byName.get(name);
      if (!result) {
        return {
          name,
          label: checkLabel(name),
          state: "idle" as CheckState,
          detail: t("servers.wizard.checkPending"),
          installable: false,
        };
      }
      return {
        name,
        label: checkLabel(name),
        state: (result.ok ? "ok" : "fail") as CheckState,
        detail: result.detail,
        installable: result.installable === true,
      };
    });
  }

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

  async function handleCreate(): Promise<void> {
    if (createdServer.value) {
      step.value = 1;
      return;
    }

    createFailure.value = null;
    validationAttempted.value = true;
    try {
      await formRef.value?.validate();
    } catch {
      return;
    }

    const token = inFlight.begin();
    creating.value = true;
    try {
      let keyId = form.keyId.trim();
      if (form.authMode === "manual") {
        if (manualKeyId === "") {
          const key = await createPrivateKey({
            name: generateKeyName(),
            private_key: form.privateKey,
            public_key: form.publicKey,
          });
          manualKeyId = key.id;
          privateKeys.value = [key, ...privateKeys.value];
        }
        keyId = manualKeyId;
      }

      const server = await serversStore.addServer({
        name: form.name.trim(),
        ip: form.ip.trim(),
        port: form.port ?? 22,
        ssh_user: form.sshUser.trim(),
        ssh_key_id: keyId,
      });

      if (!inFlight.isCurrent(token)) {
        return;
      }
      createdServer.value = server;
      emit("created", server);
      step.value = 1;
    } catch (error) {
      if (!inFlight.isCurrent(token)) {
        return;
      }
      createFailure.value = error;
    } finally {
      if (inFlight.isCurrent(token)) {
        creating.value = false;
      }
    }
  }

  async function handleValidate(): Promise<void> {
    const server = createdServer.value;
    if (!server) {
      return;
    }

    const token = inFlight.begin();
    validating.value = true;
    probeFailure.value = null;
    validationPassed.value = false;
    keyInstallNeeded.value = false;
    keyInstallError.value = "";
    fixedChecks.value = FIXED_CHECK_NAMES.map((name) => ({
      name,
      label: checkLabel(name),
      state: "running" as CheckState,
      detail: t("servers.wizard.checkRunning"),
      installable: false,
    }));
    try {
      const outcome = await serversStore.validate(server.id);
      if (!inFlight.isCurrent(token)) {
        return;
      }
      applyCheckResults(outcome.checks);
      probeFailure.value = outcome.message;
      validationPassed.value = outcome.ok;
      keyInstallNeeded.value = !outcome.ok && outcome.authFailed;
      if (outcome.ok) {
        message.success(t("servers.toasts.validationOk"));
      }
    } catch (error) {
      if (!inFlight.isCurrent(token)) {
        return;
      }
      fixedChecks.value = makeIdleChecks();
      probeFailure.value = error;
      validationPassed.value = false;
    } finally {
      if (inFlight.isCurrent(token)) {
        validating.value = false;
      }
    }
  }

  async function handleInstallKey(): Promise<void> {
    const server = createdServer.value;
    if (!server || installPassword.value === "") {
      return;
    }

    const token = inFlight.begin();
    installingKey.value = true;
    keyInstallError.value = "";
    try {
      const outcome = await serversStore.installKey(server.id, installPassword.value);
      if (!inFlight.isCurrent(token)) {
        return;
      }
      applyCheckResults(outcome.checks);
      if (outcome.ok) {
        validationPassed.value = true;
        keyInstallNeeded.value = false;
        probeFailure.value = null;
        message.success(t("servers.toasts.validationOk"));
      } else if (outcome.authFailed) {
        keyInstallError.value = outcome.message || t("servers.wizard.keyInstallFailed");
      } else if (outcome.checks.length > 0) {
        // The key install worked and the re-check ran; show its real failures
        // (for example Docker missing) instead of asking for the password again.
        keyInstallNeeded.value = false;
        probeFailure.value = outcome.message;
      } else {
        keyInstallError.value = outcome.message || t("servers.wizard.keyInstallFailed");
      }
    } catch (error) {
      if (!inFlight.isCurrent(token)) {
        return;
      }
      keyInstallError.value = failureText(error, locale.value);
    } finally {
      // The password is for this one install attempt only.
      installPassword.value = "";
      if (inFlight.isCurrent(token)) {
        installingKey.value = false;
      }
    }
  }

  function closeWizard(): void {
    emit("update:show", false);
    resetWizard();
  }

  function handleShowChange(value: boolean): void {
    emit("update:show", value);
    if (!value) {
      resetWizard();
    }
  }

  function resetWizard(): void {
    inFlight.reset();
    step.value = 0;
    form.name = "";
    form.ip = "";
    form.port = 22;
    form.sshUser = "root";
    form.authMode = "managed";
    form.keyId = "";
    form.privateKey = "";
    form.publicKey = "";
    manualKeyId = "";
    installPassword.value = "";
    installingKey.value = false;
    keyInstallNeeded.value = false;
    keyInstallError.value = "";
    keyFailure.value = null;
    createFailure.value = null;
    probeFailure.value = null;
    validationPassed.value = false;
    validationAttempted.value = false;
    fixedChecks.value = makeIdleChecks();
    createdServer.value = null;
    dockerInstallOpen.value = false;
    dockerInstall.reset();
    formRef.value?.restoreValidation();
  }

  function setFormRef(instance: FormInst | null): void {
    formRef.value = instance;
  }

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
    privateKeys,
    keysLoading,
    generatingKey,
    keyErrorMessage,
    loadPrivateKeys,
    generateKey,
    keyInstallNeeded,
    installPassword,
    installingKey,
    keyInstallError,
    handleCreate,
    handleValidate,
    handleInstallKey,
    closeWizard,
    handleShowChange,
    dockerInstall,
    dockerInstallOpen,
    dockerInstallOffer,
    openDockerInstall,
    closeDockerInstall,
    recheckAfterDockerInstall,
  };
}
