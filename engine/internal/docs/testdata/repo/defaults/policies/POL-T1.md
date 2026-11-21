---
id: POL-T1
title: Identity and access
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [IAM-01, IAM-13]
controls: {soc2: [CC6.1, CC6.2]}
enforced_by: [IAM-01]
records: []
---
# Identity and access

This policy applies to everyone who works for {{dictionary.company.legal_name}}.
It takes effect on {{generated.date}} (version {{generated.version}}).

## Rules

- Everyone signs in with **multi-factor authentication**.
- Access is granted to *groups*, never to one person.
  Exceptions need the owner's approval.

1. Ask for access.
2. The owner approves.

## Requirements

{{generated.requirement_table}}

## Settings

{{generated.settings_summary}}

Report incidents to {{generated.mailbox.security}} or call {{dictionary.role.owner.phone}}. Registered in {{jurisdictions.registered}}.
