/**
 * Compose helpers shared by the wizard Source step and the detail editor.
 * extractComposeServiceNames is a best-effort suggestion scan, not a YAML
 * parser: it collects the two-space-indented keys under a top-level
 * `services:` mapping (the overwhelmingly common compose style) so the web
 * service field can offer them. Anything unusual falls back to free text;
 * the server enforces membership either way.
 */
export function extractComposeServiceNames(content: string): string[] {
  const names: string[] = [];
  const lines = content.split("\n");
  let inServices = false;
  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed === "" || trimmed.startsWith("#")) {
      continue;
    }
    const indent = line.length - line.trimStart().length;
    if (!inServices) {
      if (indent === 0 && /^services\s*:\s*(#.*)?$/.test(trimmed)) {
        inServices = true;
      }
      continue;
    }
    // A following top-level mapping ends the services block; deeper keys
    // belong to a service body and are skipped.
    if (indent === 0) {
      break;
    }
    const keyMatch = /^([A-Za-z0-9][A-Za-z0-9_.-]*)\s*:/.exec(trimmed);
    if (keyMatch && indent === 2 && !names.includes(keyMatch[1])) {
      names.push(keyMatch[1]);
    }
  }
  return names;
}
