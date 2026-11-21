---
id: PRC-04
title: Business continuity plan
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [BCK-04, IAM-07]
controls:
  soc2: [A1.2, CC6.1, CC9.1]
  iso27001_2022: [A.5.29, A.5.30, A.8.2]
enforced_by: [F17, manual]
records: ["ControlTasks:policy_review yearly", "ControlTasks:continuity_test yearly"]
---

# Business continuity plan

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved {{generated.date}} · Next review {{generated.next_review}}

Keep a sealed offline copy of this plan, together with the items in section 2, where the owner and the deputy can reach it without company systems.

## 1. Purpose and invocation

This plan sets out how {{dictionary.company.trading_name}} keeps working when it loses its Microsoft 365 tenant, its domain {{dictionary.company.primary_domain}}, or the founder's or owner's account. The owner, or the deputy ({{dictionary.contact.deputy.name}}) if the owner cannot act, invokes the plan and leads it as in PRC-01.

## 2. Kept offline

- This plan and the incident response plan (PRC-01).
- The two emergency access account credentials, sealed separately (PRC-07).
- The domain registrar and DNS account sign-in and recovery details: {{dictionary.contact.domain_registrar}}, {{dictionary.contact.dns_provider}}.
- The tenant's identifier and the subscription billing account details.
- Contact details of staff, key customers, suppliers, IT support, legal counsel and insurer.

The owner checks these items each year at the plan review and after any change.

## 3. Scenario A: loss of the tenant

*Examples: tenant taken over by an attacker, all administrator access lost, subscription suspended, major service outage.*

1. Try the emergency access accounts. If they work, follow PRC-01 to contain and recover.
2. If they do not, contact Microsoft support through the support routes for the subscription and the company's IT support, {{dictionary.contact.it_provider}}. Have the tenant identifier, billing details and proof of domain ownership ready.
3. Tell staff by phone or a pre-agreed channel outside the tenant. Do not use company mail.
4. Keep working from the independent backup ({{dictionary.backup.provider}}) and from local copies where available.
5. For a prolonged outage, the owner decides whether to set up temporary mail on the domain elsewhere by changing its mail records, and whether to tell customers.
6. After recovery, check administrator roles, access policies and settings against the approved state, and rotate secrets.

## 4. Scenario B: loss of the domain

*Examples: domain expired, registrar account taken over, DNS records changed by an attacker.*

1. Sign in to the registrar account from the offline details. If the domain has expired, renew it at once.
2. If the account is compromised, contact the registrar's support and follow its recovery process; involve legal counsel if ownership is disputed.
3. Restore the DNS records from the configuration document's approved state.
4. Tell customers and suppliers by phone that mail may have been lost or misdirected and not to act on payment changes until confirmed.
5. Prevent recurrence: auto-renewal on, registrar lock on, multi-factor sign-in on the registrar account, renewal date in the reminders.

## 5. Scenario C: loss of the founder's or owner's account or the person

*Examples: owner's account compromised or locked out; owner unavailable through illness or death.*

1. If the account is compromised, follow PRC-01 using an emergency access account.
2. If the person is unavailable, the deputy uses the offline details to act under the authority the company's governing documents give. Role holders are reassigned in the role groups so that approvals continue.
3. Registrar, billing and supplier accounts must never depend on one person's personal mail or phone. The yearly review confirms each has a second company contact.

## 6. Testing and review

The plan is reviewed every year with the policy set and after any change to domains, suppliers or key people. Once a year the owner and deputy walk through one scenario as a desk exercise, confirm the offline items are present and current, and record the result in the continuity test control task.
