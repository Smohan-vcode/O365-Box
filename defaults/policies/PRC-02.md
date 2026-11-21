---
id: PRC-02
title: Regulatory incident notification
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [INC-02]
controls:
  soc2: [CC2.3, CC7.4]
  iso27001_2022: [A.5.5, A.5.24]
enforced_by: [F17, manual]
records: ["ControlTasks:incident_register on event"]
---

# Regulatory incident notification procedure

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved: {{generated.date}} · Next review {{generated.next_review}}

This procedure helps the company meet deadlines to notify regulators, customers and affected people after an incident. It is not legal advice. The deadlines below are rendered from the jurisdiction rules the company has selected; rules marked as not yet reviewed by counsel must be confirmed with counsel before they are relied on.

## 1. Deadlines

The company is registered in {{jurisdictions.registered}} and operates in {{jurisdictions.operating}}. The following deadlines apply:

{{jurisdictions.notification_deadlines}}

Contracts with customers may set shorter deadlines than the law. The Vendors register and customer agreements record them; check them in step 3.

## 2. When the clock starts

Most deadlines run from when the company becomes **aware** that a reportable incident has happened, not from when the investigation ends. The incident record (PRC-01, step 1) holds that time. If in doubt, treat the earliest reasonable time as the start.

## 3. Steps

1. **Within the first hours.** The incident lead asks: does the incident involve personal data, a service the company provides to customers, or a sector under a security law? If yes to any, this procedure applies.
2. **Fill the deadline table.** The record keeper adds to the incident record one row per possible duty:

   | Duty | Jurisdiction or contract | Recipient | Deadline (date and time) | Status |
   | --- | --- | --- | --- | --- |
   | e.g. notify the privacy regulator | | | | open, sent, not required |

   Each row is tracked until it is sent or a decision not to notify is recorded with the reason.
3. **Check contracts.** Check customer and supplier agreements for notification duties and contacts.
4. **Decide.** The incident lead decides, with legal counsel ({{dictionary.contact.legal_counsel}}) where needed, whether each duty applies. Record the decision, the reasons and who took part. A decision not to notify is recorded as carefully as a decision to notify.
5. **Notify in stages if needed.** If the facts are not all known before a deadline, send an initial notice with what is known and follow up. Do not miss a deadline waiting for certainty.
6. **Content.** A notice normally covers: what happened and when; the kinds and approximate number of people and records affected; the likely consequences; what the company has done and will do; a contact point ({{generated.mailbox.privacy}} or {{generated.mailbox.security}}). Use the regulator's own form where one exists.
7. **People affected.** Where the law requires, or the risk to them is high, tell affected people in plain language what happened and what they can do to protect themselves.
8. **Keep the record.** Save copies of every notice, the time sent and any reply in the incident's folder. Keep a record of incidents that were assessed and found not reportable as well.

## 4. Registered details for notices

Notices are sent in the name of {{dictionary.company.legal_name}}, {{dictionary.company.registered_address}}, by the incident lead or a person the owner names.

## 5. Review

The control owner checks this procedure and the jurisdiction list every year, when the company starts operating in a new place, and after any notified incident.
