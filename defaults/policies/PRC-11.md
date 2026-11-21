---
id: PRC-11
title: Supplier assessment and review
version: "1.0"
owner_role: control-owner
approver_role: owner
review_months: 12
requirements: [SUP-01, SUP-02]
controls:
  soc2: [CC6.2, CC9.2]
  iso27001_2022: [A.5.16, A.5.19, A.5.20, A.5.22]
enforced_by: [SUP-02, F17]
records: ["ControlTasks:vendor_review yearly", "Vendors:register on event"]
---

# Supplier assessment and review procedure

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved {{generated.date}} · Next review {{generated.next_review}}

## 1. Scope

Every supplier that holds, processes or can reach company data, or whose failure would stop the company working: software and cloud services, IT support, accountants, payroll, legal and other advisers.

## 2. Before use

1. **Record.** Add the supplier to the Vendors register: name, service, data shared, the account address used to sign up (the company's accounts address, never a personal one) and the person responsible.
2. **Rate criticality.**

   | Criticality | When |
   | --- | --- |
   | High | Holds Restricted data or large amounts of personal data, or the company cannot work without it for a day. |
   | Medium | Holds Confidential or some personal data, or its loss would disrupt work. |
   | Low | Holds no company data beyond contact details and is easily replaced. |

3. **Assess.** Answer these questions from the supplier's published security information, certifications (such as ISO/IEC 27001 or a SOC 2 report) or a short questionnaire. Depth follows criticality: all questions for high, questions 1 to 5 for medium, 1 to 3 for low.
   1. What company data will it hold, and where (which countries)?
   2. Does it support multi-factor sign-in and company-controlled accounts?
   3. Can the company export and delete its data when it leaves?
   4. Does it encrypt data in transit and at rest?
   5. Will it notify the company of a breach, and how fast?
   6. Does it hold an independent security certification or report, and is it current?
   7. Which sub-processors does it use?
   8. What are its availability commitments and backup arrangements?
4. **Agreement.** Make sure an agreement is in place that covers confidentiality, security, breach notification, sub-processors, data return and deletion, and, for personal data, the terms the applicable privacy law requires. Link it in the register.
5. **Approve.** The owner approves high and medium suppliers. Record the date.
6. **Access.** If the supplier's people need access to company systems, invite each as a named guest with an end date, sponsored by a member of staff. Shared supplier accounts are not allowed. Apps are added only through the allowed-apps list (PRC-13).

## 3. Yearly review

The vendor review control task is raised once a year. For each supplier in the register:

1. Confirm it is still used and the data shared is still accurate; remove suppliers no longer used and their access.
2. For high and medium suppliers, check the certification or report is current and note any breach or major change in the year.
3. Confirm guests from the supplier still need access and have an end date.
4. Update criticality and the review date in the register.

## 4. Record

Close the control task with the exported Vendors register, the suppliers added, changed or removed, and any concern with the action agreed. The owner approves the record.

## 5. Ending a supplier

Remove guest and app access, retrieve or confirm deletion of company data as the agreement says, change any shared credentials, and update the register.
