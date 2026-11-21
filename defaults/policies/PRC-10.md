---
id: PRC-10
title: Awareness training programme
version: "1.0"
owner_role: control-owner
approver_role: owner
review_months: 12
requirements: [AWR-01, AWR-02]
controls:
  soc2: [CC1.4, CC2.2]
  iso27001_2022: [A.5.10, A.6.2, A.6.3]
enforced_by: [AWR-02, F18, F17]
records: ["ControlTasks:awareness yearly", "Acknowledgements:on joining and each version", "JoinerChecklists:joiner on event"]
---

# Awareness training programme

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved: {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

Everyone at {{dictionary.company.trading_name}} should know how to spot and report the threats they are most likely to meet, and what the company's rules expect of them.

## 2. Who and when

| When | Who | What |
| --- | --- | --- |
| On joining, before access to Confidential data and within the first week | Every joiner | Core training (section 3) and acknowledgement of the policies |
| Every year | Everyone | Core training refreshed with the year's lessons |
| On each new policy version | Everyone it applies to | Acknowledgement of the new version |
| On taking an administrator or approver role | The role holder | Role training (section 4) |
| After an incident or a near miss | Those involved, or everyone | A short lesson on what happened |

## 3. Core training (about 45 minutes)

1. Why security matters to the company and to customers.
2. Phishing and fraud: recognising suspicious mail, links, attachments, calls and payment-change requests; using the Report button.
3. Accounts: multi-factor sign-in, unexpected sign-in requests, passwords and password managers.
4. Devices: screen lock, updates, lost devices, personal phones and protected apps.
5. Handling information: the four labels, sharing with named people, personal mail accounts.
6. AI tools: what is approved and what must never be pasted into them.
7. Reporting incidents and mistakes: how and why at once.

The training is delivered by {{dictionary.training.provider}}, or by the control owner using the company's own material. It ends with a short quiz.

## 4. Role training

Administrators: emergency access, change proposals, alert review, the access review. Approvers and signers: the approval rules and signing authority. Control owners: the control tasks and how to record them.

## 5. Records

- Completion of training by each person, with date and quiz result, is recorded and attached to the yearly awareness control task.
- Acknowledgements of each policy version are recorded automatically.
- The joiner checklist shows training and acknowledgement as complete before it closes.

## 6. Follow-up

The control owner reports completion to the owner each year. Anyone overdue by more than 30 days is reminded; after 60 days the owner decides on action, which may include suspending access.
