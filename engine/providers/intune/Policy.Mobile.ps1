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
# Enrolment restriction payload key -> custom platform restriction.
$script:ImEnrolment = [ordered]@{
  iosRestriction           = @{ platform = 'ios'; name = 'Box iOS enrolment restriction' }
  androidForWorkRestriction = @{ platform = 'androidForWork'; name = 'Box Android work profile enrolment restriction' }
  androidRestriction       = @{ platform = 'android'; name = 'Box Android device administrator enrolment restriction' }
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
      # MOB-001: app protection targeting for both platforms, and personal
      # enrolment blocked through custom platform restrictions on the target
      # groups (the default restriction would reach every user at once).
      if ($p.appProtection) { $r = Resolve-ImPart $In (Remove-ImAssignments $p.appProtection) "$($s.id) (appProtection)" $skipped; if ($r) { Merge-ImProps $appBase $r } }
      foreach ($key in @($script:ImEnrolment.Keys)) {
        if ($null -eq $p.enrollmentRestriction.$key) { continue }
        $r = Resolve-ImPart $In $p.enrollmentRestriction.$key "$($s.id) ($key)" $skipped
        if ($null -eq $r) { continue }
        $e = $script:ImEnrolment[$key]
        $o = Get-ImObject $objects 'enrollmentRestriction' $e.name '#microsoft.graph.deviceEnrollmentPlatformRestrictionConfiguration'
        $o.body.platformType = $e.platform
        $o.body.platformRestriction = $r
        Add-ImUse $o $s.id $groups
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

$script:Steps['policy.intune-mobile'] = @{
  test     = { param($In) Test-ImInput $In }
  plan     = { param($In) Invoke-ImStepOp $In 'Get-ImMobileObjects' 'plan' 'intune-mobile' }
  apply    = { param($In) Invoke-ImStepOp $In 'Get-ImMobileObjects' 'apply' 'intune-mobile' }
  verify   = { param($In) Invoke-ImStepOp $In 'Get-ImMobileObjects' 'verify' 'intune-mobile' }
  evidence = { param($In) Invoke-ImStepOp $In 'Get-ImMobileObjects' 'evidence' 'intune-mobile' }
}
