# Provider: teams. Meeting, external access, guest and app policies (T1-05,
# T13-05, T14-09). Connects with MicrosoftTeams: a Graph token and a token for
# the Teams administration resource, app-only from the pipeline app
# (engine/internal/cli/orchestrate.go) or, for box audit, delegated as a
# reader-only account (engine/internal/cli/audit.go, T16-06).
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxProvider.psm1') -Force

$script:Connected = $null
$script:AuditSkipped = @()

function Connect-Teams {
  $t = (Get-BoxAuth 'teams').access_token
  if ($script:Connected -ne $t) {
    Connect-MicrosoftTeams -AccessTokens @((Get-BoxAuth 'graph').access_token, $t) -ErrorAction Stop | Out-Null
    $script:Connected = $t
  }
}

function Get-BoxProp($Obj, [string] $Name) {
  if ($null -eq $Obj) { return $null }
  if ($Obj -is [System.Collections.IDictionary]) { return $Obj[$Name] }
  $p = $Obj.PSObject.Properties[$Name]
  if ($p) { return $p.Value }
  return $null
}
function Get-BoxProps($Obj) {
  if ($Obj -is [System.Collections.IDictionary]) { return @($Obj.GetEnumerator() | ForEach-Object { @{ Name = [string]$_.Key; Value = $_.Value } }) }
  return @($Obj.PSObject.Properties | ForEach-Object { @{ Name = $_.Name; Value = $_.Value } })
}

# One comparable string per value: app lists compare by id, the
# AllowAllKnownDomains object by its type name.
function ConvertTo-BoxComparable($Value) {
  if ($null -eq $Value) { return '' }
  if (($Value -is [string] -or $Value -isnot [System.Collections.IEnumerable]) -and [string]$Value -match 'AllowAllKnownDomains$') { return 'allowallknowndomains' }
  if ($Value -is [string]) { return $Value.ToLower() }
  if ($Value -is [System.Collections.IEnumerable]) {
    return (@($Value | ForEach-Object { $id = Get-BoxProp $_ 'Id'; if ($id) { ([string]$id).ToLower() } else { ([string]$_).ToLower() } }) | Sort-Object) -join ','
  }
  return ([string]$Value).ToLower()
}

# ---------- policy.teams (T14-09) ----------
# Builds, from the resolved settings, the wanted parameters per Set- cmdlet:
#   @{ 'Set-CsTeamsMeetingPolicy' = [ordered]@{ Identity = 'Global'; ... } }
# Payload parts written by another area are left out: a key with a bracket,
# such as 'Set-ReportSubmissionPolicy(exchange)', is the Exchange half of
# TMS-009, which the defender area owns (DEF report submission policy).
# A payload with 'core' and 'enhanced' parts is picked by the policy tier.
# '{config:<name>}' placeholders read $Config.policy.<name>; with no value
# they become an empty list (nothing approved yet).
function Resolve-TeamsPayload {
  param($Settings, [string] $Tier = 'core', $Config = $null)
  $out = [ordered]@{}
  $from = @{}
  $placeholders = @{}
  $skipped = @()
  foreach ($s in @($Settings)) {
    $payload = $s.payload
    $tierPart = Get-BoxProp $payload $Tier
    if ($null -ne $tierPart) { $payload = $tierPart }
    elseif ($null -ne (Get-BoxProp $payload 'core')) { $payload = Get-BoxProp $payload 'core' }
    foreach ($part in Get-BoxProps $payload) {
      if ($part.Name -notmatch '^Set-[A-Za-z]+$') { $skipped += "$($s.id) $($part.Name)"; continue }
      if (-not $out.Contains($part.Name)) { $out[$part.Name] = [ordered]@{} }
      foreach ($q in Get-BoxProps $part.Value) {
        $v = $q.Value
        $key = "$($part.Name) $($q.Name)"
        if ($v -is [string] -and $v -match '^\{config:([A-Za-z0-9_.]+)\}$') {
          $placeholders[$key] = $Matches[1]
          $v = @(Get-BoxProp (Get-BoxProp $Config 'policy') $Matches[1] | Where-Object { $_ })
        }
        if ($out[$part.Name].Contains($q.Name) -and (ConvertTo-BoxComparable $out[$part.Name][$q.Name]) -ne (ConvertTo-BoxComparable $v)) {
          throw "$($s.id) and $($from[$key]) want different values for $key"
        }
        $out[$part.Name][$q.Name] = $v
        # Every setting that wants the value, so a difference names each one.
        $from[$key] = if ($from.ContainsKey($key) -and $from[$key] -notmatch "\b$($s.id)\b") { "$($from[$key]), $($s.id)" } else { [string]$s.id }
      }
    }
  }
  # from: "<cmdlet> <parameter>" -> setting id; placeholders: the same key ->
  # the configuration value it was filled from.
  return @{ cmdlets = $out; skipped = $skipped; from = $from; placeholders = $placeholders }
}

# True when Teams answered 403 / errorCode 40301. On the test tenant
# (2026-11-21) Teams refuses every app-only write to TeamsMeetingPolicy
# (Set- and New-) and ExternalAccessPolicy with this code, while reads and
# writes to the other policy types succeed with the same tokens, with
# -AccessTokens and with -Certificate alike; the Teams Administrator role is
# assigned at tenant scope (docs/spec-changes.md SC-28).
function Test-TeamsForbidden($ErrorRecord) {
  $text = "$($ErrorRecord.Exception.Message) $($ErrorRecord.ErrorDetails.Message)"
  return ($text -match '"errorCode"\s*:\s*40301' -or $text -match '"code"\s*:\s*"Forbidden"')
}

# Differences between an object read from Teams and the wanted parameters
# (Identity selects the object and is not compared).
function Compare-TeamsValues {
  param($Current, [System.Collections.IDictionary] $Want)
  foreach ($k in $Want.Keys) {
    if ($k -eq 'Identity') { continue }
    $cur = Get-BoxProp $Current $k
    if ((ConvertTo-BoxComparable $cur) -ne (ConvertTo-BoxComparable $Want[$k])) {
      [pscustomobject]@{ name = $k; current = (ConvertTo-BoxComparable $cur); want = (ConvertTo-BoxComparable $Want[$k]) }
    }
  }
}

# Parameter values in the form the cmdlet takes.
function ConvertTo-TeamsParameter([string] $Name, $Value) {
  if ($Name -eq 'AllowedDomains' -and [string]$Value -eq 'AllowAllKnownDomains') { return New-CsEdgeAllowAllKnownDomains }
  if ($Name -in @('GlobalCatalogApps', 'PrivateCatalogApps')) {
    # The parameter is a PSListModifier: Replace sets the whole list.
    $type = if ($Name -eq 'GlobalCatalogApps') { 'Microsoft.Teams.Policy.Administration.Cmdlets.Core.GlobalCatalogApp' } else { 'Microsoft.Teams.Policy.Administration.Cmdlets.Core.PrivateCatalogApp' }
    return @{ Replace = @(@($Value) | Where-Object { $_ } | ForEach-Object { New-Object -TypeName $type -Property @{ Id = [string]$_ } }) }
  }
  return $Value
}

# Policy types Teams refuses to let an app change (SPEC §22 gate G-11, SC-28):
# the owner sets them in the Teams admin centre; the box only compares them.
$script:ManualCmdlets = @('Set-CsTeamsMeetingPolicy', 'Set-CsExternalAccessPolicy')
$script:ManualDrift = @()

function Get-TeamsPolicyChanges($In, [switch] $Apply, [switch] $ManualOnly) {
  if ($In.policy_error) { throw "policy resolution failed: $($In.policy_error)" }
  $all = @($In.policy.settings | Where-Object { $_ })
  foreach ($s in $all | Where-Object { $_.mode -ne 'enforce' }) {
    Write-Warning "$($s.id) is in mode $($s.mode): Teams has no audit form; compared, not written"
  }
  $enf = @($all | Where-Object { $_.mode -eq 'enforce' })
  $tier = [string](Get-BoxProp (Get-BoxProp $In.config 'policy') 'tier')
  if (-not $tier) { $tier = 'core' }
  $r = Resolve-TeamsPayload $enf $tier $In.config
  foreach ($k in $r.skipped) { Write-Warning "$k is written by another area's step; not here" }
  # box audit (SPEC §26) reads a tenant the box did not build: a value the
  # company's configuration supplies (an approved list) cannot be judged and
  # is reported not read, never compared with an empty list.
  $audit = ([string]$In.environment -eq 'audit')
  $script:AuditSkipped = @()
  Connect-Teams
  $changes = @()
  $failed = @()
  $script:ManualDrift = @()
  foreach ($cmd in $r.cmdlets.Keys) {
    $manual = $cmd -in $script:ManualCmdlets
    if ($ManualOnly -and -not $manual) { continue }
    $want = $r.cmdlets[$cmd]
    if ($audit) {
      foreach ($k in @($want.Keys)) {
        $key = "$cmd $k"
        if (-not $r.placeholders.ContainsKey($key)) { continue }
        foreach ($id in ($r.from[$key] -split ', ')) { $script:AuditSkipped += @{ id = $id; reason = "$key is the company's own list (configuration policy.$($r.placeholders[$key])), which an audit does not have; not compared" } }
        $want.Remove($k)
      }
    }
    if ($want.Contains('AllowedDomainsAsAList') -and @($want['AllowedDomainsAsAList']).Count -eq 0) {
      # An empty allow list would block every organisation: wait for the list.
      Write-Warning "$cmd AllowedDomainsAsAList: no domains configured; not written"
      $want.Remove('AllowedDomainsAsAList')
    }
    $get = $cmd -replace '^Set-', 'Get-'
    $gp = @{ ErrorAction = 'Stop' }
    if ($want.Contains('Identity')) { $gp.Identity = $want['Identity'] }
    $cur = & $get @gp
    $diff = @(Compare-TeamsValues $cur $want)
    if (-not $diff.Count) { continue }
    if ($manual) {
      # Not a change the box makes (nothing to approve or apply): a guided
      # step for the owner, checked here until the values match.
      foreach ($d in $diff) {
        $ids = $r.from["$cmd $($d.name)"]
        $script:ManualDrift += New-BoxChange update "$ids $cmd $($d.name)" "$($d.current) -> $($d.want): set it in the Teams admin centre (gate G-11)"
        if (-not $ManualOnly) { Write-Warning "gate G-11 ($ids): set $($d.name) to $($d.want) in the Teams admin centre; it is $($d.current)" }
      }
      continue
    }
    foreach ($d in $diff) { $changes += New-BoxChange update "$cmd $($d.name)" "$($d.current) -> $($d.want) ($($r.from["$cmd $($d.name)"]))" }
    if ($Apply) {
      $sp = @{ ErrorAction = 'Stop' }
      if ($want.Contains('Identity')) { $sp.Identity = $want['Identity'] }
      foreach ($d in $diff) { $sp[$d.name] = ConvertTo-TeamsParameter $d.name $want[$d.name] }
      # One cmdlet's failure does not stop the others: each write is
      # independent and repeatable (R4); the step still fails below.
      try { & $cmd @sp | Out-Null }
      catch {
        $ids = @($diff | ForEach-Object { $r.from["$cmd $($_.name)"] } | Sort-Object -Unique) -join ', '
        if (Test-TeamsForbidden $_) {
          $failed += "$cmd ($ids): Teams refused the app-only write (403, errorCode 40301) although the pipeline app holds Teams Administrator; see docs/spec-changes.md SC-28"
        } else {
          $failed += "$cmd ($ids): $($_.Exception.Message)"
        }
      }
    }
  }
  if ($failed.Count) { throw ("teams policy writes failed; the other cmdlets were applied:`n  " + ($failed -join "`n  ")) }
  return $changes
}

function Result-Changes($c) { return @{ changes = @($c | Where-Object { $null -ne $_ }) } }
function New-Evidence([string] $Kind, [string] $Summary) { return @{ items = @(@{ kind = $Kind; ref = "tenant:$Kind"; summary = $Summary }) } }
$ok = { param($In) @{ ok = $true } }

# Step handlers, keyed by step id (see common/BoxProvider.psm1).
$script:Steps = [ordered]@{
  'policy.teams' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-TeamsPolicyChanges $In) }
    apply = { param($In) Result-Changes (Get-TeamsPolicyChanges $In -Apply) }
    verify = { param($In) $c = @(Get-TeamsPolicyChanges $In | Where-Object { $_ }) + @($script:ManualDrift); @{ ok = ($c.Count -eq 0); drift = $c; skipped = @($script:AuditSkipped) } }
    evidence = { param($In) New-Evidence 'policy-teams' "resolved teams settings verified: $((@($In.policy.settings | ForEach-Object { $_.id })) -join ', ')" }
  }
  'teams.gate-g11' = @{
    test = $ok; plan = { param($In) Result-Changes @() }; apply = { param($In) Result-Changes @() }
    verify = { param($In) Get-TeamsPolicyChanges $In -ManualOnly | Out-Null; @{ ok = (@($script:ManualDrift).Count -eq 0); drift = @($script:ManualDrift) } }
    evidence = { param($In) New-Evidence 'gate-g11' 'Teams meeting and external access policy values match the resolved settings' }
  }
}

function Invoke-BoxStep {
  param([string] $Step, [string] $Operation, $InputObject)
  Invoke-BoxDispatch -Steps $script:Steps -Step $Step -Operation $Operation -InputObject $InputObject
}

function Get-BoxStepIds { @($script:Steps.Keys) }

function Get-BoxStepProblems { Test-BoxStepTable -Steps $script:Steps }

Export-ModuleMember -Function Invoke-BoxStep, Get-BoxStepIds, Get-BoxStepProblems, Resolve-TeamsPayload, Compare-TeamsValues
