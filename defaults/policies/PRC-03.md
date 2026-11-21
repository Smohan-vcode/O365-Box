---
id: PRC-03
title: Backup and restore test
version: "1.0"
owner_role: control-owner
approver_role: owner
review_months: 12
requirements: [BCK-01, BCK-02, BCK-03]
controls:
  soc2: [A1.2, A1.3]
  iso27001_2022: [A.5.30, A.8.13, A.8.14]
enforced_by: [BCK-01, BCK-02, F17]
records: ["ControlTasks:restore_test yearly"]
---

# Backup and restore test procedure

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved: {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

A backup is only useful if it can be restored. This procedure describes how company data is protected from loss and how recovery is tested every year.

## 2. What protects company data

| Layer | Covers | Recovers from |
| --- | --- | --- |
| Version history | Every file in every library | Unwanted edits, ransomware encryption of a file |
| Recycle bins | Deleted files, sites, mail items | Accidental deletion, for the period the service keeps them |
| Saved user folders | Desktop, Documents, Pictures on company computers | Lost or broken computer |
| Independent backup ({{dictionary.backup.provider}}) | Mail and files, held outside the live service | Mass deletion, malicious administrator, loss of the tenant |
| Emergency access accounts | Administrator sign-in | Loss of normal administrator access |

If the company has no independent backup, the owner's decision and the risk it leaves are recorded in the Risks register.

## 3. Yearly restore test

The restore test control task is raised once a year. The control owner, with an administrator, carries out each step and notes the time taken.

1. **File version.** In a test folder, create a file, change it twice, then restore the first version. Confirm the content.
2. **Deleted file.** Delete a test file, then restore it from the recycle bin. Confirm it returns with its permissions.
3. **Deleted mail item.** Delete a test message from a test mailbox, purge it from Deleted Items, then recover it.
4. **Independent backup: files.** Restore one folder from the independent backup to a separate location. Compare it with the original.
5. **Independent backup: mail.** Restore one mailbox folder from the independent backup to a separate location. Confirm messages open.
6. **Computer loss.** Sign in to a spare or reset computer with a test account and confirm the saved folders return.
7. **Administrator access.** Confirm the break-glass test (PRC-07) passed in the last six months; if not, run it now.
8. **Clean up.** Remove the test data and restored copies.

Use test data only. Never restore a real person's mailbox to a location others can read.

## 4. Record

Close the control task with a short report: the date, who took part, each step's result (pass or fail), the time taken, and any problem found with the action agreed. Attach screenshots where helpful. A failed step is fixed and retested within 30 days, or recorded as a risk.

## 5. When to test outside the cycle

Also test after changing backup provider, after a major change to the tenant's storage, and after any incident that needed a restore.
