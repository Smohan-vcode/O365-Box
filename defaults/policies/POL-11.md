---
id: POL-11
title: Vendor accounts and third-party risk
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [SUP-01, SUP-02, IAM-11, COL-04, EML-07, APP-02]
controls:
  soc2: [CC6.2, CC6.8, CC9.2]
  iso27001_2022: [A.5.15, A.5.16, A.5.19, A.5.20, A.5.22, A.5.23, A.8.19]
enforced_by: [SUP-02, IAM-11, COL-04, EML-07, APP-02, F17]
records: ["ControlTasks:vendor_review yearly", "Vendors:register on event"]
---

# Vendor accounts and third-party risk policy

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved: {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

{{dictionary.company.trading_name}} relies on suppliers for software, services and advice. This policy makes sure suppliers that hold company data are chosen with care, are under agreement, and reach company systems only in a controlled way.

## 2. Rules

1. **Register.** Every supplier that holds or can reach company data is listed in the Vendors register with the service, the data shared, its criticality and the agreement.
2. **Assess before use.** A new supplier is assessed before any company data is shared (PRC-11). The depth of the assessment follows the data and how critical the service is.
3. **Agreement.** Suppliers that process personal or Confidential data sign an agreement that covers confidentiality, security, breach notification, sub-processors, return or deletion of data and, where personal data is processed, the terms the privacy law requires.
4. **Review.** Suppliers are reviewed every year and when their service or the data they hold changes.
5. **Supplier accounts.** Supplier sign-ups and invoices use the company's accounts address, never a personal address, so that the company keeps control when people leave.
6. **Supplier people.** Suppliers' people reach company systems as named guests with an end date. Shared accounts are not allowed. Guests are invited only by administrators and see only what they are given.
7. **Apps and integrations.** Outside apps, add-ins and storage providers are used only when on the allowed-apps list. Users cannot buy or trial services against the company's tenant themselves.
8. **Leaving a supplier.** When a supplier relationship ends, guest access and app access are removed and the data is returned or deleted as the agreement says.

## 3. Roles

The owner approves new suppliers that hold Confidential or Restricted data and the allowed-apps list. The control owner runs the yearly review.

## 4. Standards

{{generated.requirement_table}}
