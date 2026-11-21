# T14-03 findings: intune-windows setting resolution

Date: 2026-11-21. Area: `policy/settings/intune-windows.yaml` (90 settings,
WIN-001 to WIN-090). Output: `expected-state/setting-ids/intune-windows.json`.
Provider: `intune` (Graph over REST, `common/BoxGraph.psm1`), step
`policy.intune-windows`.

## Summary

| Area | Settings | resolved | gate | unresolved | not-applied |
| --- | --- | --- | --- | --- | --- |
| intune-windows | 90 | 88 | 1 | 1 | 0 |

No setting in this area is tier `addon`. 69 settings need `intune_p1` and
21 need `defender_business`; Business Premium includes both.

How each part is applied:

| Settings | Object | API |
| --- | --- | --- |
| WIN-001 to WIN-011 | one Windows compliance policy | `deviceCompliancePolicies` (#microsoft.graph.windows10CompliancePolicy) |
| WIN-012 to WIN-031, WIN-035, WIN-041 to WIN-056, WIN-065 to WIN-090 | settings-catalogue policies | `configurationPolicies` |
| WIN-032, WIN-033, WIN-036, WIN-037, WIN-057 | endpoint security template policies | `configurationPolicies` with `templateReference` |
| WIN-038 | Autopilot profile | `windowsAutopilotDeploymentProfiles` |
| WIN-039, WIN-040 | default enrolment status page and default enrolment restriction | `deviceEnrollmentConfigurations` |
| WIN-059 to WIN-063 | two update rings (pilot, broad) | `deviceConfigurations` (#microsoft.graph.windowsUpdateForBusinessConfiguration) |
| WIN-064 | feature update policy | `windowsFeatureUpdateProfiles` |
| WIN-034 | Defender portal switch | none (gate) |

The settings fall into 15 settings-catalogue or template policies, one
compliance policy, two update rings, one feature update profile, one
Autopilot profile and two default enrolment objects. Several settings share
one parent definition (firewall profile children, the ASR rules group,
BitLocker recovery options), so the step must merge every applied setting
under the same parent into one instance before it PUTs the policy.

## How it was verified (read-only)

- Intune is provisioned on the test tenant. `GET
  /beta/deviceManagement/deviceCompliancePolicies`, `configurationPolicies`,
  `deviceConfigurations`, `windowsFeatureUpdateProfiles`,
  `notificationMessageTemplates` and `configurationPolicyTemplates` all
  returned 200. The tenant has no policies yet.
- All 171 settings-catalogue definition ids in the file were read with
  `GET /beta/deviceManagement/configurationSettings/{id}` and returned 200.
  Every option id and integer range in the payloads was taken from those
  definitions. The script that wrote the JSON stops if any id or option is
  missing or a value is out of range.
- The template instance references for the template-only definitions (LAPS,
  local user group membership, App Control, EDR, tamper protection) were
  read from `GET /beta/deviceManagement/configurationPolicyTemplates/{id}/settingTemplates`.
- Compliance, update ring, enrolment, Autopilot and feature update property
  names and enum values were checked against the Graph beta `$metadata`.
- Ids tried that do not exist on the tenant (404), so they are not used:
  `..._lsa_configurelsaprotectedprocess` (the right id is
  `..._localsecurityauthority_configurelsaprotectedprocess`),
  `..._defender_controlledfolderaccessenabled`, the BitLocker ADMX "Disable
  new DMA devices when this computer is locked" (not in the catalogue under
  any name searched) and `..._onedrivengsc_kfmsilentoptin`.

## Gate

| Setting | Reason | Guide | Check |
| --- | --- | --- | --- |
| WIN-034 EDR in block mode | The switch exists only in the Defender portal (Settings, Endpoints, Advanced features). Graph and the Intune API have no property for it. | Turn on "Enable EDR in block mode" as a Security Administrator. | The owner's confirmation is recorded as an `Events` item. EDR block mode only acts when another antivirus is primary, so with Defender Antivirus active there is nothing on the device to read back. |

**Proposed new gate: Defender for Endpoint to Intune connection.** WIN-010,
WIN-032 and WIN-033 resolve to API objects. They only work once the
Defender portal switch "Microsoft Intune connection" is on (Settings,
Endpoints, Advanced features), and that switch has no API. The Intune side
(`mobileThreatDefenseConnectors/fc780465-2017-40d4-a0c5-307022471b92`,
`windowsEnabled`, `partnerState`) can be read and set through Graph, but
only with DeviceManagementServiceConfig permissions (below). SPEC §22 has no
gate for this. It needs a spec change (`docs/spec-changes.md`), which this
task did not make. Suggested check:
`partnerState == 'enabled' and windowsEnabled == true` on that connector.

## Unresolved (decision needed)

| Setting | Stated value | What is available |
| --- | --- | --- |
| WIN-058 Microsoft Store: private store only and automatic app updates | on | Automatic updates resolve exactly (`applicationmanagement_allowappstoreautoupdate_1`). "Private store only" cannot be delivered as stated because the private store belonged to the retired Microsoft Store for Business. Options: (a) `requireprivatestoreonly_1`, which leaves users with an empty store, or (b) "Turn off the Store application" (`admx_windowsstore_removewindowsstore_2_1`) while Intune deploys required apps from the new Store integration. Pick one, or reword the requirement. |

## Resolved, but blocked by an empty configuration value

These values are tenant choices. No configuration path holds them yet, so
the payload carries a `{config:...}` placeholder. Per CLAUDE.md the step must
stop on an empty required value. Configuration keys proposed:

| Setting | Proposed key |
| --- | --- |
| WIN-008 minimum OS build | `devices.windows.minimum_os_version` |
| WIN-064 feature update held at | `devices.windows.feature_update_version` |
| WIN-069 extension allow list (enhanced) | `devices.browser.extension_allowlist` |
| WIN-070 password manager off "where a company password manager is used" (enhanced) | `devices.browser.company_password_manager` |

Values taken from definition or portal defaults where the requirement is
silent are listed in each entry's `notes`. Examples: quick scan at 02:00,
LAPS complexity 4 with a 24-hour reset delay, active hours 08:00 to 17:00,
and the removable-drive cipher in WIN-013.

## Permissions and roles not in expected-state/app-permissions.yaml

None were granted. These need a proposal in `docs/spec-changes.md` and a
decision.

| Permission | Needed by | Evidence |
| --- | --- | --- |
| graph:DeviceManagementServiceConfig.ReadWrite.All | WIN-038 (Autopilot profile), WIN-039 (enrolment status page), WIN-040 (enrolment restriction); also the Defender connector for WIN-010, WIN-032 and WIN-033, and the Windows licence attestation for WIN-064 | `GET deviceEnrollmentConfigurations`, `windowsAutopilotDeploymentProfiles`, `mobileThreatDefenseConnectors` and `dataProcessorServiceForWindowsFeaturesOnboarding` returned 403 with "must have one of DeviceManagementServiceConfig.Read.All / .ReadWrite.All" |
| graph:Policy.ReadWrite.DeviceConfiguration | WIN-036 prerequisite: Entra device setting "Enable Microsoft Entra LAPS" (`PUT /beta/policies/deviceRegistrationPolicy`, `localAdminPassword.isEnabled`). It reads `false` on the test tenant. Without it, Windows LAPS cannot back up to Entra. | `GET /beta/policies/deviceRegistrationPolicy` returned 200 with Policy.Read.All; writing needs Policy.ReadWrite.DeviceConfiguration |

No Entra directory role is needed: Intune Graph application permissions are
enough. WIN-034 needs a person with Security Administrator at the gate, not
the pipeline app.

## Preview and beta APIs

- Everything except the gate uses Graph **beta**. `configurationPolicies`,
  `configurationSettings`, `configurationPolicyTemplates`,
  `windowsFeatureUpdateProfiles`, `deviceEnrollmentConfigurations` (the
  enrolment status page type), `windowsAutopilotDeploymentProfiles` and the
  compliance `scheduleActionsForRules` action are beta-only. The v1.0
  `deviceCompliancePolicies` and `deviceConfigurations` exist but lack
  properties used here, so beta is used throughout.
- The tamper protection definition
  (`vendor_msft_defender_configuration_tamperprotection_options`) shows as
  "Configuration Protection (Device) (Preview)" on the tenant.
- App Control for Business built-in controls (`applicationcontrolv2_*`,
  template `d3849ba8-bf95-467c-9640-aa2334eae9e3_1`) are template-only. The
  nesting of the audit and trust switches under `buildoptions` should be
  checked against a portal-created policy on the first apply.

## Points for the reviewer

- **Staged settings with no audit mode:** BitLocker (WIN-012) and Remote
  Desktop (WIN-050) have no audit or report-only form. Their
  `payload_audit` is the same settings assigned to the pilot group only.
  The ASR rules, network protection, App Control and controlled folder
  access have real audit options, recorded in `payload_audit`.
- **WIN-014 enhanced:** requiring TPM+PIN conflicts with silent encryption
  (WIN-012). The user has to set the PIN, so the device stays unencrypted
  until they do.
- **WIN-037:** replacing the local Administrators group removes the Global
  Administrator role SID that Entra join adds. The built-in Administrator
  (managed by LAPS) is kept. Members are the Entra "Joined Device Local
  Administrator" role SID, derived at plan time. Review before enforcing.
- **WIN-072:** "block personal OneDrive accounts" exists only as a
  user-scoped definition, so that policy must also be assigned to users.
- **WIN-067:** Edge has no single "block sync to personal profile" switch.
  Forced, restricted sign-in with no extra or guest profiles delivers the
  value. The sign-in pattern is built at plan time from verified domains and
  never stored.
- **TCM:** S-02 is not done. Each entry carries the Microsoft365DSC/UTCM
  resource name expected to cover the object, marked unverified.
