# Proposed specification changes: guided onboarding engine

Status: SC-01 to SC-16 APPROVED 2026-11-21 and applied to `docs/SPEC.md`
v1.1; SC-19 APPROVED 2026-11-21 and applied to v1.2. This file is kept as
the record of why the specification changed.

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

*Superseded by SC-19 (SPEC v1.2): presets and overrides are replaced by
the policy tier and dated exceptions.*

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

## SC-17  SharePoint resource permission for the pipeline app (2026-11-21)

Found while building T0-04 and T3. PnP.PowerShell app-only sign-in with a
certificate calls SharePoint's own API. That API needs `Sites.FullControl.All`
on the **Office 365 SharePoint Online** resource; the Graph permission of
the same name does not cover it. `expected-state/app-permissions.yaml`
gains `sharepoint: ["Sites.FullControl.All"]`. The intent, full control of
the company's sites, is unchanged. Recorded here so the widening is
visible; the owner asked for the remaining work to be completed.

## SC-18  Directory settings permissions (approved 2026-11-21)

T1-05 declares directory settings in `baseline/identity.yaml > directory`
(app registration by users, user consent, guest invitations, admin consent
workflow). Applying them needs the application permissions
`Policy.ReadWrite.Authorization` and `Policy.ReadWrite.ConsentRequest`.
Approved by the owner on 2026-11-21 and added to
`expected-state/app-permissions.yaml`; granted by re-running `box bootstrap`
and applied by the step `identity.directory-settings`.

## SC-19  Policy sets (v1.1 package merge, 2026-11-21)

**Source.** An updated upstream package ("policy sets v1.1") added a
master policy catalogue to the v1.0 specification as its §18 "Policy
sets", with tasks T13-01 to T13-16, four register lists, a Python
resolver and generated per-company `baseline/` files. The owner asked for
it to be merged into this specification and backlog.

**What was merged.**

- SPEC new §23 "Policy sets": the catalogue in `policy/` (90 requirements
  in 14 domains; 333 settings across 12 admin areas; five frameworks: NIST
  CSF 2.0, NIS2 Article 21, ISO/IEC 27001:2022 Annex A, SOC 2, CIS
  Controls v8), staged rollout with audit first, add-on settings never
  applied, standards rendered from requirements, and the six
  data-protection settings groups in their order.
- `engine/register/lists.yaml`: lists `Frameworks`, `Requirements`,
  `PolicySettings`, `Exceptions` and enums `req_status`, `policy_tier`,
  with the source's columns. Purpose texts now describe this engine's
  flow (configuration in the tenant) instead of pull requests to
  `company.yaml`.
- Backlog: a new track **T14 Policy sets**, T14-01 to T14-16.

**Adapted to this design.**

| Upstream | Here |
| --- | --- |
| Selection in `company.yaml > policy`; the `Frameworks` list writes back by pull request | Selection in the configuration document's `policy` section (§9.1), edited in the TUI Security step or on the `Frameworks` list; a list change is saved as a new configuration version with one `Events` item and applied only when the owner runs the plan |
| `engine/policy/resolve.py` (Python) | `engine/internal/policy` (Go); `box policy validate`, `resolve`, `coverage`; `box policy check` became `validate` |
| Generated `baseline/` committed per company | Resolved at plan time, never committed; recorded in the tenant (`PolicySettings`, `Requirements`, coverage report in the Compliance site). The engine's own `baseline/` files are read until T14-04 moves each area, then removed |
| `box policy apply <centre>`, `box policy verify` | Provider steps `policy.<area>` run by the orchestrator with the pipeline app (§14): `box apply policy.<area>`, `box check policy.<area>` |
| Human steps "Apple push certificate", "review DLP matches" | Gates G-07 and G-08 (§22) |
| `expected-state/service-plans.yaml` | Service plans per capability key added to `engine/capabilities.yaml` (one plan map, R11) |
| Standards written to `content/policies/standards/` | Rendered to the Compliance site's `Policies` library |

**Hardening presets replaced by the policy tier.** The v1.1 presets
(`essential`, `recommended`, `strict`) and `baseline: {preset, overrides}`
(SC-08) are withdrawn. The configuration holds `policy: {frameworks, tier:
core|enhanced, rollout: {stage, audit_days, pilot_group}, exceptions:
[{id, reason, reference, ends}]}`. An override with a reason becomes an
exception with a reference and an end date; an expired exception fails
validation. The fixed rules (§20.2) and capabilities (§20.3) stay;
add-on settings are listed as not applied. The upstream schema spelled the
default tier `baseline` and the exception fields `requirement, reference,
expires`; this specification uses `core` (as the upstream README and
`requirements.yaml` do) and the fields above. The optional
`consumer_mail_domains` and `company_marker_terms` lists are kept for
data loss prevention (T14-11).

**Renumbering.** Upstream T13 clashes with this backlog's T13 (Onboarding
TUI). Upstream T13-01 to T13-16 become T14-01 to T14-16, same order and
titles. Dependencies mapped: upstream T0-01 to T0-01; upstream T0-03
(pipeline identity) to T0-04 (sign-in, discovery and bootstrap, which
creates the pipeline app); upstream T13-xx to T14-xx; T1-04, T2-02,
T3-01, T3-02, T3-03, T9-01, T10-01 and S-02 unchanged. T1-05 and T2-02
gain T14-03, as upstream; T1-04 gains T14-03 too, because it reads the
resolved device settings. T13-04 (Security screen) now offers frameworks,
tier, rollout and exceptions, and depends on T14-01.

**Not imported.** The package's `company.yaml`, `directory/`, `content/`
and `sharepoint/` hold a real company's data, and its coverage report was
generated for that company. None of it was copied (R1). The Python
resolver is replaced by the Go port.

**Open (not decided here).** An exception scoped to a user, group or
device, applied as an exclusion group, needs a `scope` field in
`policy.exceptions`; T14-14 stops for a proposal. T14-03 is expected to
need permissions or Entra roles not in `expected-state/app-permissions.yaml`
(for example for Purview, Defender for Office and Intune app protection);
each is proposed when found, not granted.

## SC-20  Configuration-monitor API (proposed, not granted)

Spike S-02: the Tenant Configuration Management APIs (Graph beta) monitor
and snapshot tenant configuration; they do not apply it. Adopting them
needs `ConfigurationMonitoring.ReadWrite.All` for the pipeline app and,
in a Global Administrator bootstrap sign-in, the service's own service
principal with read access to each workload. Until approved, drift is
monitored by `box drift`; `expected-state/baseline-coverage.yaml` records
the monitor resource type per setting where one is known, so adoption is a
data change.

## SC-21  Permissions, roles and gates to apply the policy settings (proposed, not granted)

T14-03 mapped all 333 settings (expected-state/setting-ids/*.json):
283 resolved to an exact API, 27 gates, 13 unresolved, 10 add-on settings
not applied. Applying them (T14-04 to T14-12) needs the following, none of
which is granted. Each line names how many settings depend on it.

| Need | Kind | Settings |
| --- | --- | --- |
| `DeviceManagementApps.ReadWrite.All` | Graph application permission | 16 (app protection: MOB-001, MOB-011 to MOB-025) |
| `DeviceManagementServiceConfig.ReadWrite.All` | Graph application permission | 7 (enrolment restrictions, Apple and Android enrolment, Autopilot, Defender connector; also the G-07 check) |
| `DeviceManagementScripts.ReadWrite.All` | Graph application permission | 3 (macOS scripts MAC-018, MAC-019, MAC-022) |
| `Policy.ReadWrite.DeviceConfiguration` | Graph application permission | 5 (device registration policy ENT-039 to ENT-042; LAPS switch for WIN-036) |
| `Directory.ReadWrite.All` | Graph application permission, broad | 4 (directory setting templates: ENT-026, ENT-027, ENT-034, TMS-014) |
| `Domain.ReadWrite.All` | Graph application permission | 2 (password expiry ENT-028, ADM-004; already "never" on the test tenant) |
| `Policy.ReadWrite.SecurityDefaults` | Graph application permission | 1 (ENT-018); also needed by the existing `identity.security-defaults` fallback step |
| `OrgSettings-AppsAndServices.ReadWrite.All`, `OrgSettings-Forms.ReadWrite.All`, `PeopleSettings.ReadWrite.All`, `Policy.ReadWrite.ApplicationConfiguration` | Graph application permissions | 1 each (ADM-001, ADM-005, ADM-011, ADM-003) |
| `Application.ReadWrite.All` | Graph application permission, high privilege | 1 (ADM-007); a gate with an automated read-only check is the alternative |
| Compliance Administrator | Entra role for the pipeline app | 24 (sensitivity labels, label policy, DLP, alert policies) |
| Security Administrator | Entra role for the pipeline app | 1 (DEF-019 alert policy) |
| Teams app-only connection | the pipeline certificate, and the Teams admin hosts allowed | 13 (TMS-001 to TMS-013) |
| Power Platform management-app registration | one-time `pac admin application register`, by a person with Power Platform Administrator | 7 (PPL-001 to PPL-007), and the admin API hosts allowed |
| ExchangeOnlineManagement newer than 3.6.0 | module pin in expected-state/modules.yaml | Security & Compliance PowerShell with a token (all Purview settings) |
| DNS provider token | network secret | 1 (EXO-017, with T2-03) |

Proposed new gates (SPEC §22), each a portal-only switch with no API:
Defender for Endpoint to Intune connection (WIN-010, WIN-032, WIN-033);
Apple Business Manager token; managed Google Play binding; macOS Defender
onboarding package; first turn-on of the preset security policies
(DEF-009, DEF-010).

Proposed configuration keys (values only the company can supply):
`devices.windows.minimum_os_version`, `devices.windows.feature_update_version`,
`devices.macos.minimum_version`, `devices.ios.minimum_version`,
`devices.android.minimum_version`, `devices.browser.extension_allowlist`,
`devices.browser.company_password_manager`, allowed countries (ENT-014),
the device-join group (ENT-039), the managed-plan user group (ENT-004,
ENT-005), allow lists for EWS, sharing domains, Teams federation and
approved apps, and the per-country DLP information types.

## SC-22  Decisions on the unresolved settings (approved 2026-11-21)

The owner accepted the recommendations for the 13 settings T14-03 could not
resolve as written. The catalogue values now state what is delivered, and
`expected-state/setting-ids/*.json` maps each one.

| Setting | Decision |
| --- | --- |
| WIN-058 | Store app off; apps deployed by Intune; automatic app updates on (the private store is retired) |
| MOB-005 | 6 digits, simple values blocked; personally owned Android: medium complexity |
| MOB-022 | operating system version only; no app version check |
| DEF-013 | Safe Attachments Block through the Standard preset; no Dynamic Delivery |
| DEF-021 | expiry set on entries the pipeline adds; others reported as drift |
| ENT-045 | gate until a Purview alert policy can be created app-only (SC-21) |
| ADM-003 | idle session timeout 1 hour on all devices |
| ADM-010 | Copilot and web grounding off until the AI policy approves (gate) |
| ADM-011 | not applicable |
| ADM-012 | gate in the Azure portal; subscription creation noted, not enforced |
| OFC-011 | OneDrive Known Folder Move on Windows; cloud default on macOS |
| PPL-003 | blockable connectors blocked, unblockable ones non-business (gate until SC-21) |
| PPL-006 | Copilot off, generative AI off per environment (gate until SC-21) |

ENT-012 (block device code flow) is not applied on the test tenant while
the owner tests `box` end to end, because `box` signs in with a device
code. It is a dated exception in the test tenant's configuration
(reference SC-22-ENT-012, ends 2027-03-31), not a catalogue change.

ADM-005 (external responses in Microsoft Forms off) is likewise not
applied for now: the public NDA and demo request forms (gate G-04) need
external responses. Dated exception SC-22-ADM-005, ends 2027-03-31.

## SC-23  Directory, domain control and Azure at no additional cost (proposed)

Requested by the owner on 2026-11-21: add Active Directory management,
domain control, and every Azure setting that costs nothing extra. Not
built until the owner answers the open questions below.

### A. Directory (Active Directory)

The company's directory is **Microsoft Entra ID**; the engine already
manages users, groups, roles, authentication, Conditional Access and
directory settings in it. Recommended design for a small company:
**cloud-only, no domain controllers**. Windows devices are Entra-joined
and managed by Intune (T14-05), with Windows LAPS for the local
administrator (WIN-036). This needs no server and no extra licence.

Optional module `hybrid`, only for a company that already runs on-premises
Active Directory:

| Item | Mechanism | Cost |
| --- | --- | --- |
| Synchronise users and groups to Entra ID | Microsoft Entra Cloud Sync (lightweight agent on an existing server) | included |
| Password hash synchronisation; seamless sign-on | Cloud Sync settings | included |
| Writeback of passwords (self-service reset) | Cloud Sync | included with Entra ID P1 (Business Premium) |
| Hardening of the on-premises domain (tiering, Group Policy, domain controller patching) | outside Microsoft 365: runbook and gate only | n/a |

Excluded because they cost extra: **Microsoft Entra Domain Services** (a
managed domain is billed per hour), Entra ID P2 features (Privileged
Identity Management, risk-based access, access reviews), and any virtual
machine hosting a domain controller.

### B. Domain control

Settings for the company's internet domains, all free:

| Setting | Mechanism |
| --- | --- |
| Every domain verified, no stale unverified domains | Graph `domains` (bootstrap; drift report) |
| SPF, DKIM, DMARC at reject, MTA-STS, TLS reporting, CAA, null MX on non-mail hosts | DNS adapter (T2-03; EXO-017) |
| DNSSEC where the DNS provider supports it at no charge | DNS adapter, provider-specific |
| Registrar lock, registrar account with multi-factor, auto-renew on | gate (registrars have no common API) |
| Users cannot create tenants or verify domains in other tenants | Entra authorization policy (already planned) |

### C. Azure (only when the company has an Azure subscription)

Microsoft 365 does not include an Azure subscription. Creating one is free,
but resources in it are billed. These settings cost nothing and harden the
subscription. They apply only when discovery finds a subscription
(capability `azure_subscription`):

| Setting | Mechanism | Free because |
| --- | --- | --- |
| Management group for the company, subscriptions placed under it | Azure Resource Manager REST | no charge |
| No standing Owner; administrators by group; break-glass accounts as emergency owners | Azure role-based access control | no charge |
| Conditional Access requires multi-factor for Azure management | Entra (Business Premium) | already licensed |
| Subscription policies: no leaving or entering the directory (ADM-012) | Azure portal (gate) | no charge |
| Built-in Azure Policy assignments: allowed regions, required tags, no public network access on storage and databases, secure transfer, TLS 1.2 minimum, no public blob access | Azure Policy | built-in policies and their evaluation are free |
| Microsoft cloud security benchmark and secure score | Defender for Cloud **foundational** posture management | the foundational tier is free; paid Defender plans stay off |
| Budget with alerts at 50, 80 and 100 per cent; cost anomaly alerts | Cost Management | free |
| Service Health and Advisor alerts to the alerts address | Azure Monitor action group (email) | email notifications within the free monthly allowance |
| Activity log kept for its free 90 days; no export to a paid workspace | default | free |
| Resource locks on the company's critical resources | Azure Resource Manager | free |
| Classic administrators removed | Azure Resource Manager | free |

What the pipeline app would need: the Azure Resource Manager host
(`management.azure.com`) allowed, and Azure roles (Reader, Resource Policy
Contributor, Cost Management Contributor, and Security Admin for Defender
for Cloud) at the company's management group. These are Azure role
assignments, granted once by a Global Administrator who elevates access;
Entra permissions do not change. Proposed as part of this change; not
granted.

### How it would be built

- New catalogue files `policy/settings/azure.yaml` and additions to
  `entra.yaml` and `exchange.yaml` for domain control, each setting tied to
  requirements and frameworks like the others; `tier: core` and capability
  `free` or `azure_subscription`.
- A new provider `azure` (Azure Resource Manager over REST from PowerShell,
  no Azure module, the same pattern as Graph).
- The optional `hybrid` module as a configuration module (SPEC §18.4) with
  a gate for installing the Cloud Sync agent.

### Questions become choices (SC-24)

The open questions are asked of every owner in onboarding, not decided
once here: `directory.mode` (cloud or hybrid, detected from the tenant),
`azure.subscription` (existing, create, none; detected), and
`domain.registrar`. The Azure roles and host are the `azure` permission
bundle, approved by the owner at the bootstrap sign-in when they choose
Azure. Status: approved as product design (owner, 2026-11-21); built in
track T15.

## SC-24  Choices in one window; permission bundles (approved 2026-11-21)

Owner direction: the product asks every owner the decisions made so far
for the test tenant, with options and a recommendation, so anyone can set
up the whole company system with full policies in one window.

- New SPEC §24 and `defaults/choices.yaml`: every decision is a choice with
  plain-language help, options, costs, a recommendation and discovery
  detection. SC-22's decisions become the recommended options (for
  example device-code sign-in allowed while setting up; Store app off with
  apps from Intune).
- Permissions become bundles granted by what the configuration selects
  (§24.3). SC-21's list becomes the bundles `devices`, `purview`,
  `powerplatform`, `teams` and the rest; the owner approves them on the
  Review screen and at the bootstrap sign-in instead of one change record
  per company.
- SC-23's questions are choices (`directory.mode`, `azure.subscription`,
  `domain.registrar`), with the hybrid directory and Azure as modules.
- New tasks: T0-07 (permission bundles), T13-09 (choices engine), T13-10
  (choices screens), T15-01 to T15-04 (directory, domain control, Azure).

On the test tenant, SC-21 still needs the owner's approval of the bundles
before they are granted; nothing is granted by this change.

## Exceptions on the test tenant (2026-11-21)

- **No FIDO2 keys** for the break-glass accounts this round (owner's
  decision). Gate G-02 stays open. The accounts are created with a random
  password that is discarded, so nobody can sign in with them until keys
  are registered. Because `box check break-glass` cannot pass, T1-05
  creates every Conditional Access policy in **report-only** mode (SPEC
  §20.2 forbids enabling blocking policies without working break-glass
  accounts). Security defaults stay on until then.

## Decision

| Change | Decision | Date | By |
| --- | --- | --- | --- |
| SC-01 to SC-16 | APPROVED, applied to SPEC v1.1 | 2026-11-21 | Owner |
| SC-19 Policy sets (v1.1 package merge) | APPROVED (owner asked for it), applied to SPEC v1.2 | 2026-11-21 | Owner |

## SC-25  The owner approves every change; the product stays simple (2026-11-21)

Owner direction: "When you are writing policies or anything you should give
the users what you are suggesting and get the approvals before you apply.
The policies are in so many places, which is why Company in a Box
simplifies the roll-out. It is not going to make decisions for the user."
And: "Organize all the features in a super simple manner for anyone to roll
out a massive Microsoft ecosystem and protect their assets like a world-class
company on day one."

### A. Suggest, then approve (approved by the owner)

1. Every value the box would otherwise pick (minimum OS versions, update
   times, wording shown to staff, which group a policy reaches) is a
   **choice with a recommended answer** (SPEC §24), shown before anything is
   applied. The box never fills a company value silently and never skips one
   silently: an unanswered value is listed on the Review screen.
2. **Nothing is applied without approval.** `box plan` produces a proposal: every
   change in plain words, grouped by area, with why (requirement, framework)
   and what it affects. The owner approves all of it, an area, or single
   changes; a rejected change becomes an exception with the owner's reason.
   The approval (who, when, the digest of the approved changes) is an
   `Events` item. `box apply` refuses any change that is not in an approved
   proposal and says which ones need approval.
3. **Two approvals for policies:** first to switch on in audit / report-only
   (nobody is blocked), then, after the audit report, to enforce. Enforcing
   is never automatic.
4. Later changes (a new choice, a catalogue update, drift repair) arrive as a
   new proposal on the home screen, never applied in the background.
5. Development and testing on a test tenant may apply directly (owner,
   2026-11-21); the approval gate is how the product treats every company.

### B. Super simple for anyone (approved 2026-11-21; SPEC §25)

Organise everything by what an owner cares about, not by Microsoft admin centre:

| Area (home screen card) | What it covers | Behind it |
| --- | --- | --- |
| Sign-in and accounts | Who can get in, two-step sign-in, emergency accounts | Entra |
| Email | Spam, phishing, impersonation, forwarding, your domain's mail records | Exchange, Defender, DNS |
| Files and sharing | Who can share what, with whom, for how long | SharePoint, OneDrive |
| Meetings and chat | Guests, lobby, recordings, outside chat | Teams |
| Computers and phones | Encryption, updates, antivirus, lost devices | Intune, Defender for Endpoint |
| Sensitive information | Labels, leak prevention, audit trail | Purview |
| Apps and automation | Office apps, AI features, Power Automate | M365 apps, Power Platform |
| Your web address | Domain lock, DNS | Registrar, DNS |
| Cloud | Free Azure safety settings | Azure |

The onboarding stays at five questions a non-technical owner can answer,
with everything else on the recommendation:

1. **Sign in.** We read what Microsoft knows about your company.
2. **What do you run?** Switches: computers and phones, files and records,
   sales, legal, HR, Azure, your own servers.
3. **How careful?** One dial: Essential, Recommended (preselected), Strict. It
   sets the tier and every choice's default. "Let me fine-tune" opens the
   individual choices.
4. **Who works here?** People, their roles and how they get their first sign-in.
5. **Review and approve.** One card per area in plain words ("Staff need a
   second sign-in step", "Laptops are encrypted"), with a count of the
   settings behind it, the cost (none unless stated), and Approve / Show
   details / Not now. Then build, in audit mode.

After the build the home screen shows one status per area (protected,
watching in audit, needs you), the audit report, and an **Enforce** button
per area when its audit period is clean.

## SC-26  Audit pack: a read-only entry point (approved 2026-11-21; SPEC §26)

Owner idea (2026-11-21): a simple feature that extracts everything an IT
audit (SOC 2, ISO 27001, NIST CSF, Cyber Essentials, HIPAA, …) asks for,
organised for the auditor, without writing anything: just reading.

**Entry point.** The first screen offers two paths: "Set up and protect my
company" (the onboarding) and **"Prepare for an audit"**. The audit path,
and `box audit --framework soc2 --period 2026-01-01..2026-12-31 --out <dir>`,
works on any Microsoft 365 tenant, including one the box never built.

**Read only, by construction.**
- Sign-in is delegated, as a person holding **Global Reader** (plus Security
  Reader and Compliance Reader for the Purview and Defender reads), with
  read-only scopes. No app registration, no certificate, no break-glass
  accounts, no tenant writes. The worker protocol runs only `test`, `verify`
  and `evidence`; `plan` and `apply` are refused in audit mode.
- Nothing about the company is stored in the repository. The pack is
  written to the folder the person chooses.

**What it reads.**
1. Every catalogue setting's live value, through the mapping's `read` and
   `check` (the same verify code the build uses), as pass, fail or not
   applicable, with the raw value and the time read.
2. Inventories: users and admins with MFA registration and last sign-in,
   role assignments, guests, Conditional Access policies, authentication
   methods, devices and compliance, sharing settings and external sharing
   links, mailbox forwarding, DLP, labels, retention, the audit log status,
   alert policies, licences.
3. Activity over the audit period where the tenant keeps it: admin changes
   (directory audit), sign-in risk, alerts.

**How it is organised.** For the chosen framework: control → requirements
(policy/requirements.yaml) → settings and inventories → evidence files.
- `index.html` and `controls.csv`: one row per control with status
  (met, partly met, not met, procedure: needs a document), the evidence
  file names, and the gap in plain words.
- `evidence/<control>/…json|csv`: the raw reads, each stamped with the
  tenant id, the time and the query used, plus a hash list of the files
  (`manifest.sha256`) so the auditor can check nothing changed afterwards.
- `gaps.md`: what is not met, ordered by risk, each with "fix with Company in
  a Box" (the proposal it would create), which links to the onboarding.
- Procedural requirements (policies, reviews, training) are listed as
  "document needed", with the template from the legal pack where one exists.

**Tasks (new track T16 Audit pack):** T16-01 read-only sign-in and audit
mode in the executor; T16-02 inventories; T16-03 control mapping and
status; T16-04 pack writer (HTML, CSV, evidence, manifest); T16-05 the
"Prepare for an audit" path in the TUI.

## SC-27  MFA registration campaign as a catalogue setting (approved 2026-11-21, T14-17)

**Why.** IAM-01 and IAM-02 (docs/gap-closure.md, cause D) need people to
register a sign-in method before CA01 can be enforced. The authentication
methods registration campaign prompts them after sign-in. No catalogue
setting covers it (policy/settings/entra.yaml, expected-state/setting-ids/
entra.json have no `registrationEnforcement` entry), so T14-17 sets it from
`baseline/identity.yaml` (`registration_campaign`) in the
`identity.auth-methods` step, which owns it until this change is approved.

**Proposed catalogue entry** (next free ENT id, centre entra, tier core,
mode enforce, requirements IAM-01, IAM-02):

- name: Authentication methods: registration campaign (Microsoft Authenticator)
- apply: `PATCH /beta/policies/authenticationMethodsPolicy` with
  `registrationEnforcement.authenticationMethodsRegistrationCampaign`:
  `state: enabled`, `snoozeDurationInDays: 1`,
  `enforceRegistrationAfterAllowedSnoozes: true`, `includeTargets:
  [{id: all_users, targetType: group, targetedAuthenticationMethod:
  microsoftAuthenticator}]`, `excludeTargets: [{id: {group:grp-break-glass},
  targetType: group}]`
- read: `GET /beta/policies/authenticationMethodsPolicy` (the
  enforce-after-snoozes property is beta only; v1.0 omits it)
- permissions: `graph:Policy.ReadWrite.AuthenticationMethod` (already in
  the `base` bundle)
- verified: applied on the test tenant 2026-11-21; second plan 0 changes.

**Also proposed.**
1. The audit pack reads MFA registration (`GET
   /reports/authenticationMethods/userRegistrationDetails`, counts and object
   ids only) as evidence for IAM-01 and IAM-02. Today the audit runs only the
   policy steps, so the registration count shows in `identity.auth-methods`
   plan, verify and evidence but not in the pack.
2. The home screen's "Enforce" action for Sign-in and accounts (§25.3) is
   offered for CA01 only when `Get-BoxMfaEnforcementReadiness` (identity
   provider) is ready: every non-break-glass member registered and gate G-02
   passing. Enforcing stays an owner approval (§25.1).
3. `snooze_days` could become a choice (§24) if owners want a different
   prompt rhythm; 1 day is the suggestion.

When approved: add the ENT entry and mapping, remove `registration_campaign`
from `baseline/identity.yaml`, and let `policy.entra` own the object.

**Applied (2026-11-21).** ENT-050 added (policy/settings/entra.yaml,
expected-state/setting-ids/entra.json, IAM-01 and IAM-02); the snooze is
choice `signin.mfa_snooze_days` (0, 1 recommended, 7) filling
`values.mfa_snooze_days`. `registration_campaign` removed from
`baseline/identity.yaml`; `policy.entra` owns the campaign and
`identity.auth-methods` keeps the registration report and
`Get-BoxMfaEnforcementReadiness`. Catalogue: 334 settings (SPEC §23 count
updated). Items 1 and 2 stay proposed.

## SC-28  Teams meeting policy refuses app-only writes (approved 2026-11-21: option 2, gate G-11; T14-09)

**Finding (test tenant, 2026-11-21, MicrosoftTeams 6.7.0).** The pipeline
app connects with `Connect-MicrosoftTeams -AccessTokens` (a Graph token and
a token for `48ac35b8-9aa8-4d74-927d-1f4a14a0b239`, both app-only, exactly
as the module's own help example asks). Every `Get-Cs*` the step needs
reads. Writes to the tenant federation configuration, client
configuration, messaging, channels, app permission, custom app, events,
calling, meeting configuration and guest meeting configuration policies
succeed. Every write to `TeamsMeetingPolicy` (`Set-` on Global, `New-` on
a new name) and to `ExternalAccessPolicy` is refused:
`PolicyRpException {"code":"Forbidden","message":"You are not authorized
to perform this action","errorCode":40301}`.

Ruled out:
- token audience or form: `-Certificate` (the module gets its own tokens)
  gives the same 403;
- the role: Teams Administrator is assigned to the service principal at
  directory scope `/`, 14 hours before the test, and is in the token's
  `wids`; other writes succeed with the same token;
- Graph permissions Microsoft documents for app-only `-Cs` cmdlets:
  `Organization.Read.All` (all cmdlets) is granted. The documented
  `RoleManagement.Read.Directory` applies to apps used with administrative
  units, which this app is not; `GroupMember.Read.All` applies to the group
  policy assignment cmdlets only;
- the "Skype and Teams Tenant Admin API" application permission: Microsoft
  says not to add it for app-based authentication.

`policy.teams` now applies every other cmdlet and fails naming the refused
cmdlet and its settings (TMS-004 to TMS-008), so `box check policy.teams`
cannot pass while meeting policy differs.

**Proposed.**
1. Optional experiment, owner's choice: add `RoleManagement.Read.Directory`
   to the `teams` bundle in `expected-state/app-permissions.yaml`, re-run
   bootstrap on the test tenant, retry. Documented only for administrative
   units, so it may not help; remove it again if it does not.
2. Otherwise a gate (next free id, R5): "Teams meeting policy" - the owner
   sets the Global meeting policy values from TMS-004 to TMS-008 in the
   Teams admin centre (Meetings > Meeting policies > Global); the check is
   the existing `Get-CsTeamsMeetingPolicy` comparison, which `policy.teams`
   already does app-only. T14-09's "Human steps: None" changes to name it.
3. Open a Microsoft support case with the correlation ids from the test
   run; drop the gate if Microsoft enables app-only meeting policy writes.

## SC-29  Gate G-12: phone enrolment restrictions (proposed 2026-11-21, T14-07)

**Why.** MOB-001 blocks personal enrolment of iOS and Android work profile
devices and blocks Android device administrator. Intune refuses every
app-only write to `deviceEnrollmentConfigurations`: creating a per-platform
`deviceEnrollmentPlatformRestrictionConfiguration` and patching the default
restriction both answer 403 "Tenant is not Global Admin or Intune Service
Admin. Operation is restricted." with the Intune Administrator role assigned
to the pipeline app at tenant scope and
`DeviceManagementServiceConfig.ReadWrite.All` granted (test tenant,
2026-11-21, beta and v1.0; v1.0 rejects the per-platform type and the
single-policy form is "no longer supported"). Reading works app-only. By
SPEC §22 a portal-only step is a gate with a guide and a check.

**Proposed §22 row.**

| Gate | Guide | Check |
| --- | --- | --- |
| G-12 Phone enrolment restrictions | In the Intune admin centre, create iOS/iPadOS and Android restrictions that block personally owned devices and block Android device administrator; assign them to the pilot group, then to all users at enforce (T14-07) | iOS and Android work profile personal enrolment blocked and Android device administrator blocked, in the default restriction or a per-platform restriction assigned to a group (step `intune.gate-g12`, read-only) |

Applied ahead of approval in `engine/gates.yaml` (G-12, check
`intune.gate-g12`) so `policy.intune-mobile` keeps passing; the step reports
the gate state and creates nothing. Remove or renumber when the §22 table is
updated.

**Also noted.**
1. `engine/gates.yaml` numbers G-07 to G-10 differently from the §22 table
   (§22: G-07 Apple push certificate, G-08 data loss prevention review;
   gates.yaml: G-07 Cloud Sync agent, G-08 Azure subscription, G-09
   registrar lock, G-10 Power Platform registration). One numbering should
   win.
2. WIN-040 (default Windows enrolment restriction, held until the enforce
   stage) hits the same refusal; at enforce it needs the same kind of gate.

## SC-30  Four labels, mail-only DLP-06 and DLP-07, personal mail domains as a choice (approved by the owner 2026-11-21, T14-10, T14-11)

**Why.** Applying `policy.purview` on the test tenant found three things the
catalogue assumed and Microsoft does not offer:

1. **Sub-labels (PVW-005).** The tenant uses Microsoft's modern label
   scheme: a sub-label can sit only under a label group, and a label group
   carries no marking and cannot label sites. Confidential, with its header
   marking and site protection (PVW-008), cannot be the parent of Sales,
   Legal and People.
2. **Site rules (PVW-015, PVW-016).** Every SharePoint or OneDrive DLP rule
   needs a content condition, and none matches every file:
   `DocumentNameMatchesPatterns` is Exchange and Endpoint only, and
   `DocumentSizeOver` starts at 10 KB on SharePoint (tested).
3. **Personal mail domains (PVW-017, PVW-018).** DLP-08 and DLP-09 need
   `policy.consumer_mail_domains`, which no step of onboarding asked for, so
   the settings were skipped.

**Decision (approved by the owner 2026-11-21).**

1. Four labels: Public, Internal, Confidential, Restricted. The Sales, Legal
   and People sub-labels are dropped from PVW-005, the label policy
   (PVW-007) and DLP-02 (PVW-011). The engine does not create, change or
   remove sub-labels a tenant already has.
2. DLP-06 and DLP-07 are mail-only: the policy location is Exchange
   (`ExchangeLocation All`; an existing policy's SharePoint and OneDrive
   locations are removed). The rule covers mail with an attachment
   (`DocumentNameMatchesPatterns '.'`, any attachment name;
   `DocumentSizeOver` starts at 10 KB on Exchange too) sent outside the organisation (`AccessScope
   NotInOrganization`) by the members of the sites: the users in the groups
   that `structure.sites[*].access` makes owner or member of the legal,
   finance, people and corporate sites (DLP-06) or the sales site (DLP-07),
   expanded on every apply because the access groups are security groups.
   Sharing files from the sites themselves stays with SPO-002.
3. A choice `data.personal_mail_domains` (Security step) asks whether to
   watch mail to personal accounts. The recommended option `common` supplies
   `policy.consumer_mail_domains` = gmail.com, outlook.com, hotmail.com,
   live.com, yahoo.com, icloud.com, proton.me, aol.com; the option `none`
   leaves PVW-017 and PVW-018 out. A list in the configuration still wins.
   A choice's `values` may now fill `policy.consumer_mail_domains` as a
   comma-separated list (each item a domain, checked by `box choices
   validate`); the purview step splits it.

**Found while applying (no decision needed).** DLP-08 uses the same
attachment condition (`DocumentSizeOver 1` was refused: minimum 10 KB).
DLP-09's recipient patterns are limited to 128 characters each, so the
engine writes one pattern per consumer domain (split per name form if still
too long). DLP-06 and DLP-07 drop `BlockAccessScope`, which applies only to
SharePoint and OneDrive (Microsoft warns that `PerUser` acts as a plain
block in Exchange).

**Files.** `policy/settings/purview.yaml` (PVW-005 name, PVW-015 and
PVW-016 values), `expected-state/setting-ids/purview.json` (PVW-005,
PVW-007, PVW-011, PVW-015 to PVW-018), `defaults/choices.yaml`,
`engine/internal/choices/choices.go`,
`engine/providers/exchange/Box.exchange.policy.ps1`, `tasks/T14-10.md`,
`tasks/T14-11.md`. No policy document named the sub-labels (POL-05 and
PRC-10 already speak of four labels).

**Proposed §25.1 wording** (SPEC not edited): "Choices may carry `values`
that fill configuration keys the settings read (`devices.*`,
`ai_policy.*`, `values.*`, and the list `policy.consumer_mail_domains`)."
