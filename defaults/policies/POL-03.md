---
id: POL-03
title: Identity and access
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [IAM-01, IAM-02, IAM-03, IAM-04, IAM-05, IAM-06, IAM-07, IAM-08, IAM-09, IAM-10, IAM-11, IAM-12, IAM-13, IAM-14, IAM-15, IAM-16, IAM-17]
controls:
  soc2: [A1.2, CC6.1, CC6.2, CC6.3, CC6.7, CC7.2]
  iso27001_2022: [A.5.11, A.5.15, A.5.16, A.5.17, A.5.18, A.5.19, A.5.23, A.5.29, A.8.1, A.8.2, A.8.5, A.8.16, A.8.19]
enforced_by: [IAM-01, IAM-02, IAM-03, IAM-04, IAM-05, IAM-06, IAM-08, IAM-09, IAM-10, IAM-11, IAM-14, F17]
records: ["ControlTasks:access_review quarterly", "ControlTasks:break_glass_test every six months", "JoinerChecklists:joiner and leaver on event"]
---

# Identity and access policy

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

This policy makes sure that only the right people reach company systems and data, with only the access they need, for only as long as they need it.

## 2. Principles

1. **One person, one account.** Every account belongs to a named person. Shared accounts are not allowed, apart from the two emergency access accounts and shared mailboxes that nobody signs in to.
2. **Strong sign-in.** Everyone signs in with multi-factor authentication. Administrators use phishing-resistant methods. Text message, voice call and email codes are not accepted. Old sign-in protocols that cannot use multi-factor authentication are blocked.
3. **Trusted devices.** Company data is reachable only from a compliant company device or a protected app.
4. **Least privilege.** Access is granted through groups linked to a role, never directly to a person, and only what the role needs.
5. **No standing administrator rights.** Day-to-day accounts hold no administrator role. Standing administrator rights are held only by the emergency access accounts and the pipeline identity. Administrator accounts have no mailbox.
6. **Timely removal.** Access changes on the day a person changes role and is removed on the day they leave (PRC-12).
7. **Review.** Users, administrator roles, guests, site permissions and application identities are reviewed every quarter (PRC-06).

## 3. Accounts

| Kind | Rule |
| --- | --- |
| Staff | Created from the People list; plan and groups follow the person's role. |
| Administrator | Separate from the staff account, no mailbox, phishing-resistant sign-in, re-authenticated every eight hours. |
| Emergency access | Two accounts, excluded from access policies, credentials sealed offline, an alert on every use, tested twice a year (PRC-07). |
| Guest | Invited only by an administrator, named, with an end date; sees only what is shared with them (POL-11). |
| Application identity | Least privilege, no stored secrets where federation is possible, reviewed every quarter (PRC-13). |

## 4. Passwords

Passwords are checked against a list of banned and common passwords and are protected by lockout. They are not forced to expire; they are changed whenever there is a sign that someone else knows them. Self-service reset needs two methods.

## 5. Applications

Users cannot register applications or grant outside applications access to company data. Requests go to an administrator, who decides against the allowed-apps list (PRC-13, POL-21).

## 6. Devices joining the directory

Joining a device to the company directory needs multi-factor authentication and is limited to named users.

## 7. Risk-based and time-limited access

Where the subscription includes them, risky sign-ins are challenged or blocked automatically and administrator roles are activated on request for a limited time. Where it does not, the quarterly access review and the daily alert review (PRC-08) cover these risks.

## 8. Roles

The owner, {{dictionary.role.owner.name}}, approves role definitions, administrator assignments and exceptions. The control owner runs the access review and keeps its record.

## 9. Standards

{{generated.requirement_table}}

## 10. Current settings

{{generated.settings_summary}}
