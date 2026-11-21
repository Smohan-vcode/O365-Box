---
id: PRC-07
title: Break-glass test
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [IAM-07]
controls:
  soc2: [A1.2, CC6.1]
  iso27001_2022: [A.5.29, A.8.2]
enforced_by: [IAM-07, LOG-04, F17]
records: ["ControlTasks:break_glass_test every six months"]
---

# Break-glass test procedure

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

The company keeps two emergency access ("break-glass") accounts so that it can regain control of its tenant if normal administrator sign-in fails, for example during an outage of the sign-in method or after an attack. This procedure keeps them working and watched.

## 2. Rules for the accounts

- Two accounts, cloud-only, on the tenant's own initial domain, holding the Global Administrator role permanently.
- Excluded from every Conditional Access and other access policy, so that a faulty policy cannot lock them out. They are never made subject to Conditional Access.
- Long random passwords, plus a phishing-resistant method where the company uses one. Each credential is split or sealed and stored offline in two separate places known to the owner and the deputy.
- Not used for day-to-day work. Any sign-in raises an alert to {{generated.mailbox.security}} and the owner.
- Credentials are never written in mail, chat, documents, tickets or logs, and never stored in the tenant.

## 3. Test (every six months)

The break-glass test control task is raised twice a year. The owner, or the deputy with a second person present, carries it out.

1. Tell the security mailbox reviewer that a test is about to happen.
2. Retrieve the first account's sealed credential.
3. From a clean, trusted computer, sign in to the admin centre with the account. Confirm the sign-in succeeds and the account still holds its role.
4. Confirm the alert for the sign-in arrived at the security mailbox within the expected time.
5. Sign out. If the password was revealed during the test, set a new long random password and reseal it. Do not record the password anywhere but the sealed store.
6. Repeat steps 2 to 5 for the second account.
7. Check that both accounts are still excluded from every access policy, that no mailbox or licence has been added, and that no other account has been added to the emergency access group.

## 4. Record

Close the control task with: the date, who took part, for each account whether sign-in succeeded and whether the alert arrived (with its time), whether credentials were resealed, and any problem found. Do not attach screenshots that show a credential.

## 5. Failure

If an account cannot sign in or no alert arrives, treat it as a level 2 incident (PRC-01): fix it the same day and repeat the test.

## 6. Real use

Any real use of an account is an incident. Afterwards, change its credential, reseal it, and review the audit log for everything done with it.
