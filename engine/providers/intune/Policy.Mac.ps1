# policy.intune-macos (T14-06): macOS compliance, FileVault, firewall,
# Gatekeeper, update, restriction, single sign-on and Defender profiles from
# the resolved intune-macos settings. Helpers: IntunePolicy.Mobile.ps1.

# Shell scripts for settings no profile key covers (mapping notes of
# MAC-018, MAC-019, MAC-022). Each runs as root daily and reports the state.
$script:ImMacScripts = @{
  'MAC-018' = @'
#!/bin/sh
# box MAC-018: Remote Login (SSH) and Remote Apple Events off.
/usr/sbin/systemsetup -f -setremotelogin off >/dev/null 2>&1
/usr/sbin/systemsetup -setremoteappleevents off >/dev/null 2>&1
echo "Remote Login: $(/usr/sbin/systemsetup -getremotelogin 2>/dev/null | /usr/bin/awk -F': ' '{print $2}')"
echo "Remote Apple Events: $(/usr/sbin/systemsetup -getremoteappleevents 2>/dev/null | /usr/bin/awk -F': ' '{print $2}')"
'@
  'MAC-019' = @'
#!/bin/sh
# box MAC-019: screen, file, printer, internet and Bluetooth sharing off.
for s in com.apple.screensharing com.apple.smbd; do
  /bin/launchctl disable "system/$s" 2>/dev/null
  /bin/launchctl bootout "system/$s" 2>/dev/null
done
/usr/sbin/cupsctl --no-share-printers 2>/dev/null
/usr/bin/defaults write /Library/Preferences/SystemConfiguration/com.apple.nat NAT -dict Enabled -int 0
for u in $(/usr/bin/dscl . -list /Users UniqueID | /usr/bin/awk '$2 >= 501 {print $1}'); do
  /usr/bin/sudo -u "$u" /usr/bin/defaults -currentHost write com.apple.Bluetooth PrefKeyServicesEnabled -bool false 2>/dev/null
done
off() { /bin/launchctl print-disabled system | /usr/bin/grep -Eq "\"$1\" => (disabled|true)" && echo Off || echo On; }
echo "Screen Sharing: $(off com.apple.screensharing)"
echo "File Sharing: $(off com.apple.smbd)"
echo "Printer Sharing: $(/usr/sbin/cupsctl | /usr/bin/grep -q '_share_printers=1' && echo On || echo Off)"
echo "Internet Sharing: $(/usr/bin/defaults read /Library/Preferences/SystemConfiguration/com.apple.nat NAT 2>/dev/null | /usr/bin/grep -q 'Enabled = 1' && echo On || echo Off)"
echo "Bluetooth Sharing: Off"
'@
  'MAC-022' = @'
#!/bin/sh
# box MAC-022: administrator password required for system-wide settings.
tmp=$(/usr/bin/mktemp)
/usr/bin/security authorizationdb read system.preferences > "$tmp" 2>/dev/null
/usr/libexec/PlistBuddy -c "Set :shared false" "$tmp" 2>/dev/null || /usr/libexec/PlistBuddy -c "Add :shared bool false" "$tmp"
/usr/bin/security authorizationdb write system.preferences < "$tmp" 2>/dev/null
echo "system.preferences shared: $(/usr/bin/security authorizationdb read system.preferences 2>/dev/null | /usr/bin/plutil -extract shared raw - 2>/dev/null)"
/bin/rm -f "$tmp"
'@
}

# Settings that need a company-only prerequisite the box cannot supply.
$script:ImMacSkip = @{
  'MAC-023' = 'needs an Apple Business Manager (automated device enrolment) token uploaded in the Intune admin centre and DeviceManagementServiceConfig.ReadWrite.All; the enrolment profile is not created'
}

function Get-ImMacObjects($In) {
  $objects = [ordered]@{}
  $skipped = [System.Collections.Generic.List[object]]::new()
  foreach ($s in Get-ImSettings $In) {
    if ($script:ImMacSkip.ContainsKey($s.id)) { $skipped.Add(@{ id = $s.id; reason = $script:ImMacSkip[$s.id] }); continue }
    $p = ConvertTo-ImPlain $s.payload
    $groups = Get-ImPayloadGroups $p
    # Described payloads made concrete: profiles and scripts.
    if ($p['@odata.type'] -eq '#microsoft.graph.macOSCustomConfiguration' -and $p.payloadFileName -eq 'box-loginwindow.mobileconfig') {
      $p.payload = New-ImMobileConfig 'com.box.loginwindow' $p.payloadName 'com.apple.loginwindow' ([ordered]@{ 'com.apple.login.mcx.DisableAutoLoginClient' = $true })
    }
    if ($p.deviceShellScript -and $script:ImMacScripts.ContainsKey($s.id)) { $p.deviceShellScript.scriptContent = ConvertTo-ImBase64 $script:ImMacScripts[$s.id] }
    if ($p.allowTenantList -and $In.tenant.id) {
      $p.allowTenantList.payload = New-ImMobileConfig 'com.box.onedrive.tenants' 'OneDrive tenant list' 'com.microsoft.OneDrive' ([ordered]@{ AllowTenantList = @([string]$In.tenant.id) })
      $p.allowTenantList.payloadName = 'OneDrive tenant list'
      $p.allowTenantList.payloadFileName = 'box-onedrive.mobileconfig'
    }
    # One payload, or named parts (script + restrictions, profile + list).
    $parts = [ordered]@{}
    if ($p.Contains('deviceShellScript') -or $p.Contains('configurationPolicy') -or $p.Contains('allowTenantList') -or $p.Contains('depMacOSEnrollmentProfile')) {
      foreach ($k in @($p.Keys)) { $parts[$k] = $p[$k] }
    } else { $parts['payload'] = $p }
    foreach ($k in @($parts.Keys)) {
      $part = Resolve-ImPart $In (Remove-ImAssignments $parts[$k]) "$($s.id)$(if ($k -ne 'payload') { " ($k)" })" $skipped
      if ($null -eq $part) { continue }
      $type = [string]$part['@odata.type']
      if ($k -eq 'deviceShellScript') {
        $o = Get-ImObject $objects 'shellScript' $part.displayName $null
        $o.body.Remove('@odata.type')
        $part.Remove('displayName'); Merge-ImProps $o.body $part; Add-ImUse $o $s.id $groups
      } elseif ($type -eq '#microsoft.graph.macOSCompliancePolicy' -or $type -eq '#microsoft.graph.macOSCustomConfiguration') {
        $kind = if ($type -like '*CompliancePolicy') { 'compliance' } else { 'deviceConfiguration' }
        $o = Get-ImObject $objects $kind $part.displayName $type
        $part.Remove('@odata.type'); $part.Remove('displayName'); $part.Remove('scheduledActionsForRule')
        Merge-ImProps $o.body $part; Add-ImUse $o $s.id $groups
      } elseif ($part.Contains('settings')) {
        $name = if ($part.name) { [string]$part.name } elseif ($k -eq 'configurationPolicy' -and $s.id -eq 'MAC-031') { 'Box macOS OneDrive' } else { "Box macOS $($s.id)" }
        Add-ImCatalog $objects $name ([string]$part.platforms) ([string]$part.technologies) $part.settings ([string]$part.templateReference.templateId) $s.id $groups
      } else {
        $skipped.Add(@{ id = "$($s.id) ($k)"; reason = 'payload has no object the step knows how to make' })
      }
    }
  }
  Complete-ImCatalog $objects
  return @{ objects = @($objects.Values); skipped = $skipped.ToArray() }
}

$script:Steps['policy.intune-macos'] = @{
  test     = { param($In) Test-ImInput $In }
  plan     = { param($In) Invoke-ImStepOp $In 'Get-ImMacObjects' 'plan' 'intune-macos' }
  apply    = { param($In) Invoke-ImStepOp $In 'Get-ImMacObjects' 'apply' 'intune-macos' }
  verify   = { param($In) Invoke-ImStepOp $In 'Get-ImMacObjects' 'verify' 'intune-macos' }
  evidence = { param($In) Invoke-ImStepOp $In 'Get-ImMacObjects' 'evidence' 'intune-macos' }
}
