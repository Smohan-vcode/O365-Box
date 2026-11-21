---
id: PRC-06
title: Access review
version: "1.0"
owner_role: control-owner
approver_role: owner
review_months: 12
requirements: [IAM-13, IAM-06, IAM-11, IAM-17, SUP-02]
controls:
  soc2: [CC6.1, CC6.2, CC6.3, CC9.2]
  iso27001_2022: [A.5.15, A.5.16, A.5.17, A.5.18, A.5.19, A.8.2]
enforced_by: [IAM-06, IAM-11, SUP-02, F17]
records: ["ControlTasks:access_review quarterly"]
---

# Access review procedure

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved: {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose and frequency

Access builds up over time. Every quarter the control owner checks that each person, administrator, guest and application holds only the access they need. The access review control task is raised on the first working day of each quarter and is due within 15 working days.

## 2. Inputs

The box collects these lists for the review and attaches them to the task:

- users with their status, plan, groups, last sign-in and multi-factor methods;
- administrator role assignments;
- guests with their sponsor, invitation date, end date and last sign-in;
- site and library permissions, including any direct (per-person) permissions and sharing links;
- application identities and their permissions (PRC-13 covers user-consented apps monthly).

## 3. Steps

**1. Users.** For each enabled account confirm, against the People list, that the person still works for the company and that their groups match their current role. Flag:
- accounts of people who have left or not signed in for 90 days;
- groups that do not match the role;
- accounts without a registered multi-factor method.

**2. Administrator roles.** Confirm that standing administrator roles are held only by the two emergency access accounts and the pipeline identity, and that any other assignment is time-limited and recorded. Confirm administrator accounts have no mailbox.

**3. Guests.** For each guest confirm with the sponsor that access is still needed and that the end date is right. Remove guests whose end date has passed, whose sponsor has left, or who have not signed in for 90 days. Confirm there are no shared supplier accounts.

**4. Site permissions.** For each site and library confirm access is granted to groups, not individuals, except where the system sets per-item permissions for a stated reason. Remove unexpected direct permissions and expired or unneeded sharing links.

**5. Application identities.** Confirm each has an owner, still serves its purpose, holds the least permission it needs and uses certificates or federation rather than secrets where possible. Confirm the pipeline identity holds only the permissions the configuration approves.

**6. Act.** Make removals and corrections through the normal process (group membership, People list, proposal) on the day of the review. Anything that cannot be fixed at once is recorded with an owner and date, or as an exception (PRC-14).

## 4. Record

Close the control task with: the date, the reviewer, the lists reviewed (attached), the number of items checked in each area, each change made, and any item left open with its reason. The owner reviews and approves the record. A reviewer does not approve changes to their own access.
