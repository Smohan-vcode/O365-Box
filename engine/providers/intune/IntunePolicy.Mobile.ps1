# Shared helpers for the intune policy steps policy.intune-macos,
# policy.intune-mobile and policy.m365-apps (T14-05, T14-06, T14-07).
# Dot-sourced by Box.intune.psm1. Every name carries the 'Im' prefix so it
# cannot collide with the Windows step's helpers in the same module.
#
# A step turns $In.policy.settings into desired objects:
#   @{ kind; name; body; settings (catalog only); ids; groups }
# and Invoke-ImObjects finds each by name, creates or updates it, and
# assigns it. In the audit stage every object goes to the pilot group only.

$script:ImBeta = 'https://graph.microsoft.com/beta'

# Graph collection, name property, update verb, assign body key, whether the
# product allows an exclusion target, and properties sent on create only.
$script:ImKinds = @{
  compliance            = @{ uri = 'deviceManagement/deviceCompliancePolicies'; name = 'displayName'; update = 'PATCH'; assign = 'assignments'; exclude = $true; createOnly = @('scheduledActionsForRule') }
  deviceConfiguration   = @{ uri = 'deviceManagement/deviceConfigurations'; name = 'displayName'; update = 'PATCH'; assign = 'assignments'; exclude = $true; createOnly = @() }
  catalog               = @{ uri = 'deviceManagement/configurationPolicies'; name = 'name'; update = 'PUT'; assign = 'assignments'; exclude = $true; createOnly = @() }
  iosAppProtection      = @{ uri = 'deviceAppManagement/iosManagedAppProtections'; name = 'displayName'; update = 'PATCH'; assign = 'assignments'; exclude = $true; createOnly = @() }
  androidAppProtection  = @{ uri = 'deviceAppManagement/androidManagedAppProtections'; name = 'displayName'; update = 'PATCH'; assign = 'assignments'; exclude = $true; createOnly = @() }
  shellScript           = @{ uri = 'deviceManagement/deviceShellScripts'; name = 'displayName'; update = 'PATCH'; assign = 'deviceManagementScriptAssignments'; exclude = $false; createOnly = @(); expandAssign = $true }
}
# Properties never compared: identity, create-only and read-only fields.
$script:ImSkipProps = @('@odata.type', 'displayName', 'name', 'description', 'assignments', 'scheduledActionsForRule', 'platformType', 'settings', 'templateReference', 'platforms', 'technologies')
$script:ImBreakGlass = 'grp-break-glass'

# ---------- pure helpers (Pester: IntunePolicy.Mobile.Tests.ps1) ----------

function ConvertTo-ImPlain($o) {
  # JSON objects (PSCustomObject) to ordered hashtables, recursively.
  if ($null -eq $o) { return $null }
  if ($o -is [string] -or $o -is [ValueType]) { return $o }
  if ($o -is [System.Collections.IDictionary]) {
    $h = [ordered]@{}; foreach ($k in @($o.Keys)) { $h[[string]$k] = ConvertTo-ImPlain $o[$k] }; return $h
  }
  if ($o -is [System.Management.Automation.PSCustomObject]) {
    $h = [ordered]@{}; foreach ($p in $o.PSObject.Properties) { $h[$p.Name] = ConvertTo-ImPlain $p.Value }; return $h
  }
  if ($o -is [System.Collections.IEnumerable]) {
    $l = [System.Collections.Generic.List[object]]::new()
    foreach ($x in $o) { $l.Add((ConvertTo-ImPlain $x)) }
    return , $l.ToArray()
  }
  return $o
}

function Get-ImProp($o, [string] $Name) {
  if ($null -eq $o) { return $null }
  if ($o -is [System.Collections.IDictionary]) { return $o[$Name] }
  $p = $o.PSObject.Properties[$Name]
  if ($p) { return $p.Value }
  return $null
}

function Get-ImConfigValue($In, [string] $Path) {
  $cur = Get-ImProp $In 'config'
  foreach ($part in $Path.Split('.')) {
    if ($null -eq $cur -or $part -notmatch '^[A-Za-z0-9_-]+$') { return $null }
    $cur = Get-ImProp $cur $part
  }
  return $cur
}

# Values the step works out at run time from the tenant (Invoke-ImStepOp
# fills them; placeholder -> value) for {text:x} placeholders no choice
# supplies, such as the Edge sign-in pattern from the verified domains.
$script:ImRuntimeValues = @{}

function Get-ImChoiceValue($In, [string] $Kind, [string] $Key) {
  # The value of {text:x} or {list:x}: @{ found; value }. A key the
  # company's choices supply is an answer even when empty ("" for a list
  # means none: "only approved extensions, none at first"); an absent key
  # is missing.
  $cur = Get-ImProp $In 'config'
  $found = $true
  foreach ($part in @('values') + $Key.Split('.')) {
    if ($null -eq $cur -or $part -notmatch '^[A-Za-z0-9_-]+$') { $found = $false; break }
    $has = if ($cur -is [System.Collections.IDictionary]) { $cur.Contains($part) } else { $null -ne $cur.PSObject.Properties[$part] }
    if (-not $has) { $found = $false; break }
    $cur = Get-ImProp $cur $part
  }
  if ($found) { return @{ found = $true; value = $cur } }
  $rk = "{${Kind}:$Key}"
  if ($script:ImRuntimeValues.ContainsKey($rk)) { return @{ found = $true; value = $script:ImRuntimeValues[$rk] } }
  return @{ found = $false; value = $null }
}

function ConvertTo-ImList($v) {
  # A list answer: an array, or text with one entry per comma or line.
  if ($null -eq $v) { return , @() }
  if ($v -is [string]) { return , @($v -split '[,\r\n]+' | ForEach-Object { $_.Trim() } | Where-Object { $_ }) }
  if ($v -is [System.Collections.IEnumerable]) { return , @($v | ForEach-Object { "$_".Trim() } | Where-Object { $_ }) }
  return , @([string]$v)
}

function Resolve-ImValue($o, $In, [System.Collections.Generic.List[string]] $Missing) {
  # Fills {tenant:id}, {config:a.b}, {text:x} and {list:x} placeholders. A
  # value that cannot be filled ({file:..}, an empty configuration key, a
  # {text:}/{list:} key no choice supplies, a described value such as
  # 'base64(...)' or 'decision needed') goes to $Missing and stays as it is.
  # In a list, an item that is (or whose 'value' is) exactly {list:x}
  # becomes one item per entry of the answer: none for an empty answer.
  if ($null -eq $o) { return $null }
  if ($o -is [string]) {
    if ($o -match '^base64\(' -or $o -match 'decision needed') { $Missing.Add($o); return $o }
    $tenantId = [string](Get-ImProp (Get-ImProp $In 'tenant') 'id')
    return [regex]::Replace($o, '\{(tenant|config|text|list|file):([^}]*)\}', {
        param($m)
        $kind = $m.Groups[1].Value; $key = $m.Groups[2].Value
        if ($kind -eq 'tenant' -and $key -eq 'id' -and $tenantId) { return $tenantId }
        if ($kind -eq 'config') {
          $v = Get-ImConfigValue $In $key
          if ($null -ne $v -and ($v -is [string] -or $v -isnot [System.Collections.IEnumerable]) -and "$v" -ne '') { return "$v" }
        }
        if ($kind -in 'text', 'list') {
          # Filled from the values the company's choices supply (configuration
          # values.x) or from the tenant at run time, never invented here.
          $c = Get-ImChoiceValue $In $kind $key
          if ($c.found) { if ($kind -eq 'list') { return ((ConvertTo-ImList $c.value) -join ',') }; return [string]$c.value }
        }
        $Missing.Add($m.Value); return $m.Value
      })
  }
  if ($o -is [ValueType]) { return $o }
  if ($o -is [System.Collections.IDictionary]) {
    $h = [ordered]@{}; foreach ($k in @($o.Keys)) { $h[$k] = Resolve-ImValue $o[$k] $In $Missing }; return $h
  }
  if ($o -is [System.Collections.IEnumerable]) {
    $l = [System.Collections.Generic.List[object]]::new()
    foreach ($x in $o) {
      $inner = if ($x -is [string]) { $x } elseif ($x -is [System.Collections.IDictionary] -and $x['value'] -is [string]) { $x['value'] } else { $null }
      if ($inner -and $inner -match '^\{list:([^}]*)\}$') {
        $c = Get-ImChoiceValue $In 'list' $Matches[1]
        if ($c.found) {
          foreach ($item in (ConvertTo-ImList $c.value)) {
            if ($x -is [string]) { $l.Add($item) } else { $copy = ConvertTo-ImPlain $x; $copy['value'] = $item; $l.Add($copy) }
          }
          continue
        }
      }
      $l.Add((Resolve-ImValue $x $In $Missing))
    }
    return , $l.ToArray()
  }
  return $o
}

function Remove-ImEmptySettings($Settings) {
  # Leaves out settings whose value list is empty (an empty allow list): no
  # list and an empty list mean the same on the device, and Intune keeps no
  # empty collection to compare with.
  return , @(@($Settings) | Where-Object {
      $i = Get-ImProp $_ 'settingInstance'; if ($null -eq $i) { $i = $_ }
      $t = [string](Get-ImProp $i '@odata.type')
      $simple = $t -like '*SimpleSettingCollectionInstance' -and @(Get-ImProp $i 'simpleSettingCollectionValue' | Where-Object { $null -ne $_ }).Count -eq 0
      $choice = $t -like '*ChoiceSettingCollectionInstance' -and @(Get-ImProp $i 'choiceSettingCollectionValue' | Where-Object { $null -ne $_ }).Count -eq 0
      -not ($simple -or $choice)
    })
}

function Get-ImPayloadGroups($o) {
  # Group names in {group:<name>} placeholders anywhere in a payload.
  $json = $o | ConvertTo-Json -Depth 40 -Compress
  return @([regex]::Matches($json, '\{group:([^}]+)\}') | ForEach-Object { $_.Groups[1].Value } | Select-Object -Unique)
}

function Remove-ImAssignments($o) {
  # Copy of a payload without 'assignments' (the step decides the targets).
  $h = [ordered]@{}
  foreach ($k in @($o.Keys)) { if ($k -ne 'assignments') { $h[$k] = $o[$k] } }
  return $h
}

function Format-ImValue($v) {
  if ($null -eq $v) { return '' }
  if ($v -is [bool]) { return $v.ToString().ToLowerInvariant() }
  if ($v -is [string] -or $v -is [ValueType]) { return [string]$v }
  if ($v -is [System.Collections.IDictionary] -or $v -is [System.Management.Automation.PSCustomObject]) {
    $h = ConvertTo-ImPlain $v
    return '{' + ((@($h.Keys) | Where-Object { $_ -ne '@odata.type' } | Sort-Object | ForEach-Object { "$_=$(Format-ImValue $h[$_])" }) -join ';') + '}'
  }
  if ($v -is [System.Collections.IEnumerable]) { return '[' + ((@($v | ForEach-Object { Format-ImValue $_ }) | Sort-Object) -join ',') + ']' }
  return [string]$v
}

function Compare-ImProps($Desired, $Actual, [string] $Prefix = '') {
  # Differences for the desired properties only; nested objects compare
  # their desired keys; arrays compare as sets.
  $diff = [System.Collections.Generic.List[string]]::new()
  foreach ($k in @($Desired.Keys)) {
    if ($Prefix -eq '' -and $k -in $script:ImSkipProps) { continue }
    if ($k -eq '@odata.type') { continue }
    $d = $Desired[$k]; $a = Get-ImProp $Actual $k
    if ($d -is [System.Collections.IDictionary]) {
      foreach ($x in (Compare-ImProps $d $a "$Prefix$k.")) { $diff.Add($x) }
      continue
    }
    $fd = Format-ImValue $d; $fa = Format-ImValue $a
    if ($fd -ne $fa) { $diff.Add("$Prefix$k $fa -> $fd") }
  }
  return $diff.ToArray()
}

function Add-ImFlat($inst, [string] $Prefix, [System.Collections.Generic.List[string]] $Out) {
  $id = [string](Get-ImProp $inst 'settingDefinitionId')
  $t = [string](Get-ImProp $inst '@odata.type')
  switch -Wildcard ($t) {
    '*ChoiceSettingInstance' {
      $cv = Get-ImProp $inst 'choiceSettingValue'
      $Out.Add("$Prefix$id=$(Get-ImProp $cv 'value')")
      foreach ($c in @(Get-ImProp $cv 'children')) { if ($c) { Add-ImFlat $c "$Prefix$id/" $Out } }
    }
    '*ChoiceSettingCollectionInstance' {
      $vals = @(Get-ImProp $inst 'choiceSettingCollectionValue' | ForEach-Object { [string](Get-ImProp $_ 'value') } | Sort-Object)
      $Out.Add("$Prefix$id=[$($vals -join ',')]")
    }
    '*SimpleSettingInstance' { $Out.Add("$Prefix$id=$(Get-ImProp (Get-ImProp $inst 'simpleSettingValue') 'value')") }
    '*SimpleSettingCollectionInstance' {
      $vals = @(Get-ImProp $inst 'simpleSettingCollectionValue' | ForEach-Object { [string](Get-ImProp $_ 'value') } | Sort-Object)
      $Out.Add("$Prefix$id=[$($vals -join ',')]")
    }
    '*GroupSettingCollectionInstance' {
      $i = 0
      foreach ($item in @(Get-ImProp $inst 'groupSettingCollectionValue')) {
        foreach ($c in @(Get-ImProp $item 'children')) { if ($c) { Add-ImFlat $c "$Prefix$id[$i]/" $Out } }
        $i++
      }
    }
    '*GroupSettingInstance' {
      foreach ($c in @(Get-ImProp (Get-ImProp $inst 'groupSettingValue') 'children')) { if ($c) { Add-ImFlat $c "$Prefix$id/" $Out } }
    }
    default { $Out.Add("$Prefix$id=?$t") }
  }
}

function Get-ImFlatSettings($Settings) {
  # A settings-catalogue settings list as a sorted list of 'path=value'.
  $out = [System.Collections.Generic.List[string]]::new()
  foreach ($s in @($Settings)) {
    if ($null -eq $s) { continue }
    $inst = Get-ImProp $s 'settingInstance'
    if ($null -eq $inst) { $inst = $s }
    Add-ImFlat $inst '' $out
  }
  return , @($out | Sort-Object)
}

function ConvertTo-ImSetting($s) {
  # Wraps a bare setting instance in a deviceManagementConfigurationSetting.
  if ($null -ne (Get-ImProp $s 'settingInstance')) { return $s }
  return [ordered]@{ '@odata.type' = '#microsoft.graph.deviceManagementConfigurationSetting'; settingInstance = $s }
}

function Merge-ImChildren($a, $b) {
  # Children lists merged by settingDefinitionId; b wins.
  $byId = [ordered]@{}
  foreach ($c in @($a) + @($b)) { if ($c) { $byId[[string]$c.settingDefinitionId] = $c } }
  return , @($byId.Values)
}

function Merge-ImSettings($Lists) {
  # Several settings lists for one policy merged by top-level definition id;
  # group collections with the same id merge their first item's children.
  $byId = [ordered]@{}
  foreach ($list in @($Lists)) {
    foreach ($s in @($list)) {
      if ($null -eq $s) { continue }
      $s = ConvertTo-ImSetting $s
      $id = [string]$s.settingInstance.settingDefinitionId
      if ($byId.Contains($id) -and $s.settingInstance['@odata.type'] -like '*GroupSettingCollectionInstance' -and $byId[$id].settingInstance['@odata.type'] -like '*GroupSettingCollectionInstance') {
        $old = $byId[$id].settingInstance.groupSettingCollectionValue[0].children
        $new = $s.settingInstance.groupSettingCollectionValue[0].children
        $merged = ConvertTo-ImPlain $byId[$id]
        $merged.settingInstance.groupSettingCollectionValue[0].children = Merge-ImChildren $old $new
        $byId[$id] = $merged
      } else { $byId[$id] = $s }
    }
  }
  return , @($byId.Values)
}

function Merge-ImProps($Target, $Source) {
  # Shallow merge of payload properties into an ordered hashtable.
  foreach ($k in @($Source.Keys)) { $Target[$k] = $Source[$k] }
}

function Get-ImTargetKey($t) {
  $type = [string](Get-ImProp $t '@odata.type')
  switch -Wildcard ($type) {
    '*exclusionGroupAssignmentTarget' { return "exclude:$(Get-ImProp $t 'groupId')" }
    '*groupAssignmentTarget' { return "group:$(Get-ImProp $t 'groupId')" }
    '*allLicensedUsersAssignmentTarget' { return 'allUsers' }
    '*allDevicesAssignmentTarget' { return 'allDevices' }
    default { return "other:$type" }
  }
}

function Test-ImAuditStage($In) {
  # True unless the rollout stage is enforce: objects then go to the pilot
  # group only (SPEC §20, T14 brief).
  return ([string]$In.policy.rollout.stage) -ne 'enforce'
}

function New-ImMobileConfig([string] $Identifier, [string] $DisplayName, [string] $PayloadType, $Content) {
  # A configuration profile (.mobileconfig) with one payload. UUIDs derive
  # from the identifier so the bytes are the same on every run.
  function uuid([string] $s) {
    $h = [System.Security.Cryptography.MD5]::HashData([Text.Encoding]::UTF8.GetBytes($s))
    return ([guid]::new($h)).ToString().ToUpperInvariant()
  }
  function esc([string] $s) { return [Security.SecurityElement]::Escape($s) }
  function val($v) {
    if ($v -is [bool]) { if ($v) { return '<true/>' } else { return '<false/>' } }
    if ($v -is [int] -or $v -is [long]) { return "<integer>$v</integer>" }
    if ($v -is [System.Collections.IEnumerable] -and $v -isnot [string]) { return '<array>' + (($v | ForEach-Object { val $_ }) -join '') + '</array>' }
    return "<string>$(esc ([string]$v))</string>"
  }
  $keys = (@($Content.Keys) | ForEach-Object { "<key>$(esc $_)</key>$(val $Content[$_])" }) -join ''
  $xml = '<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict>' +
  '<key>PayloadContent</key><array><dict>' + $keys +
  "<key>PayloadDisplayName</key><string>$(esc $DisplayName)</string><key>PayloadIdentifier</key><string>$(esc "$Identifier.payload")</string>" +
  "<key>PayloadType</key><string>$(esc $PayloadType)</string><key>PayloadUUID</key><string>$(uuid "$Identifier.payload")</string><key>PayloadVersion</key><integer>1</integer>" +
  '</dict></array>' +
  "<key>PayloadDisplayName</key><string>$(esc $DisplayName)</string><key>PayloadIdentifier</key><string>$(esc $Identifier)</string>" +
  "<key>PayloadScope</key><string>System</string><key>PayloadType</key><string>Configuration</string><key>PayloadUUID</key><string>$(uuid $Identifier)</string><key>PayloadVersion</key><integer>1</integer>" +
  '</dict></plist>'
  return [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($xml))
}

function ConvertTo-ImBase64([string] $Text) {
  return [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes(($Text -replace "`r`n", "`n")))
}

# ---------- settings built from definitions (short form) ----------

$script:ImDefinitions = @{}
function Get-ImDefinition([string] $Id) {
  if (-not $script:ImDefinitions.ContainsKey($Id)) {
    $script:ImDefinitions[$Id] = Invoke-BoxGraph -Uri "$script:ImBeta/deviceManagement/configurationSettings('$([uri]::EscapeDataString($Id))')"
  }
  return $script:ImDefinitions[$Id]
}

function Resolve-ImOption($Def, [string] $Value) {
  # The option itemId for a value given as an itemId, an itemId suffix
  # ('_1', '_true'), or an option name.
  $opts = @($Def.options)
  foreach ($o in $opts) { if ($o.itemId -eq $Value) { return $o.itemId } }
  if ($Value.StartsWith('_')) { foreach ($o in $opts) { if ($o.itemId -eq "$($Def.id)$Value") { return $o.itemId } } }
  $word = @{ '_true' = 'True'; '_false' = 'False' }[$Value]
  foreach ($o in $opts) { if ($o.displayName -eq $Value -or $o.name -eq $Value -or ($word -and ($o.displayName -eq $word -or $o.name -eq $word))) { return $o.itemId } }
  # A yes/no option given in words ('show notification'): the text names the yes case.
  $yes = @($opts | Where-Object { $_.displayName -eq 'Yes' })
  if ($opts.Count -eq 2 -and $yes.Count -eq 1 -and @($opts | Where-Object { $_.displayName -eq 'No' }).Count -eq 1 -and $Value -notmatch '^(no|off|false|0)$') { return $yes[0].itemId }
  throw "setting $($Def.id) has no option '$Value'"
}

function ConvertFrom-ImShortSetting($s) {
  # {settingDefinitionId, choice|value, children} to a setting instance,
  # typed from the setting's definition.
  $id = [string](Get-ImProp $s 'settingDefinitionId')
  $def = Get-ImDefinition $id
  $t = [string]$def.'@odata.type'
  if ($t -like '*ChoiceSettingDefinition') {
    $v = Get-ImProp $s 'choice'; if ($null -eq $v) { $v = Get-ImProp $s 'value' }
    $kids = [System.Collections.Generic.List[object]]::new()
    $c = Get-ImProp $s 'children'
    if ($c -is [System.Collections.IDictionary] -and -not $c.Contains('settingDefinitionId')) { foreach ($k in @($c.Keys)) { $kids.Add((ConvertFrom-ImShortSetting ([ordered]@{ settingDefinitionId = $k; value = $c[$k] }))) } }
    elseif ($null -ne $c) { foreach ($x in @($c)) { $kids.Add((ConvertFrom-ImShortSetting $x)) } }
    return [ordered]@{ '@odata.type' = '#microsoft.graph.deviceManagementConfigurationChoiceSettingInstance'; settingDefinitionId = $id
      choiceSettingValue = [ordered]@{ value = (Resolve-ImOption $def ([string]$v)); children = $kids.ToArray() } }
  }
  if ($t -like '*SimpleSettingDefinition') {
    $v = Get-ImProp $s 'value'
    $sv = if ([string]$def.valueDefinition.'@odata.type' -like '*Integer*') { [ordered]@{ '@odata.type' = '#microsoft.graph.deviceManagementConfigurationIntegerSettingValue'; value = [int]$v } }
    else { [ordered]@{ '@odata.type' = '#microsoft.graph.deviceManagementConfigurationStringSettingValue'; value = [string]$v } }
    return [ordered]@{ '@odata.type' = '#microsoft.graph.deviceManagementConfigurationSimpleSettingInstance'; settingDefinitionId = $id; simpleSettingValue = $sv }
  }
  throw "setting $id has definition type $t, which the short form does not cover"
}

# ---------- tenant reads and writes ----------

$script:ImGroupIds = @{}
function Get-ImGroupId([string] $Name) {
  if (-not $script:ImGroupIds.ContainsKey($Name)) {
    $g = @(Get-BoxGraphAll "/groups?`$filter=displayName eq '$(ConvertTo-BoxFilterValue $Name)'&`$select=id")
    if ($g.Count -ne 1) { throw "expected one group named '$Name', found $($g.Count)" }
    $script:ImGroupIds[$Name] = $g[0].id
  }
  return $script:ImGroupIds[$Name]
}

function Get-ImTargets($In, $Object) {
  # Assignment targets: the pilot group in the audit stage, else the groups
  # the payload names. grp-break-glass is excluded where the product allows.
  $t = [System.Collections.Generic.List[object]]::new()
  if (Test-ImAuditStage $In) {
    $t.Add([ordered]@{ '@odata.type' = '#microsoft.graph.groupAssignmentTarget'; groupId = (Get-ImGroupId ([string]$In.policy.rollout.pilot_group)) })
  } else {
    foreach ($g in @($Object.groups)) {
      switch ($g) {
        'all-users' { $t.Add([ordered]@{ '@odata.type' = '#microsoft.graph.allLicensedUsersAssignmentTarget' }) }
        'all-devices' { $t.Add([ordered]@{ '@odata.type' = '#microsoft.graph.allDevicesAssignmentTarget' }) }
        default { $t.Add([ordered]@{ '@odata.type' = '#microsoft.graph.groupAssignmentTarget'; groupId = (Get-ImGroupId $g) }) }
      }
    }
  }
  if ($script:ImKinds[$Object.kind].exclude) {
    $t.Add([ordered]@{ '@odata.type' = '#microsoft.graph.exclusionGroupAssignmentTarget'; groupId = (Get-ImGroupId $script:ImBreakGlass) })
  }
  return , $t.ToArray()
}

function Get-ImCreateBody($Object) {
  $k = $script:ImKinds[$Object.kind]
  $b = [ordered]@{}
  Merge-ImProps $b $Object.body
  if ($Object.kind -eq 'catalog') {
    $b.settings = @($Object.settings)
    if ($b.templateReference) { $b.settings = Add-ImTemplateRefs $b.settings (Get-ImTemplateIds $b.templateReference.templateId) }
  }
  $b[$k.name] = $Object.name
  return $b
}

$script:ImTemplates = @{}
function Get-ImTemplateIds([string] $TemplateId) {
  # settingDefinitionId -> @{ instance; value } template ids of an
  # endpoint security template, nested ones included.
  if (-not $script:ImTemplates.ContainsKey($TemplateId)) {
    $map = @{}
    $walk = {
      param($n)
      if ($null -eq $n -or $n -is [string] -or $n -is [ValueType]) { return }
      if ($n -is [System.Collections.IEnumerable] -and $n -isnot [System.Collections.IDictionary]) { foreach ($x in $n) { & $walk $x }; return }
      $h = ConvertTo-ImPlain $n
      if ($h.settingDefinitionId -and $h.settingInstanceTemplateId) {
        $v = $null
        foreach ($vt in 'choiceSettingValueTemplate', 'simpleSettingValueTemplate') { if ($h[$vt]) { $v = $h[$vt].settingValueTemplateId } }
        $map[[string]$h.settingDefinitionId] = @{ instance = $h.settingInstanceTemplateId; value = $v }
      }
      foreach ($k in @($h.Keys)) { & $walk $h[$k] }
    }
    & $walk @(Get-BoxGraphAll "$script:ImBeta/deviceManagement/configurationPolicyTemplates('$TemplateId')/settingTemplates")
    $script:ImTemplates[$TemplateId] = $map
  }
  return $script:ImTemplates[$TemplateId]
}

function Add-ImTemplateRefs($Settings, $Map) {
  # Copies of the settings with template references on every instance the
  # template knows (a policy created from a template needs them).
  $mark = {
    param($inst)
    $t = $Map[[string]$inst.settingDefinitionId]
    if ($t) {
      $inst.settingInstanceTemplateReference = [ordered]@{ settingInstanceTemplateId = $t.instance }
      foreach ($vk in 'choiceSettingValue', 'simpleSettingValue') {
        if ($inst[$vk] -and $t.value) { $inst[$vk].settingValueTemplateReference = [ordered]@{ settingValueTemplateId = $t.value; useTemplateDefault = $false } }
      }
    }
    foreach ($c in @($inst.choiceSettingValue.children)) { if ($c) { & $mark $c } }
    foreach ($g in @($inst.groupSettingCollectionValue)) { foreach ($c in @($g.children)) { if ($c) { & $mark $c } } }
  }
  $out = ConvertTo-ImPlain $Settings
  foreach ($s in @($out)) { & $mark $s.settingInstance }
  return , @($out)
}

function Get-ImUpdateBody($Object) {
  $k = $script:ImKinds[$Object.kind]
  $b = Get-ImCreateBody $Object
  foreach ($p in $k.createOnly) { $b.Remove($p) }
  return $b
}

function Get-ImObjectDiff($Object, $Current) {
  # Differences between a desired object and the tenant's.
  $k = $script:ImKinds[$Object.kind]
  $uri = "$script:ImBeta/$($k.uri)/$($Current.id)"
  if ($Object.kind -eq 'catalog') {
    $have = Get-ImFlatSettings @(Get-BoxGraphAll "$uri/settings")
    $want = Get-ImFlatSettings $Object.settings
    $missing = @($want | Where-Object { $_ -notin $have })
    $extra = @($have | Where-Object { $_ -notin $want })
    $d = @()
    if ($missing.Count) { $d += "missing $($missing -join '; ')" }
    if ($extra.Count) { $d += "extra $($extra -join '; ')" }
    return $d
  }
  $full = Invoke-BoxGraph -Uri $uri
  return Compare-ImProps $Object.body $full
}

function Invoke-ImObject($In, $o, $existing, $denied, $changes, [switch] $Apply) {
  $k = $script:ImKinds[$o.kind]
  $target = "$($o.kind) '$($o.name)' ($(@($o.ids) -join ','))"
  if ($denied.ContainsKey($o.kind)) { $changes.Add((New-BoxChange create $target "blocked: $($denied[$o.kind])")); return }
  $cur = @($existing[$o.kind] | Where-Object { $_.($k.name) -eq $o.name })
  if ($cur.Count -gt 1) { throw "$($cur.Count) objects are named '$($o.name)'; keep one and remove the others" }
  $targets = Get-ImTargets $In $o
  $want = @($targets | ForEach-Object { Get-ImTargetKey $_ } | Sort-Object)
  $id = $null
  if ($cur.Count -eq 0) {
    $changes.Add((New-BoxChange create $target "assigned to $($want -join ', ')"))
    if (-not $Apply) { return }
    $new = Invoke-BoxGraph -Method POST -Uri "$script:ImBeta/$($k.uri)" -Body (Get-ImCreateBody $o)
    $id = $new.id
  } else {
    $id = $cur[0].id
    $diff = @(Get-ImObjectDiff $o $cur[0])
    if ($diff.Count) {
      $changes.Add((New-BoxChange update $target ($diff -join '; ')))
      if ($Apply) { Invoke-BoxGraph -Method $k.update -Uri "$script:ImBeta/$($k.uri)/$id" -Body (Get-ImUpdateBody $o) | Out-Null }
    }
    # Shell scripts have no /assignments route; their assignments come with $expand.
    $rows = if ($k.expandAssign) { @((Invoke-BoxGraph -Uri "$script:ImBeta/$($k.uri)/$id`?`$expand=assignments").assignments) }
            else { @(Get-BoxGraphAll "$script:ImBeta/$($k.uri)/$id/assignments") }
    $have = @($rows | Where-Object { $_ } | ForEach-Object { Get-ImTargetKey $_.target } | Sort-Object)
    if (($have -join '|') -eq ($want -join '|')) { return }
    $changes.Add((New-BoxChange update "$target assignments" "$($have -join ', ') -> $($want -join ', ')"))
    if (-not $Apply) { return }
  }
  $body = @{ $k.assign = @($targets | ForEach-Object { [ordered]@{ target = $_ } }) }
  Invoke-BoxGraph -Method POST -Uri "$script:ImBeta/$($k.uri)/$id/assign" -Body $body | Out-Null
}

function Invoke-ImObjects($In, $Objects, [switch] $Apply) {
  # Changes for the desired objects; with -Apply, makes them. A collection
  # the pipeline app may not read (403) is reported, not thrown, so the
  # other objects still apply; verify then fails for it.
  $changes = [System.Collections.Generic.List[object]]::new()
  $existing = @{}
  $denied = @{}
  foreach ($kind in @($Objects | ForEach-Object { $_.kind } | Select-Object -Unique)) {
    $k = $script:ImKinds[$kind]
    try { $existing[$kind] = @(Get-BoxGraphAll "$script:ImBeta/$($k.uri)?`$select=id,$($k.name)") }
    catch {
      if ("$_" -notmatch 'Forbidden|Authorization|not authorized|403') { throw }
      $denied[$kind] = (("$_" -replace '(?s)^.*(Application must have one of the following scopes: [^-]+).*$', '$1').Trim())
      Write-Warning "$kind cannot be read: $($denied[$kind])"
    }
  }
  $failed = [System.Collections.Generic.List[string]]::new()
  foreach ($o in @($Objects)) {
    if (-not $Apply) { Invoke-ImObject $In $o $existing $denied $changes; continue }
    # Apply each object on its own so one refusal does not stop the rest.
    try { Invoke-ImObject $In $o $existing $denied $changes -Apply }
    catch {
      $m = "$_" -replace '(?s)^.*"Message":\s*"(.*?)( - Operation ID.*)?",.*$', '$1'
      $failed.Add("$($o.kind) '$($o.name)': $m"); Write-Warning "failed $($o.kind) '$($o.name)': $m"
    }
  }
  if ($failed.Count) { throw "$($failed.Count) object(s) failed: $($failed -join ' | ')" }
  return $changes.ToArray()
}

# ---------- step plumbing ----------

# Compliance policies need a block action on create.
$script:ImBlockNow = @(@{ ruleName = 'PasswordRequired'; scheduledActionConfigurations = @(@{ actionType = 'block'; gracePeriodHours = 0 }) })

function Get-ImObject($Objects, [string] $Kind, [string] $Name, [string] $Type) {
  if (-not $Objects.Contains($Name)) {
    $body = [ordered]@{ '@odata.type' = $Type }
    if ($Kind -eq 'compliance') { $body.scheduledActionsForRule = $script:ImBlockNow }
    if ($Kind -like '*AppProtection') { $body.appGroupType = 'allCoreMicrosoftApps'; $body.targetedAppManagementLevels = 'unspecified' }
    $Objects[$Name] = @{ kind = $Kind; name = $Name; body = $body; ids = [System.Collections.Generic.List[string]]::new(); groups = [System.Collections.Generic.List[string]]::new() }
  }
  return $Objects[$Name]
}

function Add-ImUse($Object, [string] $Id, $Groups) {
  if ($Id -notin $Object.ids) { $Object.ids.Add($Id) }
  foreach ($g in @($Groups)) { if ($g -and $g -notin $Object.groups) { $Object.groups.Add($g) } }
}


function Add-ImCatalog($Objects, [string] $Name, [string] $Platforms, [string] $Technologies, $Settings, [string] $Template, [string] $Id, $Groups) {
  # A settings-catalogue policy collecting settings lists from one or more
  # settings; Complete-ImCatalog merges them.
  if (-not $Objects.Contains($Name)) {
    $Objects[$Name] = @{ kind = 'catalog'; name = $Name; body = [ordered]@{ description = 'Managed by box'; platforms = $Platforms; technologies = $Technologies }
      lists = [System.Collections.Generic.List[object]]::new(); settings = @()
      ids = [System.Collections.Generic.List[string]]::new(); groups = [System.Collections.Generic.List[string]]::new() }
  }
  $o = $Objects[$Name]
  if ($Template) { $o.body.templateReference = [ordered]@{ templateId = $Template } }
  $o.lists.Add((Remove-ImEmptySettings $Settings))
  Add-ImUse $o $Id $Groups
}

function Complete-ImCatalog($Objects) {
  foreach ($o in @($Objects.Values | Where-Object { $_.kind -eq 'catalog' })) { $o.settings = Merge-ImSettings $o.lists.ToArray() }
}

function Get-ImSettings($In) {
  if ($In.policy_error) { throw "policy input: $($In.policy_error)" }
  return @($In.policy.settings | Where-Object { $_ })
}

function Resolve-ImPart($In, $Part, [string] $Label, $Skipped) {
  # A payload part with its placeholders filled, or $null (and a skipped
  # entry) when a value the company must supply is missing.
  $missing = [System.Collections.Generic.List[string]]::new()
  $r = Resolve-ImValue (ConvertTo-ImPlain $Part) $In $missing
  if ($missing.Count) { $Skipped.Add(@{ id = $Label; reason = "needs value(s) not in the configuration: $(@($missing | Select-Object -Unique) -join ', ')" }); return $null }
  return $r
}

function Write-ImSkipped($Skipped) {
  foreach ($s in @($Skipped)) { Write-Warning "skipped $($s.id): $($s.reason)" }
}

function Test-ImInput($In) {
  if ($In.policy_error) { return @{ ok = $false; reason = [string]$In.policy_error } }
  return @{ ok = $true }
}

function Get-ImSigninPattern([string[]] $Domains) {
  # Edge RestrictSigninToPattern: work accounts of the verified domains
  # (same form as the Windows step's {pattern:company-accounts}).
  $alt = @($Domains | Where-Object { $_ } | ForEach-Object { $_.ToLowerInvariant() } | Sort-Object -Unique | ForEach-Object { [regex]::Escape($_) }) -join '|'
  return ".*@($alt)$"
}

function Set-ImRuntimeValues($In) {
  # Values worked out from the tenant for the placeholders the settings use.
  $script:ImRuntimeValues = @{}
  $json = (@(Get-ImSettings $In) | ForEach-Object { $_.payload } | ConvertTo-Json -Depth 64 -Compress)
  if ($json -match '\{text:edge_signin_pattern\}') {
    $d = @(Get-BoxGraphAll '/domains?$select=id,isVerified' | Where-Object { $_.isVerified } | ForEach-Object { [string]$_.id })
    if ($d.Count) { $script:ImRuntimeValues['{text:edge_signin_pattern}'] = Get-ImSigninPattern $d }
  }
}

function Invoke-ImStepOp($In, [string] $Builder, [string] $Operation, [string] $Kind) {
  # plan, apply, verify or evidence for a builder function that returns
  # @{ objects; skipped }. Handlers in Policy.*.ps1 call this.
  Set-ImRuntimeValues $In
  $b = & $Builder $In
  if ($Operation -eq 'evidence') {
    $skip = @($b.skipped | ForEach-Object { $_.id })
    $to = if (Test-ImAuditStage $In) { "the pilot group $($In.policy.rollout.pilot_group) (audit stage)" } else { 'the target groups (enforce stage)' }
    $sum = "$(@($b.objects).Count) object(s) from $(@(Get-ImSettings $In).Count) resolved setting(s), assigned to $to; skipped: $(if ($skip.Count) { $skip -join ', ' } else { 'none' })"
    return @{ items = @(@{ kind = $Kind; ref = "tenant:$Kind"; summary = $sum }) }
  }
  Write-ImSkipped $b.skipped
  $c = @(Invoke-ImObjects $In $b.objects -Apply:($Operation -eq 'apply'))
  if ($Operation -eq 'verify') { return @{ ok = ($c.Count -eq 0); drift = $c } }
  return @{ changes = $c }
}
