---
id: POL-01
title: Information security
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [GOV-01, GOV-04, GOV-05]
controls:
  soc2: [CC1.4, CC4.1, CC4.2, CC5.3, CC7.1, CC8.1]
  iso27001_2022: [A.5.1, A.5.35, A.5.36, A.8.9, A.8.32]
enforced_by: [GOV-05, GOV-04, F17, F18]
records: ["ControlTasks:policy_review yearly", "Acknowledgements:POL-01 on joining and each version", "Evidence:coverage monthly"]
---

# Information security policy

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved: {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

This policy sets out how {{dictionary.company.trading_name}} protects the information it holds for itself, its customers, its staff and its suppliers. It is the top document of the security policy set: every other policy and procedure sits beneath it.

## 2. Scope

This policy applies to everyone who works for or on behalf of {{dictionary.company.trading_name}}: employees, directors, contractors and suppliers' people with access to company systems. It covers all company information, wherever it is held, and every device and service used to reach it, including the company's Microsoft 365 tenant for {{dictionary.company.primary_domain}}.

## 3. Commitments

{{dictionary.company.trading_name}} will:

1. Protect the confidentiality, integrity and availability of company information in proportion to its value and the harm its loss would cause.
2. Meet the security requirements listed in section 7 through settings applied to the tenant, written procedures and recurring tasks.
3. Change tenant settings only through a reviewed change, and detect changes made any other way (POL-14).
4. Assess risks every year and after any major change, and record how each risk is treated (POL-15).
5. Record any departure from a requirement with a reason, an approver and an end date (PRC-14).
6. Report coverage of the requirements and the tenant's security score every month (PRC-15).
7. Train everyone on joining and every year, and ask them to acknowledge the policy set (PRC-10).
8. Respond to incidents promptly and notify authorities and affected people when the law requires (POL-13).

## 4. Roles

| Role | Responsibility |
| --- | --- |
| Owner ({{dictionary.role.owner.name}}) | Approves the policy set, accepts residual risk, approves exceptions, holds overall accountability for security. |
| Control owner ({{dictionary.role.control-owner.name}}) | Runs the recurring control tasks, keeps the records and reports gaps to the owner. |
| Everyone | Follows the policies, completes training, reports suspected incidents at once to {{generated.mailbox.security}}. |

Where one person holds several roles, the records show who acted and when.

## 5. Policy set

The policy set is made of the policies POL-01 to POL-21 and the procedures PRC-01 onwards. Each document states its owner, its approver, how often it is reviewed and the requirement identifiers it answers. Technical standards are not repeated in the text: they are rendered from the requirement catalogue, so that the documents and the tenant cannot disagree.

## 6. Approval, publication and review

- The owner approves each document before it is published to the Policies library.
- Each published version is acknowledged by everyone it applies to.
- Every document is reviewed at least every {{generated.review_months}} months, after a significant incident, and after a major change to the business or the tenant. The review is a recurring control task; its outcome is recorded even when nothing changes (PRC-15).
- A document edited in the Policies library is never overwritten by a new render; the edited text is reviewed and approved as the next version.

## 7. Requirements and standards

{{generated.requirement_table}}

## 8. Current settings

{{generated.settings_summary}}

## 9. Breaches of this policy

A breach of this policy may lead to access being withdrawn and to action under the terms of employment or contract. Anyone unsure how a rule applies should ask the owner before acting.
