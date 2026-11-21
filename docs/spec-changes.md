# Proposed specification changes: guided onboarding engine

Status: APPROVED 2026-11-21 and applied to `docs/SPEC.md` v1.1. This file
is kept as the record of why the specification changed.

Decisions already taken by the owner:

| Date | Decision |
| --- | --- |
| 2026-11-21 | The orchestrator and terminal UI are written in Go (Bubble Tea). |
| 2026-11-21 | Every value the setup needs, including tenant ID, domains, addresses and legal disclaimers, is collected by the TUI when it runs. Nobody edits YAML by hand to get started. |
| 2026-11-21 | Before anything is applied, the user sees and can change what will be built: modules, hardening settings, sites, libraries, folders, groups, mailboxes and permissions. |
| 2026-11-21 | Company data is **not** stored in the repository. It is asked for at run time and stored in the company's own tenant. This replaces the earlier choice of "profiles in the repository". |
| 2026-11-21 | Jurisdictions are chosen in the onboarding: one place of registration, plus checkboxes for every place the company operates. |
| 2026-11-21 | The app handles first sign-in: it captures the tenant ID from the sign-in and continues. For a company without Microsoft 365, it guides the user through Microsoft's sign-up page, then signs in. |

## 0. Summary

The repository becomes a **product with no company in it**. Any company
installs it by running one command:

```
box
```

The TUI opens and asks whether the user already has Microsoft 365. It
signs them in, or guides them through Microsoft's sign-up page first. It
reads the tenant ID and the organisation's details from the sign-in, then
walks a non-technical owner through eight steps, pre-filling everything
Microsoft already knows. Answers are kept in an encrypted draft on the
user's machine until the user confirms. The build then creates the Admin
site first and stores the company's configuration **in the tenant**,
before building and hardening everything else. It stops only at the few
steps Microsoft does not let software do, and checks those steps itself.

Later runs sign in, load the configuration from the tenant, and open a
home screen. Scheduled jobs read the same configuration with the pipeline
app. Settings stay declared, versioned and checked for drift. The record
of truth moves from git to a version-controlled, admin-only library in the
tenant.

| ID | Change | Spec sections | Tasks |
| --- | --- | --- | --- |
| SC-01 | Engine-only repository; company configuration stored in the tenant | §1, §2 R1, §4, §9 | T0-01, new T0-06, T12 |
| SC-02 | Go orchestrator with PowerShell providers | §3, §4 | T0-02, T0-03 |
| SC-03 | Step contract and event stream | §2 R4–R6, §14 | T0-02, T7 |
| SC-04 | Onboarding TUI (eight steps) | new §18 | new T13 |
| SC-05 | Sign-in, sign-up and discovery | §3, §6, new §18 | T13-02 |
| SC-06 | Automated bootstrap from a Global Administrator | §3, §6, runbook M-02–M-04 | T0-04, T1-01 |
| SC-07 | Jurisdictions, legal disclaimers and mandatory disclosures | §8, §12, new §19 | T2-02, T4, new T13-05 |
| SC-08 | Hardening presets with per-setting overrides | §1, `baseline/` | T1-04, T1-05, T2-02, T7-02 |
| SC-09 | Structure and permission editor with guardrails | §7, §2 R8 | T3-01, T3-02, new T13-06 |
| SC-10 | Licence and capability detection | §3, §6 | T1-02, T1-04, T1-05 |
| SC-11 | DNS provider adapters | §3, schema | T2-03 |
| SC-12 | Secrets and local draft handling | §3 | T0-04, T0-05 |
| SC-13 | Human gates with automatic checks | §2 R5, `runbooks/` | all with human steps |
| SC-14 | New spikes S-05 to S-08 | §16 | new |
| SC-15 | Corrections to engine defaults | `defaults/`, `content/` | T9-01 |
| SC-16 | Revised track and task plan | §5 | `tasks/` |

---

## SC-01  Engine-only repository; company configuration stored in the tenant

**Change to §1.** Remove "The first deployment is described by
`company.yaml`, `directory/`, `sharepoint/` and `content/`." Replace it
with: "The repository contains the engine only. A company's configuration
is collected by the onboarding TUI and stored in that company's tenant."

**R1 becomes:** "No company value in the repository. `box check-literals`
fails on any value from the company's configuration (read from the tenant
with `--from-tenant`, or from a file with `--from-file`) and on anything
under `examples/` appearing outside `examples/`."

### What stays in the repository

```
engine/                  Go orchestrator, TUI, PowerShell providers
defaults/                roles, structure, controls, stages, checklists, policy texts
defaults/jurisdictions/  rules per jurisdiction (SC-07); no company values
baseline/presets/        hardening presets and the settings catalogue (SC-08)
schema/                  JSON schema for the configuration document
flows/                   flow catalogue and solution source
expected-state/          app permissions, bootstrap scopes, connector allow-list
examples/acme/           fictional configuration used by tests and the CI dry run
upgrades/  runbooks/  tasks/  spikes/  docs/
```

### Where company data lives

| Data | Store in the tenant | Who can change it |
| --- | --- | --- |
| Configuration document `box-config.yaml`: tenant, domains, modules, mail objects, users, groups, sites, libraries, folders, permissions, hardening overrides, jurisdictions, disclaimer texts | Admin site, library `Configuration`, major versions on, access `grp-admins` and the pipeline app only | The TUI, as the signed-in owner |
| Company data values (`company.*` keys: address, registration and tax numbers, telephone, representatives) | `Dictionary` list (§9), as today | The `owner` role, in the TUI or the list form |
| Legal pack Word files | Legal site, `Templates` library, folder `Source` | Uploaded through the TUI |
| Bracket confirmations (today's `brackets.csv`) | List `Bindings` (§9), with `confirmed` and `field` columns editable by `company-legal-admin` | The TUI's template screen or the list form |
| Run state and evidence | `Setup` list and the `Setup evidence` library (§14) | The orchestrator only |

The configuration document refers to `Dictionary` keys and does not copy
their values. Each value has one owner and one place.

**Change to §9.** "`Dictionary` is seeded from `company.yaml > data`"
becomes "seeded from the onboarding answers in step 8".

**Versioning and review.** Every save from the TUI creates a new version of
`box-config.yaml` and one `Events` item (R10), recording who changed what.
This replaces pull-request review of company changes. Engine changes are
still reviewed by pull request.

**Backup and portability.** `box config export` writes the document and
the dictionary values to an encrypted file. `box config import <file>`
pre-fills the wizard from it, for example to rebuild on a new tenant or to
move a test run to live.

### Migration of the first deployment's files

The repository is public. The first deployment's files (`company.yaml`,
`directory/`, `sharepoint/`, `content/`) are deleted from the repository
outright; generic defaults found among them move to `defaults/`.
`examples/acme/` replaces them with fictional values. The owner keeps
their own copy outside the repository, and `box config import --legacy
<dir>` can pre-fill the wizard from it.

---

## SC-02  Go orchestrator with PowerShell providers

**Change to §3.** Replace the row "CLI: `box`, PowerShell 7 module in
`engine/cli/`" with:

| Item | Decision |
| --- | --- |
| Orchestrator, CLI and TUI | Go, single static binary `box`; TUI built on Bubble Tea, Bubbles and Lip Gloss |
| Providers | PowerShell 7 modules in `engine/providers/<name>/`, one per Microsoft surface, tested with Pester |
| Provider process model | Each provider runs in its own `pwsh` process, started by the orchestrator, speaking JSON lines on stdin/stdout |
| Prerequisites check | `box doctor` checks for pwsh 7.4+, the four modules at pinned versions, and the Power Platform CLI. It offers to install whatever is missing |

**New layout under `engine/`:**

```
engine/
  cmd/box/                main package
  internal/
    auth/                 sign-in, token handling, tenant discovery (SC-05)
    config/               configuration document: schema, load/save to the tenant, local draft
    plan/                 step graph, dependency resolution, diff aggregation
    run/                  executor, parallelism, retries, resume
    state/                run state, evidence index, Setup list writer
    events/               event types and JSON-lines codec
    gates/                human gates and their checks
    tui/                  Bubble Tea models: wizard, editors, plan view, run view, home
    providers/            process supervisor for pwsh workers
  providers/
    identity/  exchange/  sharepoint/  teams/  intune/  powerplatform/  dns/
  terraform/dns/          kept as the Cloudflare adapter's implementation (SC-11)
  register/lists.yaml
  templates/
```

**Why separate processes.** Microsoft.Graph, ExchangeOnlineManagement and
PnP.PowerShell ship different versions of the same identity libraries and
are known to conflict in one session. Separate processes avoid that and let
independent tracks (for example T2 and T3) run in parallel.

**Why Go.** One binary with no runtime to install, strong concurrency for
supervising the workers, and the best terminal UI toolkit available.

---

## SC-03  Step contract and event stream

**Change.** Every unit of work is a *step* with a fixed contract, carried
out by a provider:

| Operation | Meaning | Must be |
| --- | --- | --- |
| `test` | Can this step run here? Licences, prerequisites, permissions | read-only |
| `plan` | Declared state versus actual state, as a list of changes | read-only |
| `apply` | Make the changes in the plan | repeatable (R4) |
| `verify` | Re-read and confirm the declared state holds | read-only |
| `evidence` | Describe what was done and where the proof is | read-only |

`box drift` is `plan` on every step with nothing applied. That replaces the
separate drift logic in T7-02, except for what the Tenant Configuration
Management monitors cover (S-02).

**Step manifest.** Each provider declares its steps in
`engine/providers/<name>/steps.yaml`: id, track, depends_on, the
configuration paths it reads, the permissions and capabilities it needs,
and whether a gate precedes it.

**Event stream.** The orchestrator emits one JSON object per line:

```json
{"t":"2026-11-21T14:02:11Z","run":"r-0007","step":"sharepoint.library","ev":"step.progress","msg":"created library 7 of 9","done":7,"of":9}
```

Event kinds: `run.started`, `step.planned`, `step.started`,
`step.progress`, `step.changed`, `gate.waiting`, `gate.passed`,
`step.done`, `step.failed`, `step.skipped`, `run.finished`. The TUI, the
CLI's `--json` output, the CI logs and the `Setup` list all read this one
stream. Front-ends hold no logic. Events never contain `never_store`
values or secrets.

**State.** Each step's last result, plan hash and evidence reference are
written to the `Setup` list. A local cache in the user's config directory
speeds up the TUI and is rebuilt from the tenant when missing. Raw logs go
to the Admin site's `Automation logs` library. `setup-status.json` (T0-02)
is dropped.

**Before the Admin site exists** (the start of step 8), state is held in
the encrypted local draft (SC-12). It is written to the `Setup` list as
soon as that list exists.

**Resume.** Running `box up` again skips steps whose plan is empty and
whose verify passes. It continues from the first incomplete step.

---

## SC-04  Onboarding TUI

**New §18 "Onboarding".** `box` with no arguments opens the TUI. With no
configuration found, it starts the wizard. Every screen has the same
layout:

```
┌ Company in a Box ─────────────────────────────── Step 3 of 8 · Addresses ┐
│ ● Start  ● Company  ◉ Addresses  ○ Security  ○ Structure  ○ People       │
│ ○ Review ○ Build                                                          │
├───────────────────────────────────────────┬──────────────────────────────┤
│  Shared addresses                          │ What is this?                │
│  [x] info@     aliases: hello, contact     │ A shared address is a mail-  │
│  [x] support@  aliases: help   auto-reply  │ box several people can read  │
│  [x] sales@                                │ and reply from. It needs no  │
│  [ ] careers@                              │ paid licence.                │
│  [+] Add an address                        │                              │
│                                            │ Recommended for most         │
│                                            │ companies: info, support,    │
│                                            │ sales, billing, security,    │
│                                            │ privacy.                     │
├───────────────────────────────────────────┴──────────────────────────────┤
│ ↑↓ move · space toggle · enter edit · a add · ? help · ctrl+s save & quit │
└───────────────────────────────────────────────────────────────────────────┘
```

Principles for a non-technical user:

- **Ask only what Microsoft does not already know.** Discovered values are
  pre-filled and shown with a "from your Microsoft 365 account" tag. The
  user confirms or corrects them.
- **Plain language.** No Microsoft product names unless the user asks for
  details. Every field has a "What is this?" panel and a "Why we ask"
  line.
- **Safe defaults.** The recommended choice is preselected. A user who
  presses enter on every screen gets a sound, hardened company.
- **Nothing changes in Microsoft until step 8.** Steps 1 to 7 only sign in,
  read, and write the local draft.
- **Save and resume.** The draft is saved after every screen. Quitting and
  re-opening on the same machine returns to the same place.
- **Live validation.** Domain format, DNS lookups, licence counts and
  naming clashes are checked as the user types.
- **Every choice is recorded.** Turning off a recommended setting asks for
  a one-line reason, which is stored as a documented exception (evidence
  for SOC 2).

### The eight steps

| # | Step | Collects | Saved at step 8 to |
| --- | --- | --- | --- |
| 1 | Start | Existing Microsoft 365 or sign-up; sign-in; test or real company; confirmation of the discovered organisation (SC-05) | Configuration: `tenant` |
| 2 | Company | Legal name, trading name, entity wording, registered address, registration and tax numbers, telephone, domains (pre-filled from discovery); place of registration and operating jurisdictions (checkboxes); disclaimers and disclosures (SC-07) | Configuration: `company`, `jurisdictions`, `disclosures`; Dictionary: `company.*` |
| 3 | Addresses | Founder's address; shared mailboxes, aliases, distribution lists, auto-reply text, which group reads each; DMARC report address; sending subdomains; DNS provider and its token | Configuration: `mail`, `dns`; token to the keychain (SC-12) |
| 4 | Security | Hardening preset and per-setting overrides with reasons (SC-08); break-glass account names; device rules if the plan includes them | Configuration: `baseline`, `tenant.break_glass_accounts` |
| 5 | Structure | Modules to enable; sites, libraries, folders; groups; who can open what (SC-09); legal pack upload if the Legal documents module is on | Configuration: `modules`, `sites`, `libraries`, `groups`; Legal `Templates/Source` |
| 6 | People | Initial team: name, address, plan, roles, access groups. Existing users found by discovery are listed for adoption. Shows seats available per plan | Configuration: `users` |
| 7 | Review | The full plan as a tree with counts and every change; licence use; gates ahead; estimated time; exceptions recorded. The user types the primary domain to confirm | none |
| 8 | Build | Bootstrap (SC-06), then the Admin site and configuration store, then everything else; live run view, gates, final report | Tenant |

### Step 5: modules

| Module | Default | Includes |
| --- | --- | --- |
| Core (always on) | on | Identity, break-glass accounts, mail, DNS, hardening, configuration store, Setup list |
| Records | on | Hub, sites, libraries, register lists |
| People and roles | on | People list, role sync, joiner and leaver checklists |
| Legal documents | off unless a pack is uploaded | Template build, document flows, packages, notices |
| Sales | off | Inbound mailbox, leads, campaigns, orders |
| Policies | on | Policy rendering, publication, acknowledgement |
| Compliance | off | Controls, evidence collectors, recurring tasks; framework choice (SOC 2 to start) |
| Devices | on if the plan includes Intune | Enrolment, compliance, app protection |

A module that is off produces no steps.

### Step 8: build order

1. **Bootstrap** as the Global Administrator (SC-06): domains, pipeline
   app, break-glass accounts. Then the Global Administrator session ends.
2. **Configuration store**, as the pipeline app: hub, Admin site,
   `Configuration` library, `Setup`, `Events` and `Dictionary` lists.
   Upload `box-config.yaml`, seed the dictionary, copy run state from the
   local draft. Delete the local draft.
3. **Everything else** in dependency order, with independent tracks in
   parallel.

### Run view

```
┌ Tracks ──────────────┬ Sites and records ─────────────────────────┬ Waiting on you ──┐
│ ✔ Foundation         │ ✔ Hub site                      0.8s       │ ⏸ Security keys   │
│ ✔ Identity           │ ✔ 9 sites                      14.2s       │   admin-1 ✔       │
│ ◐ Mail and DNS   4/6 │ ◐ Libraries          ███████░░  7/9        │   admin-2 waiting │
│ ◐ Sites          2/4 │ · Register lists                queued     │   [o] open guide  │
│ · Templates          │ · Counterparty folders          queued     │                   │
│ ⏸ Flows (waiting)    │                                            │                   │
├──────────────────────┴────────────────────────────────────────────┴───────────────────┤
│ 14:02:11 sharepoint  created library 7 of 9                         [l] logs  [d] diff │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

Keys: expand a step to see its changes, retry a failed step, skip an
optional step with a reason, open a gate's guide, pause the run. When the
run ends, the TUI shows a summary and a list of what to do next. It also
writes a report to the Admin site.

### Later runs

`box` signs in, finds `box-config.yaml` in the tenant, and opens a home
screen: status, drift, pending gates, and "Change something". "Change
something" reopens the relevant editor, then plan, then apply, and saves a
new configuration version.

### Headless parity

Every TUI action has a CLI equivalent:

```
box                                   TUI: wizard or home screen
box plan | up | status | drift        --tenant <domain or id>, --json, --yes
box gate <id> [--check]
box config show | export | import <file> | validate
box evidence | doctor
box check-literals | check-connectors
```

Exit codes: 0 ok, 1 failure, 2 validation error, 3 waiting on a gate.

### Scheduled jobs

Hourly people sync and weekly drift and evidence (T0-05) run as GitHub
Actions in the company's own private copy of the engine repository. That
copy holds no company data. It holds only repository variables for the
tenant ID and the pipeline app's client ID, which are not secrets, plus
the federated credential trust (SC-06). Each job reads `box-config.yaml`
from the tenant with the pipeline app. `box handoff` sets this up at the
end of step 8.

---

## SC-05  Sign-in, sign-up and discovery

### First screen

```
Do you already have Microsoft 365 for your company?

  ▸ Yes, sign me in
    No, help me sign up
    I'm setting this up for someone else (I have their admin sign-in)
```

**Yes.** The orchestrator signs in as a Global Administrator:

- Default: a browser sign-in (authorisation code with PKCE on a
  `localhost` redirect).
- Fallback: device code, used automatically when no browser is available
  (SSH, containers) or chosen with `--device-code`. Some tenants block
  device-code sign-in through Conditional Access. If that happens, the TUI
  says so and offers the browser option.

**No.** The app cannot create the tenant. Microsoft offers no public API
for a direct customer to do so; only Microsoft partners in the reseller
(CSP) programme can, through Partner Center. The app therefore:

1. explains which plan to choose and why (Business Premium recommended,
   SC-10), and what Microsoft will ask for;
2. opens Microsoft's sign-up page in the browser;
3. waits on a screen that says "When you have finished and can sign in to
   Microsoft 365, press Enter";
4. continues with the sign-in above.

### Sign-in client

The sign-in needs an app identity that every tenant accepts. Version 1
uses Microsoft's first-party *Microsoft Graph Command Line Tools* public
client. The Global Administrator consents once, at sign-in, to the
delegated scopes listed in the new file
`expected-state/bootstrap-scopes.yaml`:

```yaml
delegated:
  - Organization.Read.All
  - Domain.ReadWrite.All
  - Application.ReadWrite.All
  - AppRoleAssignment.ReadWrite.All
  - RoleManagement.ReadWrite.Directory
  - User.ReadWrite.All
  - Group.ReadWrite.All
  - Directory.Read.All
```

The session is used for discovery and bootstrap only (SC-06). A later
upgrade can register a publisher-verified multi-tenant "Company in a Box"
app, so the consent screen shows the product's name. That upgrade is
listed in `upgrades/` and changes only `engine/internal/auth`.

### Discovery

Right after sign-in, read-only:

| Read | Gives | Pre-fills |
| --- | --- | --- |
| Token claims | Tenant ID, signed-in admin's user ID and address | `tenant.tenant_id` (never typed by the user) |
| `GET /organization` | Organisation name, street, city, state, postcode, country, technical notification address, verified domains | Company step: legal name (suggested), registered address, country, place of registration (suggested from country and state) |
| `GET /domains` | Verified and unverified domains, the `onmicrosoft.com` domain | Primary domain choice; domains still to add |
| `GET /subscribedSkus` | Plans and seats | Capabilities (SC-10); People step seat counts |
| `GET /users`, `GET /groups` | Existing accounts and groups | Founder (the signed-in admin's name), adoption candidates |
| NS lookup of the primary domain | Current DNS host | DNS adapter (SC-11) |
| Security defaults and Conditional Access state | Whether the tenant is new or already configured | Brownfield warning; adopt, skip or stop choices |

The confirmation screen reads: "We found your Microsoft 365 organisation
**Contoso Ltd** (tenant 1a2b…, 3 Business Premium seats, domain
contoso.onmicrosoft.com). Is this the right one?" Choosing no signs out
and starts again.

**Test or real.** Still on step 1, the user chooses "This is a test run"
or "This is my real company" (`tenant.environment`). The working rule in
`CLAUDE.md` still applies: a task is built and verified on test before
live. The TUI shows a warning band throughout a live run.

---

## SC-06  Automated bootstrap from a Global Administrator

**Change.** Runbook steps M-02 (domain), M-03 (break-glass accounts,
except key registration) and M-04 (app registration and consent) become
one automated step group, `bootstrap`. It is the first part of step 8 and
uses the delegated session from SC-05:

1. Add the primary domain and any extra domains. Read each verification
   record, write it through the DNS adapter, and verify.
2. Create the pipeline app registration and its service principal.
3. Grant exactly the permissions in `expected-state/app-permissions.yaml`
   through app role assignments (programmatic admin consent). Assign the
   listed Entra roles to the service principal. Refuse anything under
   `never`.
4. Add credentials: a GitHub federated credential for the company's copy of
   the repository, and, for local runs, a certificate whose private key
   stays in the OS keychain (SC-12). No client secret is ever created.
5. Create the break-glass accounts and the `grp-break-glass` exclusion
   group. Show the generated passwords once, with a print option. Open the
   gate for FIDO2 key registration (SC-13).
6. Sign out of the Global Administrator session and discard its token.
   Every later step runs as the pipeline app. Power Platform steps use the
   flow owner's sign-in (SC-13).

**§6 addition.** "The Global Administrator's delegated session is used
only for discovery and the `bootstrap` step group. It is never written to
disk."

**Later runs** sign in with the owner's own account, which needs no
directory role, to load the configuration and show the home screen. Apply
runs as the pipeline app. A new Global Administrator sign-in is needed
only to change the pipeline app's permissions, which also requires a
change to `expected-state/app-permissions.yaml` by pull request.

---

## SC-07  Jurisdictions, legal disclaimers and mandatory disclosures

### Jurisdictions in the Company step

Two questions:

```
Where is the company registered?            (choose one)
  ( ) United States ▸ Delaware    ( ) United Kingdom    ( ) Germany   ...

Where do you have customers, staff or data? (tick all that apply)
  [x] United States   [x] European Union   [ ] United Kingdom
  [x] Germany         [ ] France           [ ] Canada   [ ] Other…
```

The place of registration is suggested from the discovered country and
state. Each jurisdiction is an engine file
`defaults/jurisdictions/<code>.yaml`. These files hold rules, not company
values:

```yaml
code: gb
name: United Kingdom
correspondence:            # what must appear on emails, letters, order forms
  required: [legal_name, registration_number, place_of_registration, registered_office]
privacy:
  regime: uk_gdpr
  representative_required_if_outside: true      # asks for company.uk_representative.*
  notices: [PS-PRIV]                            # legal pack templates switched on
retention_hints: {tax: 6y}
```

Effects of the answers:

- The **place of registration** sets the fields required on correspondence
  and pre-fills a draft email disclaimer and letterhead that already
  contain them.
- Each **operating jurisdiction** adds only the fields it needs, such as
  an EU or UK representative's name and address. It also switches on the
  legal pack notices it requires and adds retention hints to the review.
  Nothing is asked for a place the user did not tick.
- "Other…" adds a generic file with nothing marked required. The user is
  told to check local requirements with counsel.

Stored in the configuration as
`jurisdictions: {registered: us-tx, operating: [us, eu, de]}`.

### Disclosures

| Disclosure | Captured as | Applied by | Licence |
| --- | --- | --- | --- |
| Email footer disclaimer for external recipients | Text and HTML with a live preview; placeholders such as `{company.legal_name}`; required items from the jurisdiction marked and checked | Exchange transport rule "apply disclaimer", outbound to external recipients; fallback "wrap" | Included |
| Internal email footer (optional) | Text | Second transport rule scoped to internal recipients | Included |
| Document footer for generated documents | Text with placeholders | Dictionary constant `company.footer.document`, bound into template footers by T4 (S-01 already tests footers) | Included |
| Letterhead block | Generated from company data and jurisdiction rules | Dictionary constants used by templates | Included |
| Meeting recording notice | Text | Teams meeting policy and recording consent setting | Included |
| Auto-reply legal lines (for example privacy response deadlines) | Text appended to each auto-reply | Shared mailbox automatic replies | Included |
| Published notices (privacy, imprint) | Legal pack `published` templates switched on by jurisdiction | Notices flow (F12) | Included |

All texts are stored in the configuration document under `disclosures`.
Placeholders are rendered from the `Dictionary` at apply time, so a change
of address updates every disclaimer on the next apply. The Exchange
provider's plan includes the disclaimer rules, so a change made in the
admin centre is reported as drift.

---

## SC-08  Hardening presets with per-setting overrides

**Change.** `baseline/*.yaml` becomes three engine presets and a
settings catalogue. Each company's choice is stored in its configuration.

```
baseline/
  presets/essential.yaml     minimum safe settings; any plan
  presets/recommended.yaml   current baseline files, merged; default
  presets/strict.yaml        adds: compliant device enforced (not report-only),
                             anonymous meeting join off, external federation
                             allow-list only, 4-hour admin sign-in frequency
  catalogue.yaml             every setting: id, plain-language title, what it
                             protects against, what users will notice,
                             required capability, SOC 2 criterion it supports
```

In the configuration document:

```yaml
baseline:
  preset: recommended
  overrides:
    - {id: teams.meetings.anonymous_join, value: off, reason: "Board meetings only"}
```

The effective baseline is the preset with the overrides applied. In step 4
the TUI shows each setting grouped by area (sign-in, email, sharing,
meetings, devices) with its catalogue text. It marks settings the plan
cannot apply (SC-10). Weakening a setting relative to `recommended`
requires a reason. Every such exception is listed on the Review screen and
in evidence.

**Fixed, not overridable:** break-glass accounts are excluded from
Conditional Access and never subject to it; there is no standing Global
Administrator other than break-glass; legacy authentication is blocked;
the pipeline app never has client secrets. These are rules, not settings.

---

## SC-09  Structure and permission editor with guardrails

**Change to §7.** The nine sites and their libraries become the
*recommended structure*, shipped as `defaults/structure.yaml`. The TUI's
step 5 starts from it, and the user can:

- add, rename or remove sites (the Admin site cannot be removed: the
  register and configuration live there);
- add libraries and nested folders under any site;
- add access groups;
- set access in a grid of groups by site or library, with the levels
  none, read (visitor), edit (member) and manage (owner);
- turn external sharing on for a site, which requires a reason.

```
┌ Who can open what ─────────────────────────────────────────────────────┐
│               Staff   Sales   Legal   Finance  Engineering  Admins     │
│ Corporate     —       —       —       —        —            manage     │
│ Legal         —       —       manage  —        —            —          │
│  └ Templates  —       read    manage  —        —            —          │
│ Finance       —       —       —       manage   —            —          │
│ Sales         read    edit    —       —        —            —          │
│ + add site · + add library · + add folder · + add group                │
└────────────────────────────────────────────────────────────────────────┘
```

**Guardrails (R8):**

- Access is granted to groups only; the editor does not offer
  individuals.
- "Everyone" and "Everyone except external users" are never offered.
- Per-item permissions are not offered. Only the items §11 names
  (executed documents) get them, set by flows.
- A library can tighten its site's access. It can loosen it only with a
  reason.
- Removing a site or library that already holds files is refused. The
  user can archive it instead.

The result is stored in the configuration document under `sites`,
`libraries` (which gains `folders: [string | {name, folders}]`) and
`groups`.

---

## SC-10  Licence and capability detection

**Change.** Discovery (SC-05) reads the tenant's subscribed plans and maps
them to capabilities in `engine/capabilities.yaml`. Examples:
`conditional_access` (Entra ID P1), `intune`, `defender_office_p1`,
`exchange`, `sharepoint`, `power_automate_standard`.

Each step's manifest declares the capabilities it needs. If one is
missing:

- the step is replaced by its fallback where the catalogue names one. For
  example, Conditional Access policies fall back to Security Defaults on
  Business Standard;
- otherwise the step is skipped and listed on the Review screen with the
  upgrade that would enable it.

In step 6 the People screen shows seats available per plan and refuses to
assign more than exist. Paid licences are still never assigned unless the
configuration names them (unchanged rule).

**§3 change.** "Subscription: Microsoft 365 Business Premium for the
founder" becomes "Business Premium recommended; Business Basic and
Standard are supported with the fallbacks in `engine/capabilities.yaml`."

---

## SC-11  DNS provider adapters

**Change.** `dns.provider` accepts `cloudflare`, `route53`, `azure`,
`godaddy` and `manual`. Each adapter implements the step contract for
records. `manual` shows a records table on screen and saves it as a file,
then opens a gate that checks DNS until the records resolve.

Cloudflare stays on Terraform (the current `engine/terraform/dns/`), with
its state stored in the Admin site's `Configuration exports` library, not
in the repository. Other adapters call the provider's API from the DNS
provider module. Discovery preselects the adapter from the domain's NS
records.

---

## SC-12  Secrets and local draft handling

| Item | Collected | Stored | Never |
| --- | --- | --- | --- |
| Global Administrator session | Sign-in in step 1 | Memory only; discarded after bootstrap | On disk, in logs |
| Owner session on later runs | Sign-in | OS token cache, as the Microsoft identity library does by default | In the repository |
| Wizard draft (steps 1–7 answers, pre-build run state) | Every screen | User config directory, encrypted with AES-256-GCM; key in the OS keychain. Deleted after upload in step 8 | In the repository, unencrypted |
| Pipeline certificate private key | Generated in bootstrap | OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service) | In the repository or the tenant |
| DNS provider API token | Masked input in step 3 | OS keychain; GitHub Actions secret when `box handoff` runs | In the configuration |
| Inbound token (M-08) | Generated | OS keychain, GitHub secret, solution environment variable | In the configuration |
| Break-glass passwords | Generated | Shown once, printable; not stored | Anywhere |

A draft lives on one machine. `box config export` produces an encrypted
file to move an unfinished draft to another machine.

The configuration document stores only references, such as
`secrets: {dns_token: keychain:box/<tenant-id>/dns}`.
`box check-literals` also fails on anything in the repository that looks
like a token or private key.

---

## SC-13  Human gates with automatic checks

**Change to R5.** "A step with no API is a *gate*: a guide written in plain
language, an expected value, and a check the orchestrator runs to confirm
it. The run continues on its own when the check passes."

| Gate | Replaces | Check |
| --- | --- | --- |
| G-00 Microsoft 365 sign-up | M-01 (tenant) | A successful sign-in (SC-05) |
| G-01 Licences bought | M-01 (plan) | Subscribed plans cover the plans chosen in step 6 |
| G-02 Security keys registered | M-03 key part | Each break-glass account lists a FIDO2 method |
| G-03 Flow owner connections | M-06 | Every connection reference in the solution is connected |
| G-04 Public forms created | M-07 | The user pastes each form link; the orchestrator checks the form is reachable and records its ID in the configuration |
| G-05 Founder's standing admin role removed | M-05 | The founder holds no directory role; offered at the end of the run |
| G-06 Manual DNS records | new, for `dns.provider: manual` | Records resolve to the expected values |

`runbooks/00-bootstrap.md` is regenerated from the gate definitions so the
two cannot drift apart.

---

## SC-14  New spikes

| ID | Question | Gates |
| --- | --- | --- |
| S-05 | Can a Go orchestrator drive Graph, Exchange, PnP and Teams providers in separate pwsh processes over JSON lines, with tokens or certificates passed safely and start-up under 5 s per provider? | T0-02 |
| S-06 | With only a delegated Global Administrator session through the Graph Command Line Tools client, can bootstrap create the app, grant app role assignments, assign Entra roles, add a federated credential and certificate, and add and verify a domain, without the admin centre? | T0-04 |
| S-07 | Does an Exchange "apply disclaimer" transport rule render the HTML disclaimer correctly in Outlook desktop, web and mobile, for replies and forwards, without stacking on long threads? | T13-05 |
| S-08 | On a freshly signed-up tenant, which `organization` fields (street, city, state, postcode, country) are populated, and does the browser sign-in with PKCE on `localhost` work for the Graph Command Line Tools client on Windows, macOS and Linux? | T13-02 |

---

## SC-15  Corrections to engine defaults

Found while reviewing the package. They do not depend on the decisions
above.

1. `defaults/policies.yaml > enforced_by` still names Google Workspace
   mechanisms: "2-Step Verification", "Drive label", "shared-drive
   structure", "Vault rules", "cockpit approvals", "Meet settings".
   Replace them with the Microsoft 365 equivalents: Conditional Access
   MFA, site classification and sharing defaults, site and library
   structure, the retention upgrade, the Approvals flow and register, and
   Teams meeting policy.
2. `content/auto-replies/README.md` says auto-reply keys are in
   `directory/groups.yaml`. They are in `directory/mail.yaml`.
3. `schema/company.schema.json > dns.provider` allows only `cloudflare`.
   SC-11 supersedes this, and the schema becomes the configuration
   document's schema (SC-01).
4. `defaults/stages.yaml` puts T10 under "Policies approved" and T11 under
   "Compliance running". By §5, policies are T9, compliance is T10, and
   T11 is upgrades.

---

## SC-16  Revised track and task plan

T1 to T12 keep their content and become steps run by providers. Wherever
they read `company.yaml`, `directory/`, `sharepoint/` or `content/`, they
read the configuration document and the `Dictionary` instead. The
foundation is redone, and a new onboarding track is added.

| ID | Title | Depends on | Replaces |
| --- | --- | --- | --- |
| T0-01 | Configuration schema, `examples/acme/`, `box config validate`, `check-literals`, `check-connectors` (Go) | none | T0-01 |
| T0-02 | Orchestrator: step contract, manifests, graph, state, events, resume | T0-01, S-05 | T0-02 |
| T0-03 | Provider runner and `box doctor` | T0-02 | new |
| T0-04 | Sign-in, discovery and bootstrap step group; `box check identity` | T0-03, S-06, S-08 | T0-03 |
| T0-05 | Pipeline: validate and test on pull request; scheduled jobs reading the tenant configuration; `box handoff` | T0-04, T0-06 | T0-04 |
| T0-06 | Configuration store: Admin site bootstrap, `Configuration` library, load and save with versions and `Events`, encrypted local draft, export and import, legacy import | T0-02, T0-04 | new |
| T13-01 | TUI shell: layout, navigation, help panel, save and resume, theming | T0-02 | new |
| T13-02 | Step 1: start screen, sign-up guidance, sign-in, discovery confirmation, test or real | T13-01, T0-04 | new |
| T13-03 | Steps 2, 3 and 6: company details with pre-fill, addresses, people with seat counts | T13-02 | new |
| T13-04 | Step 4: hardening presets and overrides (SC-08) | T13-02 | new |
| T13-05 | Jurisdictions and disclosures: catalogue, checkboxes, editors, preview, Exchange rule (SC-07) | T13-03, S-07 | new |
| T13-06 | Step 5: modules, structure and permission editor, legal pack upload (SC-09) | T13-02 | new |
| T13-07 | Review screen and typed confirmation | T13-03 to T13-06 | new |
| T13-08 | Run view, gates panel, final report, home screen for later runs | T13-07, T0-05, T0-06 | new |

Order: T0-01, S-05, T0-02, T0-03, S-06 and S-08, T0-04, T0-06, then T13
in parallel with T1 to T3 as providers. Each provider is usable headless
before its TUI screen exists. T12-01 (second-company dry run) becomes a CI
job that runs `box plan` against `examples/acme/` with recorded tenant
responses.

### Answers from the owner (2026-11-21)

1. **Jurisdictions:** the first shipment covers the major markets listed
   in SPEC §19.1, as drafts until counsel reviews each file.
2. **First sign-in passes:** the owner chooses per person, either shown on
   screen or emailed to a private address (SPEC §18.9). The pipeline app
   gains `UserAuthenticationMethod.ReadWrite.All` to issue the passes.
   Mail goes from the owner's own mailbox; `Mail.Send` stays forbidden for
   the app.
3. **Repository visibility:** the repository is public. The first
   deployment's data is removed from it.

## Decision

| Change | Decision | Date | By |
| --- | --- | --- | --- |
| SC-01 to SC-16 | APPROVED, applied to SPEC v1.1 | 2026-11-21 | Owner |
