# T14-03 findings: devices and Defender

Areas: `intune-macos` (31 settings), `intune-mobile` (25), `defender` (23).
Resolutions are in `expected-state/setting-ids/intune-macos.json`,
`intune-mobile.json` and `defender.json`. Every read against the test
tenant was read-only: Graph GET, `Get-Command` and `Get-*` cmdlets.

## Summary

| Area | resolved | gate | unresolved | not-applied | total |
| --- | --- | --- | --- | --- | --- |
| intune-macos | 30 | 1 | 0 | 0 | 31 |
| intune-mobile | 22 | 1 | 2 | 0 | 25 |
| defender | 8 | 11 | 2 | 2 | 23 |

Providers: macOS, mobile and the Defender for Endpoint settings go to
`intune` (Graph over REST; Defender for Endpoint portal items are gates).
The Defender for Office 365 and Exchange Online Protection settings go to
`exchange` (Exchange Online cmdlets). DEF-023 (add-on) is listed under
`identity`. No new provider is needed.

How it was checked:

- Settings catalogue: each definition id was fetched with
  `GET /beta/deviceManagement/configurationSettings/{id}` (200), and the
  option ids in the payloads were read from the definition. Endpoint
  security template ids were read from
  `GET /beta/deviceManagement/configurationPolicyTemplates`.
- Compliance, app protection, enrolment and custom profile properties and
  enum values were checked against the test tenant's beta `$metadata`.
- Exchange: `Connect-ExchangeOnline -AccessToken` worked app-only.
  `Get-Command` showed every cmdlet and parameter used. Default objects
  were read with `Get-*`.

## Gates

| Setting | Why it is portal-only | Check |
| --- | --- | --- |
| MAC-025 Defender onboarding on macOS | The onboarding package can only be downloaded from the Defender portal. The macOS EDR template holds device tags only. The approvals (system extensions, full disk access, network filter) are resolved through the settings catalogue. | The onboarding profile is assigned with no errors, and every Mac shows as onboarded in the Defender device inventory. |
| MOB-002 Company phones supervised or fully managed | The Apple Business Manager (ADE) token and the managed Google Play binding are portal steps that use company accounts. The enrolment profiles are API-applied after them. | `androidManagedStoreAccountEnterpriseSettings.bindStatus == bound` (it is `notBound` on test); a valid ADE token; `supervisedModeEnabled` is true. |
| DEF-001 Automated investigation, full | Defender advanced features and the device-group automation level have no API. | Portal. Evidence is attached to the gate's Events item. |
| DEF-002 Tamper protection, tenant-wide | Advanced features toggle, portal only. Intune pushes per-device tamper protection (MAC-028, Windows area). | Portal, plus `windowsProtectionState.tamperProtectionEnabled` per device (needs DeviceManagementManagedDevices.Read.All). |
| DEF-003 Web content filtering (staged) | Portal only. The audit form puts the categories under Audit. | A policy exists with the categories and scope. |
| DEF-004 Custom network indicators | Advanced features toggle, portal only. | Portal. |
| DEF-005 Device discovery, standard | Portal only. | Portal. |
| DEF-006 Weekly vulnerability notification | Portal only. | A rule exists with recipient `{mailbox:alerts}`. |
| DEF-007 Alert email, medium and above | Portal only. | A rule exists with severities and recipient. |
| DEF-008 Preview features off | Portal only. | Both toggles are off. |
| DEF-009 Standard preset | The preset's rules and policies exist only after the first turn-on in the portal (no rules exist on test). After that, `Set-`/`Enable-EOPProtectionPolicyRule` and `ATPProtectionPolicyRule` set it. | Rules are Enabled and cover all accepted domains. |
| DEF-010 Strict preset | Same as DEF-009. | Rules are Enabled for the owner, admins, finance and legal. |
| DEF-011 Impersonation protection | Presets beat custom anti-phishing policies, and preset policies cannot be edited with `Set-AntiPhishPolicy` (Microsoft documentation, not tested because writes are out of scope). The apply task should try it once on test. | `EnableTargetedUserProtection`, `TargetedUsersToProtect`, org-domain protection and Quarantine actions on the preset policies. |

Proposed new gates (SPEC §22 lists only G-07 for Apple):

- **Apple Business Manager token**: needed by MAC-023 and MOB-002.
- **Managed Google Play binding**: needed by MOB-002.
- **Defender for Endpoint to Intune connection**: turned on in Defender
  advanced features. Needed by MOB-010.
- **Defender macOS onboarding package**: needed by MAC-025.
- **Preset security policies first turn-on**: needed by DEF-009,
  DEF-010 and DEF-015.

G-07 (Apple MDM push certificate) is a prerequisite for every macOS and
iOS setting. `GET /beta/deviceManagement/applePushNotificationCertificate`
returned 403: it needs DeviceManagementServiceConfig.Read.All, which is
not granted, so the G-07 check cannot run with today's permissions.

## Unresolved (decision needed)

| Setting | Problem | What is available |
| --- | --- | --- |
| MOB-005 Passcode 6 digits, simple blocked | On Android 12 and later, personally owned work-profile devices ignore type and length for the device lock. Only `requiredPasswordComplexity` applies there: medium (4 or more, no sequences) or high (8 or more). | iOS and Android fully managed: exact. Work profile: medium complexity (weaker), high complexity (stronger), or the work-profile passcode with exact values (numericComplex, length 6), which protects only the work profile. |
| MOB-022 App protection minimum OS and app version | App versions are one fixed string per policy, applied to every app in it. "Current minus one" cannot be expressed per app. | Warn and block on OS version with fixed versions kept in configuration. For apps, either no version check or separate policies per app with fixed versions. |
| DEF-013 Safe Attachments block "with dynamic delivery" | `Action` takes one value: Block, DynamicDelivery or Allow. The presets fix Block and take precedence. | Block through the presets, or DynamicDelivery in a custom policy only for recipients outside the presets. |
| DEF-021 Allow/block entries expire in 30 days | No tenant setting exists. Expiry is set on each entry, and admins can add entries with no expiry in the portal. | `-ExpirationDate` on entries the pipeline creates, plus a drift check that reports entries kept too long. "Empty at start" is a read check. |

Resolved, but the value needs a configuration value that does not exist
yet. Intune has no relative version, so the version must be kept current
in configuration:

- MAC-005: propose `devices.macos.minimum_version`.
- MOB-004: propose `devices.ios.minimum_version` and
  `devices.android.minimum_version`.
- MOB-022 (if accepted): propose `devices.*.block_below_version`.

Placeholders used for values to be decided:

- Text: `{text:filevault_escrow_location}`, `{text:update_install_time}`,
  `{text:edge_signin_pattern}`.
- Lists: `{list:edge_extension_allowlist}`,
  `{list:android_approved_keyboards}`, `{list:impersonation_users}`.
- Other: `{file:defender-macos-onboarding}` (tenant data, never in the
  repository).

Possible approximations for the reviewer:

- MOB-020: Android restricts keyboards to an approved list. It has no
  "block third-party" switch.
- MAC-018, MAC-019 and MAC-022: shell scripts. No profile key turns SSH,
  file, printer, internet or Bluetooth sharing off, or locks System
  Settings behind an admin password. The macOS 14 "...Modification"
  restrictions only lock the current state. The proof is the run state on
  each device, not a policy property.
- MAC-014 (disable automatic login) and the OneDrive `AllowTenantList`
  part of MAC-031 need macOS custom profiles, because the settings
  catalogue has no definition for them.
- DEF-019: Microsoft has deprecated the outbound policy's notify
  parameters. Notification goes through the Purview alert policy "User
  restricted from sending email".
- DEF-009 and DEF-010: `SentToMemberOf` needs mail-enabled groups. The
  access groups may be plain security groups; if so, pass the members
  with `SentTo`. Break-glass is not excluded from mail protection. SPEC
  §23 says "exclude where the product allows", but excluding it here
  would weaken protection, so the reviewer should decide.

## Permissions and roles not in expected-state/app-permissions.yaml

None are granted. They need to be proposed in `docs/spec-changes.md`.

| Permission or role | Needed by | Evidence |
| --- | --- | --- |
| graph: DeviceManagementApps.ReadWrite.All | MOB-001, MOB-011 to MOB-025 (all app protection) | `GET .../iosManagedAppProtections` returned 403, naming DeviceManagementApps.Read.All or ReadWrite.All |
| graph: DeviceManagementServiceConfig.ReadWrite.All | MOB-001 (enrolment restriction), MOB-002 (ADE and Android enrolment profiles), MOB-010 (MTD connector), MAC-023 (ADE profile), G-07 check (APNs certificate) | `depOnboardingSettings`, `mobileThreatDefenseConnectors` and `applePushNotificationCertificate` returned 403, naming DeviceManagementServiceConfig.* |
| graph: DeviceManagementScripts.ReadWrite.All | MAC-018, MAC-019, MAC-022 (macOS shell scripts) | `deviceShellScripts` and `deviceCustomAttributeShellScripts` returned 403, naming DeviceManagementScripts.* |
| graph: DeviceManagementManagedDevices.Read.All (optional) | Per-device checks for DEF-002 and MAC-025 | Not probed. `windowsProtectionState` lives under managedDevices. |
| Entra or S&C role for Security & Compliance PowerShell (for example Security Administrator, or Compliance Administrator) | DEF-019 (`Set-ProtectionAlert`) | Not reached: `Connect-IPPSSession` in the installed ExchangeOnlineManagement 3.6.0 has no `-AccessToken`. It needs module 3.8 or later, or certificate sign-in. |

Already granted and used: DeviceManagementConfiguration.ReadWrite.All
(settings catalogue, compliance, custom profiles, device restrictions),
Exchange.ManageAsApp with Exchange Administrator (all EXO cmdlets here
were visible in the app-only session).

## Preview and newer APIs

- The whole Intune surface used here is Graph **beta**:
  configurationPolicies and configurationSettings, the
  deviceCompliancePolicies extensions, deviceShellScripts,
  depOnboardingSettings, and the managed app protections used through
  beta.
- The DDM settings (`passcode_*`, `softwareupdate_*`,
  `ddm-latestsoftwareupdate_*`, technology `appleRemoteManagement`) are
  new. `ddm-latestsoftwareupdate_delayindays` (MAC-017, "days after a
  release before the deadline") needs a recent OS. Confirm fleet OS
  support before relying on it.
- The ADE settings-catalogue profile (`ade_accountsettings_*`, technology
  `enrollment`) is newer than `depMacOSEnrollmentProfile`. Both are
  recorded for MAC-023.
- Deprecated, not used: the MDM `com.apple.mobiledevice.passwordpolicy_*`
  and `com.apple.softwareupdate_*` definitions, and
  `com.apple.managedclient.preferences_enablerealtimeprotection`.

## Tenant Configuration Management

S-02 is still TODO, so every entry has `tcm.resource: null`. The
Microsoft365DSC equivalent resource name is noted where one is known.

## Could not verify

- App protection, enrolment, ADE, MTD connector, APNs and shell script
  objects: 403 for lack of the permissions above. Properties were checked
  in `$metadata` instead.
- Apple and OneDrive profile keys outside the settings catalogue
  (`com.apple.login.mcx.DisableAutoLoginClient`, OneDrive
  `AllowTenantList`): Apple and Microsoft documentation hosts are blocked.
- Defender for Endpoint portal settings: no API, and the Defender API
  host is blocked.
- Security & Compliance cmdlets (`Get-`/`Set-ProtectionAlert`): see the
  role row above.
- That preset policies reject `Set-AntiPhishPolicy` (DEF-011) and need a
  portal first turn-on (DEF-009 and DEF-010): this comes from Microsoft's
  documentation, and the test tenant has no preset rules. Confirm in the
  apply task.
