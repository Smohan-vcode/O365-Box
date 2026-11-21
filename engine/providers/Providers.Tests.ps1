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

Describe 'policy.sharepoint comparisons (T14-09)' {
  BeforeAll { Import-Module (Join-Path $PSScriptRoot 'sharepoint' 'Box.sharepoint.psm1') -Force }
  It 'merges one payload part across settings and finds the differences' {
    $s = @(
      [pscustomobject]@{ id = 'SPO-001'; payload = [pscustomobject]@{ 'Set-PnPTenant' = [pscustomobject]@{ SharingCapability = 'ExistingExternalUserSharingOnly' } } },
      [pscustomobject]@{ id = 'SPO-005'; payload = [pscustomobject]@{ 'Set-PnPTenant' = [pscustomobject]@{ ExternalUserExpirationRequired = $true; ExternalUserExpireInDays = 30 } } },
      [pscustomobject]@{ id = 'SPO-016'; payload = [pscustomobject]@{ 'Set-PnPRequestAccessEmails' = [pscustomobject]@{ Disabled = $true } } })
    $want = Merge-BoxPayload $s 'Set-PnPTenant'
    @($want.Keys) | Should -Be @('SharingCapability', 'ExternalUserExpirationRequired', 'ExternalUserExpireInDays')
    $cur = [pscustomobject]@{ SharingCapability = 'ExistingExternalUserSharingOnly'; ExternalUserExpirationRequired = $false; ExternalUserExpireInDays = 30 }
    $d = @(Compare-BoxProperties $cur $want)
    $d.Count | Should -Be 1
    $d[0].name | Should -Be 'ExternalUserExpirationRequired'
  }
  It 'reads a property under its read name' {
    $d = @(Compare-BoxProperties ([pscustomobject]@{ SharePointAddInsDisabled = $true }) ([ordered]@{ IsSharePointAddInsDisabled = $true }) @{ IsSharePointAddInsDisabled = 'SharePointAddInsDisabled' })
    $d.Count | Should -Be 0
  }
  It 'refuses two settings that want different values' {
    $s = @(
      [pscustomobject]@{ id = 'A'; payload = @{ 'Set-PnPTenant' = @{ SharingCapability = 'Disabled' } } },
      [pscustomobject]@{ id = 'B'; payload = @{ 'Set-PnPTenant' = @{ SharingCapability = 'ExistingExternalUserSharingOnly' } } })
    { Merge-BoxPayload $s 'Set-PnPTenant' } | Should -Throw '*want different values*'
  }
  It 'reports where the baseline step and the policy step disagree' {
    @(Test-BaselineAgreement ([ordered]@{ SharingCapability = 'Disabled'; LegacyAuthProtocolsEnabled = $false }) ([ordered]@{ SharingCapability = 'Disabled'; LegacyAuthProtocolsEnabled = $false })).Count | Should -Be 0
    @(Test-BaselineAgreement ([ordered]@{ SharingCapability = 'Disabled' }) ([ordered]@{ SharingCapability = 'ExistingExternalUserSharingOnly' })).Count | Should -Be 1
  }
  It 'knows the tenant values another value decides' {
    $m = Get-SupersededTenantKeys ([pscustomobject]@{ EnableAzureADB2BIntegration = $true; OneDriveSharingCapability = 'Disabled' })
    @($m.Keys | Sort-Object) | Should -Be @('NotifyOwnersWhenInvitationsAccepted', 'RequireAcceptingAccountMatchInvitedAccount')
    (Get-SupersededTenantKeys ([pscustomobject]@{ EnableAzureADB2BIntegration = $false; OneDriveSharingCapability = 'ExternalUserSharingOnly' })).Count | Should -Be 0
  }
  It 'makes a SharePoint-generated Conditional Access policy report-only with break-glass excluded' {
    $p = [pscustomobject]@{ state = 'enabled'; conditions = [pscustomobject]@{ users = [pscustomobject]@{ excludeGroups = @('g1') } } }
    $f = Get-CaPolicyFix $p 'bg'
    $f.state | Should -Be 'enabledForReportingButNotEnforced'
    $f.conditions.users.excludeGroups | Should -Be @('g1', 'bg')
    $ok = [pscustomobject]@{ state = 'enabledForReportingButNotEnforced'; conditions = [pscustomobject]@{ users = [pscustomobject]@{ excludeGroups = @('bg') } } }
    (Get-CaPolicyFix $ok 'bg').Count | Should -Be 0
  }
}

Describe 'policy.teams payloads (T14-09)' {
  BeforeAll { Import-Module (Join-Path $PSScriptRoot 'teams' 'Box.teams.psm1') -Force }
  It 'groups parameters by cmdlet, picks the tier and leaves out other areas'' parts' {
    $s = @(
      [pscustomobject]@{ id = 'TMS-002'; payload = [pscustomobject]@{ core = [pscustomobject]@{ 'Set-CsTenantFederationConfiguration' = [pscustomobject]@{ Identity = 'Global'; AllowFederatedUsers = $true } }; enhanced = [pscustomobject]@{ 'Set-CsTenantFederationConfiguration' = [pscustomobject]@{ Identity = 'Global'; AllowedDomainsAsAList = '{config:teams_federation_allow_list}' } } } },
      [pscustomobject]@{ id = 'TMS-001'; payload = [pscustomobject]@{ 'Set-CsTenantFederationConfiguration' = [pscustomobject]@{ Identity = 'Global'; AllowTeamsConsumer = $false } } },
      [pscustomobject]@{ id = 'TMS-009'; payload = [pscustomobject]@{ 'Set-CsTeamsMessagingPolicy' = [pscustomobject]@{ Identity = 'Global'; AllowSecurityEndUserReporting = $true }; 'Set-ReportSubmissionPolicy(exchange)' = [pscustomobject]@{ ReportChatMessageEnabled = $true } } })
    $r = Resolve-TeamsPayload $s 'core'
    @($r.cmdlets.Keys) | Should -Be @('Set-CsTenantFederationConfiguration', 'Set-CsTeamsMessagingPolicy')
    @($r.cmdlets['Set-CsTenantFederationConfiguration'].Keys) | Should -Be @('Identity', 'AllowFederatedUsers', 'AllowTeamsConsumer')
    $r.skipped | Should -Be @('TMS-009 Set-ReportSubmissionPolicy(exchange)')
    $e = Resolve-TeamsPayload $s 'enhanced'
    @($e.cmdlets['Set-CsTenantFederationConfiguration']['AllowedDomainsAsAList']).Count | Should -Be 0
  }
  It 'compares values, app lists by id, and ignores Identity' {
    $cur = [pscustomobject]@{ Identity = 'Global'; AllowGuestUser = $true; GlobalCatalogApps = @([pscustomobject]@{ Id = 'abc' }); AllowedDomains = 'Microsoft.Rtc.Management.WritableConfig.Policy.Edge.AllowAllKnownDomains' }
    $want = [ordered]@{ Identity = 'Tag:Other'; AllowGuestUser = $false; GlobalCatalogApps = @(); AllowedDomains = 'AllowAllKnownDomains' }
    @(Compare-TeamsValues $cur $want | ForEach-Object { $_.name }) | Should -Be @('AllowGuestUser', 'GlobalCatalogApps')
  }
}
