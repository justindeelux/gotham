import { describe, expect, it } from "vitest";
import { createI18n } from "vue-i18n";

import type { FirewallStatus } from "@/features/firewall/api/firewall";
import en from "@/features/firewall/locales/en";
import vi from "@/features/firewall/locales/vi";
import { firewallDetailText, firewallErrorText } from "@/features/firewall/utils/errors";
import type { Translate } from "@/features/firewall/utils/errors";

function translator(locale: "en" | "vi"): Translate {
  const i18n = createI18n({
    legacy: false,
    locale,
    messages: { en: { firewall: en }, vi: { firewall: vi } },
  });
  return ((key: string, params?: Record<string, string>) =>
    String(i18n.global.t(key, params ?? {}))) as Translate;
}

const tEn = translator("en");
const tVi = translator("vi");

// apiError is the shape the http client rejects with.
function apiError(code?: string, params?: Record<string, string>): unknown {
  return {
    status: 409,
    message: "the node refused the change",
    cause: { response: { data: { code, params } } },
  };
}

function statusOf(overrides: Partial<FirewallStatus>): FirewallStatus {
  return {
    backend: "ufw",
    available: true,
    enabled: true,
    manageable: false,
    ssh_ports: [22],
    agent_port: 9443,
    control_plane_port: 8000,
    ...overrides,
  };
}

describe("firewall error codes", () => {
  it("translates every known code in Vietnamese", () => {
    const codes = [
      "no_firewall",
      "helper_missing",
      "helper_failed",
      "backend_changed",
      "state_read_failed",
      "state_write_failed",
      "rule_not_found",
      "read_only_rule",
      "invalid_action",
      "invalid_direction",
      "invalid_protocol",
      "invalid_port",
      "invalid_source",
      "invalid_outbound_source",
      "invalid_rule",
      "invalid_request",
      "agent_unavailable",
      "agent_outdated",
    ];
    for (const code of codes) {
      const text = firewallErrorText(apiError(code), tVi, "fallback");
      const expected = vi.errors[code as keyof typeof vi.errors];
      expect(text, code).toBe(expected);
      expect(text, code).not.toBe("fallback");
    }
  });

  it("renders the escaped @ of the comment rule", () => {
    const text = firewallErrorText(apiError("invalid_comment"), tVi, "fallback");
    expect(text).toContain("@");
    expect(text).not.toContain("{'@'}");
  });

  it("fills the port and the translated access into a lockout refusal", () => {
    const text = firewallErrorText(
      apiError("would_lock_out", { port: "22", access: "agent" }),
      tVi,
      "fallback",
    );
    expect(text).toContain("22");
    expect(text).toContain(vi.access.agent);
    expect(text).not.toContain("{");
  });

  it("fills the backend into an unsupported backend detail", () => {
    const text = firewallDetailText(
      statusOf({
        detail: "This node uses nftables.",
        detail_code: "unsupported_backend",
        detail_params: { backend: "nftables" },
      }),
      tVi,
    );
    expect(text).toBe(vi.errors.unsupported_backend.replace("{backend}", "nftables"));
  });

  it("shows the server message for an unknown code", () => {
    expect(firewallErrorText(apiError("network_socket_broken"), tVi, "fallback")).toBe(
      "the node refused the change",
    );
  });

  it("shows the server message for an agent without codes", () => {
    expect(firewallErrorText(apiError(), tVi, "fallback")).toBe(
      "the node refused the change",
    );
  });

  it("keeps each surface readable when the server sends nothing", () => {
    expect(firewallErrorText({}, tVi, tVi("firewall.errors.toggle"))).toBe(
      vi.errors.toggle,
    );
    expect(firewallErrorText({}, tVi, tVi("firewall.errors.save"))).toBe(vi.errors.save);
    expect(firewallErrorText({}, tVi, tVi("firewall.errors.delete"))).toBe(
      vi.errors.delete,
    );
    expect(firewallErrorText({}, tVi, tVi("firewall.errors.load"))).toBe(vi.errors.load);
    expect(firewallDetailText(statusOf({}), tVi)).toBe(vi.status.unsupportedBody);
  });

  it("keeps the English catalog for the English UI", () => {
    expect(firewallErrorText(apiError("read_only_rule"), tEn, "fallback")).toBe(
      en.errors.read_only_rule,
    );
    expect(
      firewallDetailText(
        statusOf({ detail: "No supported firewall is installed on this node.", detail_code: "no_firewall" }),
        tEn,
      ),
    ).toBe(en.errors.no_firewall);
  });

  it("shows the raw status detail when a detail code is unknown", () => {
    expect(
      firewallDetailText(
        statusOf({ detail: "A future backend says hello.", detail_code: "future_backend" }),
        tVi,
      ),
    ).toBe("A future backend says hello.");
  });
});
