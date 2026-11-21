# policy.intune-windows and policy.sharepoint.intune (T14-05): the resolved
# Windows device settings, grouped into the named policies the mapping file
# expected-state/setting-ids/intune-windows.json describes. Dot-sourced by
# Box.intune.psm1 after IntunePolicy.ps1.
#
# Audit first (SPEC §20, T14-04): while policy.rollout.stage is not
# 'enforce', and for every policy that carries an audit or report setting,
# objects are assigned to the pilot group only. Tenant-wide defaults
# (enrolment status page, enrolment restriction) and the broad update ring
# wait for the enforce stage.

$script:WinMappingFile = Join-Path $PSScriptRoot '..' '..' '..' 'expected-state' 'setting-ids' 'intune-windows.json'
$script:WinDescription = 'Managed by box (T14-05). Changes made here are overwritten on the next run.'
$script:WinNotificationName = 'Box - Non-compliant device'
# Autopilot profile names and descriptions take letters, digits, spaces,
# '.' and '_' only: Graph answers a bare BadRequest for '-' or '(' (test
# tenant, 2026-11-21).
$script:WinAutopilotDescription = 'Managed by box. Changes made here are overwritten on the next run.'
$script:DeviceLocalAdminRole = '9f06204d-73c1-4d4c-880a-6edb90606fd8'   # Microsoft Entra Joined Device Local Administrator (template id)

# ---------- placeholders (pure part tested by Pester) ----------

function Resolve-WinPlaceholders {
  # Replaces {kind:name} placeholders in a payload's JSON text. $Values maps
  # a placeholder to its value, or to $null when it cannot be resolved.
  # Returns @{ json; missing = placeholders left unresolved }.
  param([string] $Json, [hashtable] $Values)
  $missing = @()
  foreach ($m in [regex]::Matches($Json, '\{(tenant|pattern|sid|config|group):[^{}"]*\}') | ForEach-Object { $_.Value } | Sort-Object -Unique) {
    if ($Values.ContainsKey($m) -and $null -ne $Values[$m] -and "$($Values[$m])" -ne '') {
      $v = ([string]$Values[$m]).Replace('\', '\\').Replace('"', '\"')
      $Json = $Json.Replace($m, $v)
    } else { $missing += $m }
  }
  return @{ json = $Json; missing = $missing }
}

function ConvertTo-WinDomainPattern([string[]] $Domains) {
  # Edge RestrictSigninToPattern: work accounts of the tenant's verified domains.
  $alt = @($Domains | Where-Object { $_ } | Sort-Object -Unique | ForEach-Object { [regex]::Escape($_.ToLowerInvariant()) }) -join '|'
  return ".*@($alt)$"
}

function Get-WinConfigValue($In, [string] $Path) {
  $o = $In.config
  foreach ($part in $Path -split '\.') { $o = Get-IntuneProp $o $part; if ($null -eq $o) { return $null } }
  return [string]$o
}

function Get-WinPlaceholderValues($In, [string[]] $Wanted) {
  $v = @{}
  foreach ($w in $Wanted) {
    switch -Regex ($w) {
      '^\{tenant:id\}$' { $v[$w] = [string]$In.tenant.id }
      '^\{pattern:company-accounts\}$' {
        $d = @(Get-BoxGraphAll '/domains?$select=id,isVerified' | Where-Object { $_.isVerified } | ForEach-Object { [string]$_.id })
        $v[$w] = if ($d.Count) { ConvertTo-WinDomainPattern $d } else { $null }
      }
      '^\{sid:role:device-local-administrator\}$' {
        $r = @(Get-BoxGraphAll "/directoryRoles?`$filter=roleTemplateId eq '$script:DeviceLocalAdminRole'&`$select=id")
        $v[$w] = if ($r.Count) { ConvertTo-IntuneSid ([string]$r[0].id) } else { $null }
      }
      '^\{config:(.+)\}$' { $v[$w] = Get-WinConfigValue $In $Matches[1] }
      default { $v[$w] = $null }
    }
  }
  return $v
}

$script:WinPlaceholderWhy = @{
  '{sid:role:device-local-administrator}' = 'the Entra role "Microsoft Entra Joined Device Local Administrator" is not activated in the tenant (no directoryRole object, so no SID); replacing the local Administrators group without it would remove every device administrator'
}

# ---------- desired state ----------

function Get-WinMapping {
  if (-not $script:WinMapping) { $script:WinMapping = Get-Content -Raw $script:WinMappingFile | ConvertFrom-Json -AsHashtable }
  return $script:WinMapping
}

function Get-WinModel($In) {
  if ($In.policy_error) { throw "policy resolution failed: $($In.policy_error)" }
  $stage = [string]$In.policy.rollout.stage
  $pilotName = [string]$In.policy.rollout.pilot_group
  if (-not $pilotName) { throw 'policy.rollout.pilot_group is empty' }
  $m = @{
    stage = $stage; pilot = (Get-IntuneGroupId $pilotName); pilotName = $pilotName
    catalog = [ordered]@{}; compliance = @{ props = [ordered]@{}; actions = $null; ids = @() }
    rings = [ordered]@{}; ringIds = @(); autopilot = $null; esp = $null; restriction = $null; feature = $null
    lapsEntra = $false; skipped = @(); held = @(); conflicts = @(); applied = [ordered]@{}
  }
  $settings = @($In.policy.settings | Where-Object { $_ })
  $all = ($settings | ForEach-Object { $_.payload | ConvertTo-Json -Depth 64 -Compress }) -join ' '
  $wanted = @([regex]::Matches($all, '\{(tenant|pattern|sid|config|group):[^{}"]*\}') | ForEach-Object { $_.Value } | Sort-Object -Unique)
  $values = Get-WinPlaceholderValues $In $wanted
  foreach ($s in $settings) {
    $p = ConvertTo-IntuneHashtable $s.payload
    # The audit form of a setting without an audit mode only changes the assignment: use the payload.
    if ($p -is [System.Collections.IDictionary] -and $p.Contains('assignment') -and $p.settings -is [string]) {
      $p = ConvertTo-IntuneHashtable (Get-WinMapping)[$s.id].payload
    }
    # An audit form that lists only the settings (ASR rules) keeps the payload's policy.
    if ($p -is [System.Collections.IDictionary] -and $p.settings -is [array] -and -not $p.Contains('policy')) {
      $p.policy = ConvertTo-IntuneHashtable (Get-WinMapping)[$s.id].payload.policy
    }
    $r = Resolve-WinPlaceholders ($p | ConvertTo-Json -Depth 64 -Compress) $values
    if ($r.missing.Count) {
      $why = @($r.missing | ForEach-Object { if ($script:WinPlaceholderWhy.ContainsKey($_)) { $script:WinPlaceholderWhy[$_] } elseif ($_ -like '{config:*') { "configuration value $($_.Trim('{}') -replace '^config:', '') is empty" } else { "$_ cannot be resolved" } })
      $m.skipped += @{ id = $s.id; name = $s.name; reason = ($why -join '; ') }
      continue
    }
    $p = $r.json | ConvertFrom-Json -AsHashtable
    $audit = $s.mode -in 'audit', 'report'
    $type = [string]$p['@odata.type']
    if ($type -eq '#microsoft.graph.windows10CompliancePolicy') {
      foreach ($k in $p.Keys) {
        if ($k -in '@odata.type', 'displayName') { continue }
        if ($k -eq 'scheduledActionsForRule') { $m.compliance.actions = $p[$k]; continue }
        $m.compliance.props[$k] = $p[$k]
      }
      $m.compliance.ids += $s.id
      $m.applied[$s.id] = 'Box - Windows - Compliance'
    } elseif ($p.Contains('policy') -and $p.settings -is [array]) {
      Add-WinCatalog $m $p.policy $p.settings $s.id $audit
    } elseif ($p.Contains('store_app')) {
      # WIN-058: Store app off, automatic app updates on (SC-22).
      $mk = { param($def, $opt) @{ '@odata.type' = '#microsoft.graph.deviceManagementConfigurationSetting'; settingInstance = @{
        '@odata.type' = '#microsoft.graph.deviceManagementConfigurationChoiceSettingInstance'; settingDefinitionId = $def
        choiceSettingValue = @{ value = $opt; children = @() } } } }
      $list = @((& $mk $p.store_app.settingDefinitionId $p.store_app.choice))
      if ($p.available.automatic_updates) { $list += (& $mk $p.available.automatic_updates.settingDefinitionId $p.available.automatic_updates.optionId) }
      Add-WinCatalog $m @{ name = 'Box - Windows - Microsoft Store'; platforms = 'windows10'; technologies = 'mdm' } $list $s.id $audit
    } elseif ($p.Contains('pilot') -and $p.Contains('broad')) {
      foreach ($ring in 'pilot', 'broad') {
        if (-not $m.rings.Contains($ring)) { $m.rings[$ring] = [ordered]@{} }
        foreach ($k in $p[$ring].Keys) { $m.rings[$ring][$k] = $p[$ring][$k] }
      }
      $m.ringIds += $s.id
      $m.applied[$s.id] = 'Box - Windows - Update ring pilot / broad'
    } elseif ($type -eq '#microsoft.graph.azureADWindowsAutopilotDeploymentProfile') {
      $m.autopilot = @{ props = $p; id = $s.id }; $m.applied[$s.id] = $p.displayName
    } elseif ($type -eq '#microsoft.graph.windows10EnrollmentCompletionPageConfiguration') {
      $m.esp = @{ props = $p; id = $s.id }; $m.applied[$s.id] = 'default enrolment status page'
    } elseif ($type -eq '#microsoft.graph.deviceEnrollmentPlatformRestrictionsConfiguration') {
      $m.restriction = @{ props = $p; id = $s.id }; $m.applied[$s.id] = 'default enrolment restriction'
    } elseif ($p.Contains('featureUpdateVersion')) {
      $m.feature = @{ props = $p; id = $s.id }; $m.applied[$s.id] = $p.displayName
    } else {
      $m.skipped += @{ id = $s.id; name = $s.name; reason = 'payload form not recognised by the step' }
    }
    if ($s.id -eq 'WIN-036') { $m.lapsEntra = $true }
  }
  return $m
}

function Add-WinCatalog($m, $Policy, $Settings, [string] $Id, [bool] $Audit) {
  $name = [string]$Policy.name
  if (-not $m.catalog.Contains($name)) {
    $m.catalog[$name] = @{ policy = (ConvertTo-IntuneHashtable $Policy); map = [ordered]@{}; ids = @(); audit = $false; user = $false }
  }
  $e = $m.catalog[$name]
  $r = Merge-IntuneSettings -Map $e.map -Settings @($Settings)
  $m.conflicts += @($r.conflicts | ForEach-Object { "${Id}: $_" })
  $e.ids += $Id
  if ($Audit) { $e.audit = $true }
  if (@($Settings | Where-Object { $_.settingInstance.settingDefinitionId -like 'user_*' }).Count) { $e.user = $true }
  $m.applied[$Id] = $name
}

function Get-WinTargets($m, [bool] $Audit, [bool] $User) {
  if ($m.stage -ne 'enforce' -or $Audit) { return @("group:$($m.pilot)") }
  $t = @('allDevices'); if ($User) { $t += 'allUsers' }
  return $t
}

# ---------- sections ----------

$script:WinBlocked = @()
$script:WinFailed = @()

function Invoke-WinSection([string] $Label, [scriptblock] $Body) {
  # Runs one object's plan/apply. A permission refusal, or a prerequisite
  # the tenant lacks (Defender for Endpoint connection), is recorded and the
  # other objects go on; any other apply failure is recorded and thrown at
  # the end of apply (Get-WinChanges).
  try { return @(& $Body) }
  catch {
    $msg = "$_"
    if (Test-IntuneForbidden $msg) {
      $short = if ($msg -match 'must have one of the following scopes: ([^-]+?)\s+-') { "needs one of: $($Matches[1].Trim())" } elseif ($msg -match '(Forbidden|Authorization_RequestDenied)[^\r\n]{0,160}') { $Matches[0] } else { $msg.Substring(0, [Math]::Min(200, $msg.Length)) }
      $script:WinBlocked += "${Label}: refused ($short)"
      Write-Warning "${Label}: refused by Graph ($short)"
      return @()
    }
    if ($msg -match 'onboarding_fromconnector|tamperprotection_blob') {
      $script:WinBlocked += "${Label}: needs the Microsoft Defender for Endpoint connection to Intune (Defender portal > Settings > Endpoints > Advanced features > Microsoft Intune connection; no API, proposed gate): Graph requires the onboarding data the connector supplies"
      Write-Warning "${Label}: prerequisite missing: Defender for Endpoint connection to Intune"
      return @()
    }
    if (-not $Apply) { throw }
    $first = (@($msg -split "`n" | Where-Object { $_ -match 'Message|failed' } | Select-Object -First 2) -join ' ').Trim()
    $script:WinFailed += "${Label}: $first"
    Write-Warning "${Label}: failed: $first"
    return @()
  }
}

function Assert-WinDefenderConnector {
  # EDR onboarding "auto from connector" needs the Defender for Endpoint
  # connector enabled for Windows; Graph refuses the policy without it.
  $c = Get-BoxGraphOne "$script:IntuneBeta/mobileThreatDefenseConnectors/fc780465-2017-40d4-a0c5-307022471b92"
  if (-not $c -or $c.partnerState -ne 'enabled' -or -not $c.windowsEnabled) {
    throw 'onboarding_fromconnector: the Defender for Endpoint connector is not enabled for Windows'
  }
}

function Get-WinNotificationTemplateId([switch] $Apply, [ref] $Changes) {
  # The template carries no locale on create: Graph answers a bare
  # BadRequest when defaultLocale is sent (on POST and PATCH). Posting the
  # default localized message sets defaultLocale. Plan reports the create
  # and returns '' (no id yet).
  $t = Find-IntuneObject -Collection 'notificationMessageTemplates' -Name $script:WinNotificationName
  $created = -not $t
  if ($created) {
    $Changes.Value += New-BoxChange create "notification message template $script:WinNotificationName" 'e-mail to the user when a device is not compliant (WIN-011)'
    if (-not $Apply) { return '' }
    $t = Invoke-BoxGraph -Method POST -Uri "$script:IntuneBeta/notificationMessageTemplates" -Body @{
      displayName = $script:WinNotificationName; roleScopeTagIds = @('0')
      brandingOptions = 'includeCompanyLogo,includeCompanyName,includeContactInformation' }
  }
  # A new template (plan) has no id yet: the default message comes with it.
  $msgs = if ($t.id) { @((Invoke-BoxGraph -Uri "$script:IntuneBeta/notificationMessageTemplates/$($t.id)?`$expand=localizedNotificationMessages").localizedNotificationMessages | Where-Object { $_ }) } else { @() }
  if ($t.id -and -not @($msgs | Where-Object { $_.isDefault }).Count) {
    if (-not $created) { $Changes.Value += New-BoxChange update "notification message template $script:WinNotificationName" 'default message (en-us) missing' }
    if ($Apply) {
      Invoke-BoxGraph -Method POST -Uri "$script:IntuneBeta/notificationMessageTemplates/$($t.id)/localizedNotificationMessages" -Body @{
        locale = 'en-us'; isDefault = $true; subject = 'Your device does not meet the company security policy'
        messageTemplate = 'Your device no longer meets the company security policy and will lose access to company data in 3 days. Open the Company Portal app to see what to fix.' } | Out-Null
    }
  }
  return [string]$t.id
}

function Sync-WinCompliance($m, [switch] $Apply) {
  $changes = @()
  $c = $m.compliance
  $name = 'Box - Windows - Compliance'
  $actions = @()
  if ($c.actions) {
    $json = $c.actions | ConvertTo-Json -Depth 16 -Compress
    $noTemplate = $false
    if ($json -match '\{notificationTemplate:') {
      try {
        $tid = Get-WinNotificationTemplateId -Apply:$Apply -Changes ([ref]$changes)
      } catch {
        # Keep the block action and report the refusal.
        $script:WinBlocked += "WIN-011 notification message template $($script:WinNotificationName): refused ($(("$_" -split "`n")[0].Substring(0, [Math]::Min(120, ("$_" -split "`n")[0].Length)))); the e-mail action for non-compliance is left out"
        Write-Warning "WIN-011: notification message template could not be created: $(("$_" -split "`n")[0])"
        $tid = ''; $noTemplate = $true
      }
      $json = $json -replace '\{notificationTemplate:[^}]*\}', $tid
    }
    $actions = @($json | ConvertFrom-Json -AsHashtable)
    if ($noTemplate) { foreach ($a in $actions) { $a.scheduledActionConfigurations = @($a.scheduledActionConfigurations | Where-Object { $_.actionType -ne 'notification' }) } }
  }
  $wantActs = @($actions | ForEach-Object { $_.scheduledActionConfigurations } | ForEach-Object { "$($_.actionType):$($_.gracePeriodHours)" })
  $cur = Find-IntuneObject -Collection 'deviceCompliancePolicies' -Name $name
  $targets = Get-WinTargets $m $false $false
  if (-not $cur) {
    $changes += New-BoxChange create "policy $name" "$($c.props.Count) propert(ies) from $($c.ids -join ', ')"
    if (-not $Apply) { return $changes }
    $body = [ordered]@{ '@odata.type' = '#microsoft.graph.windows10CompliancePolicy'; displayName = $name; description = $script:WinDescription; roleScopeTagIds = @('0') }
    foreach ($k in $c.props.Keys) { $body[$k] = $c.props[$k] }
    $body.scheduledActionsForRule = @($actions)
    if (-not $actions.Count) { $body.scheduledActionsForRule = @(@{ ruleName = 'PasswordRequired'; scheduledActionConfigurations = @(@{ actionType = 'block'; gracePeriodHours = 0; notificationTemplateId = ''; notificationMessageCCList = @() }) }) }
    $cur = Invoke-BoxGraph -Method POST -Uri "$script:IntuneBeta/deviceCompliancePolicies" -Body $body
  } else {
    $full = Invoke-BoxGraph -Uri "$script:IntuneBeta/deviceCompliancePolicies/$($cur.id)?`$expand=scheduledActionsForRule(`$expand=scheduledActionConfigurations)"
    $diff = @(Compare-IntuneProps $c.props $full)
    if ($diff.Count) {
      $changes += New-BoxChange update "policy $name" "properties: $($diff -join ', ')"
      if ($Apply) {
        $patch = [ordered]@{ '@odata.type' = '#microsoft.graph.windows10CompliancePolicy' }
        foreach ($k in $diff) { $patch[$k] = $c.props[$k] }
        Invoke-BoxGraph -Method PATCH -Uri "$script:IntuneBeta/deviceCompliancePolicies/$($cur.id)" -Body $patch | Out-Null
      }
    }
    $haveActs = @($full.scheduledActionsForRule | ForEach-Object { $_.scheduledActionConfigurations } | ForEach-Object { "$($_.actionType):$($_.gracePeriodHours)" })
    $tplOk = $true
    foreach ($a in @($actions | ForEach-Object { $_.scheduledActionConfigurations } | Where-Object { $_.actionType -eq 'notification' })) {
      $h = @($full.scheduledActionsForRule | ForEach-Object { $_.scheduledActionConfigurations } | Where-Object { $_.actionType -eq 'notification' })
      if (-not $h.Count -or $h[0].notificationTemplateId -ne $a.notificationTemplateId) { $tplOk = $false }
    }
    if ($actions.Count -and (-not (Test-IntuneSameSet $haveActs $wantActs) -or -not $tplOk)) {
      $changes += New-BoxChange update "actions for non-compliance of $name" "$($haveActs -join ', ') -> $($wantActs -join ', ')"
      if ($Apply) {
        Invoke-BoxGraph -Method POST -Uri "$script:IntuneBeta/deviceCompliancePolicies/$($cur.id)/scheduleActionsForRules" -Body @{ deviceComplianceScheduledActionForRules = $actions } | Out-Null
      }
    }
  }
  $haveT = @(Get-IntuneAssignmentKeys 'deviceCompliancePolicies' $cur.id)
  if (-not (Test-IntuneSameSet $haveT $targets)) {
    $changes += New-BoxChange update "assignment of $name" "$($haveT -join ', ') -> $($targets -join ', ')"
    if ($Apply) { Set-IntuneAssignments 'deviceCompliancePolicies' $cur.id $targets }
  }
  return $changes
}

function Sync-WinClassic {
  # A classic Intune object (update ring, feature update profile): found by
  # display name, created, patched by property, assigned.
  param([string] $Collection, $Props, [string[]] $Targets, [string] $Detail, [switch] $Apply)
  $changes = @()
  $name = [string]$Props.displayName
  $cur = Find-IntuneObject -Collection $Collection -Name $name
  $want = [ordered]@{}; foreach ($k in $Props.Keys) { if ($k -ne 'displayName') { $want[$k] = $Props[$k] } }
  if (-not $cur) {
    $changes += New-BoxChange create "$name" $Detail
    if (-not $Apply) { return $changes }
    $body = [ordered]@{}; foreach ($k in $Props.Keys) { $body[$k] = $Props[$k] }
    $body.description = $script:WinDescription; $body.roleScopeTagIds = @('0')
    $cur = Invoke-BoxGraph -Method POST -Uri "$script:IntuneBeta/$Collection" -Body $body
  } else {
    $full = Invoke-BoxGraph -Uri "$script:IntuneBeta/$Collection/$($cur.id)"
    $diff = @(Compare-IntuneProps $want $full)
    if ($diff.Count) {
      $changes += New-BoxChange update "$name" "properties: $($diff -join ', ')"
      if ($Apply) {
        $patch = [ordered]@{}; if ($Props.Contains('@odata.type')) { $patch['@odata.type'] = $Props['@odata.type'] }
        foreach ($k in $diff) { $top = ($k -split '\.')[0]; $patch[$top] = $Props[$top] }
        Invoke-BoxGraph -Method PATCH -Uri "$script:IntuneBeta/$Collection/$($cur.id)" -Body $patch | Out-Null
      }
    }
  }
  $haveT = @(Get-IntuneAssignmentKeys $Collection $cur.id)
  if (-not (Test-IntuneSameSet $haveT $Targets)) {
    $changes += New-BoxChange update "assignment of $name" "$($haveT -join ', ') -> $(@($Targets) -join ', ')"
    if ($Apply) { Set-IntuneAssignments $Collection $cur.id $Targets }
  }
  return $changes
}

function Sync-WinAutopilot($m, [switch] $Apply) {
  $p = $m.autopilot.props
  $changes = @()
  $name = [string]$p.displayName
  if ($name -notmatch '^[A-Za-z0-9 ._]+$') { throw "Autopilot profile name '$name' has a character Autopilot refuses (letters, digits, spaces, '.' and '_' only)" }
  $cur = Find-IntuneObject -Collection 'windowsAutopilotDeploymentProfiles' -Name $name
  $want = [ordered]@{}; foreach ($k in $p.Keys) { if ($k -ne 'displayName') { $want[$k] = $p[$k] } }
  if (-not $cur) {
    $changes += New-BoxChange create $name 'user account type standard'
    if (-not $Apply) { return $changes }
    $body = [ordered]@{}; foreach ($k in $p.Keys) { $body[$k] = $p[$k] }; $body.description = $script:WinAutopilotDescription
    $cur = Invoke-BoxGraph -Method POST -Uri "$script:IntuneBeta/windowsAutopilotDeploymentProfiles" -Body $body
  } else {
    $diff = @(Compare-IntuneProps $want (Invoke-BoxGraph -Uri "$script:IntuneBeta/windowsAutopilotDeploymentProfiles/$($cur.id)"))
    if ($diff.Count) {
      $changes += New-BoxChange update $name "properties: $($diff -join ', ')"
      if ($Apply) { Invoke-BoxGraph -Method PATCH -Uri "$script:IntuneBeta/windowsAutopilotDeploymentProfiles/$($cur.id)" -Body $p | Out-Null }
    }
  }
  # Autopilot profiles target device groups; all devices only at the enforce stage.
  $targets = if ($m.stage -eq 'enforce') { @('allDevices') } else { @() }
  $haveT = @(Get-IntuneAssignmentKeys 'windowsAutopilotDeploymentProfiles' $cur.id)
  if (-not (Test-IntuneSameSet $haveT $targets)) {
    $changes += New-BoxChange update "assignment of $name" "$($haveT -join ', ') -> $($targets -join ', ')"
    if ($Apply) {
      foreach ($t in $targets) { if ($t -notin $haveT) { Invoke-BoxGraph -Method POST -Uri "$script:IntuneBeta/windowsAutopilotDeploymentProfiles/$($cur.id)/assignments" -Body @{ target = (New-IntuneTarget $t) } | Out-Null } }
    }
  }
  if ($m.stage -ne 'enforce') { $m.held += "$($m.autopilot.id): profile $name is not assigned to all devices until rollout stage enforce" }
  return $changes
}

function Sync-WinEnrolmentDefault($m, $Item, [string] $IdSuffix, [string] $Label, [switch] $Apply) {
  # Tenant-wide defaults apply to every user: changed at the enforce stage only.
  $all = @(Get-BoxGraphAll "$script:IntuneBeta/deviceEnrollmentConfigurations")
  $cur = @($all | Where-Object { ([string]$_.id).EndsWith($IdSuffix) })
  if ($cur.Count -ne 1) { throw "$Label not found (id ending $IdSuffix)" }
  $want = [ordered]@{}; foreach ($k in $Item.props.Keys) { $want[$k] = $Item.props[$k] }
  $diff = @(Compare-IntuneProps $want $cur[0])
  if (-not $diff.Count) { return @() }
  if ($m.stage -ne 'enforce') {
    $m.held += "$($Item.id): $Label differs ($($diff -join ', ')); it applies to every user, so it changes at rollout stage enforce"
    return @()
  }
  if ($Apply) { Invoke-BoxGraph -Method PATCH -Uri "$script:IntuneBeta/deviceEnrollmentConfigurations/$($cur[0].id)" -Body $Item.props | Out-Null }
  return @(New-BoxChange update $Label "properties: $($diff -join ', ')")
}

function Sync-WinLapsDirectory([switch] $Apply) {
  # Prerequisite of WIN-036: Entra device setting "Enable Microsoft Entra LAPS".
  $cur = Invoke-BoxGraph -Uri 'https://graph.microsoft.com/beta/policies/deviceRegistrationPolicy'
  if ($cur.localAdminPassword.isEnabled) { return @() }
  if (-not (Test-IntuneTokenRole 'Policy.ReadWrite.DeviceConfiguration')) {
    throw 'Forbidden: needs one of: Policy.ReadWrite.DeviceConfiguration (in the devices bundle, not yet granted) - Entra LAPS is off'
  }
  if ($Apply) {
    $body = ConvertTo-IntuneHashtable $cur
    $body.Remove('@odata.context')
    $body.localAdminPassword = @{ isEnabled = $true }
    Invoke-BoxGraph -Method PUT -Uri 'https://graph.microsoft.com/beta/policies/deviceRegistrationPolicy' -Body $body | Out-Null
  }
  return @(New-BoxChange update 'directory device setting: Microsoft Entra LAPS' 'off -> on (needed by WIN-036)')
}

function Get-WinChanges($In, [switch] $Apply) {
  $script:WinBlocked = @()
  $script:WinFailed = @()
  $m = Get-WinModel $In
  $script:WinModel = $m
  $changes = @()
  foreach ($c in $m.conflicts) { Write-Warning "settings merge conflict: $c" }
  if ($m.compliance.ids.Count) {
    $changes += Invoke-WinSection "Box - Windows - Compliance ($($m.compliance.ids -join ', '))" { Sync-WinCompliance $m -Apply:$Apply }
  }
  foreach ($name in $m.catalog.Keys) {
    $e = $m.catalog[$name]
    $targets = Get-WinTargets $m $e.audit $e.user
    $fromConnector = ($e.map.Values | ConvertTo-Json -Depth 64 -Compress) -match '_autofromconnector'
    $changes += Invoke-WinSection "$name ($($e.ids -join ', '))" {
      if ($fromConnector) { Assert-WinDefenderConnector }
      Sync-IntuneCatalogPolicy -Policy $e.policy -Map $e.map -Targets $targets -Description $script:WinDescription -Detail ($e.ids -join ', ') -Apply:$Apply }
  }
  if ($m.lapsEntra) { $changes += Invoke-WinSection 'Entra LAPS device setting (WIN-036)' { Sync-WinLapsDirectory -Apply:$Apply } }
  foreach ($ring in $m.rings.Keys) {
    $targets = if ($ring -eq 'pilot') { @("group:$($m.pilot)") } elseif ($m.stage -eq 'enforce') { @('allDevices', "exclude:$($m.pilot)") } else { @() }
    if ($ring -eq 'broad' -and $m.stage -ne 'enforce') { $m.held += "$($m.ringIds -join ', '): the broad update ring is not assigned until rollout stage enforce" }
    $props = $m.rings[$ring]
    $changes += Invoke-WinSection "$($props.displayName) ($($m.ringIds -join ', '))" {
      Sync-WinClassic -Collection 'deviceConfigurations' -Props $props -Targets $targets -Detail ($m.ringIds -join ', ') -Apply:$Apply }
  }
  if ($m.feature) {
    $changes += Invoke-WinSection "$($m.feature.props.displayName) ($($m.feature.id))" {
      Sync-WinClassic -Collection 'windowsFeatureUpdateProfiles' -Props $m.feature.props -Targets (Get-WinTargets $m $false $false) -Detail $m.feature.id -Apply:$Apply }
  }
  if ($m.autopilot) { $changes += Invoke-WinSection "Autopilot profile ($($m.autopilot.id))" { Sync-WinAutopilot $m -Apply:$Apply } }
  if ($m.esp) { $changes += Invoke-WinSection "default enrolment status page ($($m.esp.id))" { Sync-WinEnrolmentDefault $m $m.esp 'DefaultWindows10EnrollmentCompletionPageConfiguration' 'default enrolment status page' -Apply:$Apply } }
  if ($m.restriction) { $changes += Invoke-WinSection "default enrolment restriction ($($m.restriction.id))" { Sync-WinEnrolmentDefault $m $m.restriction '_DefaultPlatformRestrictions' 'default enrolment restriction' -Apply:$Apply } }
  foreach ($s in $m.skipped) { Write-Warning "not applied: $($s.id) $($s.name): $($s.reason)" }
  foreach ($h in $m.held) { Write-Warning "staged: $h" }
  if ($script:WinFailed.Count) { throw "$($script:WinFailed.Count) object(s) failed: $($script:WinFailed -join ' | ')" }
  return @($changes | Where-Object { $_ })
}

function Get-WinVerify($In) {
  $c = @(Get-WinChanges $In)
  $m = $script:WinModel
  $drift = @($c) + @($script:WinBlocked | ForEach-Object { New-BoxChange update $_ 'permission missing; granted with the devices bundle' }) +
    @($m.skipped | ForEach-Object { New-BoxChange update "setting $($_.id)" "not applied: $($_.reason)" })
  return @{ ok = ($drift.Count -eq 0); drift = $drift }
}

function Get-WinEvidence($In) {
  $null = @(Get-WinChanges $In)
  $m = $script:WinModel
  $items = @(@{ kind = 'intune-windows'; ref = 'tenant:intune-windows'
    summary = "$($m.applied.Count) setting(s) in $($m.catalog.Count + [int][bool]$m.compliance.ids.Count + $m.rings.Count) Box policies; stage $($m.stage), assigned to $($m.pilotName); not applied: $(@($m.skipped).Count); refused: $(@($script:WinBlocked).Count); staged: $(@($m.held).Count)" })
  $blockedText = $script:WinBlocked -join ' '
  foreach ($id in $m.applied.Keys) {
    $obj = $m.applied[$id]
    $state = if ($blockedText -like "*$id*") { 'refused (permission)' } else { 'matching' }
    $items += @{ kind = 'policy-setting'; ref = "setting:$id"; summary = "$obj`: $state" }
  }
  foreach ($s in $m.skipped) { $items += @{ kind = 'policy-setting'; ref = "setting:$($s.id)"; summary = "not applied: $($s.reason)" } }
  return @{ items = $items }
}

# ---------- policy.sharepoint.intune (SPO-011) ----------

$script:SpoPolicyName = 'Box - Windows - OneDrive sync restriction'
$script:SpoAllowList = 'device_vendor_msft_policy_config_onedrivengscv2~policy~onedrivengsc_allowtenantlist'

function New-SpoTenantListSetting([string] $TenantId) {
  return @{ '@odata.type' = '#microsoft.graph.deviceManagementConfigurationSetting'; settingInstance = @{
    '@odata.type' = '#microsoft.graph.deviceManagementConfigurationChoiceSettingInstance'; settingDefinitionId = $script:SpoAllowList
    choiceSettingValue = @{ value = "$($script:SpoAllowList)_1"; children = @(@{
      '@odata.type' = '#microsoft.graph.deviceManagementConfigurationSimpleSettingCollectionInstance'; settingDefinitionId = "$($script:SpoAllowList)_allowtenantlistbox"
      simpleSettingCollectionValue = @(@{ '@odata.type' = '#microsoft.graph.deviceManagementConfigurationStringSettingValue'; value = $TenantId }) }) } } }
}

function Get-SpoChanges($In, [switch] $Apply) {
  if ($In.policy_error) { throw "policy resolution failed: $($In.policy_error)" }
  $s = @($In.policy.settings | Where-Object { $_.id -eq 'SPO-011' })
  if (-not $s.Count) { return @() }
  $tid = [string]$In.tenant.id
  if (-not $tid) { throw 'tenant id unknown' }
  $leaves = Get-IntuneSettingLeaves @(New-SpoTenantListSetting $tid)
  # Covered when another Box Windows policy (WIN-072, policy.intune-windows) carries the same allow list.
  $own = $null; $coveredBy = $null
  foreach ($p in @(Get-BoxGraphAll "$script:IntuneBeta/configurationPolicies?`$select=id,name,platforms" | Where-Object { $_.name -like 'Box - Windows - *' })) {
    if ($p.name -eq $script:SpoPolicyName) { $own = $p; continue }
    $have = Get-IntuneSettingLeaves (Get-IntuneCatalogSettings $p.id)
    if (@($leaves | Where-Object { $_ -notin $have }).Count -eq 0 -and @(Get-IntuneAssignmentKeys 'configurationPolicies' $p.id).Count) { $coveredBy = $p.name; break }
  }
  $script:SpoCoveredBy = $coveredBy
  if ($coveredBy) {
    if ($own) {
      if ($Apply) { Invoke-BoxGraph -Method DELETE -Uri "$script:IntuneBeta/configurationPolicies/$($own.id)" | Out-Null }
      return @(New-BoxChange delete "policy $script:SpoPolicyName" "the allow list is carried by $coveredBy")
    }
    return @()
  }
  $stage = [string]$In.policy.rollout.stage
  $pilot = Get-IntuneGroupId ([string]$In.policy.rollout.pilot_group)
  $audit = $s[0].mode -in 'audit', 'report'
  $targets = if ($stage -ne 'enforce' -or $audit) { @("group:$pilot") } else { @('allDevices') }
  $map = (Merge-IntuneSettings -Map ([ordered]@{}) -Settings @(New-SpoTenantListSetting $tid)).map
  return @(Sync-IntuneCatalogPolicy -Policy @{ name = $script:SpoPolicyName; platforms = 'windows10'; technologies = 'mdm' } -Map $map -Targets $targets -Description $script:WinDescription -Detail 'SPO-011' -Apply:$Apply)
}

$script:Steps['policy.intune-windows'] = @{
  test = { param($In)
    if ($In.policy_error) { return @{ ok = $false; reason = "policy resolution failed: $($In.policy_error)" } }
    if (-not @($In.policy.settings | Where-Object { $_ }).Count) { return @{ ok = $false; reason = 'no resolved settings for policy.intune-windows' } }
    return @{ ok = $true } }
  plan = { param($In) @{ changes = @(Get-WinChanges $In) } }
  apply = { param($In) @{ changes = @(Get-WinChanges $In -Apply) } }
  verify = { param($In) Get-WinVerify $In }
  evidence = { param($In) Get-WinEvidence $In }
}

$script:Steps['policy.sharepoint.intune'] = @{
  test = { param($In) if ($In.policy_error) { @{ ok = $false; reason = "policy resolution failed: $($In.policy_error)" } } else { @{ ok = $true } } }
  plan = { param($In) @{ changes = @(Get-SpoChanges $In) } }
  apply = { param($In) @{ changes = @(Get-SpoChanges $In -Apply) } }
  verify = { param($In) $c = @(Get-SpoChanges $In); @{ ok = ($c.Count -eq 0); drift = $c } }
  evidence = { param($In)
    $null = @(Get-SpoChanges $In)
    $by = if ($script:SpoCoveredBy) { $script:SpoCoveredBy } else { $script:SpoPolicyName }
    @{ items = @(@{ kind = 'onedrive-sync-restriction'; ref = 'setting:SPO-011'; summary = "OneDrive AllowTenantList = this tenant only, in $by; device condition by Conditional Access is ENT-004 (identity)" }) } }
}
