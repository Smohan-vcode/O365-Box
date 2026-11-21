---
id: POL-09
title: Quotation and pricing
version: "1.0"
owner_role: finance-approver
approver_role: owner
review_months: 12
requirements: [DAT-08]
controls:
  soc2: [C1.1, CC6.7]
  iso27001_2022: [A.8.3, A.8.12]
enforced_by: [DAT-08, manual]
records: ["Documents:quotation register on event"]
---

# Quotation and pricing policy

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

Quotations commit {{dictionary.company.trading_name}} to a price. This policy makes sure prices are consistent, approved where needed and recorded.

## 2. Rules

1. **Price list.** Quotations use the current approved price list. The finance approver keeps it and the owner approves changes.
2. **Discounts.** Discounts beyond the limit the owner sets need the finance approver's approval before the quotation is sent.
3. **Register.** Every quotation has an item in the register with its reference, customer, amount, validity date and status.
4. **Copy kept.** Quotations are sent from the company's mail system. A copy of each outgoing quotation is kept automatically so that the register and the mail agree.
5. **Validity.** Each quotation states how long it is valid and the terms that apply. Expired quotations are not honoured without a new approval.
6. **Confidentiality.** Prices, margins and customer-specific terms are Confidential (POL-05). Sales records cannot be shared outside directly from their site.

## 3. Roles

| Role | Responsibility |
| --- | --- |
| Sender | Prepares quotations from the price list and records them. |
| Finance approver | Keeps the price list and approves discounts above the limit. |
| Owner | Approves the price list and the discount limit. |

## 4. Standards

{{generated.requirement_table}}
