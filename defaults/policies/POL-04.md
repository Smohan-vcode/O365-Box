---
id: POL-04
title: Email and external communication
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [EML-01, EML-02, EML-03, EML-04, EML-05, EML-06, EML-07, EML-08]
controls:
  soc2: [CC2.2, CC6.1, CC6.6, CC6.7, CC6.8, CC7.2, CC7.3]
  iso27001_2022: [A.5.14, A.5.15, A.5.16, A.5.23, A.6.8, A.8.5, A.8.7, A.8.12, A.8.19, A.8.21, A.8.23]
enforced_by: [EML-01, EML-02, EML-03, EML-04, EML-05, EML-06, EML-07, EML-08]
records: ["Evidence:mail_auth weekly", "Evidence:forwarding monthly"]
---

# Email and external communication policy

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

Email is the main way {{dictionary.company.trading_name}} talks to customers and suppliers, and the main way attackers try to get in. This policy sets out how company mail is sent, received and protected.

## 2. Addresses

- Company mail is sent from addresses at {{dictionary.company.primary_domain}} or the company's other registered domains.
- Functions such as sales, support, billing, privacy and security have shared mailboxes. Nobody signs in to a shared mailbox; access is given through a group, and people send as the mailbox only when their role allows.
- The company's domains publish sender authentication records (SPF, DKIM, DMARC at reject, MTA-STS) so that others can reject mail that pretends to come from the company.

## 3. Inbound mail

- All inbound mail is checked for spam, malware, phishing, impersonation and spoofing. Attachments and links are scanned before delivery and again when clicked.
- Mail from outside the company is marked as external.
- Use the Report button on any suspicious message. Reports go to the security mailbox and are reviewed every working day (PRC-08).

## 4. Outbound mail

- Automatic forwarding to outside addresses is blocked. An exception needs the owner's approval and is recorded (PRC-14).
- Do not send Confidential or Restricted information outside the company unless POL-05 allows it.
- External mail carries the company's disclaimer and the correspondence details the law of the place of registration requires.
- Old mail protocols and unauthenticated sending are off. Systems that send mail on the company's behalf use an approved, authenticated route.

## 5. Payment and change requests

Never act on a request by email alone to change bank details, make an urgent payment, or send credentials or personal data. Confirm it by calling a number you already hold, not one given in the message.

## 6. Add-ins and outside storage

Mail add-ins and outside storage providers in mail are restricted to those the company has approved.

## 7. Public statements

Only people the owner authorises speak for {{dictionary.company.trading_name}} in public or to the press. Do not disclose confidential information on social media.

## 8. Standards

{{generated.requirement_table}}
