---
id: PRC-14
title: Exception handling
version: "1.0"
owner_role: control-owner
approver_role: owner
review_months: 12
requirements: [GOV-03]
controls:
  soc2: [CC3.4, CC5.3]
  iso27001_2022: [A.5.36]
enforced_by: [GOV-03]
records: ["Exceptions:each exception on event", "ControlTasks:exception_review monthly"]
---

# Exception handling procedure

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

Sometimes a requirement cannot be met: a supplier's system does not support a setting, a person needs temporary access, a fix must wait. This procedure makes sure every departure is visible, justified, approved and time-limited.

## 2. What needs an exception

- Leaving out or relaxing a security setting, for a user, group, device or the whole tenant.
- A proposal the owner rejects; it is recorded as an exception with the owner's reason.
- A vulnerability, review finding or audit gap not fixed within its target time.
- An accepted risk that departs from a requirement (PRC-05).

## 3. Steps

1. **Request.** Create an item in the Exceptions register with:
   - the requirement or setting identifier;
   - the scope (user, group, device or whole tenant);
   - the reason, in plain words;
   - the compensating control, if any (what reduces the risk meanwhile);
   - the proposed end date, no more than twelve months ahead.
2. **Assess.** The control owner checks the request and the risk it creates, and suggests a compensating control where one is practical.
3. **Approve.** The owner approves or refuses. Nobody approves an exception that benefits only themselves when another approver exists.
4. **Apply.** An approved exception is copied by the box into the configuration as a new version, with a record of who approved it. The next plan leaves out the affected setting for the stated scope.
5. **Expire.** On the end date the exception lapses and the requirement applies again. The box refuses to validate a configuration with an expired exception, so an exception that is still needed must be renewed, with a fresh reason, before it ends.

## 4. Monthly check

Each month the control owner reviews the Exceptions register: exceptions ending in the next 30 days (renew or prepare to close), exceptions whose reason no longer holds (close early), and the total, which is reported to the owner with the monthly coverage report (PRC-15).

## 5. Record

The Exceptions register is the record. Each item keeps its approver, dates and state; changes are logged.
