import { ref, watch } from "vue";

import {
  addFirewallRule,
  deleteFirewallRule,
  getFirewallStatus,
  listFirewallRules,
  setFirewallEnabled,
  updateFirewallRule,
} from "../api/firewall";
import type { FirewallRule, FirewallRuleInput, FirewallStatus } from "../api/firewall";
import { errorText } from "@/shared/api/errors";

export function useFirewall(serverId: () => string) {
  const status = ref<FirewallStatus | null>(null);
  const rules = ref<FirewallRule[]>([]);
  const loading = ref(false);
  const saving = ref(false);
  const loadError = ref("");
  let requestSeq = 0;

  async function load(): Promise<void> {
    const id = serverId();
    if (id === "") {
      return;
    }
    const seq = ++requestSeq;
    loading.value = true;
    loadError.value = "";
    try {
      const next = await getFirewallStatus(id);
      if (seq !== requestSeq || serverId() !== id) {
        return;
      }
      status.value = next;
      rules.value = next.manageable ? await listFirewallRules(id) : [];
    } catch (error) {
      if (seq !== requestSeq || serverId() !== id) {
        return;
      }
      status.value = null;
      rules.value = [];
      loadError.value = errorText(error);
    } finally {
      if (seq === requestSeq) {
        loading.value = false;
      }
    }
  }

  // Each mutation returns the full rule list, so the tab never shows a state
  // derived from a stale local edit.
  async function toggle(enabled: boolean): Promise<void> {
    saving.value = true;
    try {
      const state = await setFirewallEnabled(serverId(), enabled);
      status.value = state.status;
      rules.value = state.rules;
    } finally {
      saving.value = false;
    }
  }

  async function save(input: FirewallRuleInput, ruleId?: string): Promise<void> {
    saving.value = true;
    try {
      rules.value = ruleId
        ? await updateFirewallRule(serverId(), ruleId, input)
        : await addFirewallRule(serverId(), input);
    } finally {
      saving.value = false;
    }
  }

  async function remove(ruleId: string): Promise<void> {
    saving.value = true;
    try {
      rules.value = await deleteFirewallRule(serverId(), ruleId);
    } finally {
      saving.value = false;
    }
  }

  watch(serverId, () => {
    status.value = null;
    rules.value = [];
    void load();
  });

  return { status, rules, loading, saving, loadError, load, toggle, save, remove };
}
