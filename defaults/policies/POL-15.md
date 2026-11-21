---
id: POL-15
title: Risk assessment
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [GOV-02, GOV-03]
controls:
  soc2: [CC3.1, CC3.2, CC3.4, CC5.3]
  iso27001_2022: [A.5.7, A.5.8, A.5.36]
enforced_by: [F17, GOV-03]
records: ["ControlTasks:risk_assessment yearly", "Risks:register yearly and after major change"]
---

# Risk assessment policy

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved: {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

Security effort should go where it reduces the most harm. This policy requires {{dictionary.company.trading_name}} to identify its information risks, decide what to do about each, and keep that decision under review.

## 2. Rules

1. **When.** Risks are assessed every year, and after any major change: a new service or supplier holding company data, a new place of operation, a significant incident, a large change in staff, or a change to the tenant's plan or modules.
2. **How.** The assessment follows the method in PRC-05: list what matters, what could go wrong, how likely and how harmful it would be, and what is already in place.
3. **Treatment.** Each risk above the owner's tolerance is treated: reduced by a control, transferred (for example by insurance or contract), avoided by stopping the activity, or accepted by the owner with a reason.
4. **Register.** Every risk, its score, its treatment, its owner and the date it was last reviewed are kept in the Risks register.
5. **Accepted risks and exceptions.** A risk the owner accepts that departs from a requirement is also recorded as an exception with a reason, an approver and an end date (PRC-14).
6. **Report.** The result of each assessment is reported to the owner, who approves the treatment plan.

## 3. Roles

| Role | Responsibility |
| --- | --- |
| Owner | Sets the risk tolerance, approves treatments and accepts residual risk. |
| Control owner | Runs the assessment, keeps the register, tracks treatment actions. |
| Risk owners | Named people who carry out the treatment for their risks. |

## 4. Standards

{{generated.requirement_table}}
