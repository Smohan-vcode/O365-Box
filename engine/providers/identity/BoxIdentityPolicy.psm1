# Resolved policy settings applied by the identity provider (T14-04):
# policy.entra, policy.m365-admin, policy.teams.identity and
# policy.purview.identity. Each step receives $In.policy.settings (id, mode,
# payload, check) from `box policy resolve`; the mapping files in
# expected-state/setting-ids/ say what each setting calls. Graph over REST.
#
# Ownership (no two steps write the same property):
#   - Conditional Access policies named in an entra mapping are owned by
#     policy.entra; identity.conditional-access leaves them alone.
#   - authorizationPolicy and admin-consent properties named in an entra
#     mapping are owned by policy.entra; identity.directory-settings and
#     identity.auth-methods leave them alone (Get-BoxEntraClaims).
#   - The MFA registration campaign (ENT-050, SC-27) is owned by
#     policy.entra; identity.auth-methods keeps only the registration report.
#   - The Group.Unified directory setting: policy.entra (ENT-034) writes
#     EnableGroupCreation only; policy.teams.identity (TMS-014) owns
#     GroupCreationAllowedGroupId. Both merge into the existing values.
#   - Domain password expiry is checked by ENT-028 and ADM-004 with the same
#     value, so neither can undo the other.
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxProvider.psm1') -Force
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxGraph.psm1') -Force
Import-Module (Join-Path $PSScriptRoot 'BoxIdentityMfa.psm1') -Force   # Get-BoxCampaignDrift (ENT-050)

$script:Beta = 'https://graph.microsoft.com/beta'
$script:ReportOnly = 'enabledForReportingButNotEnforced'
$script:PrivilegedRoles = @()   # set by the identity module (Set-BoxPolicyRoles)

function Set-BoxPolicyRoles([string[]] $Roles) { $script:PrivilegedRoles = $Roles }

# ---------------------------------------------------------------- pure helpers

function ConvertTo-BoxPlain($o) {
  # PSCustomObject / hashtable / array -> ordered hashtable / object[] (deep).
  if ($null -eq $o) { return $null }
  if ($o -is [string] -or $o -is [ValueType]) { return $o }
  if ($o -is [System.Collections.IDictionary]) {
    $h = [ordered]@{}
    foreach ($k in $o.Keys) { $h[[string]$k] = ConvertTo-BoxPlain $o[$k] }
    return $h
  }
  if ($o -is [System.Collections.IEnumerable]) {
    return , @($o | ForEach-Object { ConvertTo-BoxPlain $_ })
  }
  if ($o -is [psobject]) {
    $h = [ordered]@{}
    foreach ($p in $o.PSObject.Properties) { $h[$p.Name] = ConvertTo-BoxPlain $p.Value }
    return $h
  }
  return $o
}

function Get-BoxProp($o, [string] $Name) {
  if ($null -eq $o) { return $null }
  if ($o -is [System.Collections.IDictionary]) { if ($o.Contains($Name)) { return $o[$Name] }; return $null }
  $p = $o.PSObject.Properties[$Name]
  if ($p) { return $p.Value }
  return $null
}

function Test-BoxIsMap($o) { return ($o -is [System.Collections.IDictionary]) -or ($o -is [System.Management.Automation.PSCustomObject]) }
function Test-BoxIsList($o) { return ($null -ne $o) -and -not ($o -is [string]) -and -not (Test-BoxIsMap $o) -and ($o -is [System.Collections.IEnumerable]) }
function Get-BoxKeys($o) {
  if ($o -is [System.Collections.IDictionary]) { return @($o.Keys | ForEach-Object { [string]$_ }) }
  return @($o.PSObject.Properties | ForEach-Object { $_.Name })
}

function Compare-BoxValue {
  # Returns the paths where Have does not hold Want. Maps: every key of
  # Want must match (keys only in Have are ignored; '@odata.type' is
  # compared, other '@odata.*' annotations are not). Lists of scalars: same set, any order, case-insensitive.
  # Lists of maps: same length, each Want item matches a distinct Have item.
  # Scalars: case-insensitive string comparison.
  param($Want, $Have, [string] $Path = '')
  $out = @()
  if (Test-BoxIsMap $Want) {
    if (-not (Test-BoxIsMap $Have)) { return @("$Path (missing)") }
    foreach ($k in Get-BoxKeys $Want) {
      if ($k -like '@odata.*' -and $k -ne '@odata.type') { continue }
      $out += @(Compare-BoxValue -Want (Get-BoxProp $Want $k) -Have (Get-BoxProp $Have $k) -Path ($Path ? "$Path.$k" : $k))
    }
    return $out
  }
  if (Test-BoxIsList $Want) {
    $w = @($Want); $h = @(if (Test-BoxIsList $Have) { @($Have) } elseif ($null -ne $Have) { , $Have } else { })
    if ($w.Count -ne $h.Count) { return @("$Path (count $($h.Count), want $($w.Count))") }
    if (@($w | Where-Object { Test-BoxIsMap $_ }).Count -eq 0) {
      $hs = @($h | ForEach-Object { ([string]$_).ToLowerInvariant() })
      foreach ($x in $w) { if (([string]$x).ToLowerInvariant() -notin $hs) { $out += "$Path (lacks $x)" } }
      return $out
    }
    $used = @{}
    foreach ($x in $w) {
      $hit = $false
      for ($i = 0; $i -lt $h.Count; $i++) {
        if ($used[$i]) { continue }
        if (@(Compare-BoxValue -Want $x -Have $h[$i]).Count -eq 0) { $used[$i] = $true; $hit = $true; break }
      }
      if (-not $hit) { $out += "$Path (no item matches)" }
    }
    return $out
  }
  if ($null -eq $Want) {
    if ($null -eq $Have -or ((Test-BoxIsList $Have) -and @($Have).Count -eq 0)) { return @() }
    return @("$Path (want null)")
  }
  if ($null -eq $Have) { return @("$Path (missing)") }
  if (([string]$Want).ToLowerInvariant() -ne ([string]$Have).ToLowerInvariant()) { return @("$Path ($Have, want $Want)") }
  return @()
}

function Resolve-BoxPayload {
  # Replaces placeholders in a mapping payload. {group:NAME} becomes the
  # group's object id (from $Groups: name -> id, $null when missing), a
  # string starting with {roles:privileged} becomes the privileged role
  # template ids. {text:x}, {number:x} and {terms:x} come from $Fill (keys
  # 'text:x', 'number:x', 'terms:x'): values the company's choices supply
  # (configuration values.x, SPEC 24) or terms derived from its own names; a
  # string that is only {number:x} becomes a number. Any other {kind:...}
  # placeholder, or one $Fill lacks, is a value the company supplies: the
  # setting cannot be applied, so this throws "unresolved".
  param($Payload, [hashtable] $Groups, [string[]] $Roles = @(), [hashtable] $Fill = @{})
  $p = ConvertTo-BoxPlain $Payload
  return (Resolve-BoxNode $p $Groups $Roles $Fill)
}
function Resolve-BoxNode($n, [hashtable] $Groups, [string[]] $Roles, [hashtable] $Fill) {
  if ($n -is [System.Collections.IDictionary]) {
    $h = [ordered]@{}
    foreach ($k in @($n.Keys)) { $h[$k] = Resolve-BoxNode $n[$k] $Groups $Roles $Fill }
    return $h
  }
  if (Test-BoxIsList $n) {
    $items = [System.Collections.Generic.List[object]]::new()
    foreach ($x in $n) {
      if ($x -is [string] -and $x -like '{roles:privileged}*') { foreach ($r in $Roles) { $items.Add($r) }; continue }
      $items.Add((Resolve-BoxNode $x $Groups $Roles $Fill))
    }
    return , $items.ToArray()
  }
  if ($n -is [string]) {
    if ($n -like '{roles:privileged}*') { return , @($Roles) }
    if ($n -match '^\{number:([a-z0-9_]+)\}$') {
      $v = if ($Fill) { $Fill["number:$($Matches[1])"] } else { $null }
      if ($null -eq $v -or "$v" -notmatch '^-?[0-9]+$') { throw "unresolved: {number:$($Matches[1])} needs a value the company's choices supply" }
      return [int]"$v"
    }
    $s = $n
    foreach ($m in [regex]::Matches($n, '\{(text|terms):([a-z0-9_]+)\}')) {
      $v = if ($Fill) { $Fill["$($m.Groups[1].Value):$($m.Groups[2].Value)"] } else { $null }
      if ($null -eq $v -or "$v" -eq '') { throw "unresolved: $($m.Value) needs a value the company supplies" }
      $s = $s.Replace($m.Value, [string]$v)
    }
    foreach ($m in [regex]::Matches($n, '\{group:([^}]+)\}')) {
      $name = $m.Groups[1].Value
      $id = if ($Groups.ContainsKey($name)) { $Groups[$name] } else { $null }
      if (-not $id) { throw "unresolved: group $name does not exist" }
      $s = $s.Replace($m.Value, $id)
    }
    $u = [regex]::Match($s, '\{(dictionary|config|sku|namedLocation|user|app|github|site|library|text|number|terms):[^}]*\}')
    if ($u.Success) { throw "unresolved: $($u.Value) needs a value the company supplies" }
    if ($s -like '*<existing ids>*') { throw 'unresolved: placeholder <existing ids>' }
    return $s
  }
  return $n
}

function Get-BoxPlaceholderGroups($Payload) {
  $json = ConvertTo-Json -InputObject $Payload -Depth 30 -Compress
  return @([regex]::Matches($json, '\{group:([^}]+)\}') | ForEach-Object { $_.Groups[1].Value } | Select-Object -Unique)
}

function Get-BoxCAState {
  # The state a Box Conditional Access policy gets: report-only unless the
  # setting is enforced and every break-glass account has FIDO2 (gate G-02,
  # SPEC 20.2).
  param([string] $Want, [string] $Mode, [bool] $BreakGlassReady)
  if ($Want -ne 'enabled') { return $Want }
  if ($Mode -ne 'enforce' -or -not $BreakGlassReady) { return $script:ReportOnly }
  return 'enabled'
}

function Merge-BoxGroupSettingValues {
  # Current values (name/value list) with Want's names overwritten; returns
  # the merged list and the names that differ.
  param($Current, $Want)
  $map = [ordered]@{}
  foreach ($v in @($Current)) { if ($v) { $map[[string](Get-BoxProp $v 'name')] = [string](Get-BoxProp $v 'value') } }
  $diff = @()
  foreach ($v in @($Want)) {
    $n = [string](Get-BoxProp $v 'name'); $val = [string](Get-BoxProp $v 'value')
    if (-not $map.Contains($n) -or [string]$map[$n] -ne $val) { $diff += $n }
    $map[$n] = $val
  }
  $list = @($map.Keys | ForEach-Object { @{ name = $_; value = $map[$_] } })
  return @{ values = $list; diff = $diff }
}

function Get-BoxIdleTimeout($Policy) {
  # WebSessionIdleTimeout of an activityBasedTimeoutPolicy definition.
  foreach ($d in @(Get-BoxProp $Policy 'definition')) {
    try { $j = $d | ConvertFrom-Json } catch { continue }
    foreach ($a in @($j.ActivityBasedTimeoutPolicy.ApplicationPolicies)) {
      if ($a.ApplicationId -eq 'default') { return [string]$a.WebSessionIdleTimeout }
    }
  }
  return $null
}

# Directory and Conditional Access objects claimed by entra mappings: the
# identity.* T1 steps leave these to policy.entra (T14-04 step 6).
$script:DirectoryClaims = @{ users_can_register_apps = 'ENT-030'; users_can_consent_to_apps = 'ENT-031'; admin_consent_workflow = 'ENT-032'
  guest_invitations = 'ENT-036'; guest_access = 'ENT-037' }
$script:AuthMethodClaims = @{ fido2 = 'ENT-019'; microsoft_authenticator = 'ENT-020'; temporary_access_pass = 'ENT-021'; sms = 'ENT-022'; voice = 'ENT-023'; email_otp = 'ENT-024' }
function Get-BoxEntraClaims {
  param([string] $MappingFile = (Join-Path $PSScriptRoot '..' '..' '..' 'expected-state' 'setting-ids' 'entra.json'))
  $out = @{ ca = @(); directory = @(); auth_methods = @() }
  if (-not (Test-Path $MappingFile)) { return $out }
  $m = Get-Content $MappingFile -Raw | ConvertFrom-Json
  $owned = @{}
  foreach ($p in $m.PSObject.Properties) {
    if ($p.Value.step -ne 'policy.entra' -or $p.Value.status -notin 'resolved', 'not-applied') { continue }
    $owned[$p.Name] = $true
    $pl = $p.Value.payload
    $name = Get-BoxProp $pl 'displayName'
    if (-not $name) { $name = Get-BoxProp (Get-BoxProp $pl 'policy') 'displayName' }
    if ($name -and ([string]$p.Value.apply) -match 'conditionalAccess/policies') { $out.ca += [string]$name }
  }
  $out.directory = @($script:DirectoryClaims.Keys | Where-Object { $owned[$script:DirectoryClaims[$_]] })
  $out.auth_methods = @($script:AuthMethodClaims.Keys | Where-Object { $owned[$script:AuthMethodClaims[$_]] })
  return $out
}

# ---------------------------------------------------------------- tenant helpers

function Get-BoxGroupIds([string[]] $Names) {
  $h = @{}
  foreach ($n in $Names) {
    $g = @(Get-BoxGraphAll "/groups?`$filter=displayName eq '$(ConvertTo-BoxFilterValue $n)'&`$select=id,displayName")
    if ($g.Count -gt 1) { throw "$($g.Count) groups are named '$n'; keep one and remove the others" }
    $h[$n] = if ($g.Count -eq 1) { [string]$g[0].id } else { $null }
  }
  return $h
}

function Test-BoxBreakGlassReady($In) {
  $users = @($In.config.users | Where-Object { $_.break_glass })
  if ($users.Count -eq 0) { return $false }
  foreach ($u in $users) {
    $usr = Get-BoxGraphOne "/users/$([uri]::EscapeDataString("$($u.primary)@$($In.tenant.initial_domain)"))?`$select=id"
    if (-not $usr) { return $false }
    if (@(Get-BoxGraphAll "/users/$($usr.id)/authentication/fido2Methods").Count -eq 0) { return $false }
  }
  return $true
}

function Get-BoxTokenAppId {
  $t = [string](Get-BoxAuth 'graph').access_token
  $part = $t.Split('.')[1].Replace('-', '+').Replace('_', '/')
  switch ($part.Length % 4) { 2 { $part += '==' } 3 { $part += '=' } }
  return ([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($part)) | ConvertFrom-Json).appid
}

function Invoke-BoxSafe([scriptblock] $Block) {
  # Runs a read; returns @{ ok; value; error }.
  try { return @{ ok = $true; value = (& $Block) } } catch { return @{ ok = $false; error = "$_" } }
}

# ---------------------------------------------------------------- the engine

# A context collects, per run, the changes, the settings left out and the
# report items. Handlers get ($ctx, $setting, [bool] $Apply).
function New-BoxPolicyContext($In, [bool] $Apply) {
  if ($In.policy_error) { throw "policy resolution failed: $($In.policy_error)" }
  $settings = @($In.policy.settings | Where-Object { $_ })
  $names = @('grp-break-glass')
  foreach ($s in $settings) { $names += Get-BoxPlaceholderGroups $s.payload }
  return @{ In = $In; Apply = $Apply; Settings = $settings; Changes = [System.Collections.Generic.List[object]]::new()
    Skipped = [System.Collections.Generic.List[object]]::new(); Report = [System.Collections.Generic.List[object]]::new()
    Drift = [System.Collections.Generic.List[object]]::new()
    Groups = (Get-BoxGroupIds ($names | Select-Object -Unique)); Cache = @{}; Fill = (Get-BoxChoiceFill $In) }
}
function Get-BoxChoiceFill($In) {
  # The values the company's choices supply (configuration values.*, SPEC
  # 24) as Resolve-BoxPayload fill keys: 'text:x' and, for whole numbers, 'number:x'.
  $fill = @{}
  $vals = Get-BoxProp $In.config 'values'
  if ($null -eq $vals) { return $fill }
  foreach ($k in Get-BoxKeys $vals) {
    $v = Get-BoxProp $vals $k
    if ($null -eq $v -or (Test-BoxIsMap $v) -or (Test-BoxIsList $v)) { continue }
    $fill["text:$k"] = [string]$v
    if ("$v" -match '^-?[0-9]+$') { $fill["number:$k"] = [int]"$v" }
  }
  return $fill
}
function Add-BoxSkip($Ctx, $S, [string] $Why) { $Ctx.Skipped.Add(@{ id = $S.id; reason = $Why }) }
function Add-BoxChange($Ctx, $S, [string] $Action, [string] $Target, [string] $Detail) {
  $Ctx.Changes.Add((New-BoxChange $Action "$($S.id) $Target" $Detail))
}
# Drift the step reports but must not fix itself (a person or a gate does).
function Add-BoxDrift($Ctx, $S, [string] $Target, [string] $Detail) {
  $Ctx.Drift.Add((New-BoxChange update "$($S.id) $Target" $Detail))
}
function Get-BoxResolved($Ctx, $S, [hashtable] $Extra = @{}) {
  $fill = @{} + $Ctx.Fill
  foreach ($k in $Extra.Keys) { $fill[$k] = $Extra[$k] }
  try { return @{ ok = $true; body = (Resolve-BoxPayload -Payload $S.payload -Groups $Ctx.Groups -Roles $script:PrivilegedRoles -Fill $fill) } }
  catch { Add-BoxSkip $Ctx $S ("$_" -replace '^unresolved: ', ''); return @{ ok = $false } }
}
function Get-BoxCached($Ctx, [string] $Key, [scriptblock] $Read) {
  if (-not $Ctx.Cache.ContainsKey($Key)) { $Ctx.Cache[$Key] = & $Read }
  return $Ctx.Cache[$Key]
}

# --- Conditional Access ---
function Get-BoxCAPolicies($Ctx, [switch] $Fresh) {
  if ($Fresh) { $Ctx.Cache.Remove('ca') }
  return Get-BoxCached $Ctx 'ca' { , @(Get-BoxGraphAll '/identity/conditionalAccess/policies') }
}
function Get-BoxBreakGlassReady($Ctx) { return Get-BoxCached $Ctx 'g02' { Test-BoxBreakGlassReady $Ctx.In } }

function Sync-BoxCAPolicy($Ctx, $S) {
  $r = Get-BoxResolved $Ctx $S
  if (-not $r.ok) { return }
  $body = $r.body
  if ($body.Contains('policy') -or $body.Contains('namedLocation')) { Add-BoxSkip $Ctx $S 'named-location policies are not implemented'; return }
  $excl = @($body.conditions.users.excludeGroups)
  if ($Ctx.Groups['grp-break-glass'] -notin $excl) { throw "$($S.id): payload does not exclude grp-break-glass (SPEC 20.2)" }
  $body.state = Get-BoxCAState -Want ([string]$body.state) -Mode ([string]$S.mode) -BreakGlassReady (Get-BoxBreakGlassReady $Ctx)
  $name = [string]$body.displayName
  $have = @((Get-BoxCAPolicies $Ctx) | Where-Object { $_.displayName -eq $name })
  if ($have.Count -gt 1) { throw "$($have.Count) Conditional Access policies are named '$name'; keep one" }
  if ($have.Count -eq 0) {
    Add-BoxChange $Ctx $S create "policy $name" $body.state
    if ($Ctx.Apply) { Invoke-BoxGraph -Method POST -Uri '/identity/conditionalAccess/policies' -Body $body | Out-Null }
    return
  }
  $diff = @(Compare-BoxValue -Want $body -Have $have[0])
  if ($diff.Count) {
    Add-BoxChange $Ctx $S update "policy $name" ($diff -join '; ')
    if ($Ctx.Apply) { Invoke-BoxGraph -Method PATCH -Uri "/identity/conditionalAccess/policies/$($have[0].id)" -Body $body | Out-Null }
  }
}

function Sync-BoxCABreakGlass($Ctx, $S) {
  # ENT-015: every policy in the tenant, Box's or not, excludes grp-break-glass.
  $bg = $Ctx.Groups['grp-break-glass']
  if (-not $bg) { throw 'the break-glass exclusion group grp-break-glass does not exist' }
  foreach ($p in (Get-BoxCAPolicies $Ctx -Fresh:$Ctx.Apply)) {
    $ex = @($p.conditions.users.excludeGroups | Where-Object { $_ })
    if ($bg -in $ex) { continue }
    Add-BoxChange $Ctx $S update "policy $($p.displayName)" "exclude grp-break-glass (state $($p.state))"
    if ($Ctx.Apply) {
      $users = ConvertTo-BoxPlain $p.conditions.users
      $users.excludeGroups = @($ex + $bg)
      Invoke-BoxGraph -Method PATCH -Uri "/identity/conditionalAccess/policies/$($p.id)" -Body @{ conditions = @{ users = $users } } | Out-Null
    }
  }
}

function Sync-BoxSecurityDefaults($Ctx, $S) {
  # ENT-018: security defaults go off only once CA01-CA03 are enforced; until
  # then they stay as they are (switching off earlier leaves no MFA at all).
  $cur = Invoke-BoxGraph -Uri '/policies/identitySecurityDefaultsEnforcementPolicy'
  if (-not $cur.isEnabled) { return }
  $pols = Get-BoxCAPolicies $Ctx -Fresh:$Ctx.Apply
  $enforced = @('CA01 ', 'CA02 ', 'CA03 ' | Where-Object { $pre = $_; @($pols | Where-Object { $_.displayName -like "$pre*" -and $_.state -eq 'enabled' }).Count -gt 0 })
  if ($enforced.Count -lt 3) {
    $Ctx.Report.Add("$($S.id): security defaults stay on until CA01-CA03 are enforced (report-only while gate G-02 is open)")
    return
  }
  Add-BoxChange $Ctx $S update 'security defaults' 'on -> off (CA01-CA03 enforced)'
  if ($Ctx.Apply) { Invoke-BoxGraph -Method PATCH -Uri '/policies/identitySecurityDefaultsEnforcementPolicy' -Body @{ isEnabled = $false } | Out-Null }
}

# --- authentication methods ---
$script:AuthMethodIds = @{ 'ENT-019' = 'Fido2'; 'ENT-020' = 'MicrosoftAuthenticator'; 'ENT-021' = 'TemporaryAccessPass'; 'ENT-022' = 'Sms'; 'ENT-023' = 'Voice'; 'ENT-024' = 'Email' }
function Sync-BoxAuthMethod($Ctx, $S) {
  $r = Get-BoxResolved $Ctx $S
  if (-not $r.ok) { return }
  $id = $script:AuthMethodIds[$S.id]
  $uri = "/policies/authenticationMethodsPolicy/authenticationMethodConfigurations/$id"
  $cur = Invoke-BoxGraph -Uri $uri
  $diff = @(Compare-BoxValue -Want $r.body -Have $cur)
  if ($diff.Count) {
    Add-BoxChange $Ctx $S update "authentication method $id" ($diff -join '; ')
    if ($Ctx.Apply) { Invoke-BoxGraph -Method PATCH -Uri $uri -Body $r.body | Out-Null }
  }
}

function Sync-BoxMfaMigration($Ctx, $S) {
  # ENT-025: unified methods policy only, and no per-user MFA.
  $pol = Invoke-BoxGraph -Uri '/policies/authenticationMethodsPolicy'
  $st = [string]$pol.policyMigrationState
  if ($st -and $st -ne 'migrationComplete') {
    Add-BoxChange $Ctx $S update 'authentication methods policy migration' "$st -> migrationComplete"
    if ($Ctx.Apply) { Invoke-BoxGraph -Method PATCH -Uri '/policies/authenticationMethodsPolicy' -Body @{ policyMigrationState = 'migrationComplete' } | Out-Null }
  }
  $on = @(Get-BoxGraphAll '/users?$select=id&$filter=userType eq ''Member''' | Where-Object {
      [string](Invoke-BoxGraph -Uri "$script:Beta/users/$($_.id)/authentication/requirements").perUserMfaState -notin '', 'disabled' })
  if ($on.Count -eq 0) { return }
  Add-BoxChange $Ctx $S update 'per-user MFA' "$($on.Count) user(s) -> disabled"
  if ($Ctx.Apply) { foreach ($u in $on) { Invoke-BoxGraph -Method PATCH -Uri "$script:Beta/users/$($u.id)/authentication/requirements" -Body @{ perUserMfaState = 'disabled' } | Out-Null } }
}

function Sync-BoxRegistrationCampaign($Ctx, $S) {
  # ENT-050 (SC-27): the MFA registration campaign, on beta (the
  # enforce-after-snoozes property is beta only). identity.auth-methods
  # leaves it to this step and keeps the registration report.
  $r = Get-BoxResolved $Ctx $S
  if (-not $r.ok) { return }
  $want = $r.body.registrationEnforcement.authenticationMethodsRegistrationCampaign
  $snooze = [int]$want.snoozeDurationInDays
  if ($snooze -lt 0 -or $snooze -gt 14) { throw "$($S.id): snoozeDurationInDays must be 0 to 14, not $snooze (choice signin.mfa_snooze_days)" }
  $uri = "$script:Beta/policies/authenticationMethodsPolicy"
  $have = (Invoke-BoxGraph -Uri $uri).registrationEnforcement.authenticationMethodsRegistrationCampaign
  $drift = @(Get-BoxCampaignDrift -Want $want -Have $have)
  if ($drift.Count -eq 0) { return }
  Add-BoxChange $Ctx $S update 'registration campaign (Microsoft Authenticator)' ($drift -join '; ')
  if ($Ctx.Apply) { Invoke-BoxGraph -Method PATCH -Uri $uri -Body $r.body | Out-Null }
}

# --- directory settings (groupSettings from a template) ---
function Sync-BoxGroupSetting($Ctx, $S, [string] $TemplateId, $Values) {
  $all = @(Get-BoxGraphAll '/groupSettings')
  $obj = @($all | Where-Object { $_.templateId -eq $TemplateId })
  $current = if ($obj.Count) { $obj[0].values } else { (Invoke-BoxGraph -Uri "/groupSettingTemplates/$TemplateId").values | ForEach-Object { @{ name = $_.name; value = $_.defaultValue } } }
  $m = Merge-BoxGroupSettingValues -Current $current -Want $Values
  if ($m.diff.Count -eq 0) { return }
  $what = if ($obj.Count) { 'update' } else { 'create' }
  Add-BoxChange $Ctx $S $what "directory setting $TemplateId" ($m.diff -join ', ')
  if (-not $Ctx.Apply) { return }
  if ($obj.Count) { Invoke-BoxGraph -Method PATCH -Uri "/groupSettings/$($obj[0].id)" -Body @{ values = $m.values } | Out-Null }
  else { Invoke-BoxGraph -Method POST -Uri '/groupSettings' -Body @{ templateId = $TemplateId; values = $m.values } | Out-Null }
}

# --- domains ---
function Sync-BoxDomainExpiry($Ctx, $S) {
  # ENT-028 and ADM-004: passwords never expire on any verified domain.
  $r = Get-BoxResolved $Ctx $S
  if (-not $r.ok) { return }
  $want = [long]$r.body.passwordValidityPeriodInDays
  $bad = @(Get-BoxGraphAll '/domains?$select=id,isVerified,passwordValidityPeriodInDays' | Where-Object { $_.isVerified -and [long]$_.passwordValidityPeriodInDays -ne $want })
  if ($bad.Count -eq 0) { return }
  Add-BoxChange $Ctx $S update 'password expiry' "$($bad.Count) domain(s) -> never"
  if ($Ctx.Apply) { foreach ($d in $bad) { Invoke-BoxGraph -Method PATCH -Uri "/domains/$($d.id)" -Body @{ passwordValidityPeriodInDays = $want } | Out-Null } }
}

# --- authorization policy (ENT-030, 031, 033, 034, 036, 037) ---
function Add-BoxAuthorizationWant($Ctx, $S, $Body) {
  if (-not $Ctx.Cache.ContainsKey('authz')) { $Ctx.Cache['authz'] = [System.Collections.Generic.List[object]]::new() }
  $Ctx.Cache['authz'].Add(@{ s = $S; body = $Body })
}
function Sync-BoxAuthorizationPolicy($Ctx) {
  $wants = $Ctx.Cache['authz']
  if (-not $wants) { return }
  $cur = Invoke-BoxGraph -Uri '/policies/authorizationPolicy'
  $patch = [ordered]@{}
  foreach ($w in $wants) {
    $diff = @(Compare-BoxValue -Want $w.body -Have $cur)
    if ($diff.Count -eq 0) { continue }
    Add-BoxChange $Ctx $w.s update 'authorization policy' ($diff -join '; ')
    foreach ($k in $w.body.Keys) {
      if ($w.body[$k] -is [System.Collections.IDictionary] -and $patch[$k] -is [System.Collections.IDictionary]) { foreach ($kk in $w.body[$k].Keys) { $patch[$k][$kk] = $w.body[$k][$kk] } }
      elseif ($w.body[$k] -is [System.Collections.IDictionary]) { $patch[$k] = [ordered]@{} + $w.body[$k] }
      else { $patch[$k] = $w.body[$k] }
    }
  }
  if ($Ctx.Apply -and $patch.Count) { Invoke-BoxGraph -Method PATCH -Uri '/policies/authorizationPolicy' -Body $patch | Out-Null }
}

function Sync-BoxAdminConsent($Ctx, $S) {
  # ENT-032
  $r = Get-BoxResolved $Ctx $S
  if (-not $r.ok) { return }
  $cur = Invoke-BoxGraph -Uri '/policies/adminConsentRequestPolicy'
  $q = @($r.body.reviewers | ForEach-Object { [string]$_.query })
  $haveQ = @($cur.reviewers | ForEach-Object { ([string]$_.query) -replace '^/v1\.0', '' })
  $missing = @($q | Where-Object { $_ -notin $haveQ })
  $flags = @(Compare-BoxValue -Want ([ordered]@{ isEnabled = $r.body.isEnabled; notifyReviewers = $r.body.notifyReviewers; remindersEnabled = $r.body.remindersEnabled; requestDurationInDays = $r.body.requestDurationInDays }) -Have $cur)
  if ($missing.Count -eq 0 -and $flags.Count -eq 0) { return }
  Add-BoxChange $Ctx $S update 'admin consent workflow' ((@($flags) + @($missing | ForEach-Object { 'reviewer role-owner' })) -join '; ')
  if ($Ctx.Apply) { Invoke-BoxGraph -Method PUT -Uri '/policies/adminConsentRequestPolicy' -Body $r.body | Out-Null }
}

# --- device registration policy (ENT-039..042, beta, whole object) ---
function Add-BoxDeviceWant($Ctx, $S, $Body) {
  if (-not $Ctx.Cache.ContainsKey('devreg')) { $Ctx.Cache['devreg'] = [System.Collections.Generic.List[object]]::new() }
  $Ctx.Cache['devreg'].Add(@{ s = $S; body = $Body })
}
function Merge-BoxInto($Target, $Source) {
  foreach ($k in $Source.Keys) {
    if ($Source[$k] -is [System.Collections.IDictionary] -and $Target[$k] -is [System.Collections.IDictionary]) { Merge-BoxInto $Target[$k] $Source[$k] }
    else { $Target[$k] = $Source[$k] }
  }
}
function Sync-BoxDeviceRegistration($Ctx) {
  $wants = $Ctx.Cache['devreg']
  if (-not $wants) { return }
  $cur = ConvertTo-BoxPlain (Invoke-BoxGraph -Uri "$script:Beta/policies/deviceRegistrationPolicy")
  $changed = $false
  foreach ($w in $wants) {
    $diff = @(Compare-BoxValue -Want $w.body -Have $cur)
    if ($diff.Count -eq 0) { continue }
    Add-BoxChange $Ctx $w.s update 'device registration policy' ($diff -join '; ')
    $changed = $true
  }
  if (-not ($Ctx.Apply -and $changed)) { return }
  $new = ConvertTo-BoxPlain (Invoke-BoxGraph -Uri "$script:Beta/policies/deviceRegistrationPolicy")
  foreach ($k in @($new.Keys)) { if ($k -like '@odata.context') { $new.Remove($k) } }
  foreach ($w in $wants) { Merge-BoxInto $new $w.body }
  Invoke-BoxGraph -Method PUT -Uri "$script:Beta/policies/deviceRegistrationPolicy" -Body $new | Out-Null
}

# --- checks a person or a gate fixes; the step reports them ---
function Test-BoxAdminAccounts($Ctx, $S) {
  # ENT-044: directory-role holders are cloud-only and hold no mailbox.
  $n = 0
  foreach ($a in @(Get-BoxGraphAll '/roleManagement/directory/roleAssignments?$expand=principal')) {
    if ([string]$a.principal.'@odata.type' -ne '#microsoft.graph.user') { continue }
    $u = Get-BoxGraphOne "/users/$($a.principalId)?`$select=id,onPremisesSyncEnabled,assignedPlans"
    if (-not $u) { continue }
    $mail = @($u.assignedPlans | Where-Object { $_.service -eq 'exchange' -and $_.capabilityStatus -eq 'Enabled' }).Count -gt 0
    if ($u.onPremisesSyncEnabled -eq $true -or $mail) { $n++ }
  }
  if ($n) { Add-BoxDrift $Ctx $S 'administrator accounts' "$n role assignment(s) held by a synced or mailbox-licensed user (gate G-05 removes the founder's standing role)" }
}
function Test-BoxPipelineSecrets($Ctx, $S) {
  # ENT-046: the pipeline app holds no client secret.
  $app = Get-BoxGraphOne "/applications(appId='$(Get-BoxTokenAppId)')?`$select=id,passwordCredentials,keyCredentials"
  if (-not $app) { Add-BoxDrift $Ctx $S 'pipeline app' 'application object not readable'; return }
  $n = @($app.passwordCredentials).Count
  if ($n) { Add-BoxDrift $Ctx $S 'pipeline app' "$n client secret(s): remove them (SPEC 20.2)" }
  else { $Ctx.Report.Add("$($S.id): pipeline app has no client secret ($(@($app.keyCredentials).Count) certificate(s))") }
}
function Test-BoxRoleHolders($Ctx, $S) {
  # ENT-040 second half: no Entra Joined Device Local Administrator holders.
  $n = @(Get-BoxGraphAll "/roleManagement/directory/roleAssignments?`$filter=roleDefinitionId eq '9f06204d-73c1-4d4c-880a-6edb90606fd8'").Count
  if ($n) { Add-BoxDrift $Ctx $S 'device local administrators' "$n assignment(s) of Microsoft Entra Joined Device Local Administrator" }
}
function Test-BoxRegistrationMfa($Ctx, $S) {
  # ENT-010: the device registration MFA switch stays notRequired when the
  # CA user action is used.
  $d = Invoke-BoxGraph -Uri '/policies/deviceRegistrationPolicy'
  if ([string]$d.multiFactorAuthConfiguration -ne 'notRequired') { Add-BoxDrift $Ctx $S 'device registration MFA switch' "$($d.multiFactorAuthConfiguration), want notRequired" }
}

# --- report-mode settings: read once, evidence only ---
function Read-BoxReport($Ctx, $S, [string] $Label, [string[]] $Uris) {
  foreach ($u in $Uris) {
    $r = Invoke-BoxSafe { Invoke-BoxGraph -Uri $u | Out-Null }
    if (-not $r.ok) { Add-BoxDrift $Ctx $S $Label "not readable: $($r.error)"; return }
  }
  $Ctx.Report.Add("$($S.id): $Label readable")
}
function Read-BoxOldSecrets($Ctx, $S) {
  # ENT-047: secrets older than 180 days (count only; names are tenant data).
  $cut = (Get-Date).ToUniversalTime().AddDays(-180)
  $n = 0
  foreach ($a in @(Get-BoxGraphAll '/applications?$select=id,passwordCredentials')) {
    $n += @($a.passwordCredentials | Where-Object { $_.startDateTime -and ([datetime]$_.startDateTime).ToUniversalTime() -lt $cut }).Count
  }
  $Ctx.Report.Add("$($S.id): $n application secret(s) older than 180 days")
}

# --- M365 admin centre ---
function Sync-BoxAdminSetting($Ctx, $S, [string] $Uri) {
  # ADM-001: PATCH /beta/admin/<area> with {settings: {...}}.
  $r = Get-BoxResolved $Ctx $S
  if (-not $r.ok) { return }
  $cur = Invoke-BoxGraph -Uri $Uri
  $diff = @(Compare-BoxValue -Want $r.body -Have $cur)
  if ($diff.Count -eq 0) { return }
  Add-BoxChange $Ctx $S update ($Uri -replace '^.*/admin/', 'admin ') ($diff -join '; ')
  if ($Ctx.Apply) { Invoke-BoxGraph -Method PATCH -Uri $Uri -Body $r.body | Out-Null }
}
function Sync-BoxIdleTimeout($Ctx, $S) {
  # ADM-003: one organisation-default activity-based timeout policy named "Box ...".
  $r = Get-BoxResolved $Ctx $S
  if (-not $r.ok) { return }
  $want = Get-BoxIdleTimeout $r.body
  $all = @(Get-BoxGraphAll '/policies/activityBasedTimeoutPolicies')
  $mine = @($all | Where-Object { $_.displayName -eq $r.body.displayName })
  $other = @($all | Where-Object { $_.isOrganizationDefault -and $_.displayName -ne $r.body.displayName })
  if ($other.Count) { throw "$($S.id): another organisation-default idle timeout policy exists; remove it or rename it to '$($r.body.displayName)'" }
  if ($mine.Count -eq 0) {
    Add-BoxChange $Ctx $S create "idle timeout policy $($r.body.displayName)" $want
    if ($Ctx.Apply) { Invoke-BoxGraph -Method POST -Uri '/policies/activityBasedTimeoutPolicies' -Body $r.body | Out-Null }
    return
  }
  if ((Get-BoxIdleTimeout $mine[0]) -ne $want -or -not $mine[0].isOrganizationDefault) {
    Add-BoxChange $Ctx $S update "idle timeout policy $($r.body.displayName)" $want
    if ($Ctx.Apply) { Invoke-BoxGraph -Method PATCH -Uri "/policies/activityBasedTimeoutPolicies/$($mine[0].id)" -Body $r.body | Out-Null }
  }
}
function Sync-BoxServicePrincipalDisabled($Ctx, $S) {
  # ADM-007: the third-party storage app's service principal exists and is disabled.
  $r = Get-BoxResolved $Ctx $S
  if (-not $r.ok) { return }
  $appId = [string]$r.body.create.appId
  $sp = @(Get-BoxGraphAll "/servicePrincipals?`$filter=appId eq '$appId'&`$select=id,accountEnabled")
  if ($sp.Count -eq 0) {
    Add-BoxChange $Ctx $S create "service principal $appId" 'disabled'
    if ($Ctx.Apply) {
      $new = Invoke-BoxGraph -Method POST -Uri '/servicePrincipals' -Body $r.body.create
      Invoke-BoxGraph -Method PATCH -Uri "/servicePrincipals/$($new.id)" -Body $r.body.patch | Out-Null
    }
    return
  }
  if ($sp[0].accountEnabled -ne [bool]$r.body.patch.accountEnabled) {
    Add-BoxChange $Ctx $S update "service principal $appId" 'accountEnabled -> false'
    if ($Ctx.Apply) { Invoke-BoxGraph -Method PATCH -Uri "/servicePrincipals/$($sp[0].id)" -Body $r.body.patch | Out-Null }
  }
}

$script:GroupUnified = '62375ab9-6b52-47ed-826b-58e47e0e304b'

# Handlers by setting id. Settings handled together (one object) queue
# their part and are written in Complete-BoxPolicy.
$script:Handlers = @{
  # Conditional Access
  'ENT-001' = { param($c, $s) Sync-BoxCAPolicy $c $s }; 'ENT-002' = { param($c, $s) Sync-BoxCAPolicy $c $s }
  'ENT-003' = { param($c, $s) Sync-BoxCAPolicy $c $s }; 'ENT-004' = { param($c, $s) Sync-BoxCAPolicy $c $s }
  'ENT-005' = { param($c, $s) Sync-BoxCAPolicy $c $s }; 'ENT-006' = { param($c, $s) Sync-BoxCAPolicy $c $s }
  'ENT-007' = { param($c, $s) Sync-BoxCAPolicy $c $s }; 'ENT-008' = { param($c, $s) Sync-BoxCAPolicy $c $s }
  'ENT-009' = { param($c, $s) Sync-BoxCAPolicy $c $s }
  'ENT-010' = { param($c, $s) Sync-BoxCAPolicy $c $s; Test-BoxRegistrationMfa $c $s }
  'ENT-011' = { param($c, $s) Sync-BoxCAPolicy $c $s }; 'ENT-012' = { param($c, $s) Sync-BoxCAPolicy $c $s }
  'ENT-013' = { param($c, $s) Sync-BoxCAPolicy $c $s }; 'ENT-014' = { param($c, $s) Sync-BoxCAPolicy $c $s }
  'ENT-015' = { param($c, $s) Sync-BoxCABreakGlass $c $s }
  'ENT-018' = { param($c, $s) Sync-BoxSecurityDefaults $c $s }
  # authentication methods
  'ENT-019' = { param($c, $s) Sync-BoxAuthMethod $c $s }; 'ENT-020' = { param($c, $s) Sync-BoxAuthMethod $c $s }
  'ENT-021' = { param($c, $s) Sync-BoxAuthMethod $c $s }; 'ENT-022' = { param($c, $s) Sync-BoxAuthMethod $c $s }
  'ENT-023' = { param($c, $s) Sync-BoxAuthMethod $c $s }; 'ENT-024' = { param($c, $s) Sync-BoxAuthMethod $c $s }
  'ENT-025' = { param($c, $s) Sync-BoxMfaMigration $c $s }
  'ENT-050' = { param($c, $s) Sync-BoxRegistrationCampaign $c $s }
  # password rule directory setting
  'ENT-026' = { param($c, $s) $r = Get-BoxResolved $c $s; if ($r.ok) { Sync-BoxGroupSetting $c $s $r.body.templateId $r.body.values } }
  'ENT-027' = { param($c, $s) $r = Get-BoxResolved $c $s; if ($r.ok) { Sync-BoxGroupSetting $c $s $r.body.templateId $r.body.values } }
  'ENT-028' = { param($c, $s) Sync-BoxDomainExpiry $c $s }
  # authorization policy and consent
  'ENT-030' = { param($c, $s) $r = Get-BoxResolved $c $s; if ($r.ok) { Add-BoxAuthorizationWant $c $s $r.body } }
  'ENT-031' = { param($c, $s) $r = Get-BoxResolved $c $s; if ($r.ok) { Add-BoxAuthorizationWant $c $s $r.body } }
  'ENT-032' = { param($c, $s) Sync-BoxAdminConsent $c $s }
  'ENT-033' = { param($c, $s) $r = Get-BoxResolved $c $s; if ($r.ok) { Add-BoxAuthorizationWant $c $s $r.body } }
  'ENT-034' = { param($c, $s)
    $r = Get-BoxResolved $c $s
    if (-not $r.ok) { return }
    Add-BoxAuthorizationWant $c $s $r.body.authorizationPolicy
    # GroupCreationAllowedGroupId is owned by TMS-014 (policy.teams.identity).
    $v = @($r.body.groupSettings.values | Where-Object { $_.name -ne 'GroupCreationAllowedGroupId' })
    Sync-BoxGroupSetting $c $s $r.body.groupSettings.templateId $v }
  'ENT-036' = { param($c, $s) $r = Get-BoxResolved $c $s; if ($r.ok) { Add-BoxAuthorizationWant $c $s $r.body } }
  'ENT-037' = { param($c, $s) $r = Get-BoxResolved $c $s; if ($r.ok) { Add-BoxAuthorizationWant $c $s $r.body } }
  # device registration (devices module)
  'ENT-039' = { param($c, $s) if (Test-BoxDevicesOn $c $s) { $r = Get-BoxResolved $c $s; if ($r.ok) { Add-BoxDeviceWant $c $s $r.body } } }
  'ENT-040' = { param($c, $s) if (Test-BoxDevicesOn $c $s) { $r = Get-BoxResolved $c $s; if ($r.ok) { Add-BoxDeviceWant $c $s $r.body }; Test-BoxRoleHolders $c $s } }
  'ENT-041' = { param($c, $s) if (Test-BoxDevicesOn $c $s) { $r = Get-BoxResolved $c $s; if ($r.ok) { Add-BoxDeviceWant $c $s $r.body } } }
  'ENT-042' = { param($c, $s) if (Test-BoxDevicesOn $c $s) { $r = Get-BoxResolved $c $s; if ($r.ok) { Add-BoxDeviceWant $c $s $r.body } } }
  # checks and reports
  'ENT-044' = { param($c, $s) Test-BoxAdminAccounts $c $s }
  'ENT-046' = { param($c, $s) Test-BoxPipelineSecrets $c $s }
  'ENT-047' = { param($c, $s) Read-BoxOldSecrets $c $s }
  'ENT-049' = { param($c, $s) Read-BoxReport $c $s 'sign-in and directory audit logs' @('/auditLogs/signIns?$top=1', '/auditLogs/directoryAudits?$top=1') }
  # M365 admin centre
  'ADM-001' = { param($c, $s) Sync-BoxAdminSetting $c $s "$script:Beta/admin/appsAndServices" }
  'ADM-003' = { param($c, $s) Sync-BoxIdleTimeout $c $s }
  'ADM-004' = { param($c, $s) Sync-BoxDomainExpiry $c $s }
  'ADM-005' = { param($c, $s) Sync-BoxAdminSetting $c $s "$script:Beta/admin/forms" }
  'ADM-007' = { param($c, $s) Sync-BoxServicePrincipalDisabled $c $s }
  'ADM-014' = { param($c, $s) Read-BoxReport $c $s 'secure score' @('/security/secureScores?$top=1') }
  # Teams (group creation) and Purview (log export source)
  'TMS-014' = { param($c, $s) $r = Get-BoxResolved $c $s; if ($r.ok) { Sync-BoxGroupSetting $c $s $r.body.templateId $r.body.values } }
  'PVW-002' = { param($c, $s) Read-BoxReport $c $s 'directory audit and sign-in logs (weekly export: T14-12)' @('/auditLogs/directoryAudits?$top=1', '/auditLogs/signIns?$top=1') }
}

function Test-BoxDevicesOn($Ctx, $S) {
  $mods = $Ctx.In.config.modules
  if ($mods -and -not (Get-BoxProp $mods 'devices')) { Add-BoxSkip $Ctx $S 'module devices is off'; return $false }
  return $true
}

function Invoke-BoxPolicyRun($In, [switch] $Apply) {
  # Returns the context after plan (or apply) of every setting the step got.
  $ctx = New-BoxPolicyContext $In ([bool]$Apply)
  # Errors on one setting (a refused call) do not stop the others; they are
  # reported as drift with the error text.
  $order = @($ctx.Settings | Sort-Object { if ($_.id -eq 'ENT-015') { 2 } elseif ($_.id -eq 'ENT-018') { 3 } else { 1 } }, id)
  # Every handler writes right after recording its change, so when a write
  # fails the last change recorded is the one that did not happen.
  foreach ($s in $order) {
    $h = $script:Handlers[[string]$s.id]
    if (-not $h) { Add-BoxDrift $ctx $s 'no handler' 'the identity provider does not implement this setting'; continue }
    $before = $ctx.Changes.Count
    try { & $h $ctx $s } catch {
      if ($Apply -and $ctx.Changes.Count -gt $before) { $ctx.Changes.RemoveAt($ctx.Changes.Count - 1) }
      Add-BoxDrift $ctx $s 'refused' "$_"
    }
  }
  foreach ($f in 'Sync-BoxAuthorizationPolicy', 'Sync-BoxDeviceRegistration') {
    $before = $ctx.Changes.Count
    try { & $f $ctx } catch {
      # One write per object: none of its changes happened.
      if ($Apply) { while ($ctx.Changes.Count -gt $before) { $ctx.Changes.RemoveAt($ctx.Changes.Count - 1) } }
      $ids = @(if ($f -like '*Authorization*') { $ctx.Cache['authz'] } else { $ctx.Cache['devreg'] }) | ForEach-Object { $_.s.id }
      $ctx.Drift.Add((New-BoxChange update "$($ids -join ',') refused" "$_"))
    }
  }
  foreach ($k in $ctx.Skipped) { Write-Warning "$($k.id) not applied: $($k.reason)" }
  return $ctx
}

function Get-BoxPolicyPlan($In) {
  # Drift the step cannot fix itself (a refused call, a person's account)
  # is not a change: it shows in the warnings here and fails verify.
  $c = Invoke-BoxPolicyRun $In
  foreach ($d in $c.Drift) { Write-Warning "$($d.target): $($d.detail)" }
  return @{ changes = @($c.Changes); skipped = @($c.Skipped) }
}
function Invoke-BoxPolicyApply($In) {
  $c = Invoke-BoxPolicyRun $In -Apply
  if ($c.Drift.Count) { foreach ($d in $c.Drift) { Write-Warning "$($d.target): $($d.detail)" } }
  return @{ changes = @($c.Changes); skipped = @($c.Skipped); drift = @($c.Drift) }
}
function Test-BoxPolicyVerify($In) {
  $c = Invoke-BoxPolicyRun $In
  $drift = @($c.Changes) + @($c.Drift)
  return @{ ok = ($drift.Count -eq 0); drift = $drift; skipped = @($c.Skipped); report = @($c.Report) }
}
function Get-BoxPolicyEvidence($In, [string] $Kind) {
  $c = Invoke-BoxPolicyRun $In
  $n = $c.Settings.Count; $skip = $c.Skipped.Count; $drift = $c.Changes.Count + $c.Drift.Count
  $items = @(@{ kind = $Kind; ref = "tenant:$Kind"; summary = "$($n - $skip) of $n resolved setting(s) applied; $skip left out ($(@($c.Skipped | ForEach-Object { $_.id }) -join ', ')); $drift drift" })
  foreach ($r in $c.Report) { $items += @{ kind = "$Kind-report"; ref = "tenant:$Kind"; summary = $r } }
  return @{ items = $items }
}

Export-ModuleMember -Function Set-BoxPolicyRoles, ConvertTo-BoxPlain, Compare-BoxValue, Resolve-BoxPayload, Get-BoxCAState,
  Merge-BoxGroupSettingValues, Get-BoxIdleTimeout, Get-BoxEntraClaims, Get-BoxPolicyPlan, Invoke-BoxPolicyApply, Test-BoxPolicyVerify, Get-BoxPolicyEvidence
