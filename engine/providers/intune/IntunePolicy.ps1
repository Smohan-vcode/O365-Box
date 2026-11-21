# Shared helpers for the intune policy steps (T14-05): settings-catalogue
# policies (merge, compare, create, replace), classic Intune objects found by
# display name, and assignments. Dot-sourced by Box.intune.psm1. Graph over
# REST only (common/BoxGraph.psm1).

$script:IntuneBeta = 'https://graph.microsoft.com/beta/deviceManagement'

# ---------- pure helpers (tested by Pester) ----------

function ConvertTo-IntuneHashtable($Object) {
  # Deep copy of a JSON-shaped object as ordered hashtables and arrays.
  if ($null -eq $Object) { return $null }
  return ($Object | ConvertTo-Json -Depth 64 -Compress | ConvertFrom-Json -AsHashtable)
}

function Get-IntuneProp($Object, [string] $Name) {
  if ($null -eq $Object) { return $null }
  if ($Object -is [System.Collections.IDictionary]) { if ($Object.Contains($Name)) { return $Object[$Name] }; return $null }
  $p = $Object.PSObject.Properties[$Name]
  if ($p) { return $p.Value }
  return $null
}

function Get-IntuneSettingLeaves {
  # Flattens settings-catalogue setting instances into sorted "definitionId=value"
  # strings, so a desired list and the list Graph returns compare equal
  # whatever the order, template references or null fields.
  param($Instances)
  $out = [System.Collections.Generic.List[string]]::new()
  $walk = $null
  $walk = {
    param($i)
    if ($null -eq $i) { return }
    $inst = Get-IntuneProp $i 'settingInstance'
    if ($inst) { $i = $inst }
    $def = [string](Get-IntuneProp $i 'settingDefinitionId')
    $choice = Get-IntuneProp $i 'choiceSettingValue'
    $simple = Get-IntuneProp $i 'simpleSettingValue'
    $simpleColl = Get-IntuneProp $i 'simpleSettingCollectionValue'
    $choiceColl = Get-IntuneProp $i 'choiceSettingCollectionValue'
    $groupColl = Get-IntuneProp $i 'groupSettingCollectionValue'
    $group = Get-IntuneProp $i 'groupSettingValue'
    if ($null -ne $choice) {
      $out.Add("$def=$(Get-IntuneProp $choice 'value')")
      foreach ($c in @(Get-IntuneProp $choice 'children')) { & $walk $c }
    } elseif ($null -ne $simple) {
      $out.Add("$def=$(Get-IntuneProp $simple 'value')")
    } elseif ($null -ne $simpleColl) {
      $vals = @($simpleColl | ForEach-Object { [string](Get-IntuneProp $_ 'value') } | Sort-Object)
      $out.Add("$def=[$($vals -join ',')]")
    } elseif ($null -ne $choiceColl) {
      $vals = @($choiceColl | ForEach-Object { [string](Get-IntuneProp $_ 'value') } | Sort-Object)
      $out.Add("$def=[$($vals -join ',')]")
      foreach ($v in @($choiceColl)) { foreach ($c in @(Get-IntuneProp $v 'children')) { & $walk $c } }
    } elseif ($null -ne $groupColl) {
      $out.Add("$def#=$(@($groupColl).Count)")
      foreach ($v in @($groupColl)) { foreach ($c in @(Get-IntuneProp $v 'children')) { & $walk $c } }
    } elseif ($null -ne $group) {
      foreach ($c in @(Get-IntuneProp $group 'children')) { & $walk $c }
    } elseif ($def) {
      $out.Add("$def=")
    }
  }
  foreach ($i in @($Instances)) { & $walk $i }
  $arr = $out.ToArray()
  [Array]::Sort($arr, [StringComparer]::Ordinal)
  return @($arr)
}

function Merge-IntuneChildren {
  # Merges child instances (by settingDefinitionId) of two lists; returns the
  # merged list and adds conflicting values to $Conflicts.
  param($Into, $From, [System.Collections.Generic.List[string]] $Conflicts)
  $list = [System.Collections.Generic.List[object]]::new()
  foreach ($c in @($Into)) { if ($null -ne $c) { $list.Add($c) } }
  foreach ($c in @($From)) {
    if ($null -eq $c) { continue }
    $same = $null
    foreach ($x in $list) { if ($x.settingDefinitionId -eq $c.settingDefinitionId) { $same = $x; break } }
    if ($null -eq $same) { $list.Add($c) } else { Merge-IntuneInstance -Into $same -From $c -Conflicts $Conflicts }
  }
  return , $list.ToArray()
}

function Merge-IntuneInstance {
  # Merges setting instance $From into $Into (hashtables, same definition id).
  param([System.Collections.IDictionary] $Into, [System.Collections.IDictionary] $From, [System.Collections.Generic.List[string]] $Conflicts)
  if ($Into.Contains('choiceSettingValue') -and $From.Contains('choiceSettingValue')) {
    if ($Into.choiceSettingValue.value -ne $From.choiceSettingValue.value) {
      $Conflicts.Add("$($Into.settingDefinitionId): $($Into.choiceSettingValue.value) / $($From.choiceSettingValue.value)")
      $Into.choiceSettingValue = $From.choiceSettingValue
      return
    }
    $Into.choiceSettingValue.children = Merge-IntuneChildren $Into.choiceSettingValue.children $From.choiceSettingValue.children $Conflicts
    return
  }
  if ($Into.Contains('groupSettingCollectionValue') -and $From.Contains('groupSettingCollectionValue')) {
    $a = @($Into.groupSettingCollectionValue); $b = @($From.groupSettingCollectionValue)
    if ($a.Count -eq 1 -and $b.Count -eq 1) {
      $a[0].children = Merge-IntuneChildren $a[0].children $b[0].children $Conflicts
      $Into.groupSettingCollectionValue = @($a[0])
      return
    }
  }
  $j1 = $Into | ConvertTo-Json -Depth 64 -Compress; $j2 = $From | ConvertTo-Json -Depth 64 -Compress
  if ($j1 -ne $j2) {
    $Conflicts.Add("$($Into.settingDefinitionId): values differ")
    foreach ($k in @($From.Keys)) { $Into[$k] = $From[$k] }
  }
}

function Merge-IntuneSettings {
  # Merges a list of settings-catalogue settings (each with settingInstance)
  # into an ordered map definitionId -> instance. Returns @{ map; conflicts }.
  param([System.Collections.Specialized.OrderedDictionary] $Map, $Settings)
  if ($null -eq $Map) { $Map = [ordered]@{} }
  $conflicts = [System.Collections.Generic.List[string]]::new()
  foreach ($s in @($Settings)) {
    if ($null -eq $s) { continue }
    $inst = ConvertTo-IntuneHashtable $(if ($s.Contains('settingInstance')) { $s.settingInstance } else { $s })
    $id = [string]$inst.settingDefinitionId
    if ($Map.Contains($id)) { Merge-IntuneInstance -Into $Map[$id] -From $inst -Conflicts $conflicts }
    else { $Map[$id] = $inst }
  }
  return @{ map = $Map; conflicts = @($conflicts) }
}

function Compare-IntuneProps {
  # Keys of $Desired (recursively, for nested objects) whose value differs in
  # $Actual. '@odata.type' is ignored. Values compare as strings; arrays as JSON.
  param($Desired, $Actual, [string] $Prefix = '')
  $diff = @()
  foreach ($k in @($Desired.Keys)) {
    if ($k -eq '@odata.type') { continue }
    $want = $Desired[$k]; $have = Get-IntuneProp $Actual $k
    if ($want -is [System.Collections.IDictionary]) {
      if ($null -eq $have) { $diff += "$Prefix$k"; continue }
      $diff += @(Compare-IntuneProps $want $have "$Prefix$k.")
    } elseif ($want -is [array]) {
      $a = ConvertTo-Json -InputObject @($want) -Depth 16 -Compress
      $b = ConvertTo-Json -InputObject @($have | Where-Object { $null -ne $_ }) -Depth 16 -Compress
      if ($a -ne $b) { $diff += "$Prefix$k" }
    } else {
      if ([string]$want -ne [string]$have) { $diff += "$Prefix$k" }
    }
  }
  return $diff
}

function Get-IntuneTargetKey($Target) {
  # One string per assignment target: group:<id>, exclude:<id>, allDevices, allUsers.
  $t = [string](Get-IntuneProp $Target '@odata.type')
  switch -Wildcard ($t) {
    '*exclusionGroupAssignmentTarget' { return "exclude:$(Get-IntuneProp $Target 'groupId')" }
    '*groupAssignmentTarget' { return "group:$(Get-IntuneProp $Target 'groupId')" }
    '*allDevicesAssignmentTarget' { return 'allDevices' }
    '*allLicensedUsersAssignmentTarget' { return 'allUsers' }
    default { return $t }
  }
}

function New-IntuneTarget([string] $Key) {
  if ($Key -eq 'allDevices') { return @{ '@odata.type' = '#microsoft.graph.allDevicesAssignmentTarget' } }
  if ($Key -eq 'allUsers') { return @{ '@odata.type' = '#microsoft.graph.allLicensedUsersAssignmentTarget' } }
  $kind, $id = $Key -split ':', 2
  if ($kind -eq 'exclude') { return @{ '@odata.type' = '#microsoft.graph.exclusionGroupAssignmentTarget'; groupId = $id } }
  return @{ '@odata.type' = '#microsoft.graph.groupAssignmentTarget'; groupId = $id }
}

function Test-IntuneSameSet($A, $B) {
  $x = @($A | Where-Object { $_ } | Sort-Object -Unique) -join '|'
  $y = @($B | Where-Object { $_ } | Sort-Object -Unique) -join '|'
  return $x -eq $y
}

function Test-IntuneForbidden([string] $Message) {
  return $Message -match "Forbidden|not authorized|Authorization_RequestDenied|authorization_error|doesn't have the required permissions|Insufficient privileges|403"
}

function Test-IntuneTokenRole([string] $Role) {
  # True when the pipeline app's Graph token carries the application role.
  try {
    $part = (Get-BoxAuth 'graph').access_token.Split('.')[1].Replace('-', '+').Replace('_', '/')
    $part += '=' * ((4 - $part.Length % 4) % 4)
    $claims = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($part)) | ConvertFrom-Json
    return @($claims.roles) -contains $Role
  } catch { return $true }   # unknown: let Graph decide
}

function ConvertTo-IntuneSid([string] $ObjectId) {
  # SID Windows uses for an Entra object (S-1-12-1-...), from its object id.
  $b = ([guid]$ObjectId).ToByteArray()
  $parts = 0..3 | ForEach-Object { [BitConverter]::ToUInt32($b, $_ * 4) }
  return "S-1-12-1-$($parts -join '-')"
}

# ---------- Graph helpers ----------

function Get-IntuneGroupId([string] $Name) {
  $g = @(Get-BoxGraphAll "/groups?`$filter=displayName eq '$(ConvertTo-BoxFilterValue $Name)'&`$select=id,displayName")
  if ($g.Count -gt 1) { throw "$($g.Count) groups are named '$Name'; keep one" }
  if ($g.Count -eq 0) { throw "group '$Name' not found (run identity.groups)" }
  return [string]$g[0].id
}

function Find-IntuneObject {
  # An object of an Intune collection by display name (settings-catalogue
  # policies use 'name'). Fails when two objects carry the same name.
  param([Parameter(Mandatory)] [string] $Collection, [Parameter(Mandatory)] [string] $Name, [string] $NameProperty = 'displayName')
  $all = @(Get-BoxGraphAll "$script:IntuneBeta/$Collection")
  $hit = @($all | Where-Object { (Get-IntuneProp $_ $NameProperty) -eq $Name })
  if ($hit.Count -gt 1) { throw "$($hit.Count) objects in $Collection are named '$Name'; keep one and remove the others" }
  if ($hit.Count -eq 1) { return $hit[0] }
  return $null
}

function Get-IntuneCatalogSettings([string] $Id) {
  return @(Get-BoxGraphAll "$script:IntuneBeta/configurationPolicies/$Id/settings")
}

function Get-IntuneAssignmentKeys([string] $Collection, [string] $Id) {
  return @(Get-BoxGraphAll "$script:IntuneBeta/$Collection/$Id/assignments" | ForEach-Object { Get-IntuneTargetKey $_.target })
}

function Set-IntuneAssignments([string] $Collection, [string] $Id, [string[]] $Keys) {
  $body = @{ assignments = @($Keys | Where-Object { $_ } | ForEach-Object { @{ target = (New-IntuneTarget $_) } }) }
  Invoke-BoxGraph -Method POST -Uri "$script:IntuneBeta/$Collection/$Id/assign" -Body $body | Out-Null
}

function New-IntuneCatalogBody($Policy, [System.Collections.IDictionary] $Map, [string] $Description) {
  $body = [ordered]@{ name = $Policy.name; description = $Description; platforms = $Policy.platforms; technologies = $Policy.technologies
    roleScopeTagIds = @('0'); settings = @($Map.Values | ForEach-Object { @{ '@odata.type' = '#microsoft.graph.deviceManagementConfigurationSetting'; settingInstance = $_ } }) }
  if ($Policy.Contains('templateReference') -and $Policy.templateReference) { $body.templateReference = @{ templateId = $Policy.templateReference.templateId } }
  return $body
}

function Sync-IntuneCatalogPolicy {
  # Plans (and with -Apply makes) one settings-catalogue policy: created when
  # absent, replaced with the full settings list (PUT) when a setting
  # differs, assigned to exactly the given targets. Returns changes.
  param($Policy, [System.Collections.IDictionary] $Map, [string[]] $Targets, [string] $Description, [string] $Detail, [switch] $Apply)
  $changes = @()
  $name = [string]$Policy.name
  $cur = Find-IntuneObject -Collection 'configurationPolicies' -Name $name -NameProperty 'name'
  $body = New-IntuneCatalogBody $Policy $Map $Description
  $want = Get-IntuneSettingLeaves @($Map.Values)
  if (-not $cur) {
    $changes += New-BoxChange create "policy $name" "$($Map.Count) setting(s); $Detail"
    if ($Apply) { $cur = Invoke-BoxGraph -Method POST -Uri "$script:IntuneBeta/configurationPolicies" -Body $body } else { return $changes }
  } else {
    $have = Get-IntuneSettingLeaves (Get-IntuneCatalogSettings $cur.id)
    if (($want -join "`n") -ne ($have -join "`n")) {
      $missing = @($want | Where-Object { $_ -notin $have }); $extra = @($have | Where-Object { $_ -notin $want })
      $changes += New-BoxChange update "policy $name" "settings: $($missing.Count) to set, $($extra.Count) to remove; $Detail"
      if ($Apply) { Invoke-BoxGraph -Method PUT -Uri "$script:IntuneBeta/configurationPolicies/$($cur.id)" -Body $body | Out-Null }
    }
  }
  $haveT = if ($cur.id) { @(Get-IntuneAssignmentKeys 'configurationPolicies' $cur.id) } else { @() }
  if (-not (Test-IntuneSameSet $haveT $Targets)) {
    $changes += New-BoxChange update "assignment of $name" "$(@($haveT) -join ', ') -> $(@($Targets) -join ', ')"
    if ($Apply) { Set-IntuneAssignments 'configurationPolicies' $cur.id $Targets }
  }
  return $changes
}
