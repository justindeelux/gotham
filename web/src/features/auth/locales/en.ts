/**
 * English auth catalog (namespace `auth`): shell value props, sign-in /
 * registration / invite / OAuth copy, password-strength labels and the
 * validation message keys the auth schemas store. Only values are
 * translated in `vi.ts`; keys, params and interpolation stay identical.
 */
const en = {
  shell: {
    eyebrow: "Self-hosted PaaS",
    headline: "The control plane for your own infrastructure",
    lede: "One Go binary runs the control plane, a small agent on every node. No outside services, no layer whose source you cannot read.",
    footnote: "Self-hosted control plane · your infrastructure, your data.",
    selfHostedTitle: "Self-hosted on a VPS with 1 GB of RAM",
    selfHostedBody:
      "The control plane is a single Go binary with PostgreSQL and Redis; 1 vCPU and 1 GB of RAM is enough to start.",
    tlsTitle: "Control channel over server-authenticated gRPC TLS",
    tlsBody:
      "The agent drives the Docker Engine on each node; the control plane and the agent speak versioned protobuf over :9442, with certificates from the internal CA.",
    updatesTitle: "Signed Ed25519 updates with rollback",
    updatesBody:
      "The control plane and the agent self-update from GitHub Releases, verifying the signature before swapping the binary and keeping the old one for rollback.",
    enginesTitle: "4 build engines plus one-click templates",
    enginesBody:
      "Dockerfile, Railpack, Buildpacks, and static; the template library sets up WordPress, Nextcloud, n8n, or Uptime Kuma in one click.",
  },
  login: {
    switchLabel: "Sign in or create an account",
    title: "Sign in",
    subtitle:
      "Use your Gotham team account. Sessions use short-lived JWTs with rotating refresh tokens.",
    emailLabel: "Email",
    emailPlaceholder: "you{'@'}gotham.dev",
    passwordLabel: "Password",
    passwordPlaceholder: "Your password",
    submit: "Sign in",
    createAccount: "Create account",
    forgot: "Forgot password?",
    resetHint: "Password reset is not available yet.",
    divider: "or",
    oauthFailed: "GitHub sign-in failed. Please try again.",
  },
  register: {
    title: "Create account",
    firstAccount: "The first account on a new instance becomes the",
    ownerWord: "owner",
    ownerSuffix: "of the default team.",
    invitedPrefix: "You were invited to join",
    invitedFallback: "this team",
    invitedSuffix: "Choose your credentials to accept.",
    emailLabel: "Email",
    emailPlaceholder: "you{'@'}gotham.dev",
    passwordLabel: "Password",
    passwordPlaceholder: "At least 10 characters",
    passwordHint:
      "At least 10 characters with 2 character classes: lowercase, uppercase, digits, symbols.",
    confirmLabel: "Confirm password",
    confirmPlaceholder: "Repeat your password",
    terms: "I agree to the Terms of Use and to how this instance stores data.",
    submit: "Create account",
    divider: "or",
  },
  oauth: {
    githubSignin: "Sign in with GitHub",
    githubSignup: "Sign up with GitHub",
    signingIn: "Signing you in",
    failed: "Sign-in failed",
    inProgress: "Completing GitHub sign-in…",
    noSession: "GitHub did not return a usable session.",
    back: "Back to sign in",
  },
  invite: {
    title: "Team invite",
    joined: "You joined {name}.",
    openTeams: "Open teams",
    backToTeams: "Back to teams",
    noToken:
      "This link carries no invite token. Open the link from the invite exactly as it was shared.",
    hint: "Accepting uses the session you are signed in with — sign in with the invited address. Invites are single-use.",
  },
  footnote: {
    hashPrefix: "Passwords are hashed with",
    tokenMid:
      "· 15-minute JWT access tokens with 30-day rotating refresh tokens · GitHub OAuth via the",
    interfaceSuffix: "interface.",
  },
  strength: {
    level0: "Not entered",
    level1: "Very weak",
    level2: "Weak",
    level3: "Fair",
    level4: "Strong",
  },
  validation: {
    emailRequired: "Email is required",
    emailInvalid: "Enter a valid email address",
    passwordRequired: "Password is required",
    passwordPolicy:
      "Use at least 10 characters with 2 character classes (lowercase, uppercase, digits, symbols)",
    confirmRequired: "Please confirm your password",
    confirmMismatch: "Passwords do not match",
    termsRequired: "You must accept the terms to create an account",
  },
};

export default en;

/** AuthMessages is the shape every auth locale must satisfy. */
export type AuthMessages = typeof en;
