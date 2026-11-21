---
id: PRC-13
title: Application consent review
version: "1.0"
owner_role: control-owner
approver_role: owner
review_months: 12
requirements: [APP-01, IAM-10, IAM-17]
controls:
  soc2: [CC6.1, CC6.3, CC9.2]
  iso27001_2022: [A.5.9, A.5.17, A.5.23, A.8.2, A.8.19]
enforced_by: [APP-01, IAM-10, F17]
records: ["ControlTasks:app_consent_review monthly"]
---

# Application consent review procedure

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved: {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

Applications granted access to company data can read mail, files and directory information without anyone signing in. This procedure controls how access is granted and reviews it every month.

## 2. Requesting an application

1. Users cannot grant applications access to company data themselves. When an application asks for consent, the user submits the request shown on screen; it goes to an administrator.
2. The administrator checks: who publishes the application; what it does; the permissions it asks for and whether they are more than it needs; whether it is a supplier that needs assessing (PRC-11); and whether it is on the allowed-apps list or the approved AI tools list (POL-21).
3. The owner approves applications that ask for access to all users' data, or for write access to mail, files or the directory.
4. Approved applications are added to the allowed-apps list. Refused requests are answered with the reason.

## 3. Monthly review

The application consent review control task is raised each month. The box attaches the current list of applications with consents, their permissions, who granted them, when, and when each was last used.

1. Compare the list with the allowed-apps list. Any application not on it is investigated: remove its consent unless the owner approves it the same day.
2. For each application, confirm it is still used and its permissions are still needed. Remove consents for applications unused for 90 days.
3. Look for permissions that allow reading all mail or files, sending mail as users, or changing the directory. Confirm each is approved and still needed.
4. Check application identities the company owns: each has an owner, least privilege, and no secret close to expiry; federation or certificates are used where possible.
5. If any application looks malicious or was granted without approval, treat it as an incident (PRC-01).

## 4. Record

Close the control task with: the number of applications reviewed, the consents removed, the applications approved, and anything left open with its reason. The owner reviews the record.
