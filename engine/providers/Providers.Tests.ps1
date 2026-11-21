# Pester tests for every provider module (T0-03). Run with:
#   pwsh -c "Invoke-Pester -Path engine/providers -Output Detailed"
BeforeDiscovery {
  $script:providers = Get-ChildItem -Path $PSScriptRoot -Directory |
    Where-Object { Test-Path (Join-Path $_.FullName 'steps.yaml') } |
    ForEach-Object { @{ Name = $_.Name; Dir = $_.FullName } }
}

Describe 'provider <Name>' -ForEach $providers {
  BeforeAll {
    $module = Join-Path $Dir "Box.$Name.psm1"
    $manifest = Get-Content (Join-Path $Dir 'steps.yaml') -Raw
  }
  It 'has a module file named after the provider' {
    $module | Should -Exist
  }
  It 'declares its provider name in steps.yaml' {
    $manifest | Should -Match "(?m)^provider: $Name\s*$"
  }
  It 'loads at most one Microsoft module (spike S-05)' {
    $line = ($manifest -split "`n" | Where-Object { $_ -match '^modules:' })
    $line | Should -Not -BeNullOrEmpty
    ($line -replace '^modules:\s*\[(.*)\].*$', '$1' -split ',' | Where-Object { $_.Trim() }).Count | Should -BeLessOrEqual 1
  }
  It 'imports without its Microsoft module and exports Invoke-BoxStep' {
    $m = Import-Module $module -PassThru -Force
    try {
      $m.ExportedFunctions.Keys | Should -Contain 'Invoke-BoxStep'
      $m.ExportedFunctions.Keys | Should -Contain 'Get-BoxStepIds'
    } finally { Remove-Module $m.Name -Force }
  }
  It 'implements every operation for every step' {
    $m = Import-Module $module -PassThru -Force
    try { @(& "$($m.Name)\Get-BoxStepProblems") | Should -BeNullOrEmpty }
    finally { Remove-Module $m.Name -Force }
  }
  It 'rejects an unknown step and an unknown operation' {
    $m = Import-Module $module -PassThru -Force
    try {
      { & "$($m.Name)\Invoke-BoxStep" -Step 'no.such.step' -Operation 'plan' } | Should -Throw '*unknown step*'
      { & "$($m.Name)\Invoke-BoxStep" -Step 'x' -Operation 'destroy' } | Should -Throw '*unknown operation*'
    } finally { Remove-Module $m.Name -Force }
  }
}

Describe 'common helpers' {
  BeforeAll { Import-Module (Join-Path $PSScriptRoot 'common' 'BoxProvider.psm1') -Force }
  It 'dispatches to the handler' {
    $steps = @{ 's' = @{ test = { param($In) @{ ok = $In.x } }; plan = {}; apply = {}; verify = {}; evidence = {} } }
    (Invoke-BoxDispatch -Steps $steps -Step 's' -Operation 'test' -InputObject @{ x = $true }).ok | Should -BeTrue
  }
  It 'reports missing operations' {
    Test-BoxStepTable -Steps @{ 's' = @{ test = {} } } | Should -HaveCount 4
  }
  It 'builds changes' {
    (New-BoxChange -Action create -Target 'grp-x').target | Should -Be 'grp-x'
    { New-BoxChange -Action destroy -Target 'x' } | Should -Throw
  }
}

Describe 'Graph helper result shapes' {
  BeforeAll {
    Import-Module (Join-Path $PSScriptRoot 'common' 'BoxGraph.psm1') -Force
    function global:Get-BoxAuth { @{ access_token = 'x' } }
  }
  It 'returns an empty array for an empty collection' {
    Mock -ModuleName BoxGraph Invoke-BoxGraph { [pscustomobject]@{ value = @() } }
    @(Get-BoxGraphAll '/x').Count | Should -Be 0
  }
  It 'returns every item across pages' {
    $script:page = 0
    Mock -ModuleName BoxGraph Invoke-BoxGraph {
      $script:page++
      if ($script:page -eq 1) { [pscustomobject]@{ value = @([pscustomobject]@{ id = 'a' }); '@odata.nextLink' = 'https://graph.microsoft.com/v1.0/next' } }
      else { [pscustomobject]@{ value = @([pscustomobject]@{ id = 'b' }) } }
    }
    $r = @(Get-BoxGraphAll '/x')
    $r.Count | Should -Be 2
    $r[1].id | Should -Be 'b'
  }
  It 'returns one item as a one-element array' {
    Mock -ModuleName BoxGraph Invoke-BoxGraph { [pscustomobject]@{ value = @([pscustomobject]@{ id = 'only'; a = 1; b = 2 }) } }
    @(Get-BoxGraphAll '/x').Count | Should -Be 1
  }
}

Describe 'policy.capabilities: users not covered (T14-02)' {
  BeforeAll { Import-Module (Join-Path $PSScriptRoot 'identity' 'Box.identity.psm1') -Force }
  It 'lists a user on a lower plan as not covered' {
    $users = @{ 'premium@x' = @('AAD_PREMIUM', 'INTUNE_A', 'EXCHANGE_S_STANDARD'); 'standard@x' = @('EXCHANGE_S_STANDARD', 'OFFICE_BUSINESS') }
    $needed = @{ entra_p1 = @('AAD_PREMIUM'); intune_p1 = @('INTUNE_A') }
    $r = @(Get-NotCoveredUsers -UserPlans $users -Needed $needed)
    $r.Count | Should -Be 1
    $r[0].user | Should -Be 'standard@x'
    $r[0].missing | Should -Be @('entra_p1', 'intune_p1')
  }
  It 'reports nobody when every user has every capability' {
    @(Get-NotCoveredUsers -UserPlans @{ 'a@x' = @('AAD_PREMIUM') } -Needed @{ entra_p1 = @('AAD_PREMIUM') }).Count | Should -Be 0
  }
}

Describe 'policy steps of the identity provider: pure functions (T14-04)' {
  BeforeAll { Import-Module (Join-Path $PSScriptRoot 'identity' 'Box.identity.psm1') -Force }
  It 'matches when the tenant holds every wanted value, ignoring extra properties and order' {
    $want = @{ state = 'enabledForReportingButNotEnforced'; conditions = @{ users = @{ includeUsers = @('All'); excludeGroups = @('g1') }; clientAppTypes = @('exchangeActiveSync', 'other') } }
    $have = [pscustomobject]@{ id = 'x'; state = 'enabledForReportingButNotEnforced'; conditions = [pscustomobject]@{
        users = [pscustomobject]@{ includeUsers = @('All'); excludeGroups = @('g1'); excludeUsers = @() }; clientAppTypes = @('other', 'exchangeActiveSync') } }
    @(Compare-BoxValue -Want $want -Have $have).Count | Should -Be 0
  }
  It 'reports a missing exclusion, a wrong scalar and a missing object' {
    $want = @{ state = 'enabledForReportingButNotEnforced'; conditions = @{ users = @{ excludeGroups = @('g1') } }; sessionControls = @{ persistentBrowser = @{ mode = 'never' } } }
    $have = [pscustomobject]@{ state = 'enabled'; conditions = [pscustomobject]@{ users = [pscustomobject]@{ excludeGroups = @() } }; sessionControls = $null }
    $d = @(Compare-BoxValue -Want $want -Have $have)
    $d.Count | Should -Be 3
    ($d -join ' ') | Should -Match 'excludeGroups'
    ($d -join ' ') | Should -Match 'sessionControls'
  }
  It 'compares @odata.type and lists of objects' {
    $want = @{ azureADJoin = @{ localAdmins = @{ registeringUsers = @{ '@odata.type' = '#microsoft.graph.noDeviceRegistrationMembership' } } }
      includeTargets = @(@{ targetType = 'group'; id = 'all_users' }) }
    $have = [pscustomobject]@{ '@odata.context' = 'x'; azureADJoin = [pscustomobject]@{ localAdmins = [pscustomobject]@{ registeringUsers = [pscustomobject]@{ '@odata.type' = '#microsoft.graph.allDeviceRegistrationMembership' } } }
      includeTargets = @([pscustomobject]@{ targetType = 'group'; id = 'all_users'; isRegistrationRequired = $false }) }
    @(Compare-BoxValue -Want $want -Have $have).Count | Should -Be 1
  }
  It 'compares booleans and numbers by value' {
    @(Compare-BoxValue -Want @{ a = $false; b = 60 } -Have ([pscustomobject]@{ a = $false; b = 60 })).Count | Should -Be 0
    @(Compare-BoxValue -Want @{ a = $true } -Have ([pscustomobject]@{ a = $false })).Count | Should -Be 1
  }
  It 'resolves group and role placeholders' {
    $p = [pscustomobject]@{ conditions = [pscustomobject]@{ users = [pscustomobject]@{ includeRoles = @('{roles:privileged} (the 13 ids)'); excludeGroups = @('{group:grp-break-glass}') } }
      reviewers = @([pscustomobject]@{ query = '/groups/{group:role-owner}/transitiveMembers/microsoft.graph.user' }) }
    $r = Resolve-BoxPayload -Payload $p -Groups @{ 'grp-break-glass' = 'bg-id'; 'role-owner' = 'ro-id' } -Roles @('r1', 'r2')
    $r.conditions.users.includeRoles | Should -Be @('r1', 'r2')
    $r.conditions.users.excludeGroups | Should -Be @('bg-id')
    $r.reviewers[0].query | Should -Be '/groups/ro-id/transitiveMembers/microsoft.graph.user'
  }
  It 'refuses a payload that needs a missing group or a company value' {
    { Resolve-BoxPayload -Payload @{ g = @('{group:plan-x}') } -Groups @{ 'plan-x' = $null } } | Should -Throw '*unresolved*plan-x*'
    { Resolve-BoxPayload -Payload @{ v = '{dictionary:company.trading_name}\t{config:policy.x}' } -Groups @{} } | Should -Throw '*unresolved*'
  }
  It 'keeps Conditional Access report-only unless enforced with break-glass ready' {
    Get-BoxCAState -Want 'enabled' -Mode 'audit' -BreakGlassReady $true | Should -Be 'enabledForReportingButNotEnforced'
    Get-BoxCAState -Want 'enabled' -Mode 'enforce' -BreakGlassReady $false | Should -Be 'enabledForReportingButNotEnforced'
    Get-BoxCAState -Want 'enabled' -Mode 'enforce' -BreakGlassReady $true | Should -Be 'enabled'
    Get-BoxCAState -Want 'enabledForReportingButNotEnforced' -Mode 'enforce' -BreakGlassReady $true | Should -Be 'enabledForReportingButNotEnforced'
  }
  It 'merges directory setting values and lists only those that differ' {
    $cur = @([pscustomobject]@{ name = 'EnableGroupCreation'; value = 'true' }, [pscustomobject]@{ name = 'AllowToAddGuests'; value = 'false' })
    $m = Merge-BoxGroupSettingValues -Current $cur -Want @(@{ name = 'EnableGroupCreation'; value = 'false' })
    $m.diff | Should -Be @('EnableGroupCreation')
    ($m.values | Where-Object name -eq 'AllowToAddGuests').value | Should -Be 'false'
    @((Merge-BoxGroupSettingValues -Current $cur -Want @(@{ name = 'AllowToAddGuests'; value = 'false' })).diff).Count | Should -Be 0
  }
  It 'reads the idle timeout from a policy definition' {
    $p = @{ definition = @('{"ActivityBasedTimeoutPolicy":{"Version":1,"ApplicationPolicies":[{"ApplicationId":"default","WebSessionIdleTimeout":"01:00:00"}]}}') }
    Get-BoxIdleTimeout $p | Should -Be '01:00:00'
  }
  It 'lists the Conditional Access policies and directory properties policy.entra owns' {
    $c = Get-BoxEntraClaims
    $c.ca | Should -Contain 'CA01 Require MFA for all users'
    $c.ca | Should -Contain 'CA05 Sign-in frequency for administrators'
    $c.directory | Should -Contain 'guest_access'
    $c.auth_methods | Should -Contain 'fido2'
  }
}
