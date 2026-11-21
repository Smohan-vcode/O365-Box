# Upgrade: publisher-verified sign-in app

**Replaces:** the Microsoft Graph Command Line Tools client used for
onboarding sign-in (SPEC §18.2).

**Gives:** a consent screen that shows the product's own name and verified
publisher instead of Microsoft's command-line tools.

**Needs:** a multi-tenant public client app registration in the product
owner's tenant, publisher verification through a Microsoft Partner Network
account, and the same delegated scopes as `expected-state/bootstrap-scopes.yaml`.

**Changes:** `engine/internal/auth` only (the client ID and redirect URIs).
Nothing in providers, steps or configuration changes.

**Cost:** Microsoft Partner Network membership; no licence.
