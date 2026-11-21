---
id: PRC-08
title: Security alert review
version: "1.0"
owner_role: control-owner
approver_role: owner
review_months: 12
requirements: [LOG-02, LOG-03, LOG-04]
controls:
  soc2: [CC7.2, CC7.3]
  iso27001_2022: [A.5.25, A.5.28, A.8.15, A.8.16]
enforced_by: [LOG-02, LOG-03, LOG-04, F17]
records: ["ControlTasks:alert_review each working day", "Evidence:audit_logs weekly"]
---

# Security alert review procedure

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved: {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

Alerts are only useful if someone looks at them. This procedure makes sure security alerts and user reports are reviewed every working day.

## 2. Where alerts arrive

All security alerts and reported messages go to {{generated.mailbox.security}}. The control owner, {{dictionary.role.control-owner.name}}, reviews it; {{dictionary.contact.deputy.name}} covers absences. Alerts include:

- high-risk administrative actions: forwarding rules created, administrator roles changed, audit or sharing settings changed;
- any sign-in by an emergency access account;
- threats detected on devices or in mail, and what automatic investigation did;
- messages users reported as suspicious;
- drift from the approved configuration.

## 3. Daily review (each working day, about 15 minutes)

1. Open the security mailbox and the alert queue in the security portal.
2. For each new alert, decide:
   - **expected** (for example an approved change, a planned test): note the reason and close it;
   - **suspicious**: look at the user's recent sign-ins and activity; if anything is unexplained, open an incident (PRC-01);
   - **confirmed threat**: open an incident at the severity PRC-01 gives.
3. For each reported message, check the verdict. Release false positives; for real phishing, remove matching messages from other mailboxes and thank the reporter.
4. Check that automatic remediation completed. Finish anything it could not.
5. Note in the day's control task the number of alerts and reports handled and any incident opened.

## 4. Weekly checks

- Confirm the weekly audit and sign-in log export ran and the files are in the log library.
- Confirm audit logging is still on.
- Spot-check that alert policies are still active.

## 5. Record

Each working day's review closes that day's alert review control task. Missed days are visible in the register; the owner follows up any gap of more than two working days.

## 6. Keeping records

Audit and sign-in records are kept in the service for at least 180 days and exported weekly for longer keeping, as the retention schedule sets.
