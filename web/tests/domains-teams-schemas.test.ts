// Differential tests for the V9 domains sweep + teams migration (JUS-23).
// Domains: the sweep found no client-side field checks (the server owns
// redirect/certificate/provider validation), so the schemas pin exactly that
// acceptance — every draft the widgets can produce passes. Teams: the two
// disabled states pin the old trim-and-empty gating, including the kept
// non-check (any non-empty invite string passes, no email-format rule).
import { describe, expect, it } from "vitest";

import {
  isCertificateDraftValid,
  isProviderFormValid,
  isRedirectFormValid,
} from "@/features/domains/schemas/domains";
import {
  isInviteEmailValid,
  isTeamNameValid,
} from "@/features/teams/schemas/teams";

describe("domains schemas keep server-authoritative acceptance", () => {
  it("accepts every UI-producible certificate draft", () => {
    expect(
      isCertificateDraftValid({
        application_id: "",
        challenge: "http-01",
        dns_provider_id: "",
        wildcard: false,
        enabled: true,
      }),
    ).toBe(true);
    expect(
      isCertificateDraftValid({
        application_id: "app-1",
        challenge: "dns-01",
        dns_provider_id: "prov-1",
        wildcard: true,
        enabled: false,
      }),
    ).toBe(true);
  });
  it("accepts every UI-producible provider draft", () => {
    expect(
      isProviderFormValid({
        provider: "cloudflare",
        name: "",
        zones: [],
        credential: "",
        enabled: true,
      }),
    ).toBe(true);
    expect(
      isProviderFormValid({
        provider: "digitalocean",
        name: "prod",
        zones: ["example.com"],
        credential: "tok",
        enabled: false,
      }),
    ).toBe(true);
  });
  it("accepts every UI-producible redirect draft", () => {
    expect(
      isRedirectFormValid({
        application_id: "",
        source_domain: "",
        target_domain: "",
        code: 301,
        preserve_path: true,
        enabled: true,
      }),
    ).toBe(true);
    expect(
      isRedirectFormValid({
        application_id: "app-1",
        source_domain: "shop.example.com",
        target_domain: "storefront.example.com",
        code: 302,
        preserve_path: false,
        enabled: true,
      }),
    ).toBe(true);
  });
});

describe("teams gating matches the trim-and-empty disabled states", () => {
  it("team name gates like createName/renameName", () => {
    for (const value of ["", "   ", undefined, null]) {
      expect(isTeamNameValid(value)).toBe(false);
    }
    for (const value of ["Core", "  Core  ", "a"]) {
      expect(isTeamNameValid(value)).toBe(true);
    }
  });
  it("invite gates on non-empty only, with no format check", () => {
    for (const value of ["", "   ", undefined, null]) {
      expect(isInviteEmailValid(value)).toBe(false);
    }
    for (const value of ["name@example.com", "  name@example.com  ", "not-an-email"]) {
      expect(isInviteEmailValid(value)).toBe(true);
    }
  });
});
