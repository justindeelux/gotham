import { expect, test as base } from "@playwright/test";
import type { Page } from "@playwright/test";

export interface Guardrails {
  consoleErrors: string[];
  serverErrors: string[];
  apiFailures: string[];
}

function watchGuardrails(page: Page): Guardrails {
  const guardrails: Guardrails = {
    consoleErrors: [],
    serverErrors: [],
    apiFailures: [],
  };

  page.on("console", (message) => {
    if (message.type() === "error") {
      guardrails.consoleErrors.push(message.text());
    }
  });
  page.on("pageerror", (error) => {
    guardrails.consoleErrors.push(`pageerror: ${error.message}`);
  });
  page.on("response", (response) => {
    if (!response.url().includes("/api/v1/")) {
      return;
    }
    const status = response.status();
    const line = `${status} ${response.request().method()} ${response.url()}`;
    if (status >= 500) {
      guardrails.serverErrors.push(line);
    } else if (status >= 400) {
      guardrails.apiFailures.push(line);
    }
  });
  page.on("requestfailed", (request) => {
    if (request.url().includes("/api/v1/")) {
      guardrails.apiFailures.push(
        `requestfailed ${request.method()} ${request.url()} (${request.failure()?.errorText ?? "unknown"})`,
      );
    }
  });

  return guardrails;
}

interface GuardrailFixtures {
  guardrails: Guardrails;
  expectedConsoleErrors: string[];
}

export const test = base.extend<GuardrailFixtures>({
  expectedConsoleErrors: [[], { option: true }],
  guardrails: async ({ page, expectedConsoleErrors }, use) => {
    const guardrails = watchGuardrails(page);
    await use(guardrails);

    const unexpectedConsoleErrors = guardrails.consoleErrors.filter(
      (message) =>
        !expectedConsoleErrors.some((allowed) => message.includes(allowed)),
    );
    expect(
      unexpectedConsoleErrors,
      `unexpected console errors:\n${unexpectedConsoleErrors.join("\n")}`,
    ).toEqual([]);
    expect(
      guardrails.serverErrors,
      `unexpected 5xx responses:\n${guardrails.serverErrors.join("\n")}`,
    ).toEqual([]);
  },
});

export { expect };
