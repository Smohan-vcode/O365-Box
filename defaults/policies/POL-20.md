---
id: POL-20
title: Personnel security
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [AWR-01, AWR-02, IAM-12, DEV-08]
controls:
  soc2: [CC1.4, CC2.2, CC6.2, CC6.3, CC6.5]
  iso27001_2022: [A.5.9, A.5.10, A.5.11, A.5.16, A.5.18, A.6.2, A.6.3, A.7.14]
enforced_by: [AWR-02, IAM-12, DEV-08, F07, F18]
records: ["JoinerChecklists:joiner and leaver on event", "Acknowledgements:on joining and each version", "ControlTasks:awareness yearly"]
---

# Personnel security policy

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved: {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

People are the company's strongest defence and its most common point of failure. This policy covers security before, during and after someone works for {{dictionary.company.trading_name}}.

## 2. Before joining

- Checks on identity and the right to work are made as the law of the place of work allows. Further checks (references, qualifications) follow the role's access to Confidential or Restricted information.
- Employment and contractor agreements include confidentiality terms and a duty to follow the company's policies.

## 3. Joining

The joiner checklist (PRC-12) is completed before or on the first day:

- account created and plan assigned from the person's role;
- role and access groups assigned;
- multi-factor method registered;
- device enrolled and compliant;
- policies acknowledged, including acceptable use (POL-02);
- agreements executed;
- security awareness training completed (PRC-10).

## 4. During employment

- Everyone completes security awareness training every year and acknowledges each new version of the policies.
- A change of role is a "mover": access is changed on the day of the change, removing what the new role does not need.
- Breaches of policy are handled under the terms of employment or contract.

## 5. Leaving

The leaver checklist (PRC-12) is completed on the last day:

- sign-in blocked and groups removed;
- open approvals reassigned;
- mailbox converted or forwarded as decided;
- devices retired and company data wiped;
- licence released.

The confidentiality duty continues after the person leaves.

## 6. Standards

{{generated.requirement_table}}
