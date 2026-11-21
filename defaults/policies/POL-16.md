---
id: POL-16
title: Business continuity and backup
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [BCK-01, BCK-02, BCK-03, BCK-04, IAM-07]
controls:
  soc2: [A1.2, A1.3, CC6.1, CC9.1]
  iso27001_2022: [A.5.29, A.5.30, A.8.2, A.8.13, A.8.14]
enforced_by: [BCK-01, BCK-02, F17]
records: ["ControlTasks:restore_test yearly", "ControlTasks:continuity_test yearly", "ControlTasks:break_glass_test every six months"]
---

# Business continuity and backup policy

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

{{dictionary.company.trading_name}} must be able to keep working, or get back to work quickly, when something serious goes wrong: lost files, a compromised account, an outage, or loss of access to the tenant, the domain or a key person's account. This policy sets the rules; PRC-03 and PRC-04 set the steps.

## 2. Rules

1. **Recoverable by default.** Files keep version history and deleted items can be recovered. Users' working folders are saved to company storage, not only to the device.
2. **Independent backup.** Where the company has chosen a backup held independently of the live service ({{dictionary.backup.provider}}), mail and files are backed up to it. Where it has not, the owner records the decision and the risk it leaves (POL-15).
3. **Restore tested.** Recovery of files, mail and administrator access is tested every year (PRC-03). The result, the time it took and any problem found are recorded.
4. **Continuity plan.** The company keeps a continuity plan (PRC-04) that covers at least loss of the tenant, loss of the domain and loss of the founder's or owner's account, with the contacts and steps for each.
5. **Emergency access.** Two emergency access accounts exist so that the company can regain control of its tenant if normal administrator sign-in fails. They are tested twice a year (PRC-07).
6. **Offline copies of what you need to recover.** The continuity plan, the emergency access credentials, the registrar and DNS account details and the key contacts are kept in a sealed place outside the tenant, so that they are available when the tenant is not.
7. **Review.** The plan is reviewed every year with the policy set and after any change to suppliers, domains or key people.

## 3. Targets

| Service | Recovery target |
| --- | --- |
| Mail and calendars | {{dictionary.continuity.rto_mail}} |
| Files and sites | {{dictionary.continuity.rto_files}} |
| Administrator access | {{dictionary.continuity.rto_admin}} |

## 4. Roles

The owner approves the plan and decides when to invoke it. The control owner runs the yearly tests and keeps their records.

## 5. Standards

{{generated.requirement_table}}
