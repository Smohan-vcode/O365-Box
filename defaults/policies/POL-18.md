---
id: POL-18
title: Logging and monitoring
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [LOG-01, LOG-02, LOG-03, LOG-04, GOV-04]
controls:
  soc2: [CC4.1, CC4.2, CC7.2, CC7.3]
  iso27001_2022: [A.5.25, A.5.28, A.5.35, A.5.36, A.8.15, A.8.16]
enforced_by: [LOG-01, LOG-03, LOG-04, GOV-04, F17]
records: ["ControlTasks:alert_review each working day", "Evidence:audit_logs weekly"]
---

# Logging and monitoring policy

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved: {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

Logs show what happened and alerts show it quickly. This policy makes sure the right activity is recorded, kept long enough and watched.

## 2. Rules

1. **Record.** Administrator, user and mailbox activity is recorded in the audit log. Recording is never turned off.
2. **Keep.** Audit and sign-in records are kept for at least 180 days and exported every week to company storage for longer keeping, as the retention schedule sets (POL-07).
3. **Alert.** High-risk administrative actions raise an alert: new forwarding rules, role changes, and changes to audit or sharing settings. Use of an emergency access account always raises an alert.
4. **Watch.** Security alerts go to a monitored mailbox, {{generated.mailbox.security}}, and are reviewed every working day (PRC-08). A named deputy covers absences.
5. **Report.** Coverage of the requirements and the tenant's security score are reported to the owner every month (PRC-15).
6. **Protect the logs.** Only administrators and the pipeline identity can read the audit logs. Exported logs are kept in a library that ordinary users cannot change.

## 3. Privacy

Logs contain personal data. They are used for security, troubleshooting and compliance, and never to monitor individual performance without the owner's approval and a lawful basis.

## 4. Standards

{{generated.requirement_table}}
