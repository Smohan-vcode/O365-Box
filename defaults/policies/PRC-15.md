---
id: PRC-15
title: Policy and compliance review
version: "1.0"
owner_role: control-owner
approver_role: owner
review_months: 12
requirements: [GOV-01, GOV-04]
controls:
  soc2: [CC1.4, CC4.1, CC4.2, CC5.3]
  iso27001_2022: [A.5.1, A.5.35, A.5.36]
enforced_by: [GOV-04, F17, F18]
records: ["ControlTasks:policy_review yearly", "Evidence:coverage monthly"]
---

# Policy and compliance review procedure

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

This procedure keeps the policy set current and approved, and gives the owner a monthly view of how well the company meets its requirements.

## 2. Monthly coverage report

Each month the box produces a coverage report: requirements met, partly met and not met; settings not applied and why; open exceptions; the tenant's security score and its trend; and overdue control tasks. The selected frameworks are {{generated.frameworks}}.

1. The control owner reads the report and notes anything that got worse since last month.
2. For each new gap, the control owner raises a proposal, an action with an owner and date, or an exception (PRC-14).
3. The owner reviews the report and the notes. The reviewed report is stored in the Compliance site as evidence.

## 3. Yearly policy review

The policy review control task is raised once a year. For each document in the policy set:

1. Read it against the last year's incidents, risk assessment, audit findings, changes to the business and tenant, and changes in law in the places the company operates.
2. Confirm the owner, contacts and placeholders render correctly with current company values.
3. Propose changes as tracked edits in the Policies library, or note "no change".
4. The owner approves each changed document as a new version. Publishing a new version asks everyone it applies to for acknowledgement.
5. Confirm the continuity plan's offline items are current (PRC-04).

Documents are also reviewed after a significant incident or major change, without waiting for the yearly task.

## 4. Record

Close the yearly task with the list of documents, each with its new version or "no change", the approval date, and the acknowledgement status. Monthly reports are kept as evidence for each month.
