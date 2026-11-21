# Upgrade: browser extensions in use (inventory)

- Needs the Microsoft Defender Vulnerability Management add-on: its
  browser extensions assessment is the only source Microsoft Graph exposes
  for the extensions installed on devices. Microsoft 365 Business Premium
  (Defender for Business) does not include it. Intune's discovered apps
  do not list browser extensions, and the Edge management service has no
  Graph API.
- Needs the application permission `ThreatHunting.Read.All` for the
  pipeline app, read only. Proposed as a new permission bundle
  `device-inventory` in `expected-state/app-permissions.yaml`, needed only
  when the owner chooses the inventory; it is not granted and not in the
  file until the owner approves it (CLAUDE.md, SPEC §24.3), and adding it
  asks for the Global Administrator sign-in again (gate).
- Replaces one unit (R9): `extensions.ReadInventory` in
  `box extensions report` runs the Advanced hunting query
  `DeviceTvmBrowserExtensions | where BrowserName =~ "edge" | summarize
  Devices = dcount(DeviceId) ... by ExtensionId` over
  `POST /security/runHuntingQuery`. Without the upgrade the report lists
  the approved extensions and what the live Edge policy does with each,
  with devices "unknown" and the reason (T14-20).
- The audit pack (`box audit`) cannot read it either way: Advanced hunting
  is a POST and the audit sends GET only; its `browser_extensions`
  inventory says so.
- Worth it when staff use many extensions or the company must show which
  extensions run on its devices (for example for SOC 2 CC6.8).
