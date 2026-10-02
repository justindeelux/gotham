/**
 * SECRET_REF_PREFIX is the value prefix the API uses to mark a sealed secret
 * (`deploy.secretRefPrefix` in `internal/deploy/applications.go`).
 */
export const SECRET_REF_PREFIX = "secret:";

/**
 * isSecretValue reports a `secret:` reference to a sealed secret. It mirrors
 * the API's `strings.HasPrefix(entry.Value, "secret:")` exactly — deliberately
 * without trimming. Trimming would label a plaintext value such as
 * `" secret:x"` as sealed even though the API stores it as plaintext, so the
 * editor's badge would lie about what is on the server.
 */
export function isSecretValue(value: string): boolean {
  return value.startsWith(SECRET_REF_PREFIX);
}
