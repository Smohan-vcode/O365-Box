---
id: POL-17
title: Secure development and vulnerability management
version: "1.0"
owner_role: owner
approver_role: owner
review_months: 12
requirements: [CFG-01, CFG-02, CFG-03, CFG-04, CFG-05, CFG-06, CFG-07, CFG-08, MAL-01, MAL-02, MAL-03, MAL-04, MAL-05]
controls:
  soc2: [CC6.1, CC6.6, CC6.8, CC7.1, CC7.2, CC7.4, CC8.1]
  iso27001_2022: [A.8.5, A.8.7, A.8.8, A.8.9, A.8.16, A.8.19, A.8.20, A.8.23]
enforced_by: [CFG-01, CFG-02, CFG-03, CFG-05, CFG-06, CFG-07, MAL-01, MAL-02, MAL-03, MAL-04, MAL-05, manual]
records: ["ControlTasks:vulnerability_review weekly", "ControlTasks:pen_test yearly"]
---

# Secure development and vulnerability management policy

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved: {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

Attackers exploit known weaknesses in software and settings. This policy makes sure {{dictionary.company.trading_name}}'s devices and services are hardened, patched and protected against malware, and that any software the company builds is built with care.

## 2. Hardening and protection

1. A hardened operating system baseline is applied to company computers and deviation from it is detected.
2. Anti-malware runs in real time with cloud protection, updates itself and cannot be turned off by users. Endpoint detection and response covers every computer and remediates threats automatically.
3. Attack surface reduction rules block common malware delivery techniques. Office macros from the internet are blocked.
4. Malicious and unsuitable websites are blocked on managed devices. The managed browser is hardened and sign-in is limited to work accounts.
5. Credential theft protections are on. Inbound remote administration services are off unless approved.
6. Where the company has chosen it, only approved software can be installed.

## 3. Updates

- Operating system security updates install automatically within seven days of release.
- Office, browser and store applications update automatically.
- Devices that fall behind lose access to company data until they are updated.

## 4. Vulnerabilities

Software vulnerabilities are identified continuously by the device security service and reviewed every week (PRC-09). Critical and high findings are fixed or mitigated within the times PRC-09 sets, or recorded as an exception (PRC-14).

## 5. Software the company builds or configures

Where {{dictionary.company.trading_name}} builds software or automation:

- code and configuration are kept in version control and changed by reviewed change (POL-14);
- secrets are never written into code, configuration files or logs;
- third-party components come from trusted sources and are kept up to date;
- changes are tested on a test environment before live use;
- an independent penetration test is performed every year where the owner decides the exposure warrants it.

## 6. Standards

{{generated.requirement_table}}

## 7. Current settings

{{generated.settings_summary}}
