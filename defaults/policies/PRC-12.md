---
id: PRC-12
title: Joiner, mover and leaver
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [IAM-12, DEV-08]
controls:
  soc2: [CC6.2, CC6.3, CC6.5]
  iso27001_2022: [A.5.9, A.5.11, A.5.16, A.5.18, A.7.14]
enforced_by: [IAM-12, DEV-08, F07, F18]
records: ["JoinerChecklists:joiner and leaver on event"]
---

# Joiner, mover and leaver procedure

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

Access follows the person's role: given when they join, changed when their role changes, and removed on the day they leave. Devices are tracked, and can be wiped when lost or when the person leaves.

## 2. Joiner

Started when the person is added to the People list with a start date, role and plan, or when their employment document is executed. The box creates the joiner checklist and completes the automatic items.

| Item | Done by | How it is checked |
| --- | --- | --- |
| Account created and plan assigned | Box, from the People list | Automatic |
| Role and access groups assigned | Box, from the person's role | Automatic |
| Multi-factor method registered | The joiner, on first sign-in | Automatic |
| Device enrolled and compliant | The joiner, with IT support | Automatic; device added to the inventory |
| Policies acknowledged | The joiner | Automatic |
| Agreements executed | The owner or HR | Automatic |
| Awareness training completed | The joiner (PRC-10) | Recorded |

The first sign-in details are given to the joiner in person or by phone, never by mail, and are never stored or logged.

## 3. Mover

When a person's role changes, the owner updates their role in the People list on the day of the change. The box adds the new role's groups and removes groups the new role does not include. Check by hand:

- shared mailbox access and send-as rights;
- approval roles and signing authority;
- any direct access granted for a project;
- the plan, if the new role needs a different one.

Record the change in the person's checklist.

## 4. Leaver

Started when the owner sets a leaving date in the People list. On the last working day, or at once for an unplanned departure, the leaver checklist is completed:

| Item | Notes |
| --- | --- |
| Sign-in blocked | Sessions revoked and methods removed at the same time. |
| Groups removed | All role and access groups. |
| Open approvals reassigned | To another holder of the same role. |
| Mailbox converted or forwarded as decided | Convert to a shared mailbox owned by the manager, or set an auto-reply; never forward outside the company. |
| Devices retired | Company devices returned and wiped; company data wiped from personal phones. Inventory updated. |
| Licence released | After the mailbox decision is complete. |

Files remain the company's (POL-06). The account is deleted after the period the retention schedule sets.

## 5. Lost or stolen devices

The user reports at once to {{generated.mailbox.security}}. An administrator locks or wipes the device (company data only on a personal phone), records the action in the inventory and opens an incident if company data may have been exposed (PRC-01).

## 6. Records

Each joiner and leaver checklist, with the time each item was completed, is kept in the JoinerChecklists register. The quarterly access review (PRC-06) checks that no leaver still has access.
