import { describe, expect, it } from "vitest";

import { buildInstallCommand } from "@/features/servers/utils/installCommand";

describe("add server install command", () => {
  it("passes --no-file-manager when the checkbox is unchecked", () => {
    const command = buildInstallCommand(false);
    expect(command).toContain("--no-file-manager");
    expect(command).not.toContain("--file-manager");
  });

  it("passes --file-manager when the checkbox is checked", () => {
    const command = buildInstallCommand(true);
    expect(command).toMatch(/--full --file-manager/);
    expect(command).not.toContain("--no-file-manager");
  });
});
