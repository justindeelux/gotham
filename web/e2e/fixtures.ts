import { expect, test as base } from "@playwright/test";
import type { Page } from "@playwright/test";

/** Guardrail findings collected while a page is exercised. */
export interface Guardrails {
  /** Every console error (and uncaught page error) seen on the page. */
  consoleErrors: string[];
  /** Every /api/v1/* response with a 5xx status. */
  serverErrors: string[];
  /** Every /api/v1/* response with a 4xx status or a failed request. */
  apiFailures: string[];
}

/** watchGuardrails wires the console/network guardrail onto one page. */
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
}

/**
 * `test` extends the base test with a guardrail fixture that is asserted on
 * teardown: any console error or 5xx /api/v1/* response fails the test.
 * Scenarios that must also prove no 4xx requests can assert `apiFailures`.
 */
export const test = base.extend<GuardrailFixtures>({
  guardrails: async ({ page }, use) => {
    const guardrails = watchGuardrails(page);
    await use(guardrails);

    expect(
      guardrails.consoleErrors,
      `unexpected console errors:\n${guardrails.consoleErrors.join("\n")}`,
    ).toEqual([]);
    expect(
      guardrails.serverErrors,
      `unexpected 5xx responses:\n${guardrails.serverErrors.join("\n")}`,
    ).toEqual([]);
  },
});

export { expect };
