/**
 * Catalog checks shared by the foundation test and sibling feature workers.
 * Catalogs are plain nested objects of ICU-free vue-i18n messages using
 * named parameters (`{name}`) and library pluralization (`one | other`).
 */

type Dict = Record<string, unknown>;

/** leafEntries flattens a nested catalog to dotted key/value pairs. */
export function leafEntries(dict: Dict, prefix = ""): Array<[string, unknown]> {
  const out: Array<[string, unknown]> = [];
  for (const [key, value] of Object.entries(dict)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (value !== null && typeof value === "object" && !Array.isArray(value)) {
      out.push(...leafEntries(value as Dict, path));
    } else {
      out.push([path, value]);
    }
  }
  return out;
}

/** messageParams collects the named `{param}` placeholders in a message. */
export function messageParams(message: string): string[] {
  const params = new Set<string>();
  for (const match of message.matchAll(/\{(\w+)\}/g)) {
    params.add(match[1]);
  }
  return [...params].sort();
}

/** hasBalancedSyntax rejects unbalanced braces and stray interpolation. */
export function hasBalancedSyntax(message: string): boolean {
  let depth = 0;
  for (const char of message) {
    if (char === "{") {
      depth += 1;
    } else if (char === "}") {
      depth -= 1;
      if (depth < 0) {
        return false;
      }
    }
  }
  return depth === 0;
}

export interface ParityIssue {
  key: string;
  problem:
    | "missing-in-vi"
    | "missing-in-en"
    | "empty"
    | "param-mismatch"
    | "bad-syntax";
  detail?: string;
}

/**
 * checkCatalogParity compares an English reference catalog against its
 * Vietnamese translation: equal leaf-key sets, nonempty translations,
 * matching named parameters, and balanced message syntax on both sides.
 * Returns every issue found; empty means the catalogs agree.
 */
export function checkCatalogParity(en: Dict, vi: Dict): ParityIssue[] {
  const issues: ParityIssue[] = [];
  const enLeaves = new Map(leafEntries(en));
  const viLeaves = new Map(leafEntries(vi));

  for (const [key, value] of enLeaves) {
    if (typeof value !== "string") {
      continue;
    }
    if (!hasBalancedSyntax(value)) {
      issues.push({ key, problem: "bad-syntax", detail: "en" });
    }
    if (!viLeaves.has(key)) {
      issues.push({ key, problem: "missing-in-vi" });
      continue;
    }
    const translated = viLeaves.get(key);
    if (typeof translated !== "string" || translated.length === 0) {
      issues.push({ key, problem: "empty" });
      continue;
    }
    if (!hasBalancedSyntax(translated)) {
      issues.push({ key, problem: "bad-syntax", detail: "vi" });
    }
    const enParams = messageParams(value).join(",");
    const viParams = messageParams(translated).join(",");
    if (enParams !== viParams) {
      issues.push({
        key,
        problem: "param-mismatch",
        detail: `en {${enParams}} vs vi {${viParams}}`,
      });
    }
  }

  for (const key of viLeaves.keys()) {
    if (!enLeaves.has(key)) {
      issues.push({ key, problem: "missing-in-en" });
    }
  }

  return issues;
}

/**
 * registerNamespace merges one namespaced feature catalog into a locale
 * tree, rejecting duplicate namespaces (two files claiming one namespace,
 * or a feature colliding with a shared top-level key such as `common`).
 */
export function registerNamespace(
  tree: Record<string, unknown>,
  namespace: string,
  dict: Record<string, unknown>,
): void {
  if (namespace in tree) {
    throw new Error(`duplicate i18n namespace: ${namespace}`);
  }
  tree[namespace] = dict;
}
