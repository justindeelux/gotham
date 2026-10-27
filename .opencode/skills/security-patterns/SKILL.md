---
name: security-patterns
description: Use this skill when adding authentication, handling user input, working with secrets, creating API endpoints, or implementing payment/sensitive features. Provides comprehensive security checklist and patterns.
metadata:
  origin: ECC
---

# Security Patterns

Use these security checklists and patterns when implementing sensitive features.

## When to Activate

- Implementing authentication or authorization
- Handling user input or file uploads
- Creating new API endpoints
- Working with secrets or credentials
- Implementing payment features
- Storing or transmitting sensitive data
- Integrating third-party APIs

## Security Checklist

### 1. Secrets Management

#### FAIL: NEVER Do This

```typescript
const apiKey = 'sk-proj-xxxxx' // Hardcoded secret
const dbPassword = 'password123' // In source code
```

#### PASS: ALWAYS Do This

Keep secrets in server-side configuration or an encrypted secret store, never in a
Vue bundle. Installer/UI configuration should remain the primary setup path;
environment variables are an automation escape hatch.

```go
// cfg is the server configuration loaded through the existing configuration layer.
apiKey := cfg.APIKey
if apiKey == "" {
    return errors.New("API key not configured")
}
```

#### Verification Steps

- [ ] No hardcoded API keys, tokens, or passwords
- [ ] All secrets kept server-side, outside frontend build variables
- [ ] `.env.local` in .gitignore
- [ ] No secrets in git history
- [ ] Production secrets stored with restricted access

### 2. Input Validation

#### Always Validate User Input

```typescript
import { z } from "zod";

// Frontend validation; the Go server must validate independently.
const CreateUserSchema = z.object({
  email: z.email(),
  name: z.string().min(1).max(100),
  age: z.number().int().min(0).max(150),
});

const result = CreateUserSchema.safeParse(input);
if (!result.success) {
  // Show safe field errors; do not send invalid input to the API.
}
```

Keep feature schemas separate from Vue components and adapt form rules through
`ruleFrom`. On the Go server, bound the request body, decode it, validate fields,
and authorize the request before writing data.

#### File Upload Validation

Browser checks are only feedback. The Go server must independently enforce the stream
size limit and inspect uploaded content; MIME type and filename are client-controlled.

```typescript
function validateFileUpload(file: File) {
  // Size check (5MB max)
  const maxSize = 5 * 1024 * 1024
  if (file.size > maxSize) {
    throw new Error('File too large (max 5MB)')
  }

  // Type check
  const allowedTypes = ['image/jpeg', 'image/png', 'image/gif']
  if (!allowedTypes.includes(file.type)) {
    throw new Error('Invalid file type')
  }

  // Extension check
  const allowedExtensions = ['.jpg', '.jpeg', '.png', '.gif']
  const extension = file.name.toLowerCase().match(/\.[^.]+$/)?.[0]
  if (!extension || !allowedExtensions.includes(extension)) {
    throw new Error('Invalid file extension')
  }

  return true
}
```

#### Verification Steps

- [ ] All user inputs validated with schemas
- [ ] File uploads restricted (size, type, extension)
- [ ] No direct use of user input in queries
- [ ] Whitelist validation (not blacklist)
- [ ] Error messages don't leak sensitive info

### 3. SQL Injection Prevention

#### FAIL: NEVER Concatenate SQL

```go
// DANGEROUS: user input becomes SQL.
query := "SELECT id FROM users WHERE email = '" + userEmail + "'"
row := pool.QueryRow(ctx, query)
```

#### PASS: ALWAYS Use Parameterized Queries

```go
var id string
err := pool.QueryRow(ctx, "SELECT id FROM users WHERE email = $1", userEmail).Scan(&id)
if err != nil {
    return err
}
```

Use parameterized query sources and generated sqlc methods when available.

#### Verification Steps

- [ ] All database queries use parameterized queries
- [ ] No string concatenation in SQL
- [ ] pgx/sqlc query parameters used correctly

### 4. Authentication & Authorization

#### JWT Token Handling

Vue runs in an untrusted browser environment. A bearer token in browser storage can be
stolen through XSS; an HttpOnly cookie reduces that exposure but needs CSRF defenses.

Preserve the application's established token transport, refresh, and revocation
contract. Do not change it to a different storage scheme as an unrelated security fix.
Keep signing keys and refresh validation server-side, and never log tokens.

#### Authorization Checks

```go
// requester comes from authenticated middleware, not a request body.
if requester.Role != "admin" {
    http.Error(w, "Access denied", http.StatusForbidden)
    return
}
if err := service.DeleteUser(r.Context(), userID); err != nil {
    writeError(w, http.StatusInternalServerError, "internal_error", "Internal error")
    return
}
w.WriteHeader(http.StatusNoContent)
```

Check resource ownership/membership as well as roles where the operation requires it.

#### Verification Steps

- [ ] Token storage, refresh, and revocation match the established auth contract
- [ ] Authorization checks before sensitive operations
- [ ] Role-based access control implemented
- [ ] Session management secure

### 5. XSS Prevention

#### Sanitize HTML

Vue interpolation escapes untrusted text:

```vue
<script setup lang="ts">
defineProps<{ content: string }>();
</script>

<template>
  <div>{{ content }}</div>
</template>
```

Do not use `v-html` for logs, file content, or other untrusted text. If rich HTML is an
explicit requirement, sanitize it with an approved sanitizer before rendering.

#### Content Security Policy

Start strict and loosen only with a documented removal plan. Do not default to
`'unsafe-inline'` or `'unsafe-eval'`; they neutralize much of CSP's protection
and should be treated as temporary compatibility debt.

```go
w.Header().Set("Content-Security-Policy",
    "default-src 'self'; base-uri 'self'; object-src 'none'; "+
        "frame-ancestors 'none'; script-src 'self'; style-src 'self'; "+
        "img-src 'self' data: https:; font-src 'self'; connect-src 'self'")
```

Integrate the policy with Naive UI's style injection and any WebSocket connections.
Test the rendered SPA before tightening a policy; do not silently break required styles
or add broad exceptions without understanding them.

#### Verification Steps

- [ ] User-provided HTML sanitized
- [ ] CSP headers configured
- [ ] No unvalidated dynamic content rendering
- [ ] Vue's escaped interpolation used for untrusted text

### 6. CSRF Protection

#### CSRF Tokens

For cookie-authenticated state changes, use the application's CSRF verifier before
processing the operation. Validate the expected token and origin according to that
contract, and reject invalid requests with HTTP 403. A token sent by the browser is
not proof of validity by itself.

Bearer headers explicitly attached by the client have different CSRF behavior from
ambient cookies; apply the protection appropriate to the actual authentication path.

#### SameSite Cookies

```go
http.SetCookie(w, &http.Cookie{
    Name:     "session",
    Value:    sessionID,
    Path:     "/",
    HttpOnly: true,
    Secure:   true,
    SameSite: http.SameSiteStrictMode,
})
```

Choose SameSite behavior compatible with the authentication/redirect flow; do not
require a cookie design when the established API uses bearer authentication.

#### Verification Steps

- [ ] CSRF protection on cookie-authenticated state-changing operations
- [ ] Cookie SameSite and Secure attributes match the authentication flow
- [ ] The selected CSRF verification strategy is tested

### 7. Rate Limiting

#### API Rate Limiting

Use the Go service's existing limiter rather than adding Express middleware.

```go
if !limiter.Allow(requesterID) {
    w.Header().Set("Retry-After", "60")
    http.Error(w, "Too many requests", http.StatusTooManyRequests)
    return
}
// Process the allowed request.
```

For multiple processes, use an atomic Redis-backed limit when shared enforcement is
required. Handle limiter errors according to the endpoint's failure policy.

#### Expensive Operations

Set stricter limits for expensive searches, builds, uploads, or authentication attempts.
For example, an expensive search may allow 10 requests per minute while an ordinary
endpoint permits a higher rate. Key authenticated limits by the trusted principal;
derive IP limits only from the established trusted-proxy configuration.

#### Verification Steps

- [ ] Rate limiting on all API endpoints
- [ ] Stricter limits on expensive operations
- [ ] IP-based rate limiting
- [ ] User-based rate limiting (authenticated)

### 8. Sensitive Data Exposure

#### Logging

```go
// FAIL: logging passwords or bearer tokens.
slog.Info("login", "email", email, "password", password)

// PASS: record the operation and safe identifiers.
slog.Info("login", "user_id", userID)
```

#### Error Messages

```go
// FAIL: returning raw errors or stack traces to the client.
http.Error(w, err.Error(), http.StatusInternalServerError)

// PASS: safe response; log only diagnostics known not to contain secrets.
slog.Error("request failed", "operation", "create_user", "request_id", requestID)
http.Error(w, "An error occurred. Please try again.", http.StatusInternalServerError)
```

#### Verification Steps

- [ ] No passwords, tokens, or secrets in logs
- [ ] Error messages generic for users
- [ ] Detailed errors only in server logs
- [ ] No stack traces exposed to users

### 9. Dependency Security

#### Regular Updates

```bash
# Frontend dependencies (run from web/)
npm audit
npm outdated

# Go dependencies, when govulncheck is available
govulncheck ./...
```

Run Go tooling from the module root. Apply targeted fixes and inspect lockfile changes
rather than treating a broad dependency upgrade as a security check.

#### Lock Files

```bash
# ALWAYS commit lock files
git add web/package-lock.json go.mod go.sum

# Use in CI/CD for reproducible builds
npm --prefix web ci  # Instead of npm install
```

#### Verification Steps

- [ ] Dependencies up to date
- [ ] No known vulnerabilities (npm audit clean)
- [ ] Lock files committed
- [ ] Dependabot enabled on GitHub
- [ ] Regular security updates

## Security Testing

### Automated Security Tests

```typescript
import { expect, test } from "@playwright/test";

// Run against an isolated test server with no authenticated storage state.
test("requires authentication", async ({ request }) => {
  const response = await request.get("/api/protected");
  expect(response.status()).toBe(401);
});

test("requires admin role", async ({ request }) => {
  const response = await request.get("/api/admin", {
    headers: { Authorization: `Bearer ${userToken}` },
  });
  expect(response.status()).toBe(403);
});

test("rejects invalid input", async ({ request }) => {
  const response = await request.post("/api/users", {
    data: { email: "not-an-email" },
  });
  expect(response.status()).toBe(400);
});

test("enforces rate limits", async ({ request }) => {
  const responses = await Promise.all(
    Array.from({ length: 101 }, () => request.get("/api/endpoint")),
  );
  expect(responses.some((response) => response.status() === 429)).toBe(true);
});
```

The routes, token fixture, and limit above are illustrative. Use the equivalent Go
handler tests or Playwright fixtures for the actual endpoint and its configured limit.

## Pre-Deployment Security Checklist

Before ANY production deployment:

- [ ] **Secrets**: No hardcoded secrets; credentials remain server-side
- [ ] **Input Validation**: All user inputs validated
- [ ] **SQL Injection**: All queries parameterized
- [ ] **XSS**: User content sanitized
- [ ] **CSRF**: Protection enabled
- [ ] **Authentication**: Proper token handling
- [ ] **Authorization**: Role checks in place
- [ ] **Rate Limiting**: Enabled on all endpoints
- [ ] **HTTPS**: Enforced in production
- [ ] **Security Headers**: CSP, X-Frame-Options configured
- [ ] **Error Handling**: No sensitive data in errors
- [ ] **Logging**: No sensitive data logged
- [ ] **Dependencies**: Up to date, no vulnerabilities
- [ ] **CORS**: Properly configured
- [ ] **File Uploads**: Validated (size, type)

## Resources

- [OWASP Top 10](https://owasp.org/www-project-top-ten/)
- [Web Security Academy](https://portswigger.net/web-security)

---

**Remember**: Security is not optional. One vulnerability can compromise the entire platform. When in doubt, err on the side of caution.
