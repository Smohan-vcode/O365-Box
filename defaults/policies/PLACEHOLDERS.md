# Placeholders in policy and procedure templates

Templates in this folder hold no company values (SPEC §2 R1). Every company
value is a placeholder, filled when the box renders the document. A
placeholder is written `{{namespace.key}}`. Three namespaces exist:

- `dictionary.*` — a value from the tenant's `Dictionary` list (SPEC §9.2),
  edited by the owner. A missing value is listed by the renderer and the
  document stays a draft.
- `generated.*` — computed by the engine at render time from the
  configuration document, the catalogue in `policy/` and the document's own
  front matter.
- `jurisdictions.*` — rendered from the jurisdiction rules files the
  configuration selects (SPEC §19.1). Rules files still marked `DRAFT` are
  rendered with a "not yet reviewed by counsel" note.

`go test ./engine/internal/docs` fails when a template uses a key not
listed below, or a role key that names a role not in `defaults/roles.yaml`.

## Dictionary

| Key | Description |
| --- | --- |
| `dictionary.company.legal_name` | The company's full registered legal name. |
| `dictionary.company.trading_name` | The short name the company trades under. |
| `dictionary.company.primary_domain` | The company's primary mail and web domain. |
| `dictionary.company.registered_address` | The registered office address on one line, composed from the `company.address.*` values. |
| `dictionary.role.owner.name` | Name of the person who holds the `owner` role. |
| `dictionary.role.owner.email` | Work email address of the `owner` role holder. |
| `dictionary.role.owner.phone` | Phone number at which the `owner` role holder can be reached in an incident. |
| `dictionary.role.control-owner.name` | Name of the person who holds the `control-owner` role. |
| `dictionary.role.control-owner.email` | Work email address of the `control-owner` role holder. |
| `dictionary.contact.deputy.name` | The person who acts for the owner when the owner is unavailable (incident and continuity deputy). |
| `dictionary.contact.deputy.phone` | Phone number of the deputy. |
| `dictionary.contact.it_provider` | Name of the company's IT support provider, or "internal". |
| `dictionary.contact.it_provider_phone` | Phone number of IT support for urgent calls. |
| `dictionary.contact.legal_counsel` | Name of the company's legal counsel or law firm. |
| `dictionary.contact.legal_counsel_phone` | Phone number of legal counsel. |
| `dictionary.contact.insurer` | Name of the cyber insurer, or "none". |
| `dictionary.contact.insurer_phone` | The insurer's incident hotline. |
| `dictionary.contact.insurer_policy` | The cyber insurance policy number. |
| `dictionary.contact.domain_registrar` | Name of the registrar that holds the company's domains. |
| `dictionary.contact.domain_registrar_phone` | The registrar's support phone number. |
| `dictionary.contact.dns_provider` | Name of the service that hosts the company's DNS, if not the registrar. |
| `dictionary.backup.provider` | Name of the independent backup service for mail and files, or "none chosen". |
| `dictionary.training.provider` | Name of the security awareness training service, or "internal material". |
| `dictionary.continuity.rto_mail` | Target time to restore mail and calendars after a serious outage (for example "1 working day"). |
| `dictionary.continuity.rto_files` | Target time to restore files and sites. |
| `dictionary.continuity.rto_admin` | Target time to regain administrator access to the tenant. |

## Generated

| Key | Description |
| --- | --- |
| `generated.date` | Date of this render, which is the approval date once the owner approves the draft. |
| `generated.version` | The document version: front matter `version`, raised when an approved text changes. |
| `generated.next_review` | The render date plus the front matter `review_months`. |
| `generated.review_months` | The front matter `review_months`, as a number. |
| `generated.requirement_table` | Table of the document's front matter `requirements`: id, statement, tier, type and the selected frameworks' references, from `policy/requirements.yaml`, with this tenant's status for each. |
| `generated.settings_summary` | Plain-words summary of the applied settings that serve the document's requirements, from the resolved policy (SPEC §23), with settings not applied and why. |
| `generated.frameworks` | Titles of the frameworks selected in the configuration's `policy.frameworks`. |
| `generated.retention_schedule` | The retention schedule table from the configuration's `retention` section and `defaults/retention.yaml`. |
| `generated.approved_ai` | The AI features and apps the owner approved (the `ai_policy` choices and the allowed-apps list). |
| `generated.mailbox.security` | Address of the security shared mailbox, from the configuration's `mail.shared` entry `security` and the primary domain. |
| `generated.mailbox.privacy` | Address of the privacy shared mailbox, from the configuration's `mail.shared` entry `privacy` and the primary domain. |

## Jurisdictions

| Key | Description |
| --- | --- |
| `jurisdictions.registered` | Name of the place where the company is registered (`jurisdictions.registered`). |
| `jurisdictions.operating` | Names of the places where the company has customers, staff or data (`jurisdictions.operating`). |
| `jurisdictions.notification_deadlines` | Table, one row per selected jurisdiction and regime: what must be notified, to whom, and the deadline, from each rules file's incident notification entries. |
