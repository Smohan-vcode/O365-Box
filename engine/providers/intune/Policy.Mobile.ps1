# policy.intune-mobile (T14-07): phone and tablet compliance, app protection
# and enrolment restrictions from the resolved intune-mobile settings.
# Helpers: IntunePolicy.Mobile.ps1.

# Payload part @odata.type -> the object that holds it.
$script:ImMobileRoutes = @{
  '#microsoft.graph.iosCompliancePolicy'                         = @{ kind = 'compliance'; name = 'Box iOS compliance' }
  '#microsoft.graph.androidWorkProfileCompliancePolicy'          = @{ kind = 'compliance'; name = 'Box Android work profile compliance' }
  '#microsoft.graph.androidDeviceOwnerCompliancePolicy'          = @{ kind = 'compliance'; name = 'Box Android fully managed compliance' }
  '#microsoft.graph.androidDeviceOwnerGeneralDeviceConfiguration' = @{ kind = 'deviceConfiguration'; name = $null }
  '#microsoft.graph.iosManagedAppProtection'                     = @{ kind = 'iosAppProtection'; name = 'Box iOS app protection' }
  '#microsoft.graph.androidManagedAppProtection'                 = @{ kind = 'androidAppProtection'; name = 'Box Android app protection' }
}
# MOB-001 enrolment restrictions are a gate (G-12), not an object the step
# makes: Intune refuses app-only writes to deviceEnrollmentConfigurations
# (create of a per-platform restriction and PATCH of the default) with
# "Tenant is not Global Admin or Intune Service Admin. Operation is
# restricted." even with the Intune Administrator role on the pipeline app
# (test tenant, 2026-11-21, beta and v1.0). The step reads them only.
# Payload key -> platformType of a per-platform restriction, and what the
# owner reads when it is not met.
$script:ImEnrolment = [ordered]@{
  iosRestriction            = @{ platform = 'ios'; label = 'iOS personal enrolment not blocked' }
  androidForWorkRestriction = @{ platform = 'androidForWork'; label = 'Android work profile personal enrolment not blocked' }
  androidRestriction        = @{ platform = 'android'; label = 'Android device administrator not blocked' }
}
$script:ImEnrolmentGate = 'G-12'

function Get-ImEnrolmentWant($In) {
  # The MOB-001 restriction part of the resolved settings; for the gate
  # check, which has no policy input, the mapping's payload.
  foreach ($s in @($In.policy.settings | Where-Object { $_ })) {
    $p = ConvertTo-ImPlain $s.payload
    if ($null -ne $p.enrollmentRestriction) { return $p.enrollmentRestriction }
  }
  $file = Join-Path $PSScriptRoot '..' '..' '..' 'expected-state' 'setting-ids' 'intune-mobile.json'
  return (Get-Content -Raw $file | ConvertFrom-Json -AsHashtable)['MOB-001'].payload.enrollmentRestriction
}

function Test-ImEnrolmentRestrictions($In, $Want) {
  # Read-only check of gate G-12: for each platform the setting names, the
  # default restriction or a per-platform restriction assigned to a group
  # has the desired values. Returns @{ ok; missing = what is not met }.
  if ($null -eq $Want) { $Want = Get-ImEnrolmentWant $In }
  if ($null -eq $Want) { return @{ ok = $true; missing = @() } }
  $all = @(Get-BoxGraphAll "$script:ImBeta/deviceManagement/deviceEnrollmentConfigurations")
  $default = @($all | Where-Object { [string]$_.'@odata.type' -like '*deviceEnrollmentPlatformRestrictionsConfiguration' -and ([string]$_.id).EndsWith('_DefaultPlatformRestrictions') })
  $missing = [System.Collections.Generic.List[string]]::new()
  foreach ($key in @($script:ImEnrolment.Keys)) {
    $w = Get-ImProp $Want $key
    if ($null -eq $w) { continue }
    $w = ConvertTo-ImPlain $w
    $ok = $default.Count -eq 1 -and @(Compare-ImProps $w (Get-ImProp $default[0] $key)).Count -eq 0
    if (-not $ok) {
      foreach ($r in @($all | Where-Object { [string]$_.'@odata.type' -like '*deviceEnrollmentPlatformRestrictionConfiguration' -and $_.platformType -eq $script:ImEnrolment[$key].platform })) {
        if (@(Compare-ImProps $w $r.platformRestriction).Count) { continue }
        if (@(Get-BoxGraphAll "$script:ImBeta/deviceManagement/deviceEnrollmentConfigurations/$($r.id)/assignments").Count) { $ok = $true; break }
      }
    }
    if (-not $ok) { $missing.Add($script:ImEnrolment[$key].label) }
  }
  return @{ ok = ($missing.Count -eq 0); missing = $missing.ToArray() }
}

function Get-ImMobileObjects($In) {
  $objects = [ordered]@{}
  $skipped = [System.Collections.Generic.List[object]]::new()
  $appBase = [ordered]@{}
  $app001 = $null
  foreach ($s in Get-ImSettings $In) {
    $p = ConvertTo-ImPlain $s.payload
    $groups = Get-ImPayloadGroups $p
    if ($null -ne $p.appProtection -or $null -ne $p.enrollmentRestriction) {
      # MOB-001: app protection targeting for both platforms. Personal
      # enrolment blocked is gate G-12 (the owner sets the restrictions in
      # the Intune admin centre); the step reads them and reports.
      if ($p.appProtection) { $r = Resolve-ImPart $In (Remove-ImAssignments $p.appProtection) "$($s.id) (appProtection)" $skipped; if ($r) { Merge-ImProps $appBase $r } }
      if ($null -ne $p.enrollmentRestriction) {
        $g = Test-ImEnrolmentRestrictions $In $p.enrollmentRestriction
        $state = if ($g.ok) { 'gate passing' } else { "gate not passed yet ($($g.missing -join '; ')); see box gate $script:ImEnrolmentGate" }
        $skipped.Add(@{ id = "$($s.id) (enrollmentRestriction)"; reason = "gate $script:ImEnrolmentGate (Intune refuses app-only enrolment restriction writes): $state" })
      }
      $app001 = $s.id
      continue
    }
    foreach ($key in @($p.Keys)) {
      $part = Resolve-ImPart $In $p[$key] "$($s.id) ($key)" $skipped
      if ($null -eq $part) { continue }
      $type = [string]$part['@odata.type']
      if ($key -eq 'androidPersonalWorkProfile' -and -not $type) { $type = '#microsoft.graph.androidWorkProfileCompliancePolicy' }
      $route = $script:ImMobileRoutes[$type]
      if (-not $route) { $skipped.Add(@{ id = "$($s.id) ($key)"; reason = "no object for payload type '$type'" }); continue }
      $name = if ($route.name) { $route.name } else { [string]$part.displayName }
      $o = Get-ImObject $objects $route.kind $name $type
      $part.Remove('@odata.type'); $part.Remove('displayName')
      Merge-ImProps $o.body $part
      Add-ImUse $o $s.id $groups
    }
  }
  # MOB-001 app protection targeting applies to both app protection policies.
  foreach ($o in @($objects.Values | Where-Object { $_.kind -like '*AppProtection' })) {
    Merge-ImProps $o.body $appBase
    if ($app001) { Add-ImUse $o $app001 @() }
  }
  return @{ objects = @($objects.Values); skipped = $skipped.ToArray() }
}

$script:Steps['intune.gate-g12'] = @{
  test     = { param($In) @{ ok = $true } }
  plan     = { param($In) @{ changes = @() } }
  apply    = { param($In) @{ changes = @() } }
  verify   = { param($In)
    $g = Test-ImEnrolmentRestrictions $In $null
    @{ ok = $g.ok; drift = @($g.missing | ForEach-Object { New-BoxChange update 'enrolment restriction (MOB-001)' $_ }) } }
  evidence = { param($In)
    $g = Test-ImEnrolmentRestrictions $In $null
    @{ items = @(@{ kind = 'enrolment-restrictions'; ref = 'setting:MOB-001'; summary = $(if ($g.ok) { 'personal enrolment blocked (iOS, Android work profile); Android device administrator blocked' } else { "not met: $($g.missing -join ', ')" }) }) } }
}

$script:Steps['policy.intune-mobile'] = @{
  test     = { param($In) Test-ImInput $In }
  plan     = { param($In) Invoke-ImStepOp $In 'Get-ImMobileObjects' 'plan' 'intune-mobile' }
  apply    = { param($In) Invoke-ImStepOp $In 'Get-ImMobileObjects' 'apply' 'intune-mobile' }
  verify   = { param($In) Invoke-ImStepOp $In 'Get-ImMobileObjects' 'verify' 'intune-mobile' }
  evidence = { param($In) Invoke-ImStepOp $In 'Get-ImMobileObjects' 'evidence' 'intune-mobile' }
}
