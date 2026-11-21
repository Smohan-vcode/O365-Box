---
id: POL-13
title: Incident response
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [INC-01, INC-02, INC-03]
controls:
  soc2: [CC2.3, CC7.3, CC7.4]
  iso27001_2022: [A.5.5, A.5.24, A.5.26]
enforced_by: [INC-03, LOG-02, LOG-04, F17]
records: ["ControlTasks:incident_exercise yearly", "ControlTasks:incident_register on event"]
---

# Incident response policy

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

A security incident is any event that harms, or could harm, the confidentiality, integrity or availability of company information: a compromised account, malware, a lost device, data sent to the wrong person, a supplier breach, an outage. This policy makes sure incidents are reported, handled and learned from, and that the company meets any duty to notify.

## 2. Rules

1. **Report at once.** Everyone reports a suspected incident immediately to {{generated.mailbox.security}} or by phone to the owner, {{dictionary.role.owner.name}}. Do not investigate on your own, and do not delete evidence.
2. **Plan.** The company keeps an incident response plan (PRC-01) that sets severity levels, roles, contacts and steps.
3. **Automatic containment.** Detected threats are investigated and contained automatically where the platform supports it; people confirm and complete the response.
4. **Register.** Every incident is recorded, from first report to closure, with its severity, timeline, decisions and the evidence kept.
5. **Notification.** Where an incident may be reportable to a regulator, a customer or the people affected, the deadlines are tracked from the moment the company becomes aware (PRC-02).
6. **Exercise.** The plan is exercised every year, and after a major change to the team or the tenant. The exercise and what it taught are recorded.
7. **Learn.** After each significant incident a short review records the cause, what worked, what did not, and the actions agreed. Actions are tracked to completion.

## 3. Roles

| Role | Responsibility |
| --- | --- |
| Owner | Leads the response, decides on notification and outside help, approves closure. |
| Control owner | Keeps the register, runs the yearly exercise, tracks actions. |
| Everyone | Reports at once and follows instructions during an incident. |

## 4. Confidentiality

Incident details are Restricted until the owner decides otherwise. Talk to outsiders, including the press and customers, only as the owner directs.

## 5. Standards

{{generated.requirement_table}}
