# Provider: identity. Entra users, groups, break-glass accounts,
# authentication methods and Conditional Access (T1). Calls Graph with the
# pipeline app's token.
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxProvider.psm1') -Force
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxGraph.psm1') -Force

$GlobalAdmin = '62e90394-69f5-4237-9190-012177145e10'
# Roles treated as privileged by CA03 and CA05.
$PrivilegedRoles = @(
  '62e90394-69f5-4237-9190-012177145e10', 'e8611ab8-c189-46e8-94e1-60213ab1f814', '194ae4cb-b126-40b2-bd5b-6091b380977d',
  '29232cdf-9323-42fd-ade2-1d097af3e4de', 'f28a1f50-f6e7-4571-818b-6a12f2af6b6c', 'fe930be7-5e62-47db-91af-98c3a49a38b1',
  'b1be1c3e-b65d-4f19-8427-f6fa0d97feb9', '9b895d92-2cd3-44c7-9d02-a6ac2d5ea5c3', '158c047a-c907-4556-b7ef-446551a6b5f7',
  'c4e39bd9-1100-46d3-8c65-fb160da0071f', 'b0f54661-2d74-4c50-afa3-1ec803f12efe', '729827e3-9c14-49f7-bb1b-9608f156bbb8',
  '69091246-20e8-4a56-aa4d-066075b2a7a8')
$PlanSkus = @{ business_premium = 'SPB'; business_standard = 'O365_BUSINESS_PREMIUM'; business_basic = 'O365_BUSINESS_ESSENTIALS' }

function Get-Domain($In) {
  $d = [string]$In.config.company.primary_domain
  if (-not $d) { $d = [string]$In.tenant.initial_domain }
  return $d
}
function Get-BreakGlassDomain($In) { return [string]$In.tenant.initial_domain }

function Get-UserByUpn([string] $Upn) {
  return Get-BoxGraphOne "/users/$([uri]::EscapeDataString($Upn))?`$select=id,userPrincipalName,usageLocation,assignedLicenses,accountEnabled,displayName"
}
function Get-GroupByName([string] $Name) {
  $g = @(Get-BoxGraphAll "/groups?`$filter=displayName eq '$(ConvertTo-BoxFilterValue $Name)'&`$select=id,displayName,securityEnabled")
  if ($g.Count -gt 1) { throw "$($g.Count) groups are named '$Name'; keep one and remove the others" }
  if ($g.Count -eq 1) { return $g[0] }
  return $null
}
function Test-HasRole([string] $PrincipalId, [string] $RoleId) {
  $a = @(Get-BoxGraphAll "/roleManagement/directory/roleAssignments?`$filter=principalId eq '$PrincipalId' and roleDefinitionId eq '$RoleId'")
  return $a.Count -gt 0
}
function Get-MemberIds([string] $GroupId) {
  return @(Get-BoxGraphAll "/groups/$GroupId/members?`$select=id" | ForEach-Object { $_.id })
}
function Add-Member([string] $GroupId, [string] $ObjectId) {
  Invoke-BoxGraph -Method POST -Uri "/groups/$GroupId/members/`$ref" -Body @{ '@odata.id' = "https://graph.microsoft.com/v1.0/directoryObjects/$ObjectId" } | Out-Null
}
function Get-BreakGlass($In) {
  @($In.config.users | Where-Object { $_.break_glass })
}
function Result-Changes($c) { return @{ changes = @($c | Where-Object { $null -ne $_ }) } }

# ---------- identity.break-glass (T1-01) ----------
function Get-BreakGlassChanges($In) {
  $changes = @()
  $grp = Get-GroupByName 'grp-break-glass'
  if (-not $grp) { $changes += New-BoxChange create 'group grp-break-glass' 'run box bootstrap' }
  $members = if ($grp) { Get-MemberIds $grp.id } else { @() }
  foreach ($u in Get-BreakGlass $In) {
    $upn = "$($u.primary)@$(Get-BreakGlassDomain $In)"
    $usr = Get-UserByUpn $upn
    if (-not $usr) { $changes += New-BoxChange create "break-glass $upn" 'run box bootstrap'; continue }
    if (-not (Test-HasRole $usr.id $GlobalAdmin)) { $changes += New-BoxChange update "role Global Administrator for $upn" 'run box bootstrap' }
    if (@($usr.assignedLicenses).Count -gt 0) { $changes += New-BoxChange update "licence on $upn" 'break-glass accounts hold no licence' }
    if ($grp -and $usr.id -notin $members) { $changes += New-BoxChange update "membership grp-break-glass" $upn }
  }
  return $changes
}

# ---------- identity.users (T1-02) ----------
function Get-SkuMap {
  $m = @{}
  foreach ($s in @(Get-BoxGraphAll '/subscribedSkus')) {
    $m[$s.skuPartNumber] = @{ id = $s.skuId; free = [int]$s.prepaidUnits.enabled - [int]$s.consumedUnits }
  }
  return $m
}
function Get-UserChanges($In) {
  $changes = @()
  $skus = Get-SkuMap
  $need = @{}
  foreach ($u in @($In.config.users | Where-Object { -not $_.break_glass })) {
    $upn = "$($u.primary)@$(Get-Domain $In)"
    $usr = Get-UserByUpn $upn
    if (-not $usr) {
      $changes += New-BoxChange create "user $upn" "$($u.given_name) $($u.family_name)"
      $has = @()
    } else {
      if ($usr.usageLocation -ne $In.config.tenant.usage_location -and $In.config.tenant.usage_location) {
        $changes += New-BoxChange update "usage location $upn" ([string]$In.config.tenant.usage_location)
      }
      $has = @($usr.assignedLicenses | ForEach-Object { $_.skuId })
    }
    if ($u.plan -and $u.plan -ne 'none') {
      $part = $PlanSkus[[string]$u.plan]
      if (-not $skus.ContainsKey($part)) { throw "plan $($u.plan) ($part) is not subscribed in this tenant" }
      if ($skus[$part].id -notin $has) {
        $changes += New-BoxChange update "licence $part for $upn"
        $need[$part] = 1 + [int]$need[$part]
      }
    }
  }
  foreach ($p in $need.Keys) {
    if ($need[$p] -gt $skus[$p].free) { throw "plan $p needs $($need[$p]) seat(s); $($skus[$p].free) free (gate G-01)" }
  }
  return $changes
}
function Invoke-Users($In) {
  $done = @()
  $skus = Get-SkuMap
  foreach ($u in @($In.config.users | Where-Object { -not $_.break_glass })) {
    $upn = "$($u.primary)@$(Get-Domain $In)"
    $usr = Get-UserByUpn $upn
    if (-not $usr) {
      $pw = [Convert]::ToBase64String((1..24 | ForEach-Object { Get-Random -Maximum 256 }) -as [byte[]])
      $usr = Invoke-BoxGraph -Method POST -Uri '/users' -Body @{
        accountEnabled = $true; displayName = "$($u.given_name) $($u.family_name)"; givenName = $u.given_name; surname = $u.family_name
        mailNickname = $u.primary; userPrincipalName = $upn; usageLocation = $In.config.tenant.usage_location
        passwordProfile = @{ password = "Bx!$pw"; forceChangePasswordNextSignIn = $true } }
      $done += New-BoxChange create "user $upn" 'first sign-in pass issued at delivery (SPEC §18.9)'
      Start-Sleep -Seconds 5
      $usr = Get-UserByUpn $upn
    } elseif ($In.config.tenant.usage_location -and $usr.usageLocation -ne $In.config.tenant.usage_location) {
      Invoke-BoxGraph -Method PATCH -Uri "/users/$($usr.id)" -Body @{ usageLocation = $In.config.tenant.usage_location } | Out-Null
      $done += New-BoxChange update "usage location $upn"
    }
    if ($u.plan -and $u.plan -ne 'none') {
      $part = $PlanSkus[[string]$u.plan]
      $has = @($usr.assignedLicenses | ForEach-Object { $_.skuId })
      if ($skus[$part].id -notin $has) {
        Invoke-BoxGraph -Method POST -Uri "/users/$($usr.id)/assignLicense" -Body @{ addLicenses = @(@{ skuId = $skus[$part].id; disabledPlans = @() }); removeLicenses = @() } | Out-Null
        $done += New-BoxChange update "licence $part for $upn"
      }
    }
  }
  return $done
}

# ---------- identity.groups (T1-03) ----------
function Get-DesiredGroups($In) {
  $groups = [ordered]@{}
  foreach ($g in @($In.config.groups | Where-Object { $_.kind -eq 'access' })) { $groups[$g.name] = @{ desc = [string]$g.title; members = @() } }
  foreach ($r in @($In.defaults.roles.roles)) { $groups["role-$($r.name)"] = @{ desc = "Role: $($r.name)"; members = @() } }
  foreach ($u in @($In.config.users | Where-Object { -not $_.break_glass })) {
    $upn = "$($u.primary)@$(Get-Domain $In)"
    foreach ($g in @($u.access_groups)) { if ($groups.Contains($g)) { $groups[$g].members += $upn } }
    foreach ($r in @($u.roles)) { if ($groups.Contains("role-$r")) { $groups["role-$r"].members += $upn } }
  }
  return $groups
}
function Get-GroupChanges($In, [switch] $Apply) {
  $changes = @()
  $want = Get-DesiredGroups $In
  foreach ($name in $want.Keys) {
    $g = Get-GroupByName $name
    $new = $false
    if (-not $g) {
      $changes += New-BoxChange create "group $name"
      if ($Apply) {
        $g = Invoke-BoxGraph -Method POST -Uri '/groups' -Body @{ displayName = $name; description = $want[$name].desc; mailEnabled = $false; mailNickname = $name; securityEnabled = $true }
        $new = $true
      }
    }
    # A group just created has no members and is not readable for a few seconds.
    $have = if ($g -and -not $new) { Get-MemberIds $g.id } else { @() }
    foreach ($upn in $want[$name].members) {
      $usr = Get-UserByUpn $upn
      if (-not $usr) { $changes += New-BoxChange update "member $upn of $name" 'user not created yet'; continue }
      if ($usr.id -notin $have) {
        $changes += New-BoxChange update "member $upn of $name"
        if ($Apply -and $g) { Add-Member $g.id $usr.id }
      }
    }
  }
  return $changes
}

# ---------- identity.auth-methods (T1-05) ----------
$MethodIds = @{ fido2 = @('Fido2', 'fido2AuthenticationMethodConfiguration'); microsoft_authenticator = @('MicrosoftAuthenticator', 'microsoftAuthenticatorAuthenticationMethodConfiguration')
  temporary_access_pass = @('TemporaryAccessPass', 'temporaryAccessPassAuthenticationMethodConfiguration'); sms = @('Sms', 'smsAuthenticationMethodConfiguration')
  voice = @('Voice', 'voiceAuthenticationMethodConfiguration'); email_otp = @('Email', 'emailAuthenticationMethodConfiguration') }
function Get-AuthMethodChanges($In, [switch] $Apply) {
  $changes = @()
  $want = $In.defaults.baseline_identity.authentication_methods
  foreach ($p in $want.PSObject.Properties) {
    if (-not $MethodIds.ContainsKey($p.Name)) { continue }
    $id, $type = $MethodIds[$p.Name]
    $cur = Invoke-BoxGraph -Uri "/policies/authenticationMethodsPolicy/authenticationMethodConfigurations/$id"
    $state = [string]$p.Value.state
    if ($cur.state -ne $state) {
      $changes += New-BoxChange update "authentication method $($p.Name)" "$($cur.state) -> $state"
      if ($Apply) {
        Invoke-BoxGraph -Method PATCH -Uri "/policies/authenticationMethodsPolicy/authenticationMethodConfigurations/$id" -Body @{ '@odata.type' = "#microsoft.graph.$type"; state = $state } | Out-Null
      }
    }
  }
  return $changes
}

# ---------- identity.conditional-access (T1-05) ----------
function Test-BreakGlassReady($In) {
  # SPEC §20.2: blocking policies only when every break-glass account has FIDO2.
  foreach ($u in Get-BreakGlass $In) {
    $usr = Get-UserByUpn "$($u.primary)@$(Get-BreakGlassDomain $In)"
    if (-not $usr) { return $false }
    if (@(Get-BoxGraphAll "/users/$($usr.id)/authentication/fido2Methods").Count -eq 0) { return $false }
  }
  return $true
}
function Get-PolicyBody($p, [string] $ExcludeGroup, [string] $State) {
  $users = @{ includeUsers = @(); excludeGroups = @($ExcludeGroup) }
  if ($p.users -is [string] -and $p.users -eq 'all') { $users.includeUsers = @('All') }
  elseif ($p.users.roles -eq 'privileged') { $users.includeRoles = $PrivilegedRoles }
  else { $users.includeUsers = @('All') }   # plan-scoped policies target everyone; device compliance applies per platform
  $cond = @{ users = $users; applications = @{ includeApplications = @('All') }; clientAppTypes = @('all') }
  if ($p.client_apps) { $cond.clientAppTypes = @($p.client_apps) }
  if ($p.platforms) { $cond.platforms = @{ includePlatforms = @($p.platforms | ForEach-Object { $_ }) } }
  $body = @{ displayName = $p.name; state = $State; conditions = $cond }
  $grant = @($p.grant)
  if ($grant -contains 'authentication_strength_phishing_resistant') {
    $body.grantControls = @{ operator = 'OR'; authenticationStrength = @{ id = '00000000-0000-0000-0000-000000000004' } }
  } elseif ($grant.Count -gt 0 -and $grant[0]) {
    $map = @{ mfa = 'mfa'; block = 'block'; compliant_device = 'compliantDevice' }
    $body.grantControls = @{ operator = 'OR'; builtInControls = @($grant | ForEach-Object { $map[[string]$_] }) }
  }
  if ($p.session.sign_in_frequency_hours) {
    $body.sessionControls = @{ signInFrequency = @{ value = [int]$p.session.sign_in_frequency_hours; type = 'hours'; isEnabled = $true } }
  }
  return $body
}
function Get-CAChanges($In, [switch] $Apply) {
  $changes = @()
  $grp = Get-GroupByName ([string]$In.defaults.baseline_identity.conditional_access.exclude_group)
  if (-not $grp) { throw 'the break-glass exclusion group does not exist' }
  $ready = Test-BreakGlassReady $In
  $have = @{}
  foreach ($x in @(Get-BoxGraphAll '/identity/conditionalAccess/policies')) { $have[$x.displayName] = $x }
  foreach ($p in @($In.defaults.baseline_identity.conditional_access.policies)) {
    $state = 'enabled'
    if ($p.start_state -eq 'report_only' -or -not $ready) { $state = 'enabledForReportingButNotEnforced' }
    $body = Get-PolicyBody $p $grp.id $state
    $why = if (-not $ready) { 'report-only: break-glass accounts have no FIDO2 key (gate G-02)' } else { '' }
    if (-not $have.ContainsKey($p.name)) {
      $changes += New-BoxChange create "policy $($p.name)" "$state $why".Trim()
      if ($Apply) { Invoke-BoxGraph -Method POST -Uri '/identity/conditionalAccess/policies' -Body $body | Out-Null }
      continue
    }
    $cur = $have[$p.name]
    $excluded = @($cur.conditions.users.excludeGroups) -contains $grp.id
    if ($cur.state -ne $state -or -not $excluded) {
      $changes += New-BoxChange update "policy $($p.name)" "state $($cur.state) -> $state; break-glass excluded=$excluded"
      if ($Apply) { Invoke-BoxGraph -Method PATCH -Uri "/identity/conditionalAccess/policies/$($cur.id)" -Body $body | Out-Null }
    }
  }
  return $changes
}

# ---------- identity.security-defaults (fallback) ----------
function Get-SDChanges([switch] $Apply) {
  $cur = Invoke-BoxGraph -Uri '/policies/identitySecurityDefaultsEnforcementPolicy'
  if ($cur.isEnabled) { return @() }
  if ($Apply) { Invoke-BoxGraph -Method PATCH -Uri '/policies/identitySecurityDefaultsEnforcementPolicy' -Body @{ isEnabled = $true } | Out-Null }
  return @(New-BoxChange update 'security defaults' 'off -> on')
}

function New-Evidence([string] $Kind, [string] $Summary) { return @{ items = @(@{ kind = $Kind; ref = "tenant:$Kind"; summary = $Summary }) } }
$ok = { param($In) @{ ok = $true } }

$script:Steps = [ordered]@{
  'identity.break-glass' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-BreakGlassChanges $In) }
    apply = { param($In)
      $c = @(Get-BreakGlassChanges $In | Where-Object { $_ })
      $fixable = @($c | Where-Object { $_.target -eq 'membership grp-break-glass' })
      if ($c.Count -ne $fixable.Count) { throw 'break-glass accounts are missing or incomplete: run box bootstrap' }
      $grp = Get-GroupByName 'grp-break-glass'
      foreach ($f in $fixable) { $usr = Get-UserByUpn $f.detail; Add-Member $grp.id $usr.id }
      Result-Changes $fixable }
    verify = { param($In) $c = @(Get-BreakGlassChanges $In | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'break-glass' "$(@(Get-BreakGlass $In).Count) account(s), Global Administrator, in grp-break-glass" }
  }
  'identity.gate-g02' = @{
    test = $ok; plan = { param($In) Result-Changes @() }; apply = { param($In) Result-Changes @() }
    verify = { param($In) @{ ok = (Test-BreakGlassReady $In) } }
    evidence = { param($In) New-Evidence 'fido2' 'FIDO2 methods per break-glass account' }
  }
  'identity.users' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-UserChanges $In) }
    apply = { param($In) Result-Changes (Invoke-Users $In) }
    verify = { param($In) $c = @(Get-UserChanges $In | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'users' "$(@($In.config.users | Where-Object { -not $_.break_glass }).Count) user(s) with usage location and plan" }
  }
  'identity.groups' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-GroupChanges $In) }
    apply = { param($In) Result-Changes (Get-GroupChanges $In -Apply) }
    verify = { param($In) $c = @(Get-GroupChanges $In | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'groups' "$((Get-DesiredGroups $In).Count) group(s) with members" }
  }
  'identity.auth-methods' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-AuthMethodChanges $In) }
    apply = { param($In) Result-Changes (Get-AuthMethodChanges $In -Apply) }
    verify = { param($In) $c = @(Get-AuthMethodChanges $In | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'authentication-methods' 'states equal baseline/identity.yaml' }
  }
  'identity.conditional-access' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-CAChanges $In) }
    apply = { param($In) Result-Changes (Get-CAChanges $In -Apply) }
    verify = { param($In) $c = @(Get-CAChanges $In | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'conditional-access' 'policies exclude grp-break-glass' }
  }
  'identity.security-defaults' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-SDChanges) }
    apply = { param($In) Result-Changes (Get-SDChanges -Apply) }
    verify = { param($In) $c = @(Get-SDChanges | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'security-defaults' 'on' }
  }
}

function Invoke-BoxStep {
  param([string] $Step, [string] $Operation, $InputObject)
  Invoke-BoxDispatch -Steps $script:Steps -Step $Step -Operation $Operation -InputObject $InputObject
}
function Get-BoxStepIds { @($script:Steps.Keys) }
function Get-BoxStepProblems { Test-BoxStepTable -Steps $script:Steps }
Export-ModuleMember -Function Invoke-BoxStep, Get-BoxStepIds, Get-BoxStepProblems
