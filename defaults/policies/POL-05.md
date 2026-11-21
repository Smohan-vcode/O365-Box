---
id: POL-05
title: Data classification and handling
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [DAT-01, DAT-02, DAT-03, DAT-04, DAT-05, DAT-06, DAT-07, DAT-08, DAT-09, DAT-10, DAT-12]
controls:
  soc2: [C1.1, CC6.1, CC6.7, P6.1]
  iso27001_2022: [A.5.12, A.5.13, A.5.14, A.5.17, A.5.34, A.8.3, A.8.12, A.8.21, A.8.24]
enforced_by: [DAT-01, DAT-02, DAT-03, DAT-04, DAT-05, DAT-06, DAT-07, DAT-08, DAT-09, DAT-10]
records: ["Evidence:labels monthly", "Evidence:dlp monthly"]
---

# Data classification and handling policy

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved: {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

Not all information needs the same protection. This policy sets four classification levels and what each allows, so that everyone handles information the same way.

## 2. Levels

| Label | Meaning | Examples | Sharing outside |
| --- | --- | --- | --- |
| Public | Approved for anyone. | Published website text, brochures. | Allowed. |
| General | Ordinary internal business information. This is the default. | Internal notes, plans, routine mail. | With named people who need it. |
| Confidential | Harm if disclosed. | Contracts, prices, customer lists, personal data. | With named people under agreement; detected and reported. |
| Restricted | Serious harm if disclosed. | Credentials, bank details, special category personal data, legal advice. | Not allowed. Readable only by people in the company. |

## 3. Rules

1. Every document, email and site carries a label. A default applies; raise it when the content is more sensitive.
2. Labels on sites and teams set their privacy and sharing limits. Information placed there inherits them.
3. Restricted information is encrypted and readable only by people in the company. It cannot leave the company by mail or sharing.
4. Confidential information leaving the company is detected and the sender is reminded of this policy.
5. Personal data leaving the company is detected; bulk transfers are blocked unless the sender gives a reason.
6. Payment card and bank account numbers are blocked from leaving unless justified. Passwords, keys and other credentials are never sent in mail or files.
7. Legal, finance, people, corporate and sales records cannot be shared outside from their sites. Send a copy through the approved process instead (POL-08).
8. Company files sent to personal mail accounts are detected, the sender gives a reason and the transfer is reported.
9. Data in transit is encrypted with current protocols.

## 4. Handling

- Print Confidential or Restricted information only when needed, collect it at once and shred it after use.
- Do not discuss Confidential or Restricted information where you can be overheard.
- When you are unsure of the level, choose the higher one and ask the owner.

## 5. Personal data

Personal data is at least Confidential. It is collected only for a stated purpose, kept only as long as needed (POL-07) and handled as POL-12 describes.

## 6. Standards

{{generated.requirement_table}}

## 7. Current settings

{{generated.settings_summary}}
