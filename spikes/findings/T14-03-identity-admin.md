# T14-03 findings: identity and admin centre (entra, m365-admin)

Setting resolution for `policy/settings/entra.yaml` (49 settings) and
`policy/settings/m365-admin.yaml` (14 settings). Results are in
`expected-state/setting-ids/entra.json` and
`expected-state/setting-ids/m365-admin.json`.

All checks against the test tenant were read-only: Graph GET with the
pipeline app's app-only token, and `Get-*` cmdlets in Exchange Online. No
tenant value is recorded here or in the JSON files. Tenant-specific values
are placeholders such as `{group:grp-break-glass}`.

## Summary

| Area | Settings | resolved | gate | unresolved | not-applied |
| --- | --- | --- | --- | --- | --- |
| entra | 49 | 41 | 5 | 1 | 2 |
| m365-admin | 14 | 6 | 3 | 4 | 1 |
| Total | 63 | 47 | 8 | 5 | 3 |

Provider assignment: both areas go to `identity`, which calls Graph over
REST. ADM-008 goes to `exchange` (`Set-SharingPolicy`), and the Bookings
part of ADM-006 could go there too. No new provider is needed.

## Reuse of existing identity shapes

- ENT-001, 002, 003 and 007 match CA01, CA02, CA03 and CA05 in
  `baseline/identity.yaml`, built by `Get-PolicyBody`. ENT-007 adds
  `persistentBrowser {mode: never}`, which CA05 does not have yet.
- ENT-004 does not match CA04 (see "Differences" below).
- ENT-019 to 024 use the same objects that `identity.auth-methods` already
  writes. ENT-020 also needs `featureSettings.displayAppInformationRequiredState`
  and ENT-021 needs `isUsableOnce` and the lifetimes. The provider writes
  only `state` today.
- ENT-030, 031, 033, 034, 036 and 037 use `/policies/authorizationPolicy`
  (`identity.directory-settings`). ENT-033 and ENT-034 need properties the
  provider does not write yet. Both show drift on the test tenant:
  `allowedToCreateTenants` and `allowedToCreateSecurityGroups` are true.
- ENT-032 uses the `adminConsentRequestPolicy` PUT body that
  `identity.directory-settings` already sends.
- New Conditional Access policies have proposed names CA06 to CA14. Every
  one of them excludes `{group:grp-break-glass}`. Staged policies have a
  `payload_audit` with state `enabledForReportingButNotEnforced`.

## Gates (no API to write; SPEC §22, R5)

| Id | Reason | Check |
| --- | --- | --- |
| ENT-029 Self-service password reset | Graph has no write API for SSPR scope or for the number of methods. `allowedToUseSSPR` covers administrators only. | The owner confirms, recorded as an Events item. Partial automated check: `userRegistrationDetails.isSsprEnabled`. |
| ENT-035 Restrict access to the admin portal | Graph has no read or write for this switch. | The owner confirms, with a screenshot. |
| ENT-038 External collaboration allowed domains | The only API is the undocumented beta `B2BManagementPolicy` in `/beta/legacy/policies`. It is not used for writes. | Automated read-back: `GET /beta/legacy/policies` (it returned 200 on the test tenant). |
| ENT-043 Global Administrator holders | Removing a role needs `RoleManagement.ReadWrite.Directory`, which the pipeline must not hold. Covered by existing gate G-05. | Automated: the Global Administrator assignments equal the members of `{group:grp-break-glass}`. |
| ENT-048 LinkedIn account connections | Graph has no API. | The owner confirms. |
| ADM-002 Self-service purchase | The MSCommerce module signs in as a delegated user only, and its host is blocked here. Graph has no API. | The owner confirms. |
| ADM-006 Sway, Whiteboard, Bookings | Sway and Whiteboard have no API. Bookings can be set with `Set-OrganizationConfig -BookingsSocialSharingRestricted`. | Bookings is checked automatically. Sway and Whiteboard need the owner's confirmation. |
| ADM-009 Release track | Graph has no API. Standard release is the default. | The owner confirms. |

## Unresolved (decision needed)

| Id | What is available | Why it is not resolved |
| --- | --- | --- |
| ENT-045 Alert on break-glass sign-in | (a) An Azure Monitor alert, which is paid and needs a host that is blocked here. (b) A Purview alert policy on `UserLoggedIn`, but app-only S&C PowerShell is unproven: `Connect-IPPSSession` in the installed module has no `-AccessToken`. (c) A Box scheduled job that polls `/auditLogs/signIns` (readable now) and raises an Events item for the alerts path. `Mail.Send` is forbidden. | No free, app-only "mail on sign-in" API exists. |
| ADM-003 Idle session timeout | `activityBasedTimeoutPolicies` with `WebSessionIdleTimeout 01:00:00`, which applies to the whole tenant. Needs `Policy.ReadWrite.ApplicationConfiguration`. | The policy cannot be limited to "unmanaged devices". Accept 1 hour for the whole tenant, or rely on ENT-008 plus SharePoint idle sign-out. |
| ADM-010 Copilot chat and web grounding | A Cloud Policy setting, which has no app-only API (host blocked), and Copilot settings in the admin centre. | The value "as approved in the AI policy" states nothing to apply. |
| ADM-011 Viva and analytics | `/admin/people/itemInsights`, `Set-DefaultTenantMyAnalyticsFeatureConfig` (read works app-only) and Viva feature access policies. | No features are named. |
| ADM-012 Azure subscriptions | Azure subscription policies for subscriptions leaving or entering the directory: the ARM API (private preview, host blocked) or the portal with elevated access. | Subscription creation has no switch for the whole tenant. |

## Differences from what dependent tasks assume (stop and report)

- **ENT-004 compared with baseline CA04.** The setting asks for desktop and
  web apps, for users on a managed plan. CA04 targets `includeUsers: All`
  in `Get-PolicyBody` and includes iOS and Android. The resolved payload
  scopes the policy to `{group:plan-business_premium}`, with platforms
  windows and macOS. That group does not exist in the configuration schema.
  The same gap applies to ENT-005.
- **ENT-005.** `approvedApplication` is retired. The resolved payload uses
  `compliantApplication` ("Require app protection policy").
- **ENT-012.** Blocking device code flow for all users would also block the
  owner's `box` CLI sign-in (`auth.SignInDeviceCode`). Decide on an
  exclusion or another flow before this is enforced.
- **ENT-015.** It must check every CA policy in the tenant. `Get-CAChanges`
  checks only the policies it declares.
- **ADM-005 compared with gate G-04.** Turning external Forms responses off
  stops the public NDA and demo-request forms from working.
- **ENT-046.** Local runs use a certificate. The value "federated credential"
  holds only after `box handoff`.

## Configuration gaps

- ENT-014: there is no configuration key for the allowed countries. A key
  such as `policy.allowed_countries` is proposed.
- ENT-039: there is no configuration key for the "named group" allowed to
  join devices.
- ENT-026: the Dictionary has no key for product names. The trading name
  and legal name exist.
- ENT-004 and ENT-005: there is no group of managed-plan users.

## Permissions not in expected-state/app-permissions.yaml (proposed, not granted)

| Permission | Needed by |
| --- | --- |
| graph:Policy.ReadWrite.SecurityDefaults | ENT-018. The existing `identity.security-defaults` fallback step PATCHes this object too and would get 403. |
| graph:Directory.ReadWrite.All | ENT-026, ENT-027, ENT-034 (directory setting objects `/groupSettings`) |
| graph:Policy.ReadWrite.DeviceConfiguration | ENT-039, 040, 041, 042 (`PUT /beta/policies/deviceRegistrationPolicy`) |
| graph:Domain.ReadWrite.All | ENT-028, ADM-004. Needed only to correct drift; the value is already set. |
| graph:OrgSettings-AppsAndServices.ReadWrite.All | ADM-001 (the read returned 403) |
| graph:OrgSettings-Forms.ReadWrite.All | ADM-005 (the read returned 403) |
| graph:Application.ReadWrite.All (high privilege) | ADM-007. The alternative is a gate with the same automated read-back. |
| graph:Policy.ReadWrite.ApplicationConfiguration | ADM-003, if accepted for the whole tenant |
| graph:PeopleSettings.ReadWrite.All | ADM-011, if item insights is chosen |

No new Entra role is needed. Exchange Administrator, which is already
granted, covers ADM-006 (Bookings) and ADM-008. ENT-043 deliberately needs
no role-management permission.

Reads that returned 403 and are not needed by any check:
`/admin/microsoft365Apps/installationOptions`, `/admin/reportSettings`,
`/directory/deviceLocalCredentials` and `/beta/directory/onPremisesSynchronization`.

## Preview and beta APIs

- `/beta/policies/deviceRegistrationPolicy` (PUT, whole object): ENT-039 to 042.
- `/beta/users/{id}/authentication/requirements` (per-user MFA state): ENT-025.
- `/beta/admin/appsAndServices` and `/beta/admin/forms`: ADM-001 and ADM-005.
- `/beta/legacy/policies` (B2BManagementPolicy, undocumented): ENT-038.
- `/beta/reports/authenticationMethods/userRegistrationDetails`: ENT-029 check.
- ARM `Microsoft.Subscription/policies` (`api-version=2021-01-01-privatepreview`): ADM-012.

## Tenant Configuration Management

S-02 is not done. The `tcm.resource` values are candidate resource types
and are marked as not confirmed.

## Not verified, and why

- ADM-001 and ADM-005: the reads returned 403 because the permissions are
  missing.
- ADM-002: the MSCommerce module needs a delegated sign-in and its host is
  blocked.
- ADM-010: the Cloud Policy host is blocked.
- ADM-012: management.azure.com is blocked.
- ENT-045: Purview could not be inspected, because `Connect-IPPSSession`
  has no `-AccessToken` in the installed ExchangeOnlineManagement module.
- ENT-035, ENT-048 and ADM-009: there is no API to read.
- New CA policies (CA06 to CA14) have not been created, by design. Only the
  collection read was verified.
