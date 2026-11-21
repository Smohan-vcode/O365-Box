# MFA registration (T14-17): the registration report, whether CA01 can be
# enforced, and the drift check of the authentication methods registration
# campaign. Graph over REST (common/BoxGraph.psm1). Used by
# identity.auth-methods (report, readiness) and policy.entra (campaign).
#
# The campaign is catalogue setting ENT-050 (SC-27): policy.entra applies it
# from its mapping, on beta (enforceRegistrationAfterAllowedSnoozes, registration
# required after three snoozes, exists only there). Nothing here enforces CA01: enforcement stays
# an owner approval (SPEC §25.1); Get-BoxCA01Readiness only says whether it
# could be proposed.
#
# Privacy: the registration report never reads user principal names. Counts
# and object ids go into results and evidence; display names are only
# written to the screen, during plan, and never in audit mode (the audit
# keeps verify warnings in its pack).
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxProvider.psm1') -Force
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxGraph.psm1') -Force

$script:Beta = 'https://graph.microsoft.com/beta'
$script:ReportUri = '/reports/authenticationMethods/userRegistrationDetails?$select=id,userDisplayName,userType,isAdmin,isMfaRegistered'

# ---------------------------------------------------------------- pure

function Get-BoxCampaignDrift {
  # Differences between the wanted campaign (ENT-050's resolved payload) and
  # the one Graph returned (registrationEnforcement.authenticationMethodsRegistrationCampaign).
  param([Parameter(Mandatory)] $Want, $Have)
  if ($null -eq $Have) { return @('campaign (missing)') }
  $out = @()
  foreach ($k in 'state', 'snoozeDurationInDays', 'enforceRegistrationAfterAllowedSnoozes') {
    if (([string]$Want[$k]).ToLowerInvariant() -ne ([string]$Have.$k).ToLowerInvariant()) { $out += "$k ($($Have.$k), want $($Want[$k]))" }
  }
  $key = { param($t) ('{0}|{1}|{2}' -f $t.id, $t.targetType, $t.targetedAuthenticationMethod).ToLowerInvariant() }
  $wi = @($Want.includeTargets | ForEach-Object { & $key $_ } | Sort-Object)
  $hi = @($Have.includeTargets | Where-Object { $_ } | ForEach-Object { & $key $_ } | Sort-Object)
  if (($wi -join ',') -ne ($hi -join ',')) { $out += "includeTargets ($($hi -join ', '), want $($wi -join ', '))" }
  $we = @($Want.excludeTargets | ForEach-Object { ([string]$_.id).ToLowerInvariant() } | Sort-Object)
  $he = @($Have.excludeTargets | Where-Object { $_ } | ForEach-Object { ([string]$_.id).ToLowerInvariant() } | Sort-Object)
  if (($we -join ',') -ne ($he -join ',')) { $out += "excludeTargets ($(@($he).Count) group(s), want $(@($we).Count))" }
  return $out
}

function Get-BoxMfaRegistrationCounts {
  # Counts members and administrators who are not MFA-registered, from
  # userRegistrationDetails rows. Guests and the excluded ids (break-glass
  # accounts: gate G-02 covers them) are left out. Returns counts, the
  # unregistered object ids and, for the screen only, their display names.
  param([object[]] $Details = @(), [string[]] $ExcludeIds = @())
  $skip = @{}; foreach ($i in $ExcludeIds) { if ($i) { $skip[$i.ToLowerInvariant()] = $true } }
  $members = @($Details | Where-Object { $_ -and ([string]$_.userType) -eq 'member' -and -not $skip[([string]$_.id).ToLowerInvariant()] })
  $admins = @($members | Where-Object { $_.isAdmin -eq $true })
  $open = @($members | Where-Object { $_.isMfaRegistered -ne $true } | Sort-Object { [string]$_.userDisplayName }, { [string]$_.id })
  $openAdmins = @($open | Where-Object { $_.isAdmin -eq $true })
  return [ordered]@{
    members = $members.Count; members_unregistered = $open.Count
    admins = $admins.Count; admins_unregistered = $openAdmins.Count
    unregistered_ids = @($open | ForEach-Object { [string]$_.id })
    unregistered = @($open | ForEach-Object { [ordered]@{ id = [string]$_.id; name = [string]$_.userDisplayName; admin = ($_.isAdmin -eq $true) } })
  }
}

function Get-BoxCA01Readiness {
  # CA01 can be enforced only when every non-break-glass member (admins
  # included) is MFA-registered and gate G-02 passes. $Counts is $null when
  # the report could not be read. Never enforces anything.
  param($Counts, [bool] $G02, [string] $ReportError = '')
  $why = @()
  if ($null -eq $Counts) { $why += "the registration report is not readable$(if ($ReportError) { ": $ReportError" })" }
  else {
    if ($Counts.members -eq 0) { $why += 'the registration report lists no member accounts' }
    if ($Counts.members_unregistered -gt 0) { $why += "$($Counts.members_unregistered) of $($Counts.members) member(s) not registered for MFA ($($Counts.admins_unregistered) administrator(s))" }
  }
  if (-not $G02) { $why += 'gate G-02: a break-glass account has no FIDO2 key' }
  $ready = $why.Count -eq 0
  $msg = if ($ready) { 'CA01 can be enforced: every member is registered and G-02 passes; the owner approves the enforce stage (SPEC 25.1)' }
  else { "CA01 cannot be enforced yet: $($why -join '; ')" }
  return [ordered]@{ ready = $ready; reasons = $why; message = $msg }
}

function Format-BoxRegistrationSummary($Counts) {
  # Counts only: safe for evidence and logs.
  if ($null -eq $Counts) { return 'registration report not readable' }
  return "$($Counts.members_unregistered) of $($Counts.members) member(s) and $($Counts.admins_unregistered) of $($Counts.admins) administrator(s) not registered for MFA"
}

# ---------------------------------------------------------------- tenant

function Get-BoxMfaRegistration {
  # Reads the registration report. Returns @{ counts; error }.
  param([string[]] $ExcludeIds = @())
  try { $rows = @(Get-BoxGraphAll $script:ReportUri) }
  catch { return @{ counts = $null; error = "$_" } }
  return @{ counts = (Get-BoxMfaRegistrationCounts -Details $rows -ExcludeIds $ExcludeIds); error = '' }
}

function Write-BoxRegistrationWarnings {
  # Who still has to register. Names only on screen, during plan, never in
  # audit mode; otherwise counts and object ids.
  param($Registration, $Readiness, [switch] $Names)
  $c = $Registration.counts
  if ($null -eq $c) { Write-Warning "MFA registration report not readable: $($Registration.error)" }
  elseif ($c.members_unregistered -gt 0) {
    Write-Warning "MFA registration: $(Format-BoxRegistrationSummary $c)"
    foreach ($u in $c.unregistered) {
      $who = if ($Names -and $u.name) { $u.name } else { $u.id }
      Write-Warning "  still to register: $who$(if ($u.admin) { ' (administrator)' })"
    }
  }
  Write-Warning $Readiness.message
}

Export-ModuleMember -Function Get-BoxCampaignDrift, Get-BoxMfaRegistrationCounts, Get-BoxCA01Readiness,
  Format-BoxRegistrationSummary, Get-BoxMfaRegistration, Write-BoxRegistrationWarnings
