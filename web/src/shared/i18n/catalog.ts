/**
 * Catalog checks shared by the foundation test and sibling feature workers.
 * Catalogs are plain nested objects of vue-i18n messages using named
 * parameters (`{name}`) and library pluralization (`one | other`).
 *
 * Message validity is decided by the real vue-i18n compiler
 * (`@intlify/message-compiler`, the exact version backing our vue-i18n),
 * not by a hand-rolled grammar: a message the compiler rejects can never
 * ship, whatever characters it contains.
 */
import { baseCompile } from "@intlify/message-compiler";

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

/**
 * compileError returns null when the vue-i18n compiler accepts a message,
 * otherwise the compiler's error text. Literal braces, `@` and `|` must use
 * the documented escapes (`{'{'}`, `{'@'}`, `{'|'}`); nested `{{x}}` and bare
 * `@` links throw here exactly as they would at render time.
 */
export function compileError(message: string): string | null {
  try {
    baseCompile(message, {
      onError: (error) => {
        throw error;
      },
    });
    return null;
  } catch (error) {
    return error instanceof Error ? error.message : String(error);
  }
}

/**
 * pluralSegments splits a message on top-level `|` (plural separators),
 * ignoring pipes inside `{...}` groups such as a literal `{'|'}`.
 */
export function pluralSegments(message: string): string[] {
  const segments: string[] = [];
  let depth = 0;
  let current = "";
  for (const char of message) {
    if (char === "{") {
      depth += 1;
    } else if (char === "}") {
      depth = Math.max(0, depth - 1);
    }
    if (char === "|" && depth === 0) {
      segments.push(current);
      current = "";
    } else {
      current += char;
    }
  }
  segments.push(current);
  return segments;
}

export interface ParityIssue {
  key: string;
  problem:
    | "missing-in-vi"
    | "missing-in-en"
    | "empty"
    | "param-mismatch"
    | "plural-mismatch"
    | "bad-syntax";
  detail?: string;
}

/**
 * checkCatalogParity compares an English reference catalog against its
 * Vietnamese translation: equal leaf-key sets, nonempty translations,
 * matching named parameters, matching plural-segment counts, and
 * compiler-accepted message syntax on both sides. Returns every issue
 * found; empty means the catalogs agree.
 */
export function checkCatalogParity(en: Dict, vi: Dict): ParityIssue[] {
  const issues: ParityIssue[] = [];
  const enLeaves = new Map(leafEntries(en));
  const viLeaves = new Map(leafEntries(vi));

  for (const [key, value] of enLeaves) {
    if (typeof value !== "string") {
      continue;
    }
    const enError = compileError(value);
    if (enError !== null) {
      issues.push({ key, problem: "bad-syntax", detail: `en: ${enError}` });
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
    const viError = compileError(translated);
    if (viError !== null) {
      issues.push({ key, problem: "bad-syntax", detail: `vi: ${viError}` });
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
    const enSegments = pluralSegments(value).length;
    const viSegments = pluralSegments(translated).length;
    if (enSegments !== viSegments) {
      issues.push({
        key,
        problem: "plural-mismatch",
        detail: `en ${enSegments} vs vi ${viSegments} segments`,
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
 * tree, rejecting duplicate namespaces (two files claiming one namespace).
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
