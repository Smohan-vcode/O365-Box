# Provider: sharepoint. Tenant sharing, hub and sites, libraries, register
# lists and the configuration store (T3, T0-06). Connects with PnP using the
# pipeline app's token for the tenant's SharePoint host.
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxProvider.psm1') -Force
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxGraph.psm1') -Force

$script:Conn = @{}
$Levels = @{ owner = 'Full Control'; member = 'Edit'; visitor = 'Read' }

function Get-Root($In) { return [string]$In.tenant.sharepoint }
function Get-SiteUrl($In, [string] $Url) { return "$(Get-Root $In)/sites/$Url" }

function Connect-Site([string] $Url) {
  $t = (Get-BoxAuth 'app').access_token
  $key = "$Url|$t"
  if (-not $script:Conn.ContainsKey($key)) {
    $script:Conn[$key] = Connect-PnPOnline -Url $Url -AccessToken $t -ReturnConnection -ErrorAction Stop
  }
  return $script:Conn[$key]
}
function Connect-Admin($In) { return Connect-Site ([string]$In.tenant.sharepoint_admin) }

function Get-GroupLogin([string] $Name) {
  $g = @(Get-BoxGraphAll "/groups?`$filter=displayName eq '$(ConvertTo-BoxFilterValue $Name)'&`$select=id")
  if ($g.Count -ne 1) { throw "group $Name not found (run identity.groups)" }
  return "c:0t.c|tenant|$($g[0].id)"
}

function Get-BreakGlassUpns($In) {
  return @($In.config.users | Where-Object { $_.break_glass } | ForEach-Object { "$($_.primary)@$($In.tenant.initial_domain)" })
}

# ---------- sharepoint.tenant ----------
function Get-TenantChanges($In, [switch] $Apply) {
  $c = Connect-Admin $In
  $t = Get-PnPTenant -Connection $c
  $b = $In.defaults.baseline_sharepoint.tenant
  $want = [ordered]@{
    SharingCapability = @{ existing_guests_only = 'ExistingExternalUserSharingOnly'; off = 'Disabled'; new_guests = 'ExternalUserSharingOnly' }[[string]$b.sharing_capability]
    DefaultSharingLinkType = @{ specific_people = 'Direct'; organization = 'Internal' }[[string]$b.default_sharing_link]
    DefaultLinkPermission = @{ view = 'View'; edit = 'Edit' }[[string]$b.default_link_permission]
    LegacyAuthProtocolsEnabled = -not ($b.legacy_auth -eq 'off' -or $b.legacy_auth -eq $false)
    OneDriveSharingCapability = if ($b.onedrive_sharing -eq 'off' -or $b.onedrive_sharing -eq $false) { 'Disabled' } else { 'ExistingExternalUserSharingOnly' }
  }
  $changes = @()
  $set = @{}
  foreach ($k in $want.Keys) {
    if ([string]$t.$k -ne [string]$want[$k]) { $changes += New-BoxChange update "tenant $k" "$($t.$k) -> $($want[$k])"; $set[$k] = $want[$k] }
  }
  if ($Apply -and $set.Count) { Set-PnPTenant @set -Connection $c -ErrorAction Stop }
  return $changes
}

# ---------- sharepoint.sites ----------
function Get-AllSites($In) {
  $c = Connect-Admin $In
  $m = @{}
  foreach ($s in @(Get-PnPTenantSite -Connection $c)) { $m[$s.Url.ToLower().TrimEnd('/')] = $s }
  return $m
}
function New-BoxSite($In, [string] $Url, [string] $Title) {
  $c = Connect-Admin $In
  $owner = (Get-BreakGlassUpns $In)[0]
  New-PnPSite -Type TeamSiteWithoutMicrosoft365Group -Title $Title -Url $Url -Owner $owner -Wait -Connection $c -ErrorAction Stop | Out-Null
}
function Get-SiteChanges($In, [switch] $Apply) {
  $changes = @()
  $sites = Get-AllSites $In
  $hub = $In.config.structure.hub
  $hubUrl = Get-SiteUrl $In $hub.url
  $all = @(@{ name = $hub.name; url = $hub.url; access = $null; external = 'off'; isHub = $true }) + @($In.config.structure.sites | ForEach-Object { @{ name = $_.name; url = $_.url; access = $_.access; external = $_.external; isHub = $false } })
  $admin = Connect-Admin $In
  foreach ($s in $all) {
    $u = Get-SiteUrl $In $s.url
    if (-not $sites.ContainsKey($u.ToLower())) {
      $changes += New-BoxChange create "site $u" $s.name
      if (-not $Apply) { continue }
      New-BoxSite $In $u $s.name
    }
    $ts = Get-PnPTenantSite -Identity $u -Connection $admin -ErrorAction Stop
    $want = @{ off = 'Disabled'; existing_guests = 'ExistingExternalUserSharingOnly'; new_guests = 'ExternalUserSharingOnly' }[[string]$s.external]
    if ([string]$ts.SharingCapability -ne $want) {
      $changes += New-BoxChange update "sharing on $u" "$($ts.SharingCapability) -> $want"
      if ($Apply) { Set-PnPTenantSite -Identity $u -SharingCapability $want -Connection $admin -ErrorAction Stop }
    }
    # Site collection admins are the break-glass accounts only.
    $sc = Connect-Site $u
    $admins = @(Get-PnPSiteCollectionAdmin -Connection $sc | ForEach-Object { ([string]$_.Email).ToLower() })
    foreach ($bg in Get-BreakGlassUpns $In) {
      if ($bg.ToLower() -notin $admins) {
        $changes += New-BoxChange update "site collection admin $bg on $u"
        if ($Apply) { Add-PnPSiteCollectionAdmin -Owners $bg -Connection $sc -ErrorAction Stop }
      }
    }
    if ($s.isHub) {
      if (-not $ts.IsHubSite) {
        $changes += New-BoxChange update "register hub $u"
        if ($Apply) { Register-PnPHubSite -Site $u -Connection $admin -ErrorAction Stop | Out-Null }
      }
      continue
    }
    $hubInfo = Get-PnPHubSite -Identity $hubUrl -Connection $admin -ErrorAction SilentlyContinue
    if ($hubInfo -and [string]$ts.HubSiteId -ne [string]$hubInfo.ID) {
      $changes += New-BoxChange update "associate $u with hub"
      if ($Apply) { Add-PnPHubSiteAssociation -Site $u -HubSite $hubUrl -Connection $admin -ErrorAction Stop }
    } elseif (-not $hubInfo) { $changes += New-BoxChange update "associate $u with hub" 'after the hub is registered' }
    # Access: groups only, added to the site's owner, member or visitor group.
    foreach ($p in $s.access.PSObject.Properties) {
      $level = [string]$p.Value
      $login = Get-GroupLogin $p.Name
      $grp = switch ($level) { 'owner' { Get-PnPGroup -AssociatedOwnerGroup -Connection $sc } 'member' { Get-PnPGroup -AssociatedMemberGroup -Connection $sc } default { Get-PnPGroup -AssociatedVisitorGroup -Connection $sc } }
      $members = @(Get-PnPGroupMember -Group $grp -Connection $sc | ForEach-Object { [string]$_.LoginName })
      if ($login -notin $members) {
        $changes += New-BoxChange update "$($p.Name) as $level on $u"
        if ($Apply) { Add-PnPGroupMember -Group $grp -LoginName $login -Connection $sc -ErrorAction Stop }
      }
    }
  }
  return $changes
}

# ---------- sharepoint.libraries ----------
function Get-LibUrl([string] $Title) { return ($Title -replace '[^A-Za-z0-9]', '') }
function Add-Folders($sc, [string] $Parent, $Folders, [ref] $Changes, [switch] $Apply) {
  foreach ($f in @($Folders | Where-Object { $_ })) {
    $name = if ($f -is [string]) { $f } else { [string]$f.name }
    $path = "$Parent/$name"
    $exists = Get-PnPFolder -Url $path -Connection $sc -ErrorAction SilentlyContinue
    if (-not $exists) {
      $Changes.Value += New-BoxChange create "folder $path"
      if ($Apply) { Add-PnPFolder -Name $name -Folder $Parent -Connection $sc -ErrorAction Stop | Out-Null }
    }
    if ($f -isnot [string] -and $f.folders) { Add-Folders $sc $path $f.folders $Changes -Apply:$Apply }
  }
}
function Get-LibraryChanges($In, [switch] $Apply) {
  $changes = @()
  foreach ($s in @($In.config.structure.sites)) {
    $u = Get-SiteUrl $In $s.url
    $sc = Connect-Site $u
    foreach ($l in @($s.libraries | Where-Object { $_ })) {
      $url = Get-LibUrl $l.name
      $list = Get-PnPList -Identity $l.name -Connection $sc -ErrorAction SilentlyContinue
      if (-not $list) {
        $changes += New-BoxChange create "library $($s.name)/$($l.name)"
        if (-not $Apply) { continue }
        $list = New-PnPList -Title $l.name -Url $url -Template DocumentLibrary -EnableVersioning -Connection $sc -ErrorAction Stop
      }
      if (-not $list.EnableVersioning) {
        $changes += New-BoxChange update "versioning on $($s.name)/$($l.name)"
        if ($Apply) { Set-PnPList -Identity $l.name -EnableVersioning $true -MajorVersions 100 -Connection $sc | Out-Null }
      }
      $cls = Get-PnPField -List $l.name -Identity 'Classification' -Connection $sc -ErrorAction SilentlyContinue
      if (-not $cls) {
        $changes += New-BoxChange create "Classification column on $($s.name)/$($l.name)" ([string]$s.classification)
        if ($Apply) {
          $f = Add-PnPField -List $l.name -DisplayName 'Classification' -InternalName 'Classification' -Type Choice -Choices 'Public', 'Internal', 'Confidential', 'Restricted' -Required -Connection $sc -ErrorAction Stop
          Set-PnPField -List $l.name -Identity 'Classification' -Values @{ DefaultValue = [string]$s.classification } -Connection $sc | Out-Null
        }
      }
      if ($l.access) {
        $list = Get-PnPList -Identity $l.name -Includes HasUniqueRoleAssignments -Connection $sc
        if (-not $list.HasUniqueRoleAssignments) {
          $changes += New-BoxChange update "unique access on $($s.name)/$($l.name)"
          if ($Apply) { Set-PnPList -Identity $l.name -BreakRoleInheritance -CopyRoleAssignments:$false -ClearSubscopes -Connection $sc | Out-Null }
        }
        $ras = @()
        if ($list.HasUniqueRoleAssignments) {
          $ctx = Get-PnPContext -Connection $sc
          $ras = @(Get-PnPProperty -ClientObject $list -Property RoleAssignments -Connection $sc | ForEach-Object { Get-PnPProperty -ClientObject $_ -Property Member -Connection $sc | Out-Null; [string]$_.Member.LoginName })
        }
        foreach ($p in $l.access.PSObject.Properties) {
          $login = Get-GroupLogin $p.Name
          if ($login -notin $ras) {
            $changes += New-BoxChange update "$($p.Name) as $($p.Value) on $($s.name)/$($l.name)"
            if ($Apply) { Set-PnPListPermission -Identity $l.name -User $login -AddRole $Levels[[string]$p.Value] -Connection $sc -ErrorAction Stop }
          }
        }
      }
      if ($l.folders -and $list) { $root = "$url"; Add-Folders $sc $root $l.folders ([ref]$changes) -Apply:$Apply }
    }
  }
  return $changes
}

# ---------- sharepoint.register ----------
$TypeMap = @{ id = 'Text'; text = 'Text'; email = 'Text'; url = 'Text'; long = 'Note'; json = 'Note'; enum = 'Choice'; date = 'DateTime'; datetime = 'DateTime'; number = 'Number'; bool = 'Boolean' }
function Get-AdminSiteUrl($In) {
  $a = @($In.config.structure.sites | Where-Object { $_.name -eq 'Admin' })[0]
  return Get-SiteUrl $In $a.url
}
function Get-RegisterChanges($In, [switch] $Apply) {
  $changes = @()
  $sc = Connect-Site (Get-AdminSiteUrl $In)
  $def = $In.defaults.lists
  $lists = $def.lists.PSObject.Properties
  # Pass 1: lists and plain columns. Pass 2: lookups (every list exists by then).
  foreach ($pass in 1, 2) {
    foreach ($lp in $lists) {
      $name = $lp.Name
      $list = Get-PnPList -Identity $name -Connection $sc -ErrorAction SilentlyContinue
      if (-not $list) {
        if ($pass -eq 2) { continue }
        $changes += New-BoxChange create "list $name"
        if (-not $Apply) { continue }
        $list = New-PnPList -Title $name -Url "Lists/$name" -Template GenericList -EnableVersioning -Connection $sc -ErrorAction Stop
      }
      $have = @{}
      foreach ($f in @(Get-PnPField -List $name -Connection $sc)) { $have[[string]$f.InternalName] = $f }
      foreach ($col in @($lp.Value.columns)) {
        $type = [string]$col.type
        $isRef = $type -like 'ref(*'
        if (($pass -eq 1) -eq $isRef) { continue }
        if ($have.ContainsKey([string]$col.name)) { continue }
        $changes += New-BoxChange create "column $name.$($col.name)" $type
        if (-not $Apply) { continue }
        if ($isRef) {
          $target = $type.Substring(4).TrimEnd(')')
          $tl = Get-PnPList -Identity $target -Connection $sc -ErrorAction Stop
          $xml = "<Field Type='Lookup' DisplayName='$($col.name)' Name='$($col.name)' StaticName='$($col.name)' List='{$($tl.Id)}' ShowField='Title' Indexed='TRUE' />"
          Add-PnPFieldFromXml -List $name -FieldXml $xml -Connection $sc -ErrorAction Stop | Out-Null
          continue
        }
        $p = @{ List = $name; DisplayName = [string]$col.name; InternalName = [string]$col.name; Type = $TypeMap[$type]; Connection = $sc; ErrorAction = 'Stop' }
        if ($type -eq 'enum') { $p.Choices = @($def.enums.([string]$col.enum)) ; if (-not $p.Choices) { $p.Choices = @('value') } }
        if ($col.required) { $p.Required = $true }
        Add-PnPField @p | Out-Null
        if ($type -eq 'id') {
          Set-PnPField -List $name -Identity ([string]$col.name) -Values @{ Indexed = $true; EnforceUniqueValues = $true } -Connection $sc -ErrorAction Stop | Out-Null
        }
      }
    }
  }
  # Events: append-only (R10). Owners read; the flow owner may only add.
  $ev = Get-PnPList -Identity 'Events' -Includes HasUniqueRoleAssignments -Connection $sc -ErrorAction SilentlyContinue
  if ($ev -and -not $ev.HasUniqueRoleAssignments) {
    $changes += New-BoxChange update 'Events append-only permissions'
    if ($Apply) {
      if (-not (Get-PnPRoleDefinition -Identity 'Box Add Only' -Connection $sc -ErrorAction SilentlyContinue)) {
        Add-PnPRoleDefinition -RoleName 'Box Add Only' -Include ViewListItems, AddListItems, OpenItems, ViewVersions, ViewPages, Open, BrowseUserInfo, UseRemoteAPIs -Description 'Add items; never edit or delete (R10)' -Connection $sc | Out-Null
      }
      Set-PnPList -Identity 'Events' -BreakRoleInheritance -CopyRoleAssignments:$false -ClearSubscopes -Connection $sc | Out-Null
      Set-PnPListPermission -Identity 'Events' -User (Get-GroupLogin 'grp-admins') -AddRole 'Read' -Connection $sc
      $fo = @($In.config.users | Where-Object { $_.id -eq $In.config.tenant.flow_owner })[0]
      if ($fo) { Set-PnPListPermission -Identity 'Events' -User "$($fo.primary)@$($In.tenant.initial_domain)" -AddRole 'Box Add Only' -Connection $sc }
    }
  }
  return $changes
}

# ---------- sharepoint.config-store ----------
function Get-Sha([string] $s) { return [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($s))) }
function Get-StoreChanges($In, [switch] $Apply) {
  $changes = @()
  $sc = Connect-Site (Get-AdminSiteUrl $In)
  $doc = [string]$In.document
  if (-not $doc) { throw 'no configuration document in the input' }
  $hash = Get-Sha $doc
  $cur = $null
  if (-not (Get-PnPList -Identity 'Configuration' -Connection $sc -ErrorAction SilentlyContinue)) {
    if ($Apply) { throw 'the Configuration library does not exist (run sharepoint.libraries)' }
    return @(New-BoxChange update 'Configuration/box-config.yaml' 'after the Admin site exists')
  }
  try { $cur = Get-PnPFile -Url 'Configuration/box-config.yaml' -AsString -Connection $sc -ErrorAction Stop } catch { $cur = $null }
  if ($null -eq $cur -or (Get-Sha $cur) -ne $hash) {
    $changes += New-BoxChange update 'Configuration/box-config.yaml' $(if ($null -eq $cur) { 'first version' } else { 'new version' })
    if ($Apply) {
      $ms = [IO.MemoryStream]::new([Text.Encoding]::UTF8.GetBytes($doc))
      Add-PnPFile -Folder 'Configuration' -FileName 'box-config.yaml' -Stream $ms -Connection $sc -ErrorAction Stop | Out-Null
      Add-PnPListItem -List 'Events' -Values @{ Title = "configuration saved"; event_id = [guid]::NewGuid().ToString(); at = (Get-Date).ToUniversalTime().ToString('o'); actor = 'box pipeline'
        entity = 'configuration'; entity_id = 'box-config.yaml'; action = 'save'; to_state = $hash.Substring(0, 12) } -Connection $sc -ErrorAction Stop | Out-Null
    }
  }
  # Dictionary: seed values, never overwriting an existing one (T3-03 step 5).
  $have = @{}
  $dict = Get-PnPList -Identity 'Dictionary' -Connection $sc -ErrorAction SilentlyContinue
  if (-not $dict) {
    if ($Apply) { throw 'the Dictionary list does not exist (run sharepoint.register)' }
    return @($changes) + @(New-BoxChange create 'Dictionary values' 'after the register exists')
  }
  foreach ($i in @(Get-PnPListItem -List 'Dictionary' -PageSize 500 -Connection $sc)) { $have[[string]$i['key']] = $true }
  if ($In.dictionary) {
    foreach ($p in $In.dictionary.PSObject.Properties) {
      if ($have.ContainsKey($p.Name) -or -not $p.Value) { continue }
      $changes += New-BoxChange create "Dictionary $($p.Name)"
      if ($Apply) {
        Add-PnPListItem -List 'Dictionary' -Values @{ Title = $p.Name; key = $p.Name; value = [string]$p.Value; status = 'CONFIRMED' } -Connection $sc -ErrorAction Stop | Out-Null
      }
    }
  }
  return $changes
}

function Result-Changes($c) { return @{ changes = @($c | Where-Object { $null -ne $_ }) } }
function New-Evidence([string] $Kind, [string] $Summary) { return @{ items = @(@{ kind = $Kind; ref = "tenant:$Kind"; summary = $Summary }) } }
$ok = { param($In) @{ ok = $true } }
$script:Steps = [ordered]@{
  'sharepoint.tenant' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-TenantChanges $In) }
    apply = { param($In) Result-Changes (Get-TenantChanges $In -Apply) }
    verify = { param($In) $c = @(Get-TenantChanges $In | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'sharepoint-tenant' 'declared state verified' }
  }
  'sharepoint.sites' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-SiteChanges $In) }
    apply = { param($In) Result-Changes (Get-SiteChanges $In -Apply) }
    verify = { param($In) $c = @(Get-SiteChanges $In | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'sites' 'declared state verified' }
  }
  'sharepoint.libraries' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-LibraryChanges $In) }
    apply = { param($In) Result-Changes (Get-LibraryChanges $In -Apply) }
    verify = { param($In) $c = @(Get-LibraryChanges $In | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'libraries' 'declared state verified' }
  }
  'sharepoint.register' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-RegisterChanges $In) }
    apply = { param($In) Result-Changes (Get-RegisterChanges $In -Apply) }
    verify = { param($In) $c = @(Get-RegisterChanges $In | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'register' 'declared state verified' }
  }
  'sharepoint.config-store' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-StoreChanges $In) }
    apply = { param($In) Result-Changes (Get-StoreChanges $In -Apply) }
    verify = { param($In) $c = @(Get-StoreChanges $In | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'configuration-store' 'declared state verified' }
  }
}

function Invoke-BoxStep {
  param([string] $Step, [string] $Operation, $InputObject)
  Invoke-BoxDispatch -Steps $script:Steps -Step $Step -Operation $Operation -InputObject $InputObject
}
function Get-BoxStepIds { @($script:Steps.Keys) }
function Get-BoxStepProblems { Test-BoxStepTable -Steps $script:Steps }
Export-ModuleMember -Function Invoke-BoxStep, Get-BoxStepIds, Get-BoxStepProblems
