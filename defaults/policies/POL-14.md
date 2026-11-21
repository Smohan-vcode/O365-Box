---
id: POL-14
title: Change management
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [GOV-05, CFG-01, GOV-03]
controls:
  soc2: [CC3.4, CC5.3, CC7.1, CC8.1]
  iso27001_2022: [A.5.36, A.8.9, A.8.32]
enforced_by: [GOV-05, CFG-01]
records: ["Events:every configuration change on event", "Evidence:drift weekly"]
---

# Change management policy

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved: {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

Uncontrolled changes are a common cause of outages and security gaps. This policy makes sure changes to company systems are planned, approved, recorded and reversible.

## 2. Scope

Changes to the Microsoft 365 tenant's settings, access policies, groups and roles, sites and libraries, automation flows, device configuration, the company's domains and DNS, and the company's configuration document.

## 3. Rules

1. **Proposal first.** A change is first written as a proposal that says what will change, why (the requirement or business need) and what it affects.
2. **Approval.** The owner approves each proposal, in full, by area, or change by change. A rejected change is recorded as an exception with the owner's reason (PRC-14).
3. **Apply only what was approved.** Only approved changes are applied. Security settings go out in audit first, where the product allows, and are enforced only after the owner reviews the audit result.
4. **Record.** Every change and every configuration save writes a record of who changed what and when. These records are never edited.
5. **Detect drift.** Settings are compared with the approved state every week. A change made outside this process is reported and either approved as a new proposal or reversed.
6. **Engine changes.** Changes to the engine that manages the tenant are made by reviewed pull request, built and verified on a test tenant first.
7. **Emergency changes.** In an incident the owner may make a change at once. It is recorded within one working day and reviewed as if it had been proposed.
8. **Rollback.** Every proposal says how to undo the change.

## 4. Baseline

A hardened baseline is applied to devices and the tenant, and deviation from it is detected and reported.

## 5. Standards

{{generated.requirement_table}}
