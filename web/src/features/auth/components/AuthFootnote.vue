<template>
  <p class="auth-footnote">
    Passwords are hashed with <span class="mono">argon2id</span> · 15-minute
    JWT access tokens with 30-day rotating refresh tokens · GitHub OAuth
    via the <span class="mono">OAuthProvider</span> interface.
  </p>
</template>

<style scoped>
.auth-footnote {
  margin: 4px 0 0;
  font-size: var(--text-xs);
  color: var(--muted);
  text-align: center;
  line-height: 1.6;
}
</style>
