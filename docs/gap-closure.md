# Closing the audit gaps

Owner direction (2026-11-21): every requirement the audit pack reports as
not met, partly met, not read or document needed is closed as part of
development, on the test tenant first. This file maps each requirement to
why it is open on a fresh tenant and the task that closes it. It is engine
data: requirement ids and causes only, no tenant values. Re-run
`box audit --framework soc2` after each task; the pack shows progress.

## Why a requirement is open

| Cause | Meaning | Closed by |
| --- | --- | --- |
| A. Not read | The audit cannot read it yet (Exchange, SharePoint, Teams, Purview need their own read-only sessions), or the sign-in was not reader-only | T16-06; sign in as the audit reader |
| B. Not applied | The box has the mapping but the step is not applied yet, is blocked by a permission or host, or is audit-only by design | the area's T14 task |
| C. Needs a value | The setting needs a company value; per SPEC §25 it becomes a choice with a suggestion the owner confirms | T13-09 follow-ups (choice per value) |
| D. Needs people | The control depends on people acting: registering MFA, enrolling a device, removing a standing admin role | T14-17 (registration campaign), gate G-05, device enrolment guide |
| E. Needs a document | A written policy or procedure, approved by the owner | T9-01, T9-02 |
| F. Needs a record | Proof the procedure is followed (review, test, exercise) in the period | T10-02 (recurring control tasks) |
| G. Portal-only | No API; a guided gate with a check | SPEC §22 gates |
| H. Enforce | Applied in audit or report-only; met once the owner enforces it after a clean audit period (SPEC §25.1) | T14-04 enforce stage |

## Requirements

| Requirement | Causes | Tasks |
| --- | --- | --- |
| IAM-01 MFA for every user | D, H | T14-17; CA01 enforce after G-02 |
| IAM-02 Phishing-resistant MFA for admins | D, H | T14-17; G-02 |
| IAM-03 Legacy auth blocked | H | T14-04 enforce |
| IAM-04 No SMS, voice, email codes | B | T14-04 (ENT-022..024) |
| IAM-05 Banned passwords, lockout, no expiry, SSPR | C | ENT-026 banned terms choice |
| IAM-06 No standing admin rights | D | gate G-05; T1-05 |
| IAM-07 Break-glass accounts tested | D, E, F | G-02; PRC break-glass test; T10-02 |
| IAM-08 Compliant device or protected app | D, H | device enrolment; ENT-004/005 group choice |
| IAM-09 Session limits | H | T14-04 enforce |
| IAM-10 No user app consent | A, B | T16-06; ENT-030/031 |
| IAM-11 Guests by admins only | A | T16-06 |
| IAM-12 Joiners and leavers through groups | E, F | T9-01 PRC joiner-leaver; T10-02 |
| IAM-13 Quarterly access review | E, F | T9-01; T10-02 |
| IAM-14 Device join needs MFA, named users | C | ENT-039 device-join group choice |
| IAM-15 Risky sign-ins challenged | B | add-on (Entra ID P2) or report; T11 |
| IAM-16 Just-in-time admin roles | B | add-on (P2); T11 |
| IAM-17 App identities without secrets | A, F | T16-06; T10-02 app review |
| APP-01 Monthly app review | E, F | T9-01; T10-02 |
| APP-02 No self-service purchase | B | T14-09 (ADM-001) |
| APP-03 AI limited to approved | C, G | ai.copilot choice; ADM-010 gate |
| BCK-01 Versions, recycle bin, known folders | A, D | T16-06; device enrolment (OFC-011) |
| BCK-02 Independent backup | B | upgrade (Microsoft 365 Backup); T11 |
| BCK-03 Yearly restore test | E, F | T9-01; T10-02 |
| BCK-04 Continuity plan | E | T9-01 |
| CFG-01..08 Device hardening, browser, updates | A, D | T16-06 (Intune read as reader); device enrolment |
| COL-01..06 Sharing, meetings, sites | A, B | T16-06; T14-09 (Teams host and role) |
| DAT-01..13 Labels, DLP, retention | A, B | T14-10, T14-11; T16-06 |
| DEV-01..10 Devices | A, D | device enrolment; T16-06 |
| EML-01..08 Mail protection | A, B | T14-08 (exchange, defender); T16-06 |
| EML-03 SPF, DKIM, DMARC, MTA-STS | B | T2-03, T2-04 |
| GOV-01 Security policy | E | T9-01, T9-02 |
| GOV-02 Risk assessment | E, F | T9-01; T10-02 |
| GOV-03 Exceptions recorded | A | T14-14; T16-06 |
| GOV-04 Monthly coverage report | B | T14-15 |
| GOV-05 Changes reviewed, drift detected | A | T16-06 (drift monitor evidence) |
| INC-01, INC-02 Incident plan and notification | E, F | T9-01; T10-02 |
| INC-03, MAL-01..05 Threat response, malware | A, D, G | device enrolment; Defender gates; T16-06 |
| LOG-01..04 Audit log, alerts, retention, export | A, B | T14-12; T16-06 |
| SUP-01 Supplier reviews | E, F | T9-01; T10-02 |
| SUP-02 Supplier guests named with end date | A | T16-06 |
| AWR-01 Awareness training | E, F | T9-01; T10-02 |
| AWR-02 Acceptable use acknowledged | B | T9-02 (F18) |

## Limits that stay honest

- A test tenant with no computers or phones cannot meet the device
  requirements; they are met when a device is enrolled (a test device or
  a Windows 365 Cloud PC on the test tenant).
- Add-on requirements (IAM-15, IAM-16, BCK-02) are met only with the paid
  capability; the pack reports them as "needs add-on", never as met.
- "Enforce" requirements are met only after the owner approves enforcement
  following a clean audit period (SPEC §25.1); the box never enforces on
  its own.
