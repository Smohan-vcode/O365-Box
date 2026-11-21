---
id: PRC-05
title: Risk assessment method and register
version: "1.0"
owner_role: control-owner
approver_role: owner
review_months: 12
requirements: [GOV-02]
controls:
  soc2: [CC3.1, CC3.2]
  iso27001_2022: [A.5.7, A.5.8]
enforced_by: [F17]
records: ["ControlTasks:risk_assessment yearly", "Risks:register yearly and after major change"]
---

# Risk assessment method and register

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved {{generated.date}} · Next review {{generated.next_review}}

## 1. When

Once a year when the risk assessment control task is raised, and after any major change listed in POL-15. Allow two to three hours.

## 2. Steps

1. **Prepare.** Open the Risks register, the Assets and Vendors registers, the last coverage report, the exceptions list and the incident records since the last assessment.
2. **What matters.** List the information and services the company depends on: customer data, contracts, finance records, people records, mail, files, the domain, key suppliers.
3. **What could go wrong.** For each, ask what could harm its confidentiality, integrity or availability. Use the prompts in section 5 so nothing obvious is missed.
4. **Score.** For each risk, with current controls in place, score likelihood and impact from 1 to 5 using section 3. Risk score = likelihood × impact.
5. **Decide.** Compare the score with the tolerance in section 4 and choose a treatment: reduce, transfer, avoid or accept.
6. **Record.** Update the register for every risk (section 6). Close risks that no longer apply, with a note.
7. **Approve.** The owner reviews the register, approves the treatment plan and signs off accepted risks.
8. **Close the task.** Attach the exported register and a one-paragraph summary of what changed.

## 3. Scales

| Score | Likelihood (in the next year) | Impact |
| --- | --- | --- |
| 1 | Rare: not expected | Negligible: no customer impact, fixed in hours |
| 2 | Unlikely: could happen | Minor: brief disruption, little cost |
| 3 | Possible: has happened to similar firms | Moderate: days of disruption, a customer affected, notifiable to a few people |
| 4 | Likely: expected at some point | Major: weeks of disruption, regulator notified, significant cost |
| 5 | Almost certain: has happened here | Severe: threatens the business |

## 4. Tolerance

| Score | Rating | Action |
| --- | --- | --- |
| 15 to 25 | High | Treat now; the owner tracks monthly. |
| 8 to 14 | Medium | Treat within the year, or the owner accepts with a reason. |
| 1 to 7 | Low | Accept; review next year. |

## 5. Prompts

Phishing and account takeover; ransomware; lost or stolen device; data sent to the wrong person; a malicious or careless insider; a supplier breach or failure; loss of the tenant, domain or owner's account; mistakes in settings; missed legal deadlines; fraud by payment change; outage of a key service; AI tools leaking information.

## 6. Register columns

| Column | Content |
| --- | --- |
| Risk id | Given by the register |
| Description | "Because of [cause], [event] may happen, leading to [consequence]." |
| Likelihood, impact | 1 to 5 each, with current controls |
| Treatment | Reduce, transfer, avoid or accept, and the action or control (cite the requirement id) |
| Owner | The person who carries out the treatment |
| Reviewed on | Date of this assessment |

A risk the owner accepts that departs from a requirement is also recorded as an exception (PRC-14).
