---
id: POL-19
title: Encryption and key management
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [DAT-02, DAT-09, DEV-02, EML-03, IAM-17]
controls:
  soc2: [C1.1, CC6.1, CC6.6, CC6.7]
  iso27001_2022: [A.5.13, A.5.14, A.5.17, A.8.1, A.8.2, A.8.21, A.8.24]
enforced_by: [DAT-02, DAT-09, DEV-02, EML-03, manual]
records: ["Evidence:device_compliance monthly", "ControlTasks:app_consent_review monthly"]
---

# Encryption and key management policy

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved: {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

Encryption keeps information unreadable to anyone without the key. This policy sets where encryption is used and how keys and other secrets are handled.

## 2. Where encryption is used

| Where | Rule |
| --- | --- |
| Devices | Storage on laptops, desktops and phones is encrypted. Recovery keys are held by the company, not only by the user. |
| In transit | Mail, file access and web services use current transport encryption. The company's domains publish MTA-STS so that mail to them is encrypted. |
| At rest in the cloud | Company data in Microsoft 365 is encrypted at rest by the service. |
| Restricted information | Encrypted by its label and readable only by people in the company (POL-05). |
| Removable storage | Allowed only when encrypted. |

## 3. Keys, certificates and secrets

1. Keys, passwords, certificates and tokens are secrets. They are never sent in mail or chat, written in documents or code, or logged.
2. Application identities use federation or certificates rather than stored secrets where the platform allows. Where a secret is unavoidable it has an owner, a short lifetime and a place in the company's approved secret store.
3. Device recovery keys are stored in the device management service and released only to the device's user after identity checks, or to an administrator.
4. Emergency access credentials are sealed offline (PRC-07).
5. A secret that may have been exposed is replaced at once and the event is reported as an incident.
6. Certificates for the company's domains are tracked so that none expires unnoticed.

## 4. Roles

The owner approves the secret store and any exception. Administrators rotate secrets and certificates and record each rotation.

## 5. Standards

{{generated.requirement_table}}
