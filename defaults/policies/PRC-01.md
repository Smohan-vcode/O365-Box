---
id: PRC-01
title: Incident response plan
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [INC-01, INC-03]
controls:
  soc2: [CC7.3, CC7.4]
  iso27001_2022: [A.5.24, A.5.26]
enforced_by: [INC-03, LOG-02, F17]
records: ["ControlTasks:incident_exercise yearly", "ControlTasks:incident_register on event"]
---

# Incident response plan

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved: {{generated.date}} · Next review {{generated.next_review}}

Keep a printed or offline copy of this plan with the continuity plan (PRC-04). You may need it when company systems are down.

## 1. Severity levels

| Level | Meaning | Examples | Respond within |
| --- | --- | --- | --- |
| 1 Critical | Company-wide harm, or Restricted or large amounts of personal data exposed. | Administrator account compromised, ransomware, tenant or domain lost, mass data leak. | Immediately, any time |
| 2 High | One person's account or device compromised, or personal or Confidential data exposed to someone outside. | Phished account, stolen unencrypted laptop, file shared with the wrong customer. | 2 hours, working or not |
| 3 Medium | Contained threat or policy breach with limited harm. | Malware blocked and cleaned, lost encrypted phone, mail sent to the wrong colleague. | Next working day |
| 4 Low | Suspicious activity with no harm found. | Reported phishing mail not opened, single failed sign-in burst. | Daily alert review |

When unsure, choose the higher level. The owner may change the level as facts emerge.

## 2. Roles and contacts

| Role | Who | How to reach |
| --- | --- | --- |
| Incident lead | Owner, {{dictionary.role.owner.name}} | {{dictionary.role.owner.phone}} · {{dictionary.role.owner.email}} |
| Deputy lead | {{dictionary.contact.deputy.name}} | {{dictionary.contact.deputy.phone}} |
| Record keeper | Control owner, {{dictionary.role.control-owner.name}} | {{dictionary.role.control-owner.email}} |
| IT support | {{dictionary.contact.it_provider}} | {{dictionary.contact.it_provider_phone}} |
| Legal counsel | {{dictionary.contact.legal_counsel}} | {{dictionary.contact.legal_counsel_phone}} |
| Cyber insurer | {{dictionary.contact.insurer}} | {{dictionary.contact.insurer_phone}} · policy {{dictionary.contact.insurer_policy}} |
| Domain registrar | {{dictionary.contact.domain_registrar}} | {{dictionary.contact.domain_registrar_phone}} |
| Security mailbox | All reports | {{generated.mailbox.security}} |

Call the insurer before engaging outside responders if the policy requires it.

## 3. Steps

**1. Report and record.** Whoever notices sends the facts to the security mailbox or calls the incident lead. The record keeper opens an incident record with the time the company became aware: this time starts any notification deadline (PRC-02).

**2. Assess.** The incident lead sets the severity, decides who is needed and whether PRC-02 applies. For level 1 or 2, open a call or chat outside any account that may be compromised.

**3. Contain.** Stop the harm from spreading. Typical actions:
- compromised account: block sign-in, revoke sessions, reset password and methods, remove new inbox rules, forwarding and app consents;
- device: isolate it in the device security service; wipe if lost or stolen;
- data exposed: remove the share or link, ask the recipient to delete and confirm;
- administrator access lost: use an emergency access account (PRC-07).
Automatic investigation and containment by the platform runs first where available; check its actions and complete them.

**4. Preserve evidence.** Export the relevant audit and sign-in records and alerts to the incident's folder before they age out. Do not wipe a device that may be needed as evidence without the lead's decision.

**5. Eradicate and recover.** Remove the cause, restore from version history or backup (PRC-03), check settings against the approved state, and return users to work.

**6. Notify.** Follow PRC-02 for regulators, customers and affected people. Only the incident lead approves outside communication.

**7. Close and learn.** Within ten working days of recovery for levels 1 and 2, hold a short review: timeline, cause, what worked, what to change. Record actions with owners and dates. The incident lead approves closure.

## 4. Yearly exercise

Once a year, and after a major change to the team or the tenant, the control owner runs a one-hour tabletop exercise with the incident lead and deputy, using a realistic scenario (for example a phished administrator, ransomware, or a supplier breach). Walk through the steps above, check the contact table is current, and record the date, participants, scenario, gaps found and actions as the output of the incident exercise control task.

## 5. Related documents

POL-13 Incident response, PRC-02 Regulatory incident notification, PRC-04 Business continuity plan, PRC-07 Break-glass test, PRC-08 Security alert review.
