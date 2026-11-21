# Provider: sharepoint. Tenant sharing, hub and sites, libraries, register
# lists, the configuration store and the policy documents (T3, T0-06, T9).
# Connects with PnP using the pipeline app's token for the tenant's SharePoint
# host or, for box audit, a reader-only delegated token for the admin host (T16-06).
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
function Get-FieldChoices($Field) {
  # A choice column's values, read from its schema (works for any field type PnP returns).
  $x = [string]$Field.SchemaXml
  if (-not $x) { return @() }
  return @(([xml]$x).SelectNodes('//CHOICES/CHOICE') | ForEach-Object { [string]$_.InnerText })
}
function Get-MissingChoices($Field, [string[]] $Want) {
  $have = @(Get-FieldChoices $Field)
  if (-not [string]$Field.SchemaXml) { return @() } # schema not read: nothing to compare
  return @($Want | Where-Object { $_ -and $_ -notin $have })
}
function Add-FieldChoices($sc, $ListId, $Field, [string[]] $Add) {
  $ctx = $sc.Context
  $f = $ctx.Web.Lists.GetById($ListId).Fields.GetByInternalNameOrTitle([string]$Field.InternalName)
  $ctx.Load($f)
  $ctx.ExecuteQuery()
  $x = [xml]$f.SchemaXml
  $choices = $x.SelectSingleNode('//CHOICES')
  if (-not $choices) { $choices = $x.DocumentElement.AppendChild($x.CreateElement('CHOICES')) }
  foreach ($a in $Add) { $n = $x.CreateElement('CHOICE'); $n.InnerText = $a; [void]$choices.AppendChild($n) }
  $f.SchemaXml = $x.OuterXml
  $f.Update()
  $ctx.ExecuteQuery()
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
        if ($have.ContainsKey([string]$col.name)) {
          # An enum gains values over time: add the choices a column lacks.
          if ($type -eq 'enum') {
            $missing = @(Get-MissingChoices $have[[string]$col.name] @($def.enums.([string]$col.enum)))
            if ($missing.Count -gt 0) {
              $changes += New-BoxChange update "column $name.$($col.name) choices" "add $($missing -join ', ')"
              if ($Apply) { Add-FieldChoices $sc $list.Id $have[[string]$col.name] $missing }
            }
          }
          continue
        }
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

# ---------- sharepoint.control-tasks (T10-02, F17) ----------
# Keeps the next due ControlTasks item per recurring control task kind. The
# schedule (kind, control, frequency, owner role, current and next period)
# comes from the engine ($In.control_tasks, read "@controltasks"), derived
# from the documents' records and defaults/controls.yaml. The step creates
# the current period's item, and the next one once the current is DONE;
# marks an OPEN item past its due date OVERDUE; and, for kinds that prepare
# an artefact, writes the review sheet to the Compliance site's Evidence
# library and links it on the item. It never edits or deletes a DONE item
# and never marks a task done: the assignee does (box tasks done).
$DoneNeedsOutput = '=OR([state]<>"DONE",LEN([output_url])>0)'
$DoneNeedsOutputMessage = 'A control task is closed only with its output: fill output_url (the review, test or exercise record).'
$PrepareNames = @{ access_review_sheet = 'review-sheet.csv'; app_consent_list = 'app-consents.csv' }
$PrepareWords = @{
  access_review_sheet = 'review sheet of users, administrator roles, guests and site permissions'
  app_consent_list    = 'list of applications granted access, with their permissions'
}

function Get-ControlTasksInput($In) {
  if ($In.control_tasks_error) { throw "control task schedule: $($In.control_tasks_error)" }
  $ct = $In.control_tasks
  if (-not $ct -or -not $ct.today) { throw "the step's input has no control task schedule: add '@controltasks' to its reads" }
  return $ct
}

function Test-SameFormula([string] $Have, [string] $Want) {
  # SharePoint stores a validation formula without the brackets around
  # column names: compare without them and without spaces.
  $n = { param($s) ($s -replace '[\[\]\s]', '') }
  return (& $n $Have) -eq (& $n $Want)
}

function ConvertTo-BoxDay($Value) {
  # A SharePoint date-only value as yyyy-MM-dd. It is stored as midnight in
  # the site's time zone and read back in UTC: round to the nearest day.
  if ($null -eq $Value -or "$Value" -eq '') { return '' }
  if ($Value -is [string]) { if ($Value -match '^\d{4}-\d{2}-\d{2}$') { return $Value }; $Value = [datetime]::Parse($Value, [Globalization.CultureInfo]::InvariantCulture).ToUniversalTime() }
  return ([datetime]$Value).ToUniversalTime().AddHours(12).ToString('yyyy-MM-dd')
}

function Get-ControlTaskActions {
  # Pure: what the step must do. Kinds: the schedule's kinds (current and
  # next period). Items: task_id -> @{ id; state; due_on (yyyy-MM-dd);
  # prepared_url }. Today: yyyy-MM-dd. Returns actions in a stable order.
  param($Kinds, [hashtable] $Items, [string] $Today)
  $out = [System.Collections.Generic.List[object]]::new()
  foreach ($k in @($Kinds | Sort-Object { [string]$_.kind })) {
    $cur = $k.current; $next = $k.next
    $have = $Items[[string]$cur.task_id]
    if (-not $have) {
      $out.Add(@{ op = 'create'; kind = $k; period = $cur })
      if ($k.prepares) { $out.Add(@{ op = 'prepare'; kind = $k; period = $cur }) }
      continue
    }
    if ([string]$have.state -eq 'DONE') {
      if (-not $Items[[string]$next.task_id]) { $out.Add(@{ op = 'create'; kind = $k; period = $next }) }
      continue
    }
    if ($k.prepares -and -not [string]$have.prepared_url) { $out.Add(@{ op = 'prepare'; kind = $k; period = $cur }) }
  }
  foreach ($id in @($Items.Keys | Sort-Object)) {
    $i = $Items[$id]
    if ([string]$i.state -eq 'OPEN' -and [string]$i.due_on -and [string]$i.due_on -lt $Today) {
      $out.Add(@{ op = 'overdue'; task_id = $id; due_on = [string]$i.due_on })
    }
  }
  return $out.ToArray()
}

function Get-RegisterList($sc, [string] $Name, [string[]] $Includes) {
  $p = @{ Identity = "Lists/$Name"; Connection = $sc; ErrorAction = 'SilentlyContinue' }
  if ($Includes) { $p.Includes = $Includes }
  return Get-PnPList @p
}

function Get-ControlTaskItems($sc) {
  $items = @{}
  foreach ($i in @(Get-PnPListItem -List 'Lists/ControlTasks' -PageSize 500 -Connection $sc)) {
    $id = [string]$i['task_id']
    if (-not $id) { continue }
    $items[$id] = @{ id = $i.Id; state = [string]$i['state']; due_on = (ConvertTo-BoxDay $i['due_on']); prepared_url = [string]$i['prepared_url']
      output_url = [string]$i['output_url']; assignee_role = [string]$i['assignee_role']; title = [string]$i['Title']
      control = $(if ($i['control_id']) { [string]$i['control_id'].LookupValue } else { '' }); closed_at = (ConvertTo-BoxDay $i['closed_at']) }
  }
  return $items
}

function Get-ControlItems($sc) {
  $c = @{}
  foreach ($i in @(Get-PnPListItem -List 'Lists/Controls' -PageSize 500 -Connection $sc)) {
    if ([string]$i['control_id']) { $c[[string]$i['control_id']] = $i.Id }
  }
  return $c
}

function Add-BoxEvent($sc, [string] $Entity, [string] $EntityId, [string] $Action, [string] $From, [string] $To, $Detail, [string] $Actor = 'box pipeline', [string] $OnBehalfOf) {
  $v = @{ Title = "$Entity $EntityId $Action"; event_id = [guid]::NewGuid().ToString(); at = (Get-Date).ToUniversalTime().ToString('o'); actor = $Actor
    entity = $Entity; entity_id = $EntityId; action = $Action }
  if ($From) { $v.from_state = $From }
  if ($To) { $v.to_state = $To }
  if ($OnBehalfOf) { $v.on_behalf_of = $OnBehalfOf }
  if ($null -ne $Detail) { $v.detail = ($Detail | ConvertTo-Json -Depth 5 -Compress) }
  Add-PnPListItem -List 'Lists/Events' -Values $v -Connection $sc -ErrorAction Stop | Out-Null
}

function Get-ComplianceSiteUrl($In) {
  $a = @($In.config.structure.sites | Where-Object { $_.name -eq 'Compliance' })
  if ($a.Count -ne 1) { throw "the structure has no Compliance site: the control task artefacts go to its Evidence library" }
  return Get-SiteUrl $In $a[0].url
}

function Get-PreparedPath([string] $TaskId, [string] $Prepares) {
  return "Evidence/Control tasks/$TaskId/$TaskId-$($PrepareNames[$Prepares])"
}

function Save-EvidenceFile($In, [string] $Folder, [string] $FileName, [byte[]] $Bytes) {
  # Writes a file under the Compliance site's Evidence library; returns its URL.
  $cc = Connect-Site (Get-ComplianceSiteUrl $In)
  $ms = [IO.MemoryStream]::new($Bytes)
  $f = Add-PnPFile -Folder $Folder -FileName $FileName -Stream $ms -Connection $cc -ErrorAction Stop
  return "$(Get-Root $In)$($f.ServerRelativeUrl)"
}

function ConvertTo-CsvBytes($Rows, [string[]] $Columns) {
  $lines = @($Rows | Select-Object -Property $Columns | ConvertTo-Csv -NoTypeInformation -UseQuotes AsNeeded)
  if ($lines.Count -eq 0) { $lines = @(($Columns | ForEach-Object { $_ }) -join ',') }
  return [Text.Encoding]::UTF8.GetBytes(($lines -join "`r`n") + "`r`n")
}

function Get-LastSignIn($u) {
  $a = [string]$u.signInActivity.lastSignInDateTime; $b = [string]$u.signInActivity.lastNonInteractiveSignInDateTime
  if ($b -gt $a) { return $b }; return $a
}

$ReviewColumns = @('section', 'name', 'account', 'type', 'access', 'state', 'last_sign_in', 'decision')
function Get-AccessReviewRows($In) {
  # Users, administrator roles, guests and site permissions, for the
  # quarterly access review (PRC-06). A guest's address can be personal:
  # only its domain is kept. The decision column is the reviewer's.
  $rows = [System.Collections.Generic.List[object]]::new()
  $sel = 'id,displayName,userPrincipalName,userType,accountEnabled,mail,externalUserState'
  # signInActivity needs AuditLog.Read.All and Entra ID P1, and pages of at most 120.
  try { $users = @(Get-BoxGraphAll "/users?`$top=120&`$select=$sel,signInActivity") }
  catch {
    $rows.Add([pscustomobject]@{ section = 'note'; name = 'last sign-in not read'; account = ''; type = ''; access = ''; state = "$_"; last_sign_in = ''; decision = '' })
    $users = @(Get-BoxGraphAll "/users?`$top=999&`$select=$sel")
  }
  foreach ($u in @($users | Sort-Object { [string]$_.userType }, { [string]$_.displayName })) {
    $state = if ($u.accountEnabled) { 'enabled' } else { 'disabled' }
    if ([string]$u.userType -eq 'Guest') {
      $dom = ([string]$u.mail -split '@')[-1].ToLowerInvariant()
      $rows.Add([pscustomobject]@{ section = 'guest'; name = [string]$u.displayName; account = "guest from $dom"; type = 'guest'; access = ''
          state = "$state $([string]$u.externalUserState)".Trim(); last_sign_in = (Get-LastSignIn $u); decision = '' })
    } else {
      $rows.Add([pscustomobject]@{ section = 'user'; name = [string]$u.displayName; account = [string]$u.userPrincipalName; type = 'member'; access = ''
          state = $state; last_sign_in = (Get-LastSignIn $u); decision = '' })
    }
  }
  foreach ($r in @(Get-BoxGraphAll '/directoryRoles?$expand=members' | Sort-Object { [string]$_.displayName })) {
    foreach ($m in @($r.members | Where-Object { $_ })) {
      $type = ([string]$m.'@odata.type').Replace('#microsoft.graph.', '')
      $acct = [string]$m.userPrincipalName
      if ($acct -like '*#EXT#*') { $acct = 'guest' }
      if (-not $acct) { $acct = [string]$m.appId }
      $rows.Add([pscustomobject]@{ section = 'admin_role'; name = [string]$m.displayName; account = $acct; type = $type; access = [string]$r.displayName
          state = ''; last_sign_in = ''; decision = '' })
    }
  }
  foreach ($s in @($In.config.structure.sites)) {
    $sc = Connect-Site (Get-SiteUrl $In $s.url)
    foreach ($p in @(Get-SitePermissionRows $sc ([string]$s.name) '')) { $rows.Add($p) }
    foreach ($l in @(Get-PnPList -Includes HasUniqueRoleAssignments, BaseTemplate, Hidden -Connection $sc | Where-Object { $_.BaseTemplate -eq 101 -and -not $_.Hidden -and $_.HasUniqueRoleAssignments })) {
      foreach ($p in @(Get-SitePermissionRows $sc "$($s.name)/$($l.Title)" ([string]$l.Title))) { $rows.Add($p) }
    }
  }
  return $rows.ToArray()
}

function Get-SitePermissionRows($sc, [string] $Where, [string] $ListTitle) {
  # Role assignments of a site (or of one list with unique permissions),
  # with the members of each SharePoint group. Names only: a login can hold
  # a guest's address.
  $ctx = $sc.Context
  $obj = if ($ListTitle) { $ctx.Web.Lists.GetByTitle($ListTitle) } else { $ctx.Web }
  $ras = $obj.RoleAssignments
  $ctx.Load($ras)
  $ctx.ExecuteQuery()
  foreach ($ra in $ras) { $ctx.Load($ra.Member); $ctx.Load($ra.RoleDefinitionBindings) }
  $ctx.ExecuteQuery()
  $out = @()
  foreach ($ra in $ras) {
    $roles = (@($ra.RoleDefinitionBindings | ForEach-Object { $_.Name } | Where-Object { $_ -ne 'Limited Access' }) -join '; ')
    if (-not $roles) { continue }
    $m = $ra.Member
    $kind = [string]$m.PrincipalType
    if ($kind -eq 'SharePointGroup') {
      $members = @(Get-PnPGroupMember -Group $m.Title -Connection $sc -ErrorAction SilentlyContinue | Where-Object { $_.Title -and $_.Title -ne 'System Account' })
      if ($members.Count -eq 0) { $out += [pscustomobject]@{ section = 'site_permission'; name = [string]$m.Title; account = $Where; type = 'sharepoint group (empty)'; access = $roles; state = ''; last_sign_in = ''; decision = '' } }
      foreach ($x in $members) {
        $out += [pscustomobject]@{ section = 'site_permission'; name = [string]$x.Title; account = $Where; type = "via $($m.Title)"; access = $roles; state = ''; last_sign_in = ''; decision = '' }
      }
    } else {
      $out += [pscustomobject]@{ section = 'site_permission'; name = [string]$m.Title; account = $Where; type = $kind; access = $roles; state = ''; last_sign_in = ''; decision = '' }
    }
  }
  return $out
}

# Resource applications whose application permissions the consent list shows.
$ConsentResources = [ordered]@{ '00000003-0000-0000-c000-000000000000' = 'Microsoft Graph'; '00000002-0000-0ff1-ce00-000000000000' = 'Exchange Online'; '00000003-0000-0ff1-ce00-000000000000' = 'SharePoint Online' }
# Microsoft's own tenants: an app they own is published by Microsoft.
$MicrosoftTenants = @('f8cdef31-a31e-4b4a-93e4-5f571e91255a', '72f988bf-86f1-41af-91ab-2d7cd011db47')
$ConsentColumns = @('section', 'app', 'app_id', 'publisher', 'resource', 'permissions', 'granted_for', 'decision')
function Get-AppConsentRows {
  # Delegated grants and application permissions of every app, for the
  # monthly app consent review (PRC-13). User grants are counted, not named.
  $sps = @{}
  foreach ($s in @(Get-BoxGraphAll '/servicePrincipals?$top=999&$select=id,appId,displayName,appOwnerOrganizationId,publisherName,verifiedPublisher,appRoles')) { $sps[[string]$s.id] = $s }
  $publisher = {
    param($s)
    if ([string]$s.appOwnerOrganizationId -in $MicrosoftTenants) { return 'Microsoft' }
    if ($s.verifiedPublisher.displayName) { return "$($s.verifiedPublisher.displayName) (verified)" }
    if ($s.publisherName) { return "$($s.publisherName) (not verified)" }
    return 'not verified'
  }
  $rows = [System.Collections.Generic.List[object]]::new()
  $groups = @{}
  foreach ($g in @(Get-BoxGraphAll '/oauth2PermissionGrants')) {
    $key = "$($g.clientId)|$($g.resourceId)|$($g.consentType)"
    if (-not $groups.ContainsKey($key)) { $groups[$key] = @{ g = $g; scopes = [System.Collections.Generic.HashSet[string]]::new(); n = 0 } }
    $groups[$key].n++
    foreach ($sc in ([string]$g.scope -split ' ' | Where-Object { $_ })) { [void]$groups[$key].scopes.Add($sc) }
  }
  foreach ($e in $groups.Values) {
    $c = $sps[[string]$e.g.clientId]; $r = $sps[[string]$e.g.resourceId]
    $for = if ([string]$e.g.consentType -eq 'AllPrincipals') { 'everyone (administrator consent)' } else { "$($e.n) user(s), each for themselves" }
    $rows.Add([pscustomobject]@{ section = 'delegated'; app = [string]$c.displayName; app_id = [string]$c.appId; publisher = $(if ($c) { & $publisher $c } else { '' })
        resource = [string]$r.displayName; permissions = (@($e.scopes | Sort-Object) -join ' '); granted_for = $for; decision = '' })
  }
  foreach ($appId in $ConsentResources.Keys) {
    $res = @($sps.Values | Where-Object { [string]$_.appId -eq $appId })
    if ($res.Count -ne 1) { continue }
    $roleName = @{}
    foreach ($ar in @($res[0].appRoles)) { $roleName[[string]$ar.id] = [string]$ar.value }
    $byClient = @{}
    foreach ($a in @(Get-BoxGraphAll "/servicePrincipals/$($res[0].id)/appRoleAssignedTo?`$top=999")) {
      if ([string]$a.principalType -ne 'ServicePrincipal') { continue }
      $k = [string]$a.principalId
      if (-not $byClient.ContainsKey($k)) { $byClient[$k] = [System.Collections.Generic.List[string]]::new() }
      $byClient[$k].Add($(if ($roleName[[string]$a.appRoleId]) { $roleName[[string]$a.appRoleId] } else { [string]$a.appRoleId }))
    }
    foreach ($k in $byClient.Keys) {
      $c = $sps[$k]
      $rows.Add([pscustomobject]@{ section = 'application'; app = $(if ($c) { [string]$c.displayName } else { $k }); app_id = [string]$c.appId; publisher = $(if ($c) { & $publisher $c } else { '' })
          resource = $ConsentResources[$appId]; permissions = (@($byClient[$k] | Sort-Object -Unique) -join ' '); granted_for = 'the app itself (application permission)'; decision = '' })
    }
  }
  return @($rows | Sort-Object section, app, resource)
}

function New-PreparedArtefact($In, [string] $Prepares) {
  switch ($Prepares) {
    'access_review_sheet' { return ConvertTo-CsvBytes (Get-AccessReviewRows $In) $ReviewColumns }
    'app_consent_list' { return ConvertTo-CsvBytes (Get-AppConsentRows) $ConsentColumns }
    default { throw "unknown artefact '$Prepares'" }
  }
}

function Get-ControlTaskChanges($In, [switch] $Apply) {
  $ct = Get-ControlTasksInput $In
  $changes = @()
  $sc = Connect-Site (Get-AdminSiteUrl $In)
  $tl = Get-RegisterList $sc 'ControlTasks' @('ValidationFormula')
  $ready = $tl -and (Get-RegisterList $sc 'Controls') -and (Get-RegisterList $sc 'Events') -and
    (Get-PnPField -List $tl.Id -Identity 'prepared_url' -Connection $sc -ErrorAction SilentlyContinue)
  if (-not $ready) {
    if ($Apply) { throw 'the ControlTasks, Controls or Events list, or ControlTasks.prepared_url, does not exist (run sharepoint.register)' }
    return @(New-BoxChange create 'ControlTasks items' 'after sharepoint.register adds the lists and columns')
  }
  # A DONE task needs its output, whoever closes it (T10-02 exit criterion).
  if (-not (Test-SameFormula ([string]$tl.ValidationFormula) $DoneNeedsOutput)) {
    $changes += New-BoxChange update 'ControlTasks validation' 'a task is DONE only with an output_url'
    if ($Apply) {
      $tl.ValidationFormula = $DoneNeedsOutput
      $tl.ValidationMessage = $DoneNeedsOutputMessage
      $tl.Update()
      Invoke-PnPQuery -Connection $sc
    }
  }
  # The Controls item each kind's tasks point at. Existing items are left
  # as they are (T10-01 seeds and owns the control set).
  $controls = Get-ControlItems $sc
  foreach ($k in @($ct.kinds | Sort-Object { [string]$_.control_id })) {
    if ($controls.ContainsKey([string]$k.control_id)) { continue }
    $changes += New-BoxChange create "Controls $($k.control_id)" ([string]$k.statement)
    if ($Apply) {
      $item = Add-PnPListItem -List 'Lists/Controls' -Values @{ Title = [string]$k.control_id; control_id = [string]$k.control_id; statement = [string]$k.statement
        mappings = (@{ soc2 = @($k.soc2) } | ConvertTo-Json -Compress); owner_role = [string]$k.owner_role; frequency = [string]$k.frequency
        evidence_source = "task.$($k.kind)"; automated = $false; policy_ref = (@($k.policy, $k.procedure) | Where-Object { $_ }) -join ', ' } -Connection $sc -ErrorAction Stop
      $controls[[string]$k.control_id] = $item.Id
      Add-BoxEvent $sc 'Controls' ([string]$k.control_id) 'create' '' '' @{ source = "task.$($k.kind)" }
    }
  }
  $items = Get-ControlTaskItems $sc
  foreach ($a in @(Get-ControlTaskActions -Kinds $ct.kinds -Items $items -Today ([string]$ct.today))) {
    switch ($a.op) {
      'create' {
        $k = $a.kind; $p = $a.period
        $changes += New-BoxChange create "ControlTasks $($p.task_id)" "due $($p.due_on), for $($k.owner_role)"
        if ($Apply) {
          $item = Add-PnPListItem -List 'Lists/ControlTasks' -Values @{ Title = "$($k.statement) ($($p.period))"; task_id = [string]$p.task_id
            control_id = $controls[[string]$k.control_id]; due_on = [string]$p.due_on; assignee_role = [string]$k.owner_role; state = 'OPEN' } -Connection $sc -ErrorAction Stop
          $items[[string]$p.task_id] = @{ id = $item.Id; state = 'OPEN'; due_on = [string]$p.due_on; prepared_url = '' }
          Add-BoxEvent $sc 'ControlTasks' ([string]$p.task_id) 'create' '' 'OPEN' @{ kind = [string]$k.kind; due_on = [string]$p.due_on; assignee_role = [string]$k.owner_role }
        }
      }
      'prepare' {
        $k = $a.kind; $p = $a.period
        $path = Get-PreparedPath ([string]$p.task_id) ([string]$k.prepares)
        $changes += New-BoxChange create "Compliance/$path" "$($PrepareWords[[string]$k.prepares]), for $($k.owner_role) to review"
        if ($Apply) {
          $bytes = New-PreparedArtefact $In ([string]$k.prepares)
          $url = Save-EvidenceFile $In (Split-Path $path -Parent).Replace('\', '/') (Split-Path $path -Leaf) $bytes
          Set-PnPListItem -List 'Lists/ControlTasks' -Identity $items[[string]$p.task_id].id -Values @{ prepared_url = $url } -Connection $sc -ErrorAction Stop | Out-Null
          Add-BoxEvent $sc 'ControlTasks' ([string]$p.task_id) 'prepare' '' '' @{ prepared_url = $url }
        }
      }
      'overdue' {
        $changes += New-BoxChange update "ControlTasks $($a.task_id)" "OVERDUE (was due $($a.due_on))"
        if ($Apply) {
          Set-PnPListItem -List 'Lists/ControlTasks' -Identity $items[$a.task_id].id -Values @{ state = 'OVERDUE' } -Connection $sc -ErrorAction Stop | Out-Null
          Add-BoxEvent $sc 'ControlTasks' $a.task_id 'overdue' 'OPEN' 'OVERDUE' $null
        }
      }
    }
  }
  return $changes
}

function Read-ControlTasks($In) {
  $sc = Connect-Site (Get-AdminSiteUrl $In)
  if (-not (Get-RegisterList $sc 'ControlTasks')) { return @{ items = @() } }
  $items = Get-ControlTaskItems $sc
  return @{ items = @($items.Keys | Sort-Object | ForEach-Object { $i = $items[$_]; @{ task_id = $_; title = $i.title; control_id = $i.control; due_on = $i.due_on
          state = $i.state; assignee_role = $i.assignee_role; output_url = $i.output_url; prepared_url = $i.prepared_url; closed_at = $i.closed_at } }) }
}

function Test-CloseRequest($Close, $Item) {
  # Pure: the reasons a close request is refused (none: it may proceed).
  $out = @()
  if (-not $Item) { $out += "no ControlTasks item $($Close.task_id)" }
  elseif ([string]$Item.state -eq 'DONE') { $out += "$($Close.task_id) is already DONE; a completed task is never changed" }
  if (-not [string]$Close.output_url -and -not [string]$Close.file_name) { $out += 'an output is required: a URL or a file' }
  if ([string]$Close.output_url -and [string]$Close.output_url -notlike 'https://*') { $out += 'the output URL must start with https://' }
  if (-not [string]$Close.by) { $out += 'the person who did the task is required' }
  return $out
}

function Complete-ControlTask($In) {
  # box tasks done: the assignee records completion. Uploads the output when
  # it is a file, sets DONE with the output, closed_at and closed_by, and
  # writes one Evidence item and one Events item.
  $ct = Get-ControlTasksInput $In
  $close = $In.close
  if (-not $close) { throw 'no close request in the input' }
  $sc = Connect-Site (Get-AdminSiteUrl $In)
  $items = Get-ControlTaskItems $sc
  $item = $items[[string]$close.task_id]
  $why = @(Test-CloseRequest $close $item)
  if ($why.Count -gt 0) { throw ($why -join '; ') }
  $url = [string]$close.output_url
  if ([string]$close.file_name) {
    $url = Save-EvidenceFile $In "Evidence/Control tasks/$($close.task_id)" ([string]$close.file_name) ([Convert]::FromBase64String([string]$close.file_b64))
  }
  $now = (Get-Date).ToUniversalTime()
  $v = @{ state = 'DONE'; output_url = $url; closed_at = $now.ToString('o') }
  $person = @(Get-PnPListItem -List 'Lists/People' -PageSize 500 -Connection $sc -ErrorAction SilentlyContinue | Where-Object { [string]$_['person_id'] -eq [string]$close.by })
  if ($person.Count -eq 1) { $v.closed_by = $person[0].Id }
  Set-PnPListItem -List 'Lists/ControlTasks' -Identity $item.id -Values $v -Connection $sc -ErrorAction Stop | Out-Null
  $controls = Get-ControlItems $sc
  $kind = @($ct.kinds | Where-Object { [string]$_.kind -eq [string]$close.kind })[0]
  $evid = [guid]::NewGuid().ToString()
  $period = ([string]$close.task_id).Substring(([string]$close.kind).Length + 1)
  Add-PnPListItem -List 'Lists/Evidence' -Values @{ Title = "$($close.task_id) output"; evidence_id = $evid; control_id = $controls[[string]$kind.control_id]; period = $period
    artefact_url = $url; collected_at = $now.ToString('o'); collected_by = [string]$close.by } -Connection $sc -ErrorAction Stop | Out-Null
  Add-BoxEvent $sc 'ControlTasks' ([string]$close.task_id) 'close' ([string]$item.state) 'DONE' @{ output_url = $url; evidence_id = $evid; closed_by = [string]$close.by; via = 'box tasks done' } 'box pipeline' "person:$($close.by)"
  return @{ task_id = [string]$close.task_id; state = 'DONE'; output_url = $url; evidence_id = $evid; closed_by_person = ($person.Count -eq 1) }
}

# ---------- sharepoint.extension-requests (T14-18) ----------
# Opens the register's ExtensionRequests list to staff: they add requests
# with the list's form and read only their own; they edit nothing. The
# columns the owner's decision fills are left out of the form. The box
# never decides a request here: the owner does (box extensions approve or
# reject, or the home screen), and only that decision changes the
# configuration's allow list.
$ExtRequestRole = 'Box Request'
$ExtRequestStaff = 'grp-staff'
$ExtRequestFormHidden = @('Title', 'state', 'store_name', 'requester', 'decided_by', 'decided_at', 'decision_reason')

function Test-ExtFieldInNewForm($Field) {
  # A field shows in the new-item form unless its schema says otherwise.
  return ([string]$Field.SchemaXml) -notmatch 'ShowInNewForm="FALSE"'
}

function Get-ExtensionRequestChanges($In, [switch] $Apply) {
  $sc = Connect-Site (Get-AdminSiteUrl $In)
  $l = Get-RegisterList $sc 'ExtensionRequests' @('HasUniqueRoleAssignments', 'ReadSecurity', 'WriteSecurity', 'Title')
  if (-not $l) {
    if ($Apply) { throw 'the ExtensionRequests list does not exist (run sharepoint.register)' }
    return @(New-BoxChange create 'ExtensionRequests access for staff' 'after sharepoint.register adds the list')
  }
  $changes = @()
  $fields = @{}
  foreach ($f in @(Get-PnPField -List $l.Id -Connection $sc)) { $fields[[string]$f.InternalName] = $f }
  if (-not $fields.ContainsKey('state')) {
    if ($Apply) { throw 'ExtensionRequests has no state column (run sharepoint.register)' }
    return @(New-BoxChange update 'ExtensionRequests access for staff' 'after sharepoint.register adds the columns')
  }
  if ([string]$fields['state'].DefaultValue -ne 'requested') {
    $changes += New-BoxChange update 'ExtensionRequests.state default' 'requested'
    if ($Apply) { Set-PnPField -List $l.Id -Identity 'state' -Values @{ DefaultValue = 'requested' } -Connection $sc -ErrorAction Stop | Out-Null }
  }
  if ($fields['Title'].Required) {
    $changes += New-BoxChange update 'ExtensionRequests.Title' 'optional (the form asks for extension_name)'
    if ($Apply) { Set-PnPField -List $l.Id -Identity 'Title' -Values @{ Required = $false } -Connection $sc -ErrorAction Stop | Out-Null }
  }
  foreach ($name in $ExtRequestFormHidden) {
    $f = $fields[$name]
    if (-not $f -or -not (Test-ExtFieldInNewForm $f)) { continue }
    $changes += New-BoxChange update "ExtensionRequests.$name in the request form" 'hidden (filled by the box or the owner)'
    if ($Apply) {
      $cf = $sc.Context.Web.Lists.GetById($l.Id).Fields.GetByInternalNameOrTitle($name)
      $cf.SetShowInNewForm($false)
      Invoke-PnPQuery -Connection $sc
    }
  }
  # Staff read only their own requests (ReadSecurity 2) and change only
  # their own (WriteSecurity 2); their permission level has no edit right
  # at all, so they add requests and read them back.
  if ([int]$l.ReadSecurity -ne 2 -or [int]$l.WriteSecurity -ne 2) {
    $changes += New-BoxChange update 'ExtensionRequests item-level access' 'staff read only their own requests'
    if ($Apply) {
      $l.ReadSecurity = 2
      $l.WriteSecurity = 2
      $l.Update()
      Invoke-PnPQuery -Connection $sc
    }
  }
  if (-not (Get-PnPRoleDefinition -Identity $ExtRequestRole -Connection $sc -ErrorAction SilentlyContinue)) {
    $changes += New-BoxChange create "permission level '$ExtRequestRole'" 'add and read items; never edit or delete'
    if ($Apply) {
      Add-PnPRoleDefinition -RoleName $ExtRequestRole -Include ViewListItems, AddListItems, OpenItems, ViewVersions, ViewPages, Open, BrowseUserInfo, UseRemoteAPIs -Description 'Add requests and read your own; never edit or delete (T14-18)' -Connection $sc | Out-Null
    }
  }
  # Unique access: the site's own groups keep theirs; staff are added.
  if (-not $l.HasUniqueRoleAssignments) {
    $changes += New-BoxChange update 'ExtensionRequests unique access' 'keep the Admin site groups; add staff to ask'
    if ($Apply) { Set-PnPList -Identity $l.Id -BreakRoleInheritance -CopyRoleAssignments -Connection $sc -ErrorAction Stop | Out-Null }
  }
  $login = Get-GroupLogin $ExtRequestStaff
  $have = @()
  if ($l.HasUniqueRoleAssignments) { $have = @(Get-ListPrincipals $sc ([string]$l.Title)) }
  if ($login -notin $have) {
    $changes += New-BoxChange update "$ExtRequestStaff on ExtensionRequests" "$ExtRequestRole (ask for an extension)"
    if ($Apply) { Set-PnPListPermission -Identity $l.Id -User $login -AddRole $ExtRequestRole -Connection $sc -ErrorAction Stop }
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

# ---------- sharepoint.policies (T9-01, T9-02) ----------
# Saves the policy documents rendered by the engine (input "policies") in
# the Compliance site's Policies library. A document goes up as a Draft;
# publishing it (Approved, with who and when, and a PDF copy) is a separate
# planned change that the owner approves (SPEC §25.1). A file edited in
# SharePoint after the box wrote it is never overwritten: it is reported
# as a conflict. A document with values still to be completed stays a
# draft, and each missing Dictionary value is created empty for the owner.
$PolicyColumns = [ordered]@{
  PolicyId      = @{ Display = 'Policy id'; Type = 'Text' }
  PolicyVersion = @{ Display = 'Policy version'; Type = 'Text' }
  PolicyStatus  = @{ Display = 'Policy status'; Type = 'Choice'; Choices = @('Draft', 'Approved') }
  Requirements  = @{ Display = 'Requirements'; Type = 'Note' }
  ReviewDue     = @{ Display = 'Review due'; Type = 'DateTime' }
  ApprovedBy    = @{ Display = 'Approved by'; Type = 'Text' }
  ApprovedOn    = @{ Display = 'Approved on'; Type = 'DateTime' }
  BoxHash       = @{ Display = 'Box hash'; Type = 'Text' }
  BoxSource     = @{ Display = 'Box source'; Type = 'Text' }
}

function Get-PoliciesSite($In) {
  $lib = [string]$In.policies.library
  $s = @($In.config.structure.sites | Where-Object { @($_.libraries | Where-Object { $_ -and ([string]$_.name) -eq $lib }).Count -gt 0 })
  if ($s.Count -ne 1) { throw "the configuration's structure has no single site with a '$lib' library" }
  return $s[0]
}

function Get-BoxBytesHash([byte[]] $Bytes) { return [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($Bytes)).ToLower() }

# The hash of a .docx file's text parts, as engine/internal/docs TextHash
# computes it: SharePoint may write column values into the property parts.
function Get-DocxTextHash([byte[]] $Bytes) {
  Add-Type -AssemblyName System.IO.Compression
  $zip = [IO.Compression.ZipArchive]::new([IO.MemoryStream]::new($Bytes), [IO.Compression.ZipArchiveMode]::Read)
  try {
    $buf = [IO.MemoryStream]::new()
    foreach ($n in 'word/document.xml', 'word/footer1.xml', 'word/header1.xml') {
      $nb = [Text.Encoding]::UTF8.GetBytes($n)
      $buf.Write($nb, 0, $nb.Length); $buf.WriteByte(0)
      $e = $zip.GetEntry($n)
      if ($e) { $st = $e.Open(); try { $st.CopyTo($buf) } finally { $st.Dispose() } }
      $buf.WriteByte(0)
    }
    return Get-BoxBytesHash $buf.ToArray()
  } finally { $zip.Dispose() }
}

function Get-PolicyFileHash([string] $Name, [byte[]] $Bytes) {
  if ($Name -like '*.docx') { try { return Get-DocxTextHash $Bytes } catch { return 'unreadable' } }
  return Get-BoxBytesHash $Bytes
}

# A file in the library: its fields, and whether it is still what the box wrote.
function Get-PolicyFile($sc, [string] $LibUrl, [string] $Name) {
  $url = "$LibUrl/$Name"
  $item = Get-PnPFile -Url $url -AsListItem -Connection $sc -ErrorAction SilentlyContinue
  if (-not $item) { return $null }
  $ms = Get-PnPFile -Url $url -AsMemoryStream -Connection $sc -ErrorAction Stop
  $f = $item.FieldValues
  $cur = Get-PolicyFileHash $Name $ms.ToArray()
  return @{ Hash = $cur; Stored = [string]$f['BoxHash']; Source = [string]$f['BoxSource']; Status = [string]$f['PolicyStatus']
    Version = [string]$f['PolicyVersion']; ApprovedBy = [string]$f['ApprovedBy']; ApprovedOn = $f['ApprovedOn']; ReviewDue = $f['ReviewDue']
    Conflict = ($cur -ne [string]$f['BoxHash']) }
}

function Save-PolicyFile($sc, [string] $Folder, [string] $Name, [byte[]] $Bytes, [hashtable] $Values) {
  $ms = [IO.MemoryStream]::new($Bytes)
  Add-PnPFile -Folder $Folder -FileName $Name -Stream $ms -Values $Values -Connection $sc -ErrorAction Stop | Out-Null
}

# Word converts the approved document to PDF through Graph (?format=pdf).
function Get-PolicyPdf($In, $Site, [string] $Lib, [string] $Name) {
  $h = ([uri](Get-Root $In)).Host
  $s = Invoke-BoxGraph -Uri "/sites/$($h):/sites/$($Site.url)?`$select=id"
  $d = Invoke-BoxGraph -Uri "/sites/$($s.id)/lists/$([uri]::EscapeDataString($Lib))/drive?`$select=id"
  $it = Invoke-BoxGraph -Uri "/drives/$($d.id)/root:/$([uri]::EscapeDataString($Name))?`$select=id"
  $tmp = [IO.Path]::GetTempFileName()
  try {
    # The redirect to the converted file carries its own authorisation; PowerShell drops the header on it.
    Invoke-WebRequest -Uri "https://graph.microsoft.com/v1.0/drives/$($d.id)/items/$($it.id)/content?format=pdf" `
      -Headers @{ Authorization = 'Bearer ' + (Get-BoxAuth 'graph').access_token } -OutFile $tmp -ErrorAction Stop | Out-Null
    return [IO.File]::ReadAllBytes($tmp)
  } finally { Remove-Item $tmp -Force -ErrorAction SilentlyContinue }
}

function Add-PolicyEvent($In, [string] $Id, [string] $Action, [string] $To) {
  $ac = Connect-Site (Get-AdminSiteUrl $In)
  Add-PnPListItem -List 'Events' -Values @{ Title = "policy $Id $Action"; event_id = [guid]::NewGuid().ToString(); at = (Get-Date).ToUniversalTime().ToString('o')
    actor = 'box pipeline'; entity = 'policy'; entity_id = $Id; action = $Action; to_state = $To } -Connection $ac -ErrorAction Stop | Out-Null
}

function Get-PolicyChanges($In, [switch] $Apply, [switch] $Verify) {
  if ($In.policies_error) { throw "policy documents not rendered: $($In.policies_error)" }
  $p = $In.policies
  $lib = [string]$p.library
  $site = Get-PoliciesSite $In
  $sc = Connect-Site (Get-SiteUrl $In $site.url)
  $changes = @()
  if (-not (Get-PnPList -Identity $lib -Connection $sc -ErrorAction SilentlyContinue)) {
    if ($Apply) { throw "the $lib library does not exist (run sharepoint.libraries)" }
    return @(New-BoxChange create "documents in $($site.name)/$lib" 'after the library exists')
  }
  $have = @{}
  foreach ($f in @(Get-PnPField -List $lib -Connection $sc)) { $have[[string]$f.InternalName] = $true }
  foreach ($c in $PolicyColumns.Keys) {
    if ($have.ContainsKey($c)) { continue }
    $def = $PolicyColumns[$c]
    $changes += New-BoxChange create "column $($site.name)/$lib.$c" $def.Type
    if ($Apply) {
      $a = @{ List = $lib; DisplayName = $def.Display; InternalName = $c; Type = $def.Type; Connection = $sc; ErrorAction = 'Stop' }
      if ($def.Choices) { $a.Choices = $def.Choices }
      Add-PnPField @a | Out-Null
    }
  }
  # Values the owner must supply: an empty Dictionary entry each (status EMPTY).
  $needed = @($p.dictionary_needed | Where-Object { $_ })
  if ($needed.Count) {
    $ac = Connect-Site (Get-AdminSiteUrl $In)
    if (Get-PnPList -Identity 'Dictionary' -Connection $ac -ErrorAction SilentlyContinue) {
      $keys = @{}
      foreach ($i in @(Get-PnPListItem -List 'Dictionary' -PageSize 500 -Connection $ac)) { $keys[[string]$i['key']] = $true }
      foreach ($n in $needed) {
        if ($keys.ContainsKey([string]$n.key)) { continue }
        $label = [string]$n.label
        if ($label.Length -gt 250) { $label = $label.Substring(0, 250) }
        $changes += New-BoxChange create "Dictionary $($n.key)" "empty, for the owner to fill: $label"
        if ($Apply) {
          Add-PnPListItem -List 'Dictionary' -Values @{ Title = [string]$n.key; key = [string]$n.key; label = $label; question = [string]$n.label; status = 'EMPTY' } -Connection $ac -ErrorAction Stop | Out-Null
        }
      }
    }
  }
  $folder = Get-LibUrl $lib
  $web = Get-PnPWeb -Connection $sc
  $libUrl = "$(([string]$web.ServerRelativeUrl).TrimEnd('/'))/$folder"
  foreach ($d in @($p.documents | Where-Object { $_ })) {
    $id, $ver, $src = [string]$d.id, [string]$d.version, [string]$d.source
    $todo = @($d.missing | Where-Object { $_ })
    $vals = @{ PolicyId = $id; PolicyVersion = $ver; Requirements = [string]$d.requirements; BoxSource = $src }
    $draftVals = $vals + @{ PolicyStatus = 'Draft'; BoxHash = [string]$d.draft.hash; ApprovedBy = $null; ApprovedOn = $null; ReviewDue = $null }
    $note = if ($todo.Count) { "; $($todo.Count) value(s) to be completed: $(($todo | ForEach-Object { $_.key }) -join ', ')" } else { '' }
    $doc = Get-PolicyFile $sc $libUrl ([string]$d.file)
    if ($null -eq $doc) {
      $changes += New-BoxChange create "document $id v$ver" "draft (content $($src.Substring(0, 12)))$note"
      if ($Apply) {
        Save-PolicyFile $sc $folder ([string]$d.file) ([Convert]::FromBase64String([string]$d.draft.content)) $draftVals
        Add-PolicyEvent $In $id 'draft' "v$ver Draft"
      }
      continue
    }
    if ($doc.Conflict) {
      Write-Warning "conflict: $($site.name)/$lib/$($d.file) was edited in SharePoint after the box wrote it; the box does not overwrite it. Carry the edit into the template with a new version, or remove the file so the box writes it again."
      continue
    }
    if ($doc.Source -ne $src) {
      $changes += New-BoxChange update "document $id v$ver" "new draft (content $($src.Substring(0, 12))), replaces v$($doc.Version) ($($doc.Status)); the published PDF stays until the owner approves$note"
      if ($Apply) {
        Save-PolicyFile $sc $folder ([string]$d.file) ([Convert]::FromBase64String([string]$d.draft.content)) $draftVals
        Add-PolicyEvent $In $id 'draft' "v$ver Draft"
      }
      continue
    }
    if ($todo.Count) {
      Write-Warning "$id v$ver stays a draft until these values are supplied: $(($todo | ForEach-Object { "$($_.key) ($($_.what))" }) -join '; ')"
      continue
    }
    $pdf = Get-PolicyFile $sc $libUrl ([string]$d.pdf)
    if ($pdf -and $pdf.Conflict) {
      Write-Warning "conflict: $($site.name)/$lib/$($d.pdf) was changed in SharePoint after the box wrote it; the box does not overwrite it."
      continue
    }
    if ($doc.Status -eq 'Approved') {
      if (-not $pdf -or $pdf.Source -ne $src -or $pdf.Status -ne 'Approved') {
        $changes += New-BoxChange update "publication $id v$ver" 'PDF copy of the approved document'
        if ($Apply) {
          $pv = $vals + @{ PolicyStatus = 'Approved'; ApprovedBy = $doc.ApprovedBy; ApprovedOn = $doc.ApprovedOn; ReviewDue = $doc.ReviewDue }
          $bytes = Get-PolicyPdf $In $site $lib ([string]$d.file)
          Save-PolicyFile $sc $folder ([string]$d.pdf) $bytes ($pv + @{ BoxHash = (Get-BoxBytesHash $bytes) })
        }
      }
      continue
    }
    # A current, complete draft: publishing waits for the owner's approval.
    # Waiting for the owner is not drift, so verify leaves it out.
    if ($Verify) { continue }
    $changes += New-BoxChange ([string]$d.approve.action) ([string]$d.approve.target) ([string]$d.approve.detail)
    if ($Apply) {
      $a = $d.approval
      if (-not $a) { throw "$id v$ver is published only once the owner approves '$($d.approve.target): $($d.approve.detail)' (box plan --proposal, box approve)" }
      $av = $vals + @{ PolicyStatus = 'Approved'; ApprovedBy = [string]$a.by; ApprovedOn = [string]$a.on; ReviewDue = [string]$a.review_due }
      Save-PolicyFile $sc $folder ([string]$d.file) ([Convert]::FromBase64String([string]$a.content)) ($av + @{ BoxHash = [string]$a.hash })
      $bytes = Get-PolicyPdf $In $site $lib ([string]$d.file)
      Save-PolicyFile $sc $folder ([string]$d.pdf) $bytes ($av + @{ BoxHash = (Get-BoxBytesHash $bytes) })
      Add-PolicyEvent $In $id 'approve' "v$ver Approved"
    }
  }
  return $changes
}

function Get-PolicyEvidence($In) {
  $p = $In.policies
  $site = Get-PoliciesSite $In
  $sc = Connect-Site (Get-SiteUrl $In $site.url)
  $web = Get-PnPWeb -Connection $sc
  $libUrl = "$(([string]$web.ServerRelativeUrl).TrimEnd('/'))/$(Get-LibUrl ([string]$p.library))"
  $n = @{ Approved = 0; Draft = 0; conflict = 0; missing = 0 }
  foreach ($d in @($p.documents | Where-Object { $_ })) {
    $f = Get-PolicyFile $sc $libUrl ([string]$d.file)
    if ($null -eq $f) { $n.missing++ } elseif ($f.Conflict) { $n.conflict++ } elseif ($f.Status -eq 'Approved') { $n.Approved++ } else { $n.Draft++ }
  }
  return New-Evidence 'policies' "$($site.name)/$($p.library): $($n.Approved) approved, $($n.Draft) draft, $($n.conflict) conflict(s), $($n.missing) not saved"
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
  'sharepoint.policies' = @{
    test = { param($In) if ($In.policies_error) { @{ ok = $false; reason = "policy documents not rendered: $($In.policies_error)" } } else { @{ ok = $true } } }
    plan = { param($In) Result-Changes (Get-PolicyChanges $In) }
    apply = { param($In) Result-Changes (Get-PolicyChanges $In -Apply) }
    verify = { param($In) $c = @(Get-PolicyChanges $In -Verify | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) Get-PolicyEvidence $In }
  }
  'sharepoint.control-tasks' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-ControlTaskChanges $In) }
    apply = { param($In) Result-Changes (Get-ControlTaskChanges $In -Apply) }
    verify = { param($In) $c = @(Get-ControlTaskChanges $In | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'control-tasks' "next due ControlTasks item kept for: $((@($In.control_tasks.kinds | ForEach-Object { $_.kind })) -join ', ')" }
    # Narrow operations for box tasks (SPEC §25: the assignee closes a task, never the box).
    read = { param($In) Read-ControlTasks $In }
    record = { param($In) Complete-ControlTask $In }
  }
  'sharepoint.extension-requests' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-ExtensionRequestChanges $In) }
    apply = { param($In) Result-Changes (Get-ExtensionRequestChanges $In -Apply) }
    verify = { param($In) $c = @(Get-ExtensionRequestChanges $In | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'extension-requests' 'ExtensionRequests open to staff: add and read their own; the owner decides (box extensions)' }
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
Export-ModuleMember -Function Invoke-BoxStep, Get-BoxStepIds, Get-BoxStepProblems, Compare-BoxProperties, Merge-BoxPayload, Test-BaselineAgreement, Get-SupersededTenantKeys, Get-CaPolicyFix, Get-ControlTaskActions, Test-CloseRequest, ConvertTo-BoxDay, Get-MissingChoices
