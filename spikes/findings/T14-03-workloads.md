# T14-03 findings: workloads group

Areas: exchange, sharepoint, teams, m365-apps, power-platform, purview.
Output: `expected-state/setting-ids/<area>.json`, one entry per setting.
Every read on the test tenant was read-only: Graph GET, `Get-*` cmdlets and
`Get-Command`. Nothing on the tenant was changed.

## Summary

| Area | Settings | resolved | gate | unresolved | not-applied | Provider(s) |
| --- | --- | --- | --- | --- | --- | --- |
| exchange | 18 | 18 | 0 | 0 | 0 | exchange (EXO-017 is dns) |
| sharepoint | 18 | 18 | 0 | 0 | 0 | sharepoint (SPO-011 is intune) |
| teams | 14 | 14 | 0 | 0 | 0 | teams (TMS-014 is identity) |
| m365-apps | 13 | 12 | 0 | 1 | 0 | intune (OFC-009 and OFC-013 are exchange) |
| power-platform | 7 | 0 | 5 | 2 | 0 | powerplatform |
| purview | 31 | 26 | 0 | 0 | 5 | exchange (Security & Compliance PowerShell is in the same module); PVW-002 is identity |
| **Total** | **101** | **88** | **5** | **3** | **5** | |

No new provider is needed. Purview runs on the exchange provider because
`Connect-IPPSSession` ships in ExchangeOnlineManagement, the module that
provider already loads (S-05: one module per provider).

How far each area was verified:

| Area | Verified on the test tenant |
| --- | --- |
| exchange | Every cmdlet and parameter checked with `Get-Command` after an app-only `Connect-ExchangeOnline -AccessToken`. Current values read with `Get-*`. |
| sharepoint | `Set-PnPTenant` and related cmdlet parameters checked (PnP 2.12.0). `Get-PnPTenant`, `Get-PnPTenantSite`, `Get-PnPBrowserIdleSignout`, `Get-PnPTenantSyncClientRestriction` and a site's `Get-PnPWeb` read app-only. CSOM tenant properties read through the PnP context. |
| teams | Not verified on the tenant: host blocked. Parameters checked offline with `Get-Command` in MicrosoftTeams 6.7.0. App-only `Connect-MicrosoftTeams` needs a certificate (`-Certificate -ApplicationId -TenantId`) or `-AccessTokens` with a Graph token and a Teams admin API token. |
| m365-apps | All 79 settings catalogue definition ids, and every option id chosen, exist (`GET /beta/deviceManagement/configurationSettings/{id}`). |
| power-platform | Not verified: `api.bap.microsoft.com`, `api.powerplatform.com` and `api.flow.microsoft.com` are blocked here, and the app is not registered as a Power Platform management application. |
| purview | Security & Compliance PowerShell did not connect app-only (details below). Only PVW-001, PVW-002 and PVW-009 were verified, through Exchange Online and Graph. |

## Gates (power-platform)

All five gates have a documented admin API. They stay gates until a person
registers the pipeline app as a Power Platform management application
(`pac admin application register --application-id <app>`, run by a Power
Platform or Global Administrator) and the API hosts are reachable. After
that, the `api_when_registered` block in each entry applies the setting and
the gate becomes a check.

| Setting | Guide (one line) | API once registered |
| --- | --- | --- |
| PPL-001 | Tenant settings: production/sandbox, trial and developer environment creation set to "Only specific admins". | BAP `scopes/admin/updateTenantSettings` (`disableEnvironmentCreationByNonAdminUsers`, `disableTrialEnvironmentCreationByNonAdminUsers`, `powerPlatform.governance.disableDeveloperEnvironmentCreationByNonAdminUsers`) |
| PPL-002 | Data policy "Tenant data policy", all environments: the eight connectors in Business. | BAP `PowerPlatform.Governance/v2/policies` |
| PPL-004 | Tenant isolation on, empty allow list. | BAP `PowerPlatform.Governance/v1/tenants/{id}/tenantIsolationPolicy` |
| PPL-005 | Tenant setting "Share with Everyone" off for apps. | BAP `updateTenantSettings` (`powerPlatform.powerApps.disableShareWithEveryone`) |
| PPL-007 | Each engine flow: owner is the flow owner account, the owner role's group is co-owner. | Flow admin API `modifyPermissions` (or Dataverse ownership for solution flows, per S-03) |

The PPL-002 business connector list matches
`expected-state/standard-connectors.yaml`, so the engine's own flows stay
inside the policy.

## Unresolved (decision needed)

| Setting | Why | What the product offers |
| --- | --- | --- |
| OFC-011 Default save location: company storage | Windows has no settings catalogue policy that sets the default save location. Company storage is already the default for signed-in users, but no policy enforces it. | macOS `com.apple.managedclient.preferences_defaultstolocalopensave` = false (cloud by default). Windows: OneDrive Known Folder Move silent opt-in (onedrivengscv2), and office16v2 `l_onlinestoragefilter` to hide other cloud locations. |
| PPL-003 All other connectors blocked | Microsoft marks some connectors as unblockable. They can only be Business or Non-business. | Default group for new connectors set to Blocked. Every blockable connector blocked, including HTTP, HTTP with Entra ID and all custom connectors (pattern `*`). Unblockable connectors go in Non-business, so they cannot be combined with business data. |
| PPL-006 AI features off | No single tenant switch turns off every AI feature in Power Apps, Power Automate and AI Builder. | Tenant setting `powerPlatform.intelligence.disableCopilot`. Per-environment generative AI and data movement switches (Dataverse environments only). AI Builder is limited by credits, which the base plan does not include. |

## Partial: one platform cannot deliver the stated value

These are recorded as resolved for Windows, but macOS needs a decision.

| Setting | macOS gap |
| --- | --- |
| OFC-002 VBA: disable all except signed | `VisualBasicMacroExecutionState` has only "disabled with warning", "disable all" and "always allow". There is no signed-only value. |
| OFC-010 Monthly Enterprise; deadline 3 days | Microsoft AutoUpdate has no Monthly Enterprise channel. Its channels are Current, Current (Preview), Current (Deferred), Beta and Current (Monthly). The deadline (`updatedeadline.daysbeforeforcedquit` = 3) works. |
| SPO-011 OneDrive sync for the company tenant only | The settings catalogue has no macOS `AllowTenantList`; it needs a custom preference-file profile (domain `com.microsoft.OneDrive`). On both platforms the device condition comes from Conditional Access (ENT-004), because `Set-PnPTenantSyncClientRestriction -DomainGuids` works only for on-premises AD. |

OFC-001 and OFC-003 to OFC-007 cover Office features that do not exist on
Mac (internet-macro block, Protected View, DDE, ActiveX, trusted locations,
file block). They are not applicable on macOS. That is not a gap in the
policy.

## Resolved but not yet confirmed (check when apply runs)

- PVW-025: limiting the alert to files that were Restricted needs a
  `New-ProtectionAlert -Filter` on the old label. Without IPPS I could not
  confirm the filter syntax. Without the filter, the alert fires on every
  label downgrade or removal, which is wider than the setting.
- PVW-017: the condition "message has an attachment" in Exchange DLP rules
  is still to be settled (`DocumentSizeOver` or a dedicated predicate).
- PVW-015 and PVW-016: a DLP rule whose only condition is "shared outside"
  (`AccessScope` alone) still needs confirming in PowerShell.
- PVW-007 and OFC-013: label policy keys (`Settings` vs `AdvancedSettings`
  for `mandatory`, `requiredowngradejustification`, `defaultlabelid`) are
  still to be confirmed.
- PVW-014: the exact name of the bundled credential type ("All credential
  types") is still to be confirmed.
- PVW-021 to PVW-026: confirm that the plan allows custom single-event
  alert policies (Audit Standard). Confirm the operation names against the
  audit log once ingestion is on.
- SPO-007: confirm that SharePoint accepts `SharingDomainRestrictionMode
  AllowList` with an empty list.
- TMS-012: if the tenant uses Teams app-centric management, app permission
  policies are ignored. Each app is then set with `Update-M365TeamsApp`, and
  the org-wide "third-party apps" toggle becomes a gate (no documented
  cmdlet).
- EXO-016: `Get-DkimSigningConfig` returned nothing because no custom
  domain is added yet.

## Catalogue conflicts found

- Where DLP alerts and incident reports go: PVW-010, PVW-011 and PVW-014
  say the alerts address, PVW-020 says the security mailbox, and T14-11
  says incident reports go to the alerts address. The payloads follow each
  setting's words; T14-11 must pick one.
- These settings duplicate each other; one step should own each:
  - EXO-014 and OFC-009 are the same Exchange role-assignment change.
  - PVW-007 and OFC-013 are the same label policy, but PVW-007 is staged
    and OFC-013 is enforce.
  - TMS-014 and SPO-014 depend on ENT-034 (Group.Unified
    `EnableGroupCreation`).
  - SPO-009 overlaps the Microsoft 365 organisation-wide idle timeout in the
    m365-admin area.
- Values that need a home in the configuration document (R11):
  - EWS allow list (EXO-018)
  - SharePoint sharing domain allow list (SPO-007)
  - Teams federation allow list (TMS-002, enhanced tier)
  - Teams approved app lists (TMS-012)
  - AI policy decisions for Office connected experiences (OFC-012)
  - Per-jurisdiction lists of DLP information types (PVW-012, PVW-013;
    T14-11)

## Permissions and roles needed that are not in expected-state/app-permissions.yaml

These are proposed only and were not granted. T14-03 step 4 says to propose
them in `docs/spec-changes.md`; this group's brief limits output to the
files above, so they are listed here.

| Needed | Kind | For | Notes |
| --- | --- | --- | --- |
| Compliance Administrator | Entra role for the pipeline app | PVW-003 to PVW-008, PVW-010 to PVW-026, OFC-013 | The app holds Exchange, SharePoint, Teams and User Administrator only. App-only Security & Compliance PowerShell needs a compliance role. |
| Directory.ReadWrite.All (or a narrower permission if Graph accepts one for groupSettings) | Graph application permission | TMS-014 (and ENT-034), PVW-008 (Group.Unified `EnableMIPLabels`) | `GET /groupSettings` works with Directory.Read.All; writing needs ReadWrite. Check whether Policy.ReadWrite.Authorization or a group-settings permission is enough before proposing the broad one. |
| Power Platform management application registration | Power Platform admin registration (human step) | PPL-001 to PPL-007 | `pac admin application register`; the token audience is `https://service.powerapps.com/` for the BAP admin API. |
| Power Platform Administrator | Entra role, held by a person, for the registration only | PPL-* | Not for the app. |
| SecurityAlert.Read.All | Graph application permission (optional) | reading Purview alerts through Graph `security/alerts_v2` for evidence | Not needed for apply. Seen as 403. |
| AuditLogsQuery.Read.All | Graph application permission (optional) | Graph audit log query API | Not needed: PVW-002 uses `auditLogs/directoryAudits`, `auditLogs/signIns` and `Search-UnifiedAuditLog`. Seen as 403. |

Already granted and enough:
- Exchange.ManageAsApp with Exchange Administrator: all Exchange settings,
  PVW-001, PVW-009, and the Exchange half of TMS-009.
- Sites.FullControl.All (SharePoint) with SharePoint Administrator: all
  SharePoint settings.
- DeviceManagementConfiguration.ReadWrite.All: all m365-apps Intune
  settings and SPO-011.
- AuditLog.Read.All: PVW-002, and the audit form of EXO-001.
- Teams Administrator: the Teams policy cmdlets.

## Security & Compliance PowerShell connection (purview)

- ExchangeOnlineManagement 3.6.0 (the pinned version) has no
  `-AccessToken` on `Connect-IPPSSession`. A later module version would be
  needed for token-based connection, which is a modules.yaml change.
- `Connect-IPPSSession -Certificate <pipeline cert> -AppId -Organization`
  (certificate passed in memory only) failed with "Could not find the
  organization container".
- `Connect-ExchangeOnline -AccessToken -ConnectionUri .../powershell-liveid/`
  failed the same way, with both a compliance token and an Exchange token.
- Likely cause: the app has no Security & Compliance role (Compliance
  Administrator). It could also be first-use provisioning of the tenant's
  compliance organisation. This can only be settled after the role is
  granted.
- `New-ProtectionAlert` and `Get-ProtectionAlert` are not in the Exchange
  Online session; they need the IPPS session.

## Preview or beta APIs

- Intune settings catalogue: `/beta/deviceManagement/configurationPolicies`
  and `/beta/deviceManagement/configurationSettings` (all m365-apps Intune
  settings, SPO-011).
- The Windows Office "Update Channel" definition id in Microsoft's
  catalogue really begins `pdates~policy~...` (a truncated id, verified).
  The deprecated twin under `device_vendor_msft_policy_config_office16v2`
  is listed in the notes in case Microsoft fixes it.
- `Set-OrganizationConfig -RejectDirectSend` (EXO-003) is a recent setting.
- BAP admin API versions `2020-10-01` (tenant settings), `2018-11-01` (DLP
  v2) and `2020-06-01` (tenant isolation) are not part of Graph. The
  Microsoft 365 Apps cloud policy service (config.office.com) has no
  supported write API, and its host is blocked here.

## Tenant Configuration Management

S-02 is not done, so every `tcm.resource` is a candidate name marked
unconfirmed:
- `microsoft.exchange.*`, `microsoft.teams.*` and
  `microsoft.securityandcompliance.*` are candidates.
- No SharePoint, Power Platform or Intune settings catalogue resource is
  assumed. Those settings are monitored by `box drift`.
