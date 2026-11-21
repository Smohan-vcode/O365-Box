# Company-in-a-Box Specification, Microsoft 365 engine, v1.2

Status: build-ready for tracks T0 to T3, T13 and T14. T4 starts after
spike S-01, T1-05 and T14-03 after S-02, T5 after S-03 and S-04. This
file governs this package. v1.1 applied the changes SC-01 to SC-16; v1.2
applies SC-19 (policy sets, §23). Both are recorded in
`docs/spec-changes.md`.

This package implements the same model as the Google Workspace package:
the same legal document model, bracket kinds, company data, roles, approval
rules, register entities, policies and controls. Only the engine differs.

## 1. Purpose

A product that stands up any company's operating system on Microsoft 365.
The owner runs one command, `box`, once a Microsoft 365 Global
Administrator account exists, or is guided through creating one. A
terminal UI collects what the setup needs, shows the full plan, and then
builds and hardens the tenant.

The repository contains the engine only. A company's configuration is
collected by the onboarding TUI and stored in that company's tenant. No
path in the repository contains a company-specific value, except the
fictional company under `examples/`.

### Principles specific to this engine

- **Free first.** Version 1 uses only what the base subscription includes:
  Entra, Exchange, SharePoint, Power Automate with standard connectors,
  Forms, Approvals. Every step that needs a paid licence is isolated as one
  replaceable action and listed in `upgrades/`.
- **Guided, not hand-edited.** A non-technical owner completes onboarding
  in the TUI. Values Microsoft already knows are discovered and
  pre-filled. Nobody edits YAML to get started.
- **No custom application at run time.** After setup, Microsoft's admin
  centres and SharePoint lists, forms and views are the console for
  business records. The TUI is the console for setup and change.
- **Settings as code.** Tenant settings are resolved from the engine's
  policy catalogue in `policy/` for the frameworks and tier the company
  selects in its configuration document (§23), applied by the
  orchestrator and monitored for drift. The document is versioned in the
  tenant.
- **Templates are built, not reviewed at run time.** Bracket decisions are
  confirmed once, in the `Bindings` list. The engine converts each Word
  template once.

### In scope

- Onboarding: sign-in or sign-up guidance, discovery, guided questions,
  plan review, build, later changes.
- Tenant baseline: identity, mail, sharing, meetings, devices, data
  protection, resolved from the policy catalogue for a selected tier and
  frameworks, with dated exceptions (§20, §23).
- Users, groups, shared mailboxes, distribution lists, aliases, DNS.
- Jurisdiction-driven disclosures: email disclaimers, document footers,
  notices.
- SharePoint sites, libraries, folders, lists (the register) and
  permissions.
- Template build: brackets to Word fields, one content type per template.
- Document processes as Power Automate flows: send, number, fill, approve,
  hand to signature, file, remind; packages; notices and records.
- People and roles through a list that the pipeline applies.
- Setup status and drift as lists and views.
- Leads, campaigns, orders register; policies; controls and evidence.

### Out of scope for version 1 (see `upgrades/`)

- Creating the Microsoft 365 tenant from software (§18.2).
- Filling templates that contain drafted alternatives: the sender deletes
  the unused wording by hand.
- Sending for signature from a flow: the signer sends by hand and uploads
  the executed copy.
- Calling the order system from a flow.
- Enforced retention and legal hold.
- AI assistants.
- A publisher-verified sign-in app of the product's own.

## 2. Design rules

| ID | Rule | Test |
| --- | --- | --- |
| R1 | No company value in the repository | `box check-literals` fails on any company value (from the tenant with `--from-tenant` or a file with `--from-file`), on any `examples/` value outside `examples/`, and on anything that looks like a secret. |
| R2 | Subscription only | Every runtime component is included in the base subscription. Flows use standard connectors only; `box check-connectors` passes. |
| R3 | Configuration over code | A new group, address, site, library, folder, list, role, document type, package, policy, control or jurisdiction is a data change. |
| R4 | Repeatable | Every step can run twice with the same result. A second run plans no changes. |
| R5 | Manual steps are gates | A step with no API is a gate: a plain-language guide, an expected value, and a check the orchestrator runs (§22). |
| R6 | Evidence by default | Each step writes what it did to the `Setup` list and the Admin site. |
| R7 | One register | All records are lists in one site, defined in `engine/register/lists.yaml`. |
| R8 | Least access | Access is granted to groups at site or library level. Per-item permissions only where this specification names them. |
| R9 | Paid steps are replaceable | Each deferred capability is one child flow or one provider step with a fixed input and output. Upgrading replaces that unit only. |
| R10 | Events are append-only | Every state change, including every configuration save, writes one `Events` item. Flows and the pipeline app hold the only write permission on that list. |
| R11 | One home per value | Company configuration lives in the configuration document; company data values live in the `Dictionary`. Neither is copied elsewhere. |
| R12 | Global Administrator only for bootstrap | The Global Administrator's delegated session is used only for discovery and bootstrap and is never written to disk. |

## 3. Platform decisions

| Item | Decision |
| --- | --- |
| Subscription | Microsoft 365 Business Premium recommended; Business Basic and Standard supported with the fallbacks in `engine/capabilities.yaml` (§20.3) |
| Orchestrator, CLI and TUI | Go, single static binary `box`; TUI built on Bubble Tea, Bubbles and Lip Gloss |
| Providers | PowerShell 7 modules in `engine/providers/<name>/`, one per Microsoft surface, tested with Pester; modules Microsoft.Graph, ExchangeOnlineManagement, PnP.PowerShell, MicrosoftTeams at pinned versions |
| Provider process model | Each provider runs in its own `pwsh` process started by the orchestrator, speaking JSON lines on stdin and stdout |
| Prerequisites | `box doctor` checks pwsh 7.4+, the modules and the Power Platform CLI, and offers to install what is missing |
| Sign-in for onboarding | Microsoft Graph Command Line Tools public client; browser sign-in with PKCE on `localhost`, device code as fallback; scopes in `expected-state/bootstrap-scopes.yaml` |
| Pipeline identity | One Entra app registration per tenant, created by bootstrap; GitHub federated credential and a local certificate; no client secret; permissions in `expected-state/app-permissions.yaml` |
| DNS | Adapters: `cloudflare` (Terraform), `route53`, `azure`, `godaddy`, `manual` |
| Baseline | Policy catalogue in `policy/`, resolved for the configuration's `policy` section (§20.1, §23); applied and monitored through the Tenant Configuration Management APIs where S-02 shows they can write; otherwise through the providers |
| Policy resolver | Go package `engine/internal/policy`; `box policy validate`, `box policy resolve`, `box policy coverage`; no network access |
| Configuration store | `box-config.yaml` in the Admin site's `Configuration` library (§9) |
| Records | SharePoint Online: one hub; the recommended structure has nine sites |
| Register | SharePoint lists in the Admin site |
| Processes | Power Automate cloud flows in one solution, source in `flows/`, deployed with the Power Platform CLI |
| Flow owner | The founder's account at launch; flows run on its connections |
| Forms | List forms for internal entry; Microsoft Forms for public intake |
| Approvals | The built-in Approvals action |
| Document filling | Word content controls bound to library columns |
| Signature | Child flow `Signature`; version 1 implementation is manual |
| Scheduled jobs | GitHub Actions in the company's private copy of the engine repository, reading the configuration from the tenant |

## 4. Repository layout

```
engine/
  cmd/box/                main package
  internal/               auth, config, plan, run, state, events, gates, tui, providers,
                          policy (resolver, validation, coverage)
  providers/              identity, exchange, sharepoint, teams, intune, powerplatform, dns
  terraform/dns/          Cloudflare adapter
  register/lists.yaml     register lists and columns
  templates/              template build tooling
  capabilities.yaml       plan to capability map and fallbacks
schema/                   JSON schema for box-config.yaml
defaults/                 choices.yaml (§24), roles, structure, controls, stages, checklists, retention,
                          policy index and texts
defaults/jurisdictions/   rules per jurisdiction
policy/                   policy catalogue (§23): requirements.yaml, settings/<area>.yaml,
                          frameworks/<id>.yaml, capabilities.yaml, README.md
baseline/                 per-area settings read by the providers until T14-04 moves
                          each area onto resolved settings; then removed
flows/                    catalogue.yaml and solution source
expected-state/           app permissions, bootstrap scopes, connector allow-list
examples/acme/            fictional configuration for tests and the CI dry run
upgrades/                 one file per paid capability
runbooks/                 gate guides (generated from gate definitions)
tasks/, spikes/           development backlog for the engine
docs/                     this specification and change records
```

The resolved settings for a tenant are never written to the repository:
they are computed at plan time and recorded in that tenant (§23).

`tasks/` and `spikes/` are the engine's development backlog. Each file's
front matter carries `status` (`TODO`, `DONE`, `BLOCKED`). Per-tenant
progress is not stored in the repository: it is the `Setup` list (§14).

A task's `box apply <step>` and `box check <step>` name a single step. They
run that step and its plan or verify only.

## 5. Tracks

| Track | Delivers | Priority | Size | Depends on |
| --- | --- | --- | --- | --- |
| T0 Foundation | Configuration schema and checks, orchestrator, provider runner, sign-in and bootstrap, pipeline, configuration store | P0 | M | none |
| T13 Onboarding TUI | Shell, start and discovery, company, addresses, people, security (frameworks, tier, rollout, exceptions), jurisdictions and disclosures, structure, review, run view and home | P0 | L | T0, T14-01 |
| T1 Tenant and identity | Break-glass accounts, users, groups, identity and device baseline | P0 | M | T0 |
| T2 Mail and DNS | Shared mailboxes, lists, aliases, DNS, mail baseline, disclaimers, probe | P0 | M | T1 |
| T3 Sites and register | Sites, libraries, folders, permissions, register lists | P0 | M | T1 |
| T4 Template build | Confirmed bindings, converted templates, content types, dictionary | P0 | L | T3, S-01 |
| T5 Document flows | Send, number, fill, approve, signature hand-off, file, packages, notices, reminders | P0 | L | T4, S-03, S-04 |
| T6 People and roles | People list applied by the pipeline, role sync, checklists | P0 | S | T3 |
| T7 Setup and drift | Setup list and views, drift monitors, weekly report | P0 | S | T3, S-02 |
| T8 Sales and campaigns | Inbound mailbox, leads, campaigns, orders register | P1 | S | T5 |
| T9 Policies | Policy rendering, publication, acknowledgement | P1 | M | T3 |
| T10 Compliance | Controls, evidence collectors, recurring tasks | P1 | M | T1, T9 |
| T11 Upgrades | Paid capabilities, each a replaceable step | P2 | S each | T5 |
| T12 Product packaging | Second-company dry run in CI, installer guide | P2 | M | all P0 and P1 |
| T14 Policy sets | Resolver, validation and coverage; capability check; setting resolution; apply per admin area in audit then enforce; sensitivity labels; data loss prevention; audit and alerts; Frameworks list; exceptions; evidence per requirement; standards rendered from requirements | P0 | L | T0, T1, T2, T3, S-02 |
| T15 Directory, domains and Azure | Hybrid directory (Cloud Sync), domain control, Azure governance at no extra cost, each driven by choices (§24) | P1 | M | T13-09, T14-03 |
| T16 Audit pack | Read-only audit path: delegated read sign-in, live setting reads, inventories, control status, the evidence pack (§26) | P1 | M | T14-03 |

Each provider step is usable headless before its TUI screen exists.

## 6. Identity and mail model

- Bootstrap (§18.5) creates two break-glass Global Administrator accounts,
  cloud-only, no licence, FIDO2 keys, excluded from Conditional Access by
  the group `grp-break-glass`.
- The founder is a licensed user and is not a standing Global
  Administrator. Removing the founder's role is gate G-05.
- Groups, all Entra security groups:
  - `access`: prefix `grp-`, granted on sites and libraries.
  - `role`: prefix `role-`, one per role in `defaults/roles.yaml`.
- Mail objects (configuration `mail`):
  - shared mailbox: an address replied from or holding a record;
  - distribution list: an address that only forwards;
  - alias: an extra address on one of the above.
- Shared mailboxes carry no licence in version 1. Sign-in to them is
  blocked. Members get full access and send-as through access groups.
- Existing accounts and groups found by discovery are offered for
  adoption, skipping, or stopping. Nothing existing is changed without
  the owner's choice.

## 7. Records model

The recommended structure, `defaults/structure.yaml`, is one hub with nine
sites mirroring the model's nine record areas: Corporate, Legal, Finance,
Sales, Marketing, Product, Compliance, People, Admin. The owner may add,
rename or remove sites, libraries and folders in the TUI (§18.4, step 5).
The Admin site cannot be removed: it holds the register and the
configuration store.

Each site's members are access groups only. External sharing is off at
tenant level except for sites the configuration marks, each with a
recorded reason.

Permission guardrails, enforced by the editor and by `box config
validate`:

- access is granted to groups only, never to individuals;
- "Everyone" and "Everyone except external users" are never granted;
- per-item permissions are not configurable; only §11's executed
  documents receive them, from flows;
- a library may tighten its site's access, and may loosen it only with a
  recorded reason;
- a site or library that holds files is never deleted by the engine; it
  can be archived.

The register is the set of lists in the Admin site. Model types map to
columns as follows:

| Model type | SharePoint column |
| --- | --- |
| id | Text, indexed, unique, written by flows |
| text, email, url | Single line of text |
| long, json | Multiple lines, plain |
| enum | Choice |
| date, datetime | Date and time |
| number | Number |
| bool | Yes/No |
| ref(List) | Lookup to that list's id column |

## 8. Legal document model

Domains (`company`, `partner`, `notice`), classes (`signed`, `attached`,
`published`, `record`), bracket kinds (`deal`, `constant`, `choice`,
`note`, `reference`), template and document states, reference numbering
from each template's own pattern, and the `never_store` rule are identical
to the model. The pack's catalogue travels with the pack.

The Legal documents module is on when a pack is uploaded.

### How a template becomes usable

1. The owner uploads the pack in the TUI (step 5): the Word files and the
   pack's catalogue, packages and dictionary files go to the Legal site,
   `Templates` library, folder `Source`.
2. `box build scan` lists every bracket occurrence, with a suggested kind
   and key, in the `Bindings` list. The owner or counsel confirms each row
   by setting `confirmed` to yes and, for deal fields, a `field` name, in
   the TUI's template screen or the list form.
3. `box build templates` converts each confirmed template:
   - `deal`, `constant`, `note` and `reference` occurrences become content
     controls bound to a document property of that name;
   - `choice` occurrences are left as text and the template is marked
     `needs_manual_edit`;
   - `never_store` occurrences become an empty underlined gap.
4. It generates one content type per template with those properties as
   columns, in two forms: a list content type on `Documents` (the send
   form) and a library content type whose document template is the
   converted file.
5. A template is `ACTIVE` when every one of its brackets is confirmed.

### How a document is filled

A flow copies the content type's template into the counterparty's folder
and sets the file's properties: deal values from the `Documents` item,
constants and notes from the `Dictionary` list, the reference from the
`Counters` list. Word shows those values through the bound controls. No
Word-editing connector is used.

A document can be sent only when every constant and note it uses is
`CONFIRMED` in the dictionary.

## 9. Configuration and company data

### 9.1 The configuration document

`box-config.yaml`, validated by `schema/box-config.schema.json`, holds:
`tenant`, `company`, `jurisdictions`, `modules`, `dns`, `mail`, `users`,
`groups`, `structure` (hub, sites, libraries, folders, access),
`policy` (frameworks, tier, rollout, exceptions; §20.1), `disclosures`, `legal`, `integrations`, `compliance`,
`retention`, `authority`, `secrets` (references only) and `ids`.

It is stored in the Admin site, library `Configuration`, with major
versions on. Only `grp-admins` and the pipeline app can open it. Every
save from the TUI creates a new version and one `Events` item recording
who changed what. Company changes are reviewed through that history.
Engine changes are reviewed by pull request.

Before the Admin site exists, the document is an encrypted local draft
(§21). The first part of the build after bootstrap creates the store and
uploads the draft (§18.4, step 8).

`box config export` writes the document and the dictionary values to an
encrypted file. `box config import <file>` pre-fills the wizard from such
a file. `box config import --legacy <dir>` reads the v1.0 layout
(`company.yaml`, `directory/`, `sharepoint/`, `content/`) from a local
directory outside the repository. The repository never holds such files.

### 9.2 Company data

- `Dictionary` is a list in the Admin site, editable by the `owner` role
  in the TUI or the list form. It holds every `company.*` value (address,
  registration and tax numbers, telephone, representatives, footer
  texts) and the pack's notes. It is seeded from the onboarding answers
  and the pack's dictionary file.
- The configuration document refers to dictionary keys and never copies
  their values (R11).
- `Bindings` gives the "used in" view: which templates use which key.
- A flow runs when a dictionary value changes: it flags `SENT` documents
  that used the key and creates a pending `Notices` version for each
  published or record template that uses it. Disclaimers that use the key
  are re-rendered on the next apply.
- Executed documents never change. Wording changes go through the template
  build as a new version.

## 10. Roles and approvals

Roles and approval-rule fields are as in the model. Flows read role
holders from the `RoleAssignments` list, which the pipeline refreshes from
the role groups hourly.

An approval is one Approvals action addressed to the current holders of
the rule's role. The flow enforces the self-approval safeguard: the sender
is removed from the approver set when another holder exists; otherwise the
self-approval is recorded.

## 11. Signature

`Signature` is a child flow with input `document_id` and outputs `sent_at`
and, later, `executed_file`. Version 1:

1. Assigns the signer a task with the document link and signer details.
2. The signer sends the document for signature by whatever means the
   company uses and marks the task done; the document becomes `SENT`.
3. The signer drops the executed PDF into the `Executed inbox` library with
   the document's reference in the file name.
4. A flow matches it, files it in the counterparty's folder, removes edit
   access, sets `EXECUTED` and runs the execution action.

`upgrades/signature-provider.md` replaces steps 1 to 3.

## 12. Connection points

| Direction | Version 1 mechanism |
| --- | --- |
| Inbound | A shared mailbox, `inbound`. A flow reads messages whose subject starts with a known type and whose body is JSON, checks a shared token, and writes the row. Types: `lead.create`, `order.announce`, `document.request`. The token is generated by the engine (§21). Microsoft Forms for public NDA and demo requests (gate G-04). |
| Outbound | None automatic. Execution of an order form assigns the owner a task to issue the licence. `upgrades/order-system-call.md` replaces it. |

## 13. People

The initial team is entered in onboarding step 6. Afterwards, `People` is
a list. An owner adds an item with name, address and roles. The pipeline
applies it hourly: creates the account, assigns the plan named on the item
if a seat is free, adds role and access groups, opens the joiner
checklist. Removing a person is setting `active` to no; the pipeline
blocks sign-in, removes groups and opens the leaver checklist.

## 14. Steps, setup status and drift

### 14.1 Step contract

Every unit of work is a step, carried out by a provider:

| Operation | Meaning | Must be |
| --- | --- | --- |
| `test` | Can this step run here: capabilities, prerequisites, permissions | read-only |
| `plan` | Declared state versus actual state, as a list of changes | read-only |
| `apply` | Make the changes in the plan | repeatable (R4) |
| `verify` | Re-read and confirm the declared state holds | read-only |
| `evidence` | Describe what was done and where the proof is | read-only |

Each provider declares its steps in `engine/providers/<name>/steps.yaml`:
id, track, depends_on, configuration paths read, permissions and
capabilities needed, and any gate before it. The orchestrator builds the
graph from the manifests and runs independent steps in parallel.

### 14.2 Event stream

The orchestrator emits one JSON object per line, for example:

```json
{"t":"2026-11-21T14:02:11Z","run":"r-0007","step":"sharepoint.library","ev":"step.progress","msg":"created library 7 of 9","done":7,"of":9}
```

Event kinds: `run.started`, `step.planned`, `step.started`,
`step.progress`, `step.changed`, `gate.waiting`, `gate.passed`,
`step.done`, `step.failed`, `step.skipped`, `run.finished`. The TUI, the
CLI's `--json` output, CI logs and the `Setup` list all read this stream.
Events never contain `never_store` values or secrets.

### 14.3 Setup list and state

`Setup` is a list with the model's step fields plus plan hash and evidence
reference. The orchestrator writes it after every step. Views: by track,
waiting on a person, failed or drifted, stage reached. A local cache
speeds up the TUI and is rebuilt from the tenant when missing. Raw logs go
to the Admin site's `Automation logs` library.

Running `box up` again skips steps whose plan is empty and whose verify
passes, and continues from the first incomplete step.

### 14.4 Drift

`box drift` runs `plan` on every step and applies nothing. Configuration
monitors cover the resolved settings (§23) where S-02 shows they can.

## 15. Sales, policies, compliance

As in the model, with these mechanisms:

- Quotation copy: an Exchange transport rule, applied as code.
- Campaigns, leads, orders: lists with views; counts by lookup.
- Policies: Markdown templates in `defaults/policies/` rendered with
  company values to PDF in the Compliance site's `Policies` library;
  acknowledgement is a list item created by a flow from an approval-style
  request to each person. Policies cite requirement ids; the standards
  beneath them are rendered from `policy/requirements.yaml` (§23).
- Evidence collectors: pipeline jobs reading Graph; one `Evidence` item
  per artefact. Recurring control tasks: a scheduled flow.

## 16. Spikes

| ID | Question | Gates |
| --- | --- | --- |
| S-01 | Can brackets be converted to content controls bound to library columns, and do properties set by a flow appear in the document, in body, tables, headers and footers? | T4 |
| S-02 | Which baseline settings can the Tenant Configuration Management APIs write, and which only monitor? | T1-05, T7-02, T14-03 |
| S-03 | Can the flow solution be deployed from source to the default environment under the included licence? | T5 |
| S-04 | Is every action in `flows/catalogue.yaml` a standard connector, and are daily limits adequate? | T5 |
| S-05 | Can the Go orchestrator drive Graph, Exchange, PnP and Teams providers in separate pwsh processes over JSON lines, passing tokens or certificates safely, with start-up under 5 s per provider? | T0-02 |
| S-06 | With only a delegated Global Administrator session through the Graph Command Line Tools client, can bootstrap create the app, grant app role assignments, assign Entra roles, add a federated credential and certificate, and add and verify a domain? | T0-04 |
| S-07 | Does an Exchange "apply disclaimer" rule render the HTML disclaimer correctly in Outlook desktop, web and mobile, for replies and forwards, without stacking? | T13-05 |
| S-08 | On a freshly signed-up tenant, which `organization` address fields are populated, and does browser sign-in with PKCE on `localhost` work for the Graph Command Line Tools client on Windows, macOS and Linux? | T0-04, T13-02 |

## 17. Upgrades

| File | Capability | Replaces | Indicative cost |
| --- | --- | --- | --- |
| `word-template-fill.md` | Fill templates including alternatives | Manual deletion of unused wording | Power Automate Premium, one licence |
| `signature-provider.md` | Send and track signature from a flow | Manual send and upload | Provider fee; premium licence if no standard connector |
| `order-system-call.md` | Call the order system on execution | Owner task | Covered by the same premium licence |
| `retention.md` | Enforced retention and legal hold | Policy only | Licence per record-bearing mailbox |
| `assistants.md` | The five assistants | Nothing | Copilot per user |
| `verified-sign-in-app.md` | Publisher-verified multi-tenant sign-in app showing the product's name | Graph Command Line Tools client | Microsoft Partner Network membership; no licence |

## 18. Onboarding

### 18.1 Principles

`box` with no arguments opens the TUI. With no configuration found it
starts the wizard; otherwise it opens the home screen (§18.6).

- Ask only what Microsoft does not already know; discovered values are
  pre-filled and tagged "from your Microsoft 365 account".
- Plain language; every field has a "What is this?" panel and a "Why we
  ask" line.
- The recommended choice is preselected. Accepting every default yields a
  sound, hardened company.
- Nothing changes in Microsoft until step 8. Steps 1 to 7 sign in, read,
  and write the encrypted local draft.
- The draft is saved after every screen; re-opening returns to the same
  place.
- Input is validated live: formats, DNS lookups, licence counts, name
  clashes, permission guardrails.
- Turning off a setting the selected tier applies requires an exception
  with a reason, a reference and an end date (§20.1).

### 18.2 Sign-in and sign-up

The first screen asks whether the company already has Microsoft 365:

- **Yes:** browser sign-in as a Global Administrator; device code when no
  browser is available or with `--device-code`. If Conditional Access
  blocks device-code sign-in, the TUI says so and offers the browser.
- **No:** software cannot create the tenant (only Microsoft reseller
  partners can, through Partner Center). The TUI explains which plan to
  choose and what Microsoft will ask, opens Microsoft's sign-up page, and
  waits until the user presses Enter to sign in. This is gate G-00.
- **Setting up for someone else:** as Yes, with the other company's admin
  sign-in.

The sign-in uses the Microsoft Graph Command Line Tools public client. The
admin consents once to the delegated scopes in
`expected-state/bootstrap-scopes.yaml`.

### 18.3 Discovery

Read-only, right after sign-in:

| Read | Pre-fills |
| --- | --- |
| Token claims | `tenant.tenant_id` (never typed), signed-in admin's identity |
| Organization | Legal name (suggested), registered address, country, place of registration (suggested) |
| Domains | Primary domain, `onmicrosoft.com` domain, domains still to add |
| Subscribed plans | Capabilities (§20.3), seat counts |
| Users and groups | Founder (the signed-in admin), adoption candidates |
| NS records of the primary domain | DNS adapter |
| Security defaults and Conditional Access state | New or already-configured tenant; adopt, skip or stop |

The user confirms the organisation ("We found Contoso Ltd, tenant 1a2b…,
3 Business Premium seats") or signs in again. They then choose "test run"
or "my real company" (`tenant.environment`); a live run shows a warning
band throughout.

### 18.4 The eight steps

| # | Step | Collects | Stored at step 8 in |
| --- | --- | --- | --- |
| 1 | Start | Sign-in or sign-up, discovery confirmation, test or real | `tenant` |
| 2 | Company | Legal and trading name, entity wording, registered address, registration and tax numbers, telephone, domains; place of registration and operating jurisdictions (§19); disclosures | `company`, `jurisdictions`, `disclosures`; Dictionary |
| 3 | Addresses | Founder's address; shared mailboxes, aliases, lists, auto-replies, access groups; DMARC report address; sending subdomains; DNS provider and token | `mail`, `dns`; token to the keychain |
| 4 | Security | Frameworks to report against; policy tier (core or enhanced); rollout stage, audit days and pilot group; the security choices (§24); exceptions with reason, reference and end date (§20, §23); break-glass account names | `policy`, `choices`, `tenant.break_glass_accounts` |
| 5 | Structure | Modules; the structure choices (§24: directory, Azure, automation, data protection); sites, libraries, folders; groups; access grid (§7); legal pack upload | `modules`, `choices`, `structure`, `groups`; Legal `Templates/Source` |
| 6 | People | Initial team with plan, roles and access groups; existing users for adoption; seat counts; how each person receives their first sign-in pass (§18.9) | `users` |
| 7 | Review | Full plan as a tree with counts; licence use; the permissions the choices need, by bundle (§24.3); gates ahead; costs (none unless an option says so); estimated time; exceptions. Confirmation by typing the primary domain | — |
| 8 | Build | Live run view, gates, final report | tenant |

Modules: Core (always on), Records, People and roles, Policies, and
Devices (when the plan includes Intune) are on by default; Legal documents
(on when a pack is uploaded), Sales and Compliance are off by default;
Hybrid directory and Azure are switched on by their choices (§24). A
module that is off produces no steps.

Step 8 runs in this order:

1. **Bootstrap** as the Global Administrator (§18.5).
2. **Configuration store** as the pipeline app: hub, Admin site,
   `Configuration` library, `Setup`, `Events` and `Dictionary` lists;
   upload `box-config.yaml`, seed the dictionary, copy run state from the
   draft, delete the draft.
3. **Everything else** in dependency order.

The run view shows tracks, the current track's steps with progress, gates
waiting on the user with their guides, and a log line. The user can expand
a step's changes, retry, skip an optional step with a reason, or pause.
The run ends with a summary, next actions and a report in the Admin site.

### 18.5 Bootstrap

1. Add the primary and extra domains; write each verification record
   through the DNS adapter; verify.
2. Create the pipeline app registration and service principal.
3. Grant exactly the permission bundles the configuration needs (§24.3)
   by app role assignment; assign their Entra roles; refuse anything under
   `never`. The consent screen at sign-in lists the bundles and why.
4. Add a GitHub federated credential for the company's copy of the
   repository and a local certificate whose private key stays in the OS
   keychain. Never create a client secret.
5. Create the break-glass accounts and `grp-break-glass`; show the
   generated passwords once with a print option; open gate G-02.
6. Sign out of the Global Administrator session and discard its token
   (R12).

Later runs sign in with the owner's own account, which needs no directory
role. Changing the pipeline app's permissions needs a pull request to
`expected-state/app-permissions.yaml` and a new Global Administrator
sign-in.

### 18.6 Later runs

`box` signs in, loads `box-config.yaml` from the tenant, and opens the home
screen: status, drift, pending gates, and "Change something", which reopens
an editor, then plan, then apply, and saves a new version.

### 18.7 Headless parity

```
box                                   TUI: wizard or home screen
box plan | up | status | drift        --tenant <domain or id>, --only <step>, --json, --yes
box apply <step> | check <step>
box gate <id> [--check]
box config show | export | import <file> | validate [--for <task>]
box policy validate | resolve | coverage
box evidence | doctor | handoff
box check-literals | check-connectors
```

Exit codes: 0 ok, 1 failure, 2 validation error, 3 waiting on a gate.

### 18.8 Scheduled jobs

Hourly people sync and role sync, and weekly drift and evidence, run as
GitHub Actions in the company's private copy of the engine repository.
That copy holds only the tenant ID and the pipeline app's client ID as
repository variables, and the federated credential trust. Each job reads
the configuration from the tenant. `box handoff` sets this up at the end
of step 8.

### 18.9 First sign-in passes

Each new account gets a one-time Temporary Access Pass, valid for 8 hours
by default (`tenant.first_sign_in_hours`), issued by the pipeline app. In
step 6 the owner chooses, per person:

- **Show on screen:** the run's final screen lists each pass, with a
  print option. Nothing is stored.
- **Email to a private address:** the owner enters the person's personal
  address. At the end of the run the TUI sends one message per person
  from the owner's own mailbox, using the owner's delegated sign-in
  (`Mail.Send` stays in the pipeline app's `never` list). If the owner's
  mailbox is not available yet, that person falls back to on screen.

Personal addresses are held only in the encrypted draft and in memory
until the message is sent, then discarded. They are never written to the
configuration, the `People` list, events or logs. Passes are never
logged. A person who misses the window gets a new pass from the home
screen.

## 19. Jurisdictions and disclosures

### 19.1 Jurisdictions

Step 2 asks:

- **Where is the company registered?** One choice, suggested from
  discovery.
- **Where do you have customers, staff or data?** Checkboxes.

Each jurisdiction is a file `defaults/jurisdictions/<code>.yaml` holding
rules only: fields required on correspondence; privacy regime; whether a
local representative is required; legal pack notices to switch on;
retention hints. The place of registration sets the correspondence
requirements and pre-fills the disclaimer and letterhead. Each operating
jurisdiction adds only the fields it needs and switches on its notices.
"Other…" uses a generic file with nothing required and advises checking
with counsel.

Stored as `jurisdictions: {registered: <code>, operating: [<code>, ...]}`.

Rules files are drafts until counsel has reviewed them: each carries
`status: DRAFT | REVIEWED` and a `reviewed_on` date, and the TUI shows
"not yet reviewed by counsel" next to a draft jurisdiction.

The first release ships these files:

| Group | Codes |
| --- | --- |
| Generic | `generic` |
| United States | `us` (federal), `us-de`, `us-ca`, `us-tx`, `us-ny`, `us-wa`, `us-fl` |
| Canada | `ca` (federal), `ca-on`, `ca-qc`, `ca-bc` |
| United Kingdom | `gb` |
| European Union and EEA | `eu` (common rules), `de`, `fr`, `ie`, `nl`, `es`, `it` |
| Rest of Europe | `ch` |
| Asia-Pacific | `au`, `nz`, `sg`, `in`, `jp`, `hk` |
| Middle East and Latin America | `ae`, `br`, `mx` |

A state or province file extends its country file. An EU member file
extends `eu`. Other countries are added as data (R3).

### 19.2 Disclosures

| Disclosure | Applied by |
| --- | --- |
| External email disclaimer (text and HTML, placeholders, required items checked) | Exchange transport rule "apply disclaimer", outbound to external recipients; fallback "wrap" |
| Internal email footer (optional) | Transport rule scoped to internal recipients |
| Document footer | Dictionary constant `company.footer.document`, bound into template footers (T4) |
| Letterhead block | Dictionary constants from company data and jurisdiction rules |
| Meeting recording notice | Teams meeting policy and recording consent |
| Auto-reply legal lines | Shared mailbox automatic replies |
| Published notices | Legal pack `published` templates switched on by jurisdiction; notices flow |

Texts are stored under `disclosures`. Placeholders are rendered from the
`Dictionary` at apply time. Disclaimer rules are part of the Exchange
provider's plan, so changes made in the admin centre show as drift.

## 20. Hardening

### 20.1 Policy tier, rollout and exceptions

The tenant's settings are resolved from the policy catalogue (§23). The
configuration stores the selection:

```yaml
policy:
  frameworks: [nist_csf_2, nis2, iso27001_2022, soc2, cis_v8]
  tier: core                    # core | enhanced
  rollout:
    stage: audit                # audit | enforce
    audit_days: 14
    pilot_group: grp-admins
  consumer_mail_domains: [gmail.com, outlook.com]   # optional; data loss prevention (T14-11)
  exceptions:
    - {id: TMS-004, reason: "Board meeting guests skip the lobby", reference: EXC-0001, ends: 2027-05-31}
```

- `frameworks` chooses what is reported, collected and scheduled. The
  technical settings are the same whichever frameworks are ticked.
- `tier: core` (default) applies every `core` setting. `enhanced` adds the
  stricter settings that cost users some convenience. `addon` settings are
  never applied (§20.3).
- `rollout`: settings whose mode is `staged` start in audit or report-only
  on the pilot group for `audit_days`, then move to enforce when `stage`
  changes to `enforce`. That change is a configuration save like any other.
- `consumer_mail_domains`: personal mail providers that data loss
  prevention treats as outside the company (T14-11). Terms that mark the
  company's own documents are company data, so they are Dictionary values
  (`company.marker_terms`), not configuration (R11).
- `exceptions`: every departure from the selected tier. `id` names a
  setting, or a requirement to except all of its settings; `reason` is
  plain language; `reference` is the `Exceptions` list item (§23); `ends`
  is a date. An exception without all four fields, naming an unknown id,
  or whose end date has passed fails `box policy validate` and `box config
  validate`; when it expires the setting returns on the next apply.

The resolved settings are the tier's settings available on the plan,
minus the exceptions. Every exception appears on the Review screen and in
evidence. These replace the v1.1 hardening presets and overrides (SC-19).

### 20.2 Fixed rules

Not overridable: break-glass accounts are excluded from, and never
subject to, Conditional Access; no standing Global Administrator other
than break-glass; legacy authentication blocked; no client secrets on the
pipeline app.

### 20.3 Capabilities

Discovery maps subscribed plans to capabilities in
`engine/capabilities.yaml` (for example `conditional_access`, `intune`,
`defender_office_p1`, `power_automate_standard`). A step whose capability
is missing uses its declared fallback (Conditional Access falls back to
Security Defaults) or is skipped and listed on the Review screen with the
upgrade that would enable it. A policy setting whose capability is an
add-on is never applied; it is listed as not applied in the coverage
report (§23). Paid licences are never assigned unless the
configuration names them, and never beyond the seats available.

## 21. Secrets and local data

| Item | Stored | Never |
| --- | --- | --- |
| Global Administrator session | Memory only; discarded after bootstrap | On disk, in logs |
| Owner session | OS token cache | In the repository |
| Wizard draft | User config directory, AES-256-GCM, key in the OS keychain; deleted after upload | In the repository, unencrypted |
| Pipeline certificate private key | OS keychain | In the repository or the tenant |
| DNS provider token | OS keychain; GitHub Actions secret after `box handoff` | In the configuration |
| Inbound token | Generated; OS keychain, GitHub secret, solution environment variable | In the configuration |
| Break-glass passwords | Shown once, printable | Anywhere |
| First sign-in passes | Shown once on screen, or emailed once (§18.9) | In logs, events, configuration |
| Personal email addresses for passes | Encrypted draft and memory until sent | Configuration, lists, events, logs |

The configuration stores references only, for example
`secrets: {dns_token: keychain:box/<tenant-id>/dns}`. `box config export`
moves an unfinished draft to another machine as an encrypted file.

## 22. Gates

| Gate | Guide | Check |
| --- | --- | --- |
| G-00 Microsoft 365 sign-up | Plan choice and Microsoft's sign-up page | A successful sign-in |
| G-01 Licences bought | Which plans and how many seats | Subscribed plans cover the plans chosen in step 6 |
| G-02 Security keys registered | Register two FIDO2 keys per break-glass account; seal one set offline | Each break-glass account lists a FIDO2 method |
| G-03 Flow owner connections | Create the connections the solution asks for | Every connection reference is connected |
| G-04 Public forms created | Create the NDA and demo request forms from `flows/forms/` | Each pasted form link is reachable; its ID is stored in configuration `ids` |
| G-05 Founder's standing admin role removed | Remove the role after T1 passes | The founder holds no directory role |
| G-06 Manual DNS records | The records table for `dns.provider: manual` | Records resolve to the expected values |
| G-07 Apple push certificate | Create the Apple MDM push certificate with the company's Apple account and upload it in the Intune admin centre (T14-06) | Intune reports a valid certificate and its expiry date |
| G-08 Data loss prevention review | Review each policy's matches from the audit period with the owner before enforcement (T14-11) | Each policy's match report is stored as evidence and the owner's confirmation is an `Events` item |

`runbooks/00-bootstrap.md` is generated from these definitions.

## 23. Policy sets

The engine's policy catalogue is in `policy/`. `policy/README.md`
describes the mechanism; `box policy coverage` reports the current result
for a selection and a plan.

| File | Holds |
| --- | --- |
| `policy/requirements.yaml` | 90 requirements in 14 domains. Each has a plain statement, a tier (`core`, `enhanced`, `addon`), a type (technical, procedural, mixed), the settings that enforce it, its evidence source, and references to the five frameworks. |
| `policy/settings/<area>.yaml` | 333 settings across 12 admin areas. Each names the requirements it serves, the capability it needs, its tier, and its mode (`enforce`, `staged`, `report`). Values are stated in words; T14-03 resolves each to the exact API setting. |
| `policy/frameworks/<id>.yaml` | Each framework's controls and the requirements that answer them. |
| `policy/capabilities.yaml` | Which capabilities the base plans include and which need an add-on; reconciled with `engine/capabilities.yaml` (T14-02). |

- Five frameworks are mapped: NIST CSF 2.0, NIS2 Article 21, ISO/IEC
  27001:2022 Annex A, SOC 2, CIS Controls v8. Framework references are a
  working mapping, to be validated against the source texts before an
  audit relies on them.
- **Selection** is the configuration's `policy` section (§9.1, §20.1):
  frameworks, tier, rollout stage, exceptions. It is edited in the
  onboarding TUI's Security step (§18.4, step 4) or on the home screen.
  The `Frameworks` list in the Admin site is the tick-box view of the same
  selection: a change there is saved by the pipeline app as a new version
  of the configuration document with one `Events` item, and is applied
  only when the owner runs the resulting plan. Nothing is changed through
  pull requests.
- **Resolution**: `box policy resolve` keeps a setting when the plan has
  its capability, its tier is selected and no exception removes it. It
  produces the settings to apply per area, the settings not applied with
  the reason, and coverage per framework. It runs at plan time. Its output
  is never committed to the repository: the applied state is recorded in
  the tenant as `PolicySettings` and `Requirements` items, and the
  coverage report and the not-applied list are stored in the Compliance
  site.
- **Validation**: `box policy validate` fails when a setting names no
  existing requirement or an unknown capability, a technical requirement
  has no setting, a framework control names an unknown requirement, an id
  is repeated, or an exception is incomplete, unknown or expired.
- **Apply**: each admin area is a provider step (§14) run by the
  orchestrator with the pipeline app. Staged settings go out in audit
  first, on the rollout pilot group for `rollout.audit_days`, and move to
  enforce when `rollout.stage` changes. Every setting the product allows
  excludes `grp-break-glass` (§20.2).
- **Add-on settings are never applied.** A setting whose capability needs
  a paid add-on is listed as not applied in the coverage report, with the
  add-on that would enable it.
- **Ticking a framework** switches on its coverage report control by
  control, its procedural requirements as recurring tasks, its evidence
  schedule and its incident reporting deadlines. It does not change the
  technical settings.
- **Standards are rendered from requirements**: one written standard per
  domain, from `policy/requirements.yaml`, so a document and the tenant
  cannot disagree. Policies (§15) cite requirement ids instead of
  repeating rules.
- **Register lists** added in `engine/register/lists.yaml`: `Frameworks`,
  `Requirements`, `PolicySettings`, `Exceptions`. An approved
  `Exceptions` item becomes a `policy.exceptions` entry: `id` from
  `requirement_id`, `reason`, `reference` from `exception_id`, `ends` from
  `expires_on`.

Data protection for a small company rests on six settings groups, applied
in this order:

1. block automatic forwarding;
2. label with a default;
3. block Restricted from leaving;
4. restrict sharing from the record sites;
5. detect mail to personal accounts;
6. limit automation connectors.

Development of this section is track T14 (`tasks/T14-01.md` to
`tasks/T14-16.md`).

## 24. Choices

### 24.1 One window, every decision

Every decision an owner makes is a **choice** in `defaults/choices.yaml`,
asked in the onboarding window (§18.4, steps 3 to 5) in plain language.
There is no decision made only in documents: a recommendation the
engine's authors would make becomes the choice's recommended option.

Each choice has a question, help text, options with their cost, a
recommended option (or one recommended under a condition), and an
optional `detect` that discovery fills in (for example an existing Azure
subscription, or a directory synchronised from on-premises Active
Directory). Choices that do not apply to the company (`when`) are not
shown.

### 24.2 Effects

An option can switch modules on or off; select a setting variant (the
same setting delivered a different way, mapped in
`expected-state/setting-ids/*.json`); leave settings out while it holds
(recorded like an exception, with the choice as its reference); require
permission bundles; and add gates. The resolver (§23) applies the choices
before the tier and exceptions. Changing a choice later from the home
screen saves a new configuration version, then plans and applies the
difference.

The configuration stores the answers:

```yaml
choices:
  directory.mode: cloud
  azure.subscription: none
  signin.device_code: allow
```

`box config validate` fails on an unknown choice or option, and on a choice
whose `when` does not hold.

### 24.3 Permission bundles

`expected-state/app-permissions.yaml` groups the pipeline app's
permissions into bundles: `base` (always), and one per area or choice
(for example `devices`, `purview`, `powerplatform`, `azure`). Bootstrap
grants `base` plus the bundles the configuration's modules and choices
need, and nothing else; `box check identity` compares the app with that
union. The Review screen lists the bundles with the settings that need
them, so the owner approves exactly what the selection uses at the one
Global Administrator sign-in. Adding a choice that needs a new bundle
later asks for that sign-in again (gate).

### 24.4 Costs

The product applies only what the subscription includes (R2). An option
that could lead to a charge says so in its `cost` (for example creating an
Azure subscription: nothing is billed until resources are created), and
the Review screen lists every such note before the build.

## 25. Approval and simplicity

The product suggests; the owner decides (SC-25). It never makes a decision
for the company.

### 25.1 Suggest, then approve

- Every value the box would otherwise pick is a choice with a recommended
  answer (§24), shown before anything is applied. An unanswered value is
  listed on the Review screen; the box never fills one silently or skips
  one silently. Choices may carry `values` that fill configuration keys the
  settings read (`devices.*`, `ai_policy.*`, `values.*`).
- `box plan` writes a **proposal**: every change in plain words, grouped by
  area (§25.2), with why (requirement, framework) and what it affects. The
  owner approves all of it, an area, or single changes (`box approve`, or the
  Review screen); a rejected change becomes an exception with the owner's
  reason. The approval (who, when, the digest of each approved change) is
  an `Events` item and a local record.
- `box apply` and `box up` apply only approved changes. A step whose plan
  holds an unapproved change waits ("needs approval") and names the changes.
- Policies take two approvals: audit (report-only, nobody blocked), then,
  after the audit report, enforce. Enforcing is never automatic.
- Later changes (a new choice, a catalogue update, drift repair) arrive as a
  new proposal on the home screen; nothing is applied in the background.
- Development on a test tenant may approve with `box approve --all` from the
  developer's own terminal.

### 25.2 Areas

Everything is organised by what an owner cares about, not by admin centre
(`defaults/areas.yaml` maps steps and centres to areas): Sign-in and
accounts; Email; Files and sharing; Meetings and chat; Computers and
phones; Sensitive information; Apps and automation; Your web address;
Cloud.

### 25.3 Five questions

The onboarding asks five things, with everything else on the
recommendation: sign in; what do you run (modules); how careful (one dial:
Essential, Recommended, Strict, setting the tier and each choice's default,
with "fine-tune" for the individual choices); who works here; review and
approve (one card per area in plain words: Approve, Show details, Not now).
After the build the home screen shows each area as protected, watching
(audit), or needs you, with an Enforce action per area once its audit
period is clean.

## 26. Audit pack

A second entry point, "Prepare for an audit" (`box audit`), reads and
never writes (SC-26). It works on any Microsoft 365 tenant.

- Sign-in is delegated, as a person with Global Reader (plus Security Reader
  and Compliance Reader), with read-only scopes. No app registration,
  certificate or tenant write. In audit mode the executor runs only `test`,
  `verify` and `evidence`; `plan` and `apply` are refused.
- It reads every catalogue setting's live value through its mapping's
  `read` and `check`; inventories (users and admins with MFA and last
  sign-in, roles, guests, Conditional Access, authentication methods,
  devices and compliance, sharing, mailbox forwarding, DLP, labels,
  retention, audit log status, alert policies, licences); and activity over
  the audit period where the tenant keeps it.
- The pack, written to a folder the person chooses and never to the
  repository: `index.html` and `controls.csv` (one row per framework
  control: met, partly met, not met, document needed; evidence files; the
  gap in plain words); `evidence/<control>/` raw reads stamped with tenant,
  time and query; `manifest.sha256`; `gaps.md` ordered by risk, each with
  the proposal that would fix it.

