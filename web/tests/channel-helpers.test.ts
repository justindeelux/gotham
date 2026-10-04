import { describe, expect, it } from "vitest";

import {
  allowedEvents,
  buildConfig,
  configSummary,
  emptyChannelForm,
  keepDeliverableEvents,
  parseRecipients,
  resourceScopeInput,
  scopeLabel,
} from "@/features/notifications/utils/channelHelpers";
import type { NotificationChannel } from "@/features/notifications/api/notifications";

describe("channelHelpers", () => {
  it("scopes deliverable events by resource kind", () => {
    expect(allowedEvents("application")).toEqual(["deploy_success", "deploy_failure"]);
    expect(allowedEvents("database")).toEqual(["backup_success", "backup_failure"]);
    expect(allowedEvents("")).toEqual([
      "deploy_success",
      "deploy_failure",
      "backup_success",
      "backup_failure",
    ]);
  });

  it("narrows subscriptions to the scope and falls back to all deliverable", () => {
    expect(
      keepDeliverableEvents("application", ["deploy_success", "backup_success"]),
    ).toEqual(["deploy_success"]);
    expect(keepDeliverableEvents("database", ["deploy_success"])).toEqual([
      "backup_success",
      "backup_failure",
    ]);
  });

  it("splits recipient lists on commas and whitespace", () => {
    expect(parseRecipients("a@x.io, b@x.io\nc@x.io  ,")).toEqual([
      "a@x.io",
      "b@x.io",
      "c@x.io",
    ]);
    expect(parseRecipients("   ")).toEqual([]);
  });

  it("never resends an untouched masked secret", () => {
    const masked = { webhook_url: "https://…/••••", bot_token: "tok…", password: "pw…••" };
    const draft = {
      ...emptyChannelForm(),
      kind: "discord" as const,
      webhook_url: "https://…/••••",
    };
    expect(buildConfig(draft, masked)).toEqual({});
    expect(
      buildConfig({ ...draft, webhook_url: "https://new/hook" }, masked),
    ).toEqual({ webhook_url: "https://new/hook" });
  });

  it("builds email configs with parsed recipients and numeric ports", () => {
    const draft = {
      ...emptyChannelForm(),
      kind: "email" as const,
      host: "smtp.example.com",
      port: "587",
      to: "a@x.io, b@x.io",
    };
    const masked = { webhook_url: "", bot_token: "", password: "" };
    expect(buildConfig(draft, masked)).toEqual({
      host: "smtp.example.com",
      port: 587,
      to: ["a@x.io", "b@x.io"],
    });
    expect(
      buildConfig({ ...draft, port: "not-a-port" }, masked).port,
    ).toBeUndefined();
  });

  it("maps scope drafts onto the request pair", () => {
    expect(resourceScopeInput({ resourceType: "", resourceId: "" })).toEqual({
      resource_type: "",
      resource_id: "",
    });
    expect(
      resourceScopeInput({ resourceType: "application", resourceId: "app-1" }),
    ).toEqual({ resource_type: "application", resource_id: "app-1" });
  });

  it("summarizes routing facts without secrets", () => {
    const channel = {
      kind: "email",
      config: { host: "smtp.example.com", port: 587, to: ["a@x.io"] },
    } as NotificationChannel;
    expect(configSummary(channel)).toBe("smtp.example.com:587 → a@x.io");
  });

  it("falls back to a dash for a telegram channel without a chat id", () => {
    const withoutChat = { kind: "telegram", config: {} } as NotificationChannel;
    expect(configSummary(withoutChat)).toBe("chat —");
    const withChat = {
      kind: "telegram",
      config: { chat_id: "-100123" },
    } as NotificationChannel;
    expect(configSummary(withChat)).toBe("chat -100123");
  });

  it("labels scopes with resolved names and falls back to ids", () => {
    const teamWide = { resource_type: "", resource_id: "" } as NotificationChannel;
    expect(scopeLabel(teamWide, () => undefined)).toBe("Team-wide");
    const scoped = {
      resource_type: "application",
      resource_id: "app-1",
    } as NotificationChannel;
    expect(scopeLabel(scoped, () => "storefront")).toBe("App: storefront");
    expect(scopeLabel(scoped, () => undefined)).toBe("App: app-1");
  });
});
