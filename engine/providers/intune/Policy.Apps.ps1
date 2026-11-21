# policy.m365-apps (T14-05, intune part): Office settings-catalogue
# policies from the resolved m365-apps settings. The payloads give each
# setting in a short form {settingDefinitionId, choice|value, children};
# the setting instances are typed from Microsoft's definitions at run time.
# Helpers: IntunePolicy.Mobile.ps1. OFC-009 and OFC-013 belong to
# policy.m365-apps.exchange.

$script:ImAppsPlatform = @{
  windows       = @{ platforms = 'windows10'; technologies = 'mdm'; label = 'Windows' }
  macos         = @{ platforms = 'macOS'; technologies = 'mdm,appleRemoteManagement'; label = 'macOS' }
  macos_nearest = @{ platforms = 'macOS'; technologies = 'mdm,appleRemoteManagement'; label = 'macOS' }
}
# Policy names by setting and platform; the rest are 'Box Office: <id> (<platform>)'.
$script:ImAppsNames = @{
  'OFC-008' = @{ windows = 'Box Office: macro and content security (Windows)'; macos = 'Box Office: Outlook (macOS)' }
  'OFC-010' = @{ windows = 'Box Office: updates (Windows)'; macos_nearest = 'Box Office: updates (macOS)' }
  'OFC-011' = @{ windows = 'Box - Windows - OneDrive known folders'; macos = 'Box - macOS - Office default save' }
  'OFC-012' = @{ windows = 'Box Office: connected experiences (Windows)'; macos = 'Box Office: connected experiences (macOS)' }
}

function Get-ImAppsObjects($In) {
  $objects = [ordered]@{}
  $skipped = [System.Collections.Generic.List[object]]::new()
  foreach ($s in Get-ImSettings $In) {
    $p = ConvertTo-ImPlain $s.payload
    $groups = Get-ImPayloadGroups $p
    $parts = [ordered]@{}
    if ($p.Contains('policy')) { $parts['windows'] = $p } else { foreach ($k in @($p.Keys)) { $parts[$k] = $p[$k] } }
    foreach ($k in @($parts.Keys)) {
      $plat = $script:ImAppsPlatform[$k]
      $label = "$($s.id) ($k)"
      if (-not $plat) { $skipped.Add(@{ id = $label; reason = "unknown platform part '$k'" }); continue }
      $part = Resolve-ImPart $In $parts[$k] $label $skipped
      if ($null -eq $part) { continue }
      $short = if ($part.Contains('settings')) { @($part.settings) } else { @($part) }
      try { $settings = @($short | ForEach-Object { ConvertTo-ImSetting (ConvertFrom-ImShortSetting $_) }) }
      catch { $skipped.Add(@{ id = $label; reason = "$_" }); continue }
      $name = if ($part.policy) { "Box $($part.policy)" } elseif ($script:ImAppsNames[$s.id].$k) { $script:ImAppsNames[$s.id].$k } else { "Box Office: $($s.id) ($($plat.label))" }
      Add-ImCatalog $objects $name $plat.platforms $plat.technologies $settings '' $s.id $groups
    }
  }
  Complete-ImCatalog $objects
  return @{ objects = @($objects.Values); skipped = $skipped.ToArray() }
}

$script:Steps['policy.m365-apps'] = @{
  test     = { param($In) Test-ImInput $In }
  plan     = { param($In) Invoke-ImStepOp $In 'Get-ImAppsObjects' 'plan' 'm365-apps' }
  apply    = { param($In) Invoke-ImStepOp $In 'Get-ImAppsObjects' 'apply' 'm365-apps' }
  verify   = { param($In) Invoke-ImStepOp $In 'Get-ImAppsObjects' 'verify' 'm365-apps' }
  evidence = { param($In) Invoke-ImStepOp $In 'Get-ImAppsObjects' 'evidence' 'm365-apps' }
}
