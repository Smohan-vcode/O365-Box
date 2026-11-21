# Provider: identity. Entra users, groups, break-glass accounts,
# authentication methods and Conditional Access (T1). Calls Graph with the
# pipeline app's token.
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxProvider.psm1') -Force
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxGraph.psm1') -Force
Import-Module (Join-Path $PSScriptRoot 'BoxIdentityPolicy.psm1') -Force
Import-Module (Join-Path $PSScriptRoot 'BoxIdentityMfa.psm1') -Force

$GlobalAdmin = '62e90394-69f5-4237-9190-012177145e10'
# Roles treated as privileged by CA03 and CA05.
$PrivilegedRoles = @(
  '62e90394-69f5-4237-9190-012177145e10', 'e8611ab8-c189-46e8-94e1-60213ab1f814', '194ae4cb-b126-40b2-bd5b-6091b380977d',
  '29232cdf-9323-42fd-ade2-1d097af3e4de', 'f28a1f50-f6e7-4571-818b-6a12f2af6b6c', 'fe930be7-5e62-47db-91af-98c3a49a38b1',
  'b1be1c3e-b65d-4f19-8427-f6fa0d97feb9', '9b895d92-2cd3-44c7-9d02-a6ac2d5ea5c3', '158c047a-c907-4556-b7ef-446551a6b5f7',
  'c4e39bd9-1100-46d3-8c65-fb160da0071f', 'b0f54661-2d74-4c50-afa3-1ec803f12efe', '729827e3-9c14-49f7-bb1b-9608f156bbb8',
  '69091246-20e8-4a56-aa4d-066075b2a7a8')
Set-BoxPolicyRoles $PrivilegedRoles
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
  $claimed = (Get-BoxEntraClaims).auth_methods   # owned by policy.entra (T14-04)
  foreach ($p in $want.PSObject.Properties) {
    if (-not $MethodIds.ContainsKey($p.Name) -or $p.Name -in $claimed) { continue }
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
  # Registration campaign (T14-17): not a catalogue setting yet, so this
  # step owns it (baseline registration_campaign; docs/spec-changes.md SC-27).
  $rc = $In.defaults.baseline_identity.registration_campaign
  if ($rc) {
    $bg = Get-GroupByName ([string]$rc.exclude_group)
    if (-not $bg) { throw "the registration campaign exclusion group $($rc.exclude_group) does not exist (run box bootstrap)" }
    $changes += @(Get-BoxCampaignChanges -Baseline $rc -ExcludeGroupId $bg.id -Apply:$Apply)
  }
  return $changes
}

# MFA registration and CA01 readiness (T14-17). The home screen calls this
# (and plan shows it) to propose enforcing CA01; it never enforces.
function Get-MfaState($In) {
  $name = [string]$In.defaults.baseline_identity.registration_campaign.exclude_group
  if (-not $name) { $name = 'grp-break-glass' }
  $bg = Get-GroupByName $name
  $reg = Get-BoxMfaRegistration -ExcludeIds @(if ($bg) { Get-MemberIds $bg.id })
  $g02 = [bool](& $script:Steps['identity.gate-g02'].verify $In).ok
  return @{ registration = $reg; readiness = (Get-BoxCA01Readiness -Counts $reg.counts -G02 $g02 -ReportError $reg.error) }
}
function Get-BoxMfaEnforcementReadiness($In, $State) {
  # Counts and object ids only (no names): safe to keep.
  $s = if ($State) { $State } else { Get-MfaState $In }
  $c = $s.registration.counts
  $out = [ordered]@{ ready = $s.readiness.ready; message = $s.readiness.message; reasons = @($s.readiness.reasons) }
  if ($c) { foreach ($k in 'members', 'members_unregistered', 'admins', 'admins_unregistered', 'unregistered_ids') { $out[$k] = $c[$k] } }
  return $out
}
function Get-AuthMethodVerify($In) {
  $c = @(Get-AuthMethodChanges $In | Where-Object { $_ })
  $s = Get-MfaState $In
  # People still to register are not drift this step can fix: warnings
  # (counts and object ids; the audit keeps verify warnings) and the report.
  Write-BoxRegistrationWarnings $s.registration $s.readiness
  $r = Get-BoxMfaEnforcementReadiness $In $s
  return @{ ok = ($c.Count -eq 0); drift = $c; report = @((Format-BoxRegistrationSummary $s.registration.counts), $s.readiness.message); mfa = $r }
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
  # Policies an entra mapping names are owned by policy.entra (T14-04): this
  # step leaves them alone so the two never write the same policy.
  $claimed = (Get-BoxEntraClaims).ca
  foreach ($p in @($In.defaults.baseline_identity.conditional_access.policies | Where-Object { $_.name -notin $claimed })) {
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

# ---------- identity.directory-settings (T1-05, SC-18) ----------
$RestrictedGuestRole = '2af84b1e-32c8-42b7-82bc-daa82404023b'   # guests see only their own directory objects
$GuestRoleIds = @{ restricted = $RestrictedGuestRole; limited = '10dae51f-b6af-4016-8d66-8c2a99b929b3'; same_as_members = 'a0b1b346-4d3e-4e8b-98f8-753987be4970' }
$InviteFrom = @{ admins_only = 'adminsAndGuestInviters'; members = 'adminsGuestInvitersAndAllMembers'; everyone = 'everyone'; none = 'none' }
function Get-DirectoryChanges($In, [switch] $Apply) {
  $want = ConvertTo-BoxPlain $In.defaults.baseline_identity.directory
  # Properties an entra mapping names are owned by policy.entra (T14-04).
  foreach ($k in (Get-BoxEntraClaims).directory) { if ($want -and $want.Contains($k)) { $want.Remove($k) } }
  $want = [pscustomobject]$want
  $changes = @()
  $ap = Invoke-BoxGraph -Uri '/policies/authorizationPolicy'
  $patch = @{}
  $perm = @{}
  if ($null -ne $want.users_can_register_apps -and [bool]$ap.defaultUserRolePermissions.allowedToCreateApps -ne [bool]$want.users_can_register_apps) {
    $changes += New-BoxChange update 'users can register apps' "$($ap.defaultUserRolePermissions.allowedToCreateApps) -> $($want.users_can_register_apps)"
    $perm.allowedToCreateApps = [bool]$want.users_can_register_apps
  }
  if ($want.users_can_consent_to_apps -eq $false) {
    $grants = @($ap.defaultUserRolePermissions.permissionGrantPoliciesAssigned | Where-Object { $_ })
    if ($grants.Count -gt 0) {
      $changes += New-BoxChange update 'users can consent to apps' "$($grants -join ', ') -> none"
      $perm.permissionGrantPoliciesAssigned = @()
    }
  }
  if ($perm.Count) { $patch.defaultUserRolePermissions = $perm }
  if ($want.guest_invitations -and $ap.allowInvitesFrom -ne $InviteFrom[[string]$want.guest_invitations]) {
    $changes += New-BoxChange update 'who can invite guests' "$($ap.allowInvitesFrom) -> $($InviteFrom[[string]$want.guest_invitations])"
    $patch.allowInvitesFrom = $InviteFrom[[string]$want.guest_invitations]
  }
  if ($want.guest_access -and $ap.guestUserRoleId -ne $GuestRoleIds[[string]$want.guest_access]) {
    $changes += New-BoxChange update 'guest access' "$($want.guest_access)"
    $patch.guestUserRoleId = $GuestRoleIds[[string]$want.guest_access]
  }
  if ($Apply -and $patch.Count) { Invoke-BoxGraph -Method PATCH -Uri '/policies/authorizationPolicy' -Body $patch | Out-Null }

  if ($want.admin_consent_workflow -eq 'enabled') {
    $acr = Invoke-BoxGraph -Uri '/policies/adminConsentRequestPolicy'
    $owners = Get-GroupByName 'role-owner'
    if (-not $owners) { throw 'role-owner group not found (run identity.groups)' }
    $query = "/groups/$($owners.id)/transitiveMembers/microsoft.graph.user"
    # Graph stores the query with a "/v1.0" prefix.
    $hasReviewer = @($acr.reviewers | Where-Object { ($_.query -replace '^/v1\.0', '') -eq $query }).Count -gt 0
    if (-not $acr.isEnabled -or -not $hasReviewer) {
      $changes += New-BoxChange update 'admin consent workflow' 'enabled; reviewers: role-owner'
      if ($Apply) {
        Invoke-BoxGraph -Method PUT -Uri '/policies/adminConsentRequestPolicy' -Body @{
          isEnabled = $true; notifyReviewers = $true; remindersEnabled = $true; requestDurationInDays = 30
          reviewers = @(@{ query = $query; queryType = 'MicrosoftGraph'; queryRoot = $null }) } | Out-Null   # Graph rejects reviewers without queryRoot
      }
    }
  }
  return $changes
}

# ---------- policy.capabilities (T14-02) ----------
# Pure function, tested by Pester: users whose own licences lack a service
# plan that provides a needed, user-assigned capability.
function Get-NotCoveredUsers {
  param([hashtable] $UserPlans, [hashtable] $Needed)
  # UserPlans: upn -> array of enabled service plan names
  # Needed:    capability -> array of service plan names that provide it (user-assigned only)
  $out = @()
  foreach ($upn in ($UserPlans.Keys | Sort-Object)) {
    $missing = @()
    foreach ($cap in ($Needed.Keys | Sort-Object)) {
      $has = @($UserPlans[$upn] | Where-Object { $_ -in $Needed[$cap] })
      if ($has.Count -eq 0) { $missing += $cap }
    }
    if ($missing.Count) { $out += [pscustomobject]@{ user = $upn; missing = $missing } }
  }
  return $out
}

function Get-CapabilityReport($In) {
  if ($In.policy_error) { throw "policy resolution failed: $($In.policy_error)" }
  $pol = $In.policy
  $skus = @(Get-BoxGraphAll '/subscribedSkus')
  $tenantPlans = @{}       # name -> @{ id; status; appliesTo }
  foreach ($s in $skus) { foreach ($p in @($s.servicePlans)) { $tenantPlans[[string]$p.servicePlanName] = @{ id = [string]$p.servicePlanId; status = [string]$p.provisioningStatus; appliesTo = [string]$p.appliesTo } } }
  $missingCaps = @()
  $userCaps = @{}
  foreach ($cp in $pol.capabilities_needed.PSObject.Properties) {
    $cap = $cp.Name
    if ($cap -eq 'free') { continue }
    $plans = @($pol.service_plans.$cap)
    if ($plans.Count -eq 0) { $missingCaps += "$cap (no service plan known)"; continue }
    $present = @($plans | Where-Object { $tenantPlans.ContainsKey($_) -and $tenantPlans[$_].status -in 'Success', 'PendingActivation', 'PendingProvisioning' })
    if ($present.Count -eq 0) { $missingCaps += "$cap needed by $(@($cp.Value).Count) setting(s): $(@($cp.Value)[0..2] -join ', ')"; continue }
    if (@($present | Where-Object { $tenantPlans[$_].appliesTo -eq 'User' }).Count) { $userCaps[$cap] = @($present | Where-Object { $tenantPlans[$_].appliesTo -eq 'User' }) }
  }
  # Users in the configuration (break-glass accounts hold no licence by design).
  $byId = @{}; foreach ($n in $tenantPlans.Keys) { $byId[$tenantPlans[$n].id] = $n }
  $userPlans = @{}
  foreach ($u in @($In.config.users | Where-Object { -not $_.break_glass })) {
    $d = [string]$In.config.company.primary_domain; if (-not $d) { $d = [string]$In.tenant.initial_domain }
    $upn = "$($u.primary)@$d"
    $usr = Get-BoxGraphOne "/users/$([uri]::EscapeDataString($upn))?`$select=assignedPlans"
    if (-not $usr) { continue }
    $userPlans[$upn] = @($usr.assignedPlans | Where-Object { $_.capabilityStatus -eq 'Enabled' -and $byId.ContainsKey([string]$_.servicePlanId) } | ForEach-Object { $byId[[string]$_.servicePlanId] })
  }
  $notCovered = @(Get-NotCoveredUsers -UserPlans $userPlans -Needed $userCaps)
  # One read-only probe per capability that Graph can reach with the app's permissions.
  $probes = [ordered]@{ entra_p1 = '/identity/conditionalAccess/policies?$top=1'; intune_p1 = '/deviceManagement/deviceCompliancePolicies?$top=1'; sharepoint = '/sites/root?$select=id' }
  $probe = [ordered]@{}
  foreach ($k in $probes.Keys) {
    if (-not $pol.capabilities_needed.PSObject.Properties[$k]) { continue }
    try { Invoke-BoxGraph -Uri $probes[$k] | Out-Null; $probe[$k] = 'usable' } catch { $probe[$k] = "not usable: $_" }
  }
  return @{ missing = $missingCaps; not_covered = $notCovered; probes = $probe; applied = $pol.applied; plan = $pol.plan }
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
    plan = { param($In)
      $r = Result-Changes (Get-AuthMethodChanges $In)
      $s = Get-MfaState $In   # who still has to register: names on screen only, never in audit mode
      Write-BoxRegistrationWarnings $s.registration $s.readiness -Names:($In.environment -ne 'audit')
      $r }
    apply = { param($In) Result-Changes (Get-AuthMethodChanges $In -Apply) }
    verify = { param($In) Get-AuthMethodVerify $In }
    evidence = { param($In)
      $s = Get-MfaState $In
      @{ items = @(
          @{ kind = 'authentication-methods'; ref = 'tenant:authentication-methods'; summary = 'states and registration campaign equal baseline/identity.yaml' }
          @{ kind = 'mfa-registration'; ref = 'tenant:mfa-registration'; summary = "$(Format-BoxRegistrationSummary $s.registration.counts); $($s.readiness.message)" }) } }
  }
  'identity.directory-settings' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-DirectoryChanges $In) }
    apply = { param($In) Result-Changes (Get-DirectoryChanges $In -Apply) }
    verify = { param($In) $c = @(Get-DirectoryChanges $In | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'directory-settings' 'authorization policy and admin consent workflow equal baseline/identity.yaml' }
  }
  'identity.conditional-access' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-CAChanges $In) }
    apply = { param($In) Result-Changes (Get-CAChanges $In -Apply) }
    verify = { param($In) $c = @(Get-CAChanges $In | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'conditional-access' 'policies exclude grp-break-glass' }
  }
  'policy.capabilities' = @{
    test = { param($In)
      $r = Get-CapabilityReport $In
      $bad = @($r.missing) + @($r.probes.GetEnumerator() | Where-Object { $_.Value -ne 'usable' } | ForEach-Object { "$($_.Key) $($_.Value)" })
      if ($bad.Count) { return @{ ok = $false; reason = "the tenant lacks capabilities the resolved settings need: $($bad -join '; ')" } }
      return @{ ok = $true } }
    plan = { param($In) Result-Changes @() }
    apply = { param($In) Result-Changes @() }
    verify = { param($In)
      $r = Get-CapabilityReport $In
      foreach ($n in @($r.not_covered)) { Write-Warning "not covered: $($n.user) lacks $($n.missing -join ', ')" }
      @{ ok = (@($r.missing).Count -eq 0); drift = @($r.missing | ForEach-Object { New-BoxChange update "capability $_" 'licence missing' }) } }
    evidence = { param($In)
      $r = Get-CapabilityReport $In
      $nc = @($r.not_covered)
      New-Evidence 'capabilities' "$($r.applied) resolved settings on plan $($r.plan); capabilities confirmed; probes: $(($r.probes.GetEnumerator() | ForEach-Object { "$($_.Key)=$($_.Value)" }) -join ', '); users not covered: $($nc.Count)" }
  }
  'policy.entra' = @{
    test = { param($In) if ($In.policy_error) { @{ ok = $false; reason = "policy resolution failed: $($In.policy_error)" } } else { @{ ok = $true } } }
    plan = { param($In) Get-BoxPolicyPlan $In }
    apply = { param($In) Invoke-BoxPolicyApply $In }
    verify = { param($In) Test-BoxPolicyVerify $In }
    evidence = { param($In) Get-BoxPolicyEvidence $In 'policy-entra' }
  }
  'policy.m365-admin' = @{
    test = $ok
    plan = { param($In) Get-BoxPolicyPlan $In }
    apply = { param($In) Invoke-BoxPolicyApply $In }
    verify = { param($In) Test-BoxPolicyVerify $In }
    evidence = { param($In) Get-BoxPolicyEvidence $In 'policy-m365-admin' }
  }
  'policy.teams.identity' = @{
    test = $ok
    plan = { param($In) Get-BoxPolicyPlan $In }
    apply = { param($In) Invoke-BoxPolicyApply $In }
    verify = { param($In) Test-BoxPolicyVerify $In }
    evidence = { param($In) Get-BoxPolicyEvidence $In 'policy-teams-identity' }
  }
  'policy.purview.identity' = @{
    test = $ok
    plan = { param($In) Get-BoxPolicyPlan $In }
    apply = { param($In) Invoke-BoxPolicyApply $In }
    verify = { param($In) Test-BoxPolicyVerify $In }
    evidence = { param($In) Get-BoxPolicyEvidence $In 'policy-purview-identity' }
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
Export-ModuleMember -Function Invoke-BoxStep, Get-BoxStepIds, Get-BoxStepProblems, Get-NotCoveredUsers, Get-BoxMfaEnforcementReadiness,
  Get-BoxCampaignWant, Get-BoxCampaignDrift, Get-BoxMfaRegistrationCounts, Get-BoxCA01Readiness, Format-BoxRegistrationSummary,
  Compare-BoxValue, Resolve-BoxPayload, Get-BoxCAState, Merge-BoxGroupSettingValues, Get-BoxIdleTimeout, Get-BoxEntraClaims
