---
id: PRC-09
title: Vulnerability review
version: "1.0"
owner_role: control-owner
approver_role: owner
review_months: 12
requirements: [CFG-04]
controls:
  soc2: [CC7.1]
  iso27001_2022: [A.8.8]
enforced_by: [CFG-02, CFG-03, F17]
records: ["ControlTasks:vulnerability_review weekly"]
---

# Vulnerability review procedure

{{dictionary.company.legal_name}} · Version {{generated.version}} · Approved: {{generated.date}} · Next review {{generated.next_review}}

## 1. Purpose

The device security service identifies software vulnerabilities on company devices continuously. This procedure makes sure the findings are reviewed every week and acted on in time.

## 2. Weekly review (about 30 minutes)

The vulnerability review control task is raised each week.

1. Open the vulnerability view in the security portal, or the box's weekly summary of device update and vulnerability status.
2. Note the number of devices, and how many are not compliant or have missed updates.
3. List new findings since last week with severity critical or high, and any finding marked as exploited in the wild.
4. For each, decide the action:
   - an update is available: make sure it is deploying; force it on devices that are behind;
   - no update yet: apply the vendor's mitigation, or restrict the software, and note when to check again;
   - unsupported software or operating system: plan removal or replacement;
   - the risk is accepted: record an exception with an end date (PRC-14).
5. Check devices that have not reported in over 14 days; contact their users.
6. Check the tenant's security recommendations for new high-impact items and raise a proposal where a setting would fix them.

## 3. Target times

| Severity | Fix or mitigate within |
| --- | --- |
| Critical, or known to be exploited | 7 days |
| High | 14 days |
| Medium | 30 days |
| Low | Next routine update |

Operating system security updates install automatically within seven days of release; these targets cover what automatic updates do not.

## 4. Record

Close each week's control task with: devices in scope, non-compliant devices, new critical and high findings, the action for each, and any finding past its target time with the reason. Findings past target for two weeks are reported to the owner.
