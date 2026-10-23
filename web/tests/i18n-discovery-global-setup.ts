import { rmSync } from "node:fs";
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const webRoot = join(here, "..");
const probeDir = join(webRoot, "src", "features", "__i18n_probe__", "locales");

export default function setup(): () => void {
  rmSync(join(webRoot, "src", "features", "__i18n_probe__"), {
    recursive: true,
    force: true,
  });
  mkdirSync(probeDir, { recursive: true });
  writeFileSync(
    join(probeDir, "en.ts"),
    'const en = { hello: "Hello {name}", items: "No items | {count} items" };\n' +
      "export default en;\n",
  );
  writeFileSync(
    join(probeDir, "vi.ts"),
    'const vi = { hello: "Xin chào {name}", items: "Không có mục nào | {count} mục" };\n' +
      "export default vi;\n",
  );
  return () => {
    rmSync(join(webRoot, "src", "features", "__i18n_probe__"), {
      recursive: true,
      force: true,
    });
  };
}
