# Provider: sharepoint. Tenant sharing, hub and sites, libraries, register
# lists and the configuration store (T3, T0-06). Connects with PnP using the
# pipeline app's token for the tenant's SharePoint host or, for box audit, a
# reader-only delegated token for the admin host (T16-06).
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxProvider.psm1') -Force
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxGraph.psm1') -Force

$script:Conn = @{}
$script:AuditSkipped = @() # settings box audit could not judge (policy.sharepoint verify)
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
    # Unlicensed accounts have no email: compare the claims login name's UPN.
    $admins = @(Get-PnPSiteCollectionAdmin -Connection $sc | ForEach-Object { (([string]$_.LoginName) -split '\|')[-1].ToLower() })
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
function Get-ListPrincipals($Conn, [string] $Title) {
  # Login names holding a role on a list with unique permissions.
  $ctx = $Conn.Context
  $ras = $ctx.Web.Lists.GetByTitle($Title).RoleAssignments
  $ctx.Load($ras)
  $ctx.ExecuteQuery()
  foreach ($ra in $ras) { $ctx.Load($ra.Member) }
  $ctx.ExecuteQuery()
  return @($ras | ForEach-Object { [string]$_.Member.LoginName })
}
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
        New-PnPList -Title $l.name -Url $url -Template DocumentLibrary -EnableVersioning -Connection $sc -ErrorAction Stop | Out-Null
        $list = Get-PnPList -Identity $l.name -Connection $sc -ErrorAction Stop
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
        if ($list.HasUniqueRoleAssignments) { $ras = Get-ListPrincipals $sc $l.name }
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
  $a = @($In.config.structure.sites | Where-Object { $_.name -eq 'Admin' })
  if ($a.Count -ne 1) { throw "the step's input has no Admin site: add 'structure' to its reads" }
  return Get-SiteUrl $In $a[0].url
}
function Get-RegisterChanges($In, [switch] $Apply) {
  $changes = @()
  $sc = Connect-Site (Get-AdminSiteUrl $In)
  $def = $In.defaults.lists
  $lists = $def.lists.PSObject.Properties
  # A register list's title may be held by a library (the default library
  # is titled "Documents"): rename the library and remove register columns
  # a previous run added to it by mistake.
  foreach ($lp in $lists) {
    $other = Get-PnPList -Identity $lp.Name -Includes BaseTemplate, RootFolder -Connection $sc -ErrorAction SilentlyContinue
    if ($other -and $other.BaseTemplate -eq 101) {
      $changes += New-BoxChange update "rename library '$($lp.Name)' to 'Site documents'" 'its title belongs to a register list'
      if ($Apply) {
        Set-PnPList -Identity $other.Id -Title 'Site documents' -Connection $sc -ErrorAction Stop | Out-Null
        foreach ($col in @($lp.Value.columns)) {
          if (Get-PnPField -List $other.Id -Identity ([string]$col.name) -Connection $sc -ErrorAction SilentlyContinue) {
            Remove-PnPField -List $other.Id -Identity ([string]$col.name) -Force -Connection $sc -ErrorAction Stop
          }
        }
      }
    }
  }
  # Pass 1: lists and plain columns. Pass 2: lookups (every list exists by then).
  # Register lists are found by URL (Lists/<name>), never by title alone.
  foreach ($pass in 1, 2) {
    foreach ($lp in $lists) {
      $name = $lp.Name
      $list = Get-PnPList -Identity "Lists/$name" -Connection $sc -ErrorAction SilentlyContinue
      if (-not $list) {
        if ($pass -eq 2) { continue }
        $changes += New-BoxChange create "list $name"
        if (-not $Apply) { continue }
        try { New-PnPList -Title $name -Url "Lists/$name" -Template GenericList -EnableVersioning -Connection $sc -ErrorAction Stop | Out-Null }
        catch { throw "create list '$name': $($_.Exception.Message)" }
        # New-PnPList does not reliably return the list: read it back.
        $list = Get-PnPList -Identity "Lists/$name" -Connection $sc -ErrorAction Stop
      }
      $have = @{}
      foreach ($f in @(Get-PnPField -List $list.Id -Connection $sc)) { $have[[string]$f.InternalName] = $f }
      foreach ($col in @($lp.Value.columns)) {
        $type = [string]$col.type
        $isRef = $type -like 'ref(*'
        if (($pass -eq 1) -eq $isRef) { continue }
        if ($have.ContainsKey([string]$col.name)) { continue }
        $changes += New-BoxChange create "column $name.$($col.name)" $type
        if (-not $Apply) { continue }
        if ($isRef) {
          $target = $type.Substring(4).TrimEnd(')')
          $tl = Get-PnPList -Identity "Lists/$target" -Connection $sc -ErrorAction Stop
          $xml = "<Field Type='Lookup' DisplayName='$($col.name)' Name='$($col.name)' StaticName='$($col.name)' List='{$($tl.Id)}' ShowField='Title' Indexed='TRUE' />"
          Add-PnPFieldFromXml -List $list.Id -FieldXml $xml -Connection $sc -ErrorAction Stop | Out-Null
          continue
        }
        $p = @{ List = $list.Id; DisplayName = [string]$col.name; InternalName = [string]$col.name; Type = $TypeMap[$type]; Connection = $sc; ErrorAction = 'Stop' }
        if ($type -eq 'enum') { $p.Choices = @($def.enums.([string]$col.enum)) ; if (-not $p.Choices) { $p.Choices = @('value') } }
        if ($col.required) { $p.Required = $true }
        Add-PnPField @p | Out-Null
        if ($type -eq 'id') {
          Set-PnPField -List $list.Id -Identity ([string]$col.name) -Values @{ Indexed = $true; EnforceUniqueValues = $true } -Connection $sc -ErrorAction Stop | Out-Null
        }
      }
    }
  }
  # Events: append-only (R10). Owners read; the flow owner may only add.
  $ev = Get-PnPList -Identity 'Lists/Events' -Includes HasUniqueRoleAssignments -Connection $sc -ErrorAction SilentlyContinue
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

# ---------- policy.sharepoint (T14-09) ----------
# Applies the resolved sharepoint settings from their mapping payloads
# (expected-state/setting-ids/sharepoint.json). SharePoint has no audit form:
# a setting in mode audit or report is compared and reported, never written.

# Set-PnPTenant parameters whose Get-PnPTenant property has another name.
$TenantReadName = @{ IsSharePointAddInsDisabled = 'SharePointAddInsDisabled' }
# Set together: SharePoint refuses one of the version-limit values on its own.
$VersionKeys = @('EnableAutoExpirationVersionTrim', 'MajorVersionLimit', 'ExpireVersionsAfterDays')

function Get-BoxProp($Obj, [string] $Name) {
  if ($null -eq $Obj) { return $null }
  if ($Obj -is [System.Collections.IDictionary]) { return $Obj[$Name] }
  $p = $Obj.PSObject.Properties[$Name]
  if ($p) { return $p.Value }
  return $null
}

function ConvertTo-BoxComparable($Value) {
  if ($null -eq $Value) { return '' }
  if ($Value -is [timespan]) { return $Value.ToString('c') }
  if ($Value -is [System.Collections.IEnumerable] -and $Value -isnot [string]) { return (@($Value | ForEach-Object { ([string]$_).ToLower() }) | Sort-Object) -join ',' }
  return ([string]$Value).ToLower()
}

# Differences between an object read from the tenant and the wanted values.
function Compare-BoxProperties {
  param($Current, [System.Collections.IDictionary] $Want, [hashtable] $ReadName = @{})
  foreach ($k in $Want.Keys) {
    $rn = if ($ReadName.ContainsKey($k)) { $ReadName[$k] } else { $k }
    $cur = Get-BoxProp $Current $rn
    if ((ConvertTo-BoxComparable $cur) -ne (ConvertTo-BoxComparable $Want[$k])) {
      [pscustomobject]@{ name = $k; current = $cur; want = $Want[$k] }
    }
  }
}

# Merges one payload part (for example 'Set-PnPTenant') across settings; two
# settings that want different values for one parameter are a mapping error.
function Merge-BoxPayload {
  # $From, when given, is filled with parameter -> setting id.
  param($Settings, [string] $Part, [hashtable] $From = @{})
  $want = [ordered]@{}
  $from = $From
  foreach ($s in @($Settings)) {
    $p = Get-BoxProp $s.payload $Part
    if ($null -eq $p) { continue }
    $props = if ($p -is [System.Collections.IDictionary]) { $p.GetEnumerator() | ForEach-Object { @{ Name = $_.Key; Value = $_.Value } } } else { $p.PSObject.Properties }
    foreach ($q in $props) {
      if ($want.Contains($q.Name) -and (ConvertTo-BoxComparable $want[$q.Name]) -ne (ConvertTo-BoxComparable $q.Value)) {
        throw "$($s.id) and $($from[$q.Name]) want different values for $Part $($q.Name)"
      }
      $want[$q.Name] = $q.Value
      # Every setting that wants the value (SPO-001 and SPO-004 both set SharingCapability).
      $from[$q.Name] = if ($from.ContainsKey($q.Name) -and $from[$q.Name] -notmatch "\b$($s.id)\b") { "$($from[$q.Name]), $($s.id)" } else { [string]$s.id }
    }
  }
  return $want
}

# The tenant values sharepoint.tenant (T3-01) writes from baseline/sharepoint.yaml.
function Get-BaselineTenantWant($In) {
  $b = $In.defaults.baseline_sharepoint.tenant
  return [ordered]@{
    SharingCapability = @{ existing_guests_only = 'ExistingExternalUserSharingOnly'; off = 'Disabled'; new_guests = 'ExternalUserSharingOnly' }[[string]$b.sharing_capability]
    DefaultSharingLinkType = @{ specific_people = 'Direct'; organization = 'Internal' }[[string]$b.default_sharing_link]
    DefaultLinkPermission = @{ view = 'View'; edit = 'Edit' }[[string]$b.default_link_permission]
    LegacyAuthProtocolsEnabled = -not ($b.legacy_auth -eq 'off' -or $b.legacy_auth -eq $false)
    OneDriveSharingCapability = if ($b.onedrive_sharing -eq 'off' -or $b.onedrive_sharing -eq $false) { 'Disabled' } else { 'ExistingExternalUserSharingOnly' }
  }
}

# Both steps write five tenant values. They must agree, or each run of one
# would undo the other: refuse instead of flip-flopping.
function Test-BaselineAgreement {
  param([System.Collections.IDictionary] $Baseline, [System.Collections.IDictionary] $Policy)
  foreach ($k in $Baseline.Keys) {
    if ($Policy.Contains($k) -and (ConvertTo-BoxComparable $Baseline[$k]) -ne (ConvertTo-BoxComparable $Policy[$k])) {
      "tenant $k`: baseline/sharepoint.yaml (sharepoint.tenant) wants $($Baseline[$k]), the resolved policy wants $($Policy[$k])"
    }
  }
}

# Tenant values SharePoint accepts and then ignores because another tenant
# value already decides them (seen on the test tenant: Set-PnPTenant returns
# without error and the read-back is unchanged). They are not written and not
# drift; plan and check log why.
function Get-SupersededTenantKeys($Tenant) {
  $out = @{}
  if ((Get-BoxProp $Tenant 'EnableAzureADB2BIntegration') -eq $true) {
    $out['RequireAcceptingAccountMatchInvitedAccount'] = 'Entra B2B integration is on: Entra requires the invited account to sign in'
  }
  if ([string](Get-BoxProp $Tenant 'OneDriveSharingCapability') -in @('Disabled', '0')) {
    $out['NotifyOwnersWhenInvitationsAccepted'] = 'OneDrive external sharing is off: no invitation can be accepted'
  }
  return $out
}

# Set-PnPTenant -ConditionalAccessPolicy AllowLimitedAccess makes SharePoint
# create two Conditional Access policies of its own, enabled and without the
# break-glass exclusion ("[SharePoint admin center]Block access from apps on
# unmanaged devices - <date>" and "...Use app-enforced Restrictions for browser
# access - <date>"), and SharePoint re-enables them after a change (seen on
# the test tenant). Every Conditional Access policy must be report-only while
# the break-glass gate G-02 is open and must always exclude grp-break-glass
# (SPEC 20.2), so SPO-008 is held: not written, listed by plan and check.
$HeldTenantKeys = @{ ConditionalAccessPolicy = 'SPO-008 held: SharePoint would create enabled Conditional Access policies without the break-glass exclusion; apply with ENT-009 once box check break-glass passes' }
$SharePointCaPrefix = '[SharePoint admin center]'

# What a SharePoint-generated Conditional Access policy needs to be safe:
# report-only and grp-break-glass excluded.
function Get-CaPolicyFix {
  param($Policy, [string] $BreakGlassId)
  $fix = @{}
  if ([string]$Policy.state -ne 'enabledForReportingButNotEnforced') { $fix.state = 'enabledForReportingButNotEnforced' }
  $ex = @($Policy.conditions.users.excludeGroups | Where-Object { $_ })
  if ($BreakGlassId -notin $ex) { $fix.conditions = @{ users = @{ excludeGroups = @($ex + $BreakGlassId) } } }
  return $fix
}

function Get-SharePointCaChanges([switch] $Apply) {
  $bg = @(Get-BoxGraphAll "/groups?`$filter=displayName eq 'grp-break-glass'&`$select=id")
  if ($bg.Count -ne 1) { throw 'group grp-break-glass not found (run identity.groups)' }
  $changes = @()
  foreach ($p in @(Get-BoxGraphAll '/identity/conditionalAccess/policies' | Where-Object { ([string]$_.displayName).StartsWith($SharePointCaPrefix) })) {
    $fix = Get-CaPolicyFix $p $bg[0].id
    if (-not $fix.Count) { continue }
    $changes += New-BoxChange update "conditional access policy $($p.displayName)" "report-only, grp-break-glass excluded (was $($p.state))"
    if ($Apply) { Invoke-BoxGraph PATCH "/identity/conditionalAccess/policies/$($p.id)" $fix | Out-Null }
  }
  return $changes
}

function Get-PolicySettings($In) {
  if ($In.policy_error) { throw "policy resolution failed: $($In.policy_error)" }
  $all = @($In.policy.settings | Where-Object { $_ })
  foreach ($s in $all | Where-Object { $_.mode -ne 'enforce' }) {
    Write-Warning "$($s.id) is in mode $($s.mode): $($s.centre) has no audit form; compared, not written"
  }
  return $all
}

function Get-EngineSites($In) {
  # The hub and the configured sites, with the per-site sharing value sharepoint.sites uses.
  $map = @{ off = 'Disabled'; existing_guests = 'ExistingExternalUserSharingOnly'; new_guests = 'ExternalUserSharingOnly' }
  $out = @(@{ url = (Get-SiteUrl $In $In.config.structure.hub.url); sharing = 'Disabled' })
  foreach ($s in @($In.config.structure.sites)) { $out += @{ url = (Get-SiteUrl $In $s.url); sharing = $map[[string]$s.external]; site = $s } }
  return $out
}

function Get-SpPolicyChanges($In, [switch] $Apply) {
  $all = Get-PolicySettings $In
  $enf = @($all | Where-Object { $_.mode -eq 'enforce' })
  $ids = @{}
  foreach ($s in $enf) { $ids[[string]$s.id] = $s }
  # box audit (SPEC §26) reads a tenant the box did not build, as a reader:
  # the engine's own sites, the break-glass group and the company's lists
  # are not there to compare with, and a Global Reader cannot open a site's
  # content. Those parts are reported not read (skipped), never assumed.
  $audit = ([string]$In.environment -eq 'audit')
  $script:AuditSkipped = @()
  $c = Connect-Admin $In
  $changes = @()

  # Tenant values (SPO-001, 003, 004, 005, 006, 008, 010, 012, 013, 015, 017).
  $from = @{}
  $want = Merge-BoxPayload $enf 'Set-PnPTenant' $from
  $conflicts = @(Test-BaselineAgreement (Get-BaselineTenantWant $In) $want)
  if ($conflicts.Count) { throw ($conflicts -join '; ') }
  if ($audit) {
    foreach ($k in @($want.Keys)) {
      if ([string]$want[$k] -match '\{config:([A-Za-z0-9_.\[\]]+)\}') {
        $cfg = $Matches[1]
        foreach ($id in ($from[$k] -split ', ')) { $script:AuditSkipped += @{ id = $id; reason = "tenant $k is the company's own list (configuration $cfg), which an audit does not have; not compared" } }
        $want.Remove($k)
      }
    }
  }
  if ($want.Count) {
    $t = Get-PnPTenant -Connection $c
    $set = @{}
    $moot = Get-SupersededTenantKeys $t
    foreach ($d in @(Compare-BoxProperties $t $want $TenantReadName)) {
      if ($moot.ContainsKey($d.name)) { Write-Warning "tenant $($d.name) stays $($d.current): $($moot[$d.name])"; continue }
      # Held keys are a reason not to write; an audit reports the difference.
      if ($HeldTenantKeys.ContainsKey($d.name) -and -not $audit) { Write-Warning "tenant $($d.name) stays $($d.current): $($HeldTenantKeys[$d.name])"; continue }
      $changes += New-BoxChange update "tenant $($d.name)" "$($d.current) -> $($d.want) ($($from[$d.name]))"
      $set[$d.name] = $d.want
    }
    if (@($set.Keys | Where-Object { $_ -in $VersionKeys }).Count) { foreach ($k in $VersionKeys) { if ($want.Contains($k)) { $set[$k] = $want[$k] } } }
    if ($Apply -and $set.Count) { Set-PnPTenant @set -Connection $c -ErrorAction Stop }
  }
  # Policies SharePoint generated for unmanaged devices (any earlier run, or
  # a change in the admin centre) are made safe on every run. They name the
  # box's break-glass group, so an audit does not check them.
  if (-not $audit) { $changes += @(Get-SharePointCaChanges -Apply:$Apply) }

  # Site sharing (SPO-002 for the engine's sites, SPO-004 for every site),
  # custom script (SPO-015) and, below, access requests (SPO-016).
  $sites = @(Get-PnPTenantSite -Connection $c -ErrorAction Stop)
  $engine = @{}
  if ($audit) {
    $perSite = "reads each site's content, which a Global Reader cannot open (SharePoint gives it the admin centre only); not read"
    if ($ids.ContainsKey('SPO-002')) {
      $shared = @($sites | Where-Object { [string]$_.SharingCapability -ne 'Disabled' }).Count
      $script:AuditSkipped += @{ id = 'SPO-002'; reason = "which sites may share outside is the company's decision, which an audit does not have; $shared of $($sites.Count) site(s) allow external sharing" }
    }
    if ($ids.ContainsKey('SPO-013')) { $script:AuditSkipped += @{ id = 'SPO-013'; reason = "library versions: $perSite (the tenant version limits were compared)" } }
    if ($ids.ContainsKey('SPO-016')) { $script:AuditSkipped += @{ id = 'SPO-016'; reason = "access requests: $perSite" } }
  } else {
    foreach ($e in Get-EngineSites $In) { $engine[$e.url.ToLower()] = $e }
  }
  foreach ($ts in $sites) {
    $u = ([string]$ts.Url).TrimEnd('/')
    $e = $engine[$u.ToLower()]
    $wantShare, $shareId = $null, $null
    if ($ids.ContainsKey('SPO-002') -and $e) { $wantShare, $shareId = $e.sharing, 'SPO-002' }
    elseif ($ids.ContainsKey('SPO-004') -and [string]$ts.SharingCapability -eq 'ExternalUserAndGuestSharing') { $wantShare, $shareId = 'ExistingExternalUserSharingOnly', 'SPO-004' }
    if ($wantShare -and [string]$ts.SharingCapability -ne $wantShare) {
      $changes += New-BoxChange update "sharing on $u" "$($ts.SharingCapability) -> $wantShare ($shareId)"
      if ($Apply) { Set-PnPTenantSite -Identity $u -SharingCapability $wantShare -Connection $c -ErrorAction Stop }
    }
    if ($ids.ContainsKey('SPO-015') -and [string]$ts.DenyAddAndCustomizePages -ne 'Enabled') {
      $changes += New-BoxChange update "custom script on $u" "$($ts.DenyAddAndCustomizePages) -> Enabled (denied) (SPO-015)"
      if ($Apply) { Set-PnPTenantSite -Identity $u -DenyAddAndCustomizePages:$true -Connection $c -ErrorAction Stop }
    }
  }

  # SPO-009: browser idle sign-out.
  $p = Get-BoxProp $ids['SPO-009'] 'payload'
  if ($p) {
    $w = Get-BoxProp $p 'Set-PnPBrowserIdleSignout'
    $want9 = [ordered]@{ Enabled = [bool](Get-BoxProp $w 'Enabled'); WarnAfter = [timespan](Get-BoxProp $w 'WarnAfter'); SignOutAfter = [timespan](Get-BoxProp $w 'SignoutAfter') }
    $cur = Get-PnPBrowserIdleSignout -Connection $c -ErrorAction Stop
    $d = @(Compare-BoxProperties $cur $want9)
    if ($d.Count) {
      $changes += New-BoxChange update 'browser idle sign-out' ((($d | ForEach-Object { "$($_.name) $($_.current) -> $($_.want)" }) -join '; ') + ' (SPO-009)')
      if ($Apply) { Set-PnPBrowserIdleSignout -Enabled $want9.Enabled -WarnAfter $want9.WarnAfter -SignoutAfter $want9.SignOutAfter -Connection $c -ErrorAction Stop }
    }
  }

  # SPO-014: self-service site creation, a CSOM tenant property only.
  $p = Get-BoxProp $ids['SPO-014'] 'payload'
  if ($p) {
    $wantOff = [bool](Get-BoxProp (Get-BoxProp $p 'Tenant(CSOM)') 'SelfServiceSiteCreationDisabled')
    $ctx = Get-PnPContext -Connection $c
    $tn = New-Object Microsoft.Online.SharePoint.TenantAdministration.Tenant($ctx)
    $ctx.Load($tn)
    $ctx.ExecuteQuery()
    if ([bool]$tn.SelfServiceSiteCreationDisabled -ne $wantOff) {
      $changes += New-BoxChange update 'tenant SelfServiceSiteCreationDisabled' "$($tn.SelfServiceSiteCreationDisabled) -> $wantOff (SPO-014)"
      if ($Apply) { $tn.SelfServiceSiteCreationDisabled = $wantOff; $tn.Update(); $ctx.ExecuteQuery() }
    }
  }

  # SPO-016 (access requests off) and SPO-013 (library versions) on the engine's sites.
  foreach ($e in $engine.Values) {
    if (-not ($sites | Where-Object { ([string]$_.Url).TrimEnd('/') -eq $e.url })) { continue }
    $sc = $null
    if ($ids.ContainsKey('SPO-016')) {
      $sc = Connect-Site $e.url
      $web = Get-PnPWeb -Includes RequestAccessEmail, UseAccessRequestDefault -Connection $sc
      if ($web.UseAccessRequestDefault -or $web.RequestAccessEmail) {
        $changes += New-BoxChange update "access requests on $($e.url)" 'on -> off (SPO-016)'
        if ($Apply) { Set-PnPRequestAccessEmails -Disabled -Connection $sc -ErrorAction Stop }
      }
    }
    if ($ids.ContainsKey('SPO-013') -and $e.site) {
      if (-not $sc) { $sc = Connect-Site $e.url }
      foreach ($l in @($e.site.libraries | Where-Object { $_ })) {
        $list = Get-PnPList -Identity $l.name -Includes EnableVersioning, MajorVersionLimit -Connection $sc -ErrorAction SilentlyContinue
        if (-not $list) { continue } # sharepoint.libraries creates it with versioning
        if (-not $list.EnableVersioning -or $list.MajorVersionLimit -lt 100) {
          $changes += New-BoxChange update "versions on $($e.url)/$($l.name)" "$($list.EnableVersioning)/$($list.MajorVersionLimit) -> True/100 (SPO-013)"
          if ($Apply) { Set-PnPList -Identity $l.name -EnableVersioning $true -MajorVersions 100 -Connection $sc -ErrorAction Stop | Out-Null }
        }
      }
    }
  }
  return $changes
}

# SPO-018: drift the step reports and never repairs (removing a person's
# access is a decision for the owner): role assignments on the engine's sites
# held by an individual user instead of a group.
function Get-SpPolicyFindings($In) {
  $all = Get-PolicySettings $In
  if (-not ($all | Where-Object { $_.id -eq 'SPO-018' -and $_.mode -eq 'enforce' })) { return @() }
  if ([string]$In.environment -eq 'audit') {
    $script:AuditSkipped += @{ id = 'SPO-018'; reason = "role assignments: reads each site's content, which a Global Reader cannot open (SharePoint gives it the admin centre only); not read" }
    return @()
  }
  $out = @()
  foreach ($e in Get-EngineSites $In) {
    $sc = Connect-Site $e.url
    $ctx = $sc.Context
    $ras = $ctx.Web.RoleAssignments
    $ctx.Load($ras)
    $ctx.ExecuteQuery()
    foreach ($ra in $ras) { $ctx.Load($ra.Member) }
    $ctx.ExecuteQuery()
    foreach ($ra in $ras) {
      if ([string]$ra.Member.PrincipalType -eq 'User') { $out += New-BoxChange update "individual permission on $($e.url)" "$($ra.Member.LoginName) holds a role directly (SPO-018: groups only)" }
    }
  }
  return $out
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
  'policy.sharepoint' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-SpPolicyChanges $In) }
    apply = { param($In) Result-Changes (Get-SpPolicyChanges $In -Apply) }
    verify = { param($In) $c = @(@(Get-SpPolicyChanges $In) + @(Get-SpPolicyFindings $In) | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c; skipped = @($script:AuditSkipped) } }
    evidence = { param($In) New-Evidence 'policy-sharepoint' "resolved sharepoint settings verified: $((@($In.policy.settings | ForEach-Object { $_.id })) -join ', ')" }
  }
}

function Invoke-BoxStep {
  param([string] $Step, [string] $Operation, $InputObject)
  Invoke-BoxDispatch -Steps $script:Steps -Step $Step -Operation $Operation -InputObject $InputObject
}
function Get-BoxStepIds { @($script:Steps.Keys) }
function Get-BoxStepProblems { Test-BoxStepTable -Steps $script:Steps }
Export-ModuleMember -Function Invoke-BoxStep, Get-BoxStepIds, Get-BoxStepProblems, Compare-BoxProperties, Merge-BoxPayload, Test-BaselineAgreement, Get-SupersededTenantKeys, Get-CaPolicyFix
