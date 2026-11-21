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

Describe 'intune policy helpers (T14-05)' {
  BeforeAll { Import-Module (Join-Path $PSScriptRoot 'intune' 'Box.intune.psm1') -Force }
  It 'flattens settings-catalogue instances the same whatever the order and template references' {
    InModuleScope Box.intune {
      $a = @(
        @{ settingInstance = @{ settingDefinitionId = 'x_fw'; choiceSettingValue = @{ value = 'x_fw_true'; children = @(
          @{ settingDefinitionId = 'x_in'; choiceSettingValue = @{ value = 'x_in_1'; children = @() } }) } } },
        @{ settingInstance = @{ settingDefinitionId = 'x_len'; simpleSettingValue = @{ value = 6 } } })
      $b = @(
        [pscustomobject]@{ id = '1'; settingInstance = [pscustomobject]@{ settingDefinitionId = 'x_len'; settingInstanceTemplateReference = $null; simpleSettingValue = [pscustomobject]@{ value = 6 } } },
        [pscustomobject]@{ id = '0'; settingInstance = [pscustomobject]@{ settingDefinitionId = 'x_fw'; choiceSettingValue = [pscustomobject]@{ value = 'x_fw_true'; settingValueTemplateReference = $null; children = @(
          [pscustomobject]@{ settingDefinitionId = 'x_in'; choiceSettingValue = [pscustomobject]@{ value = 'x_in_1'; children = @() } }) } } })
      (Get-IntuneSettingLeaves $a) -join ';' | Should -Be ((Get-IntuneSettingLeaves $b) -join ';')
      (Get-IntuneSettingLeaves $a) | Should -Contain 'x_in=x_in_1'
    }
  }
  It 'merges children of one parent and the rules of one group collection' {
    InModuleScope Box.intune {
      $s1 = @{ settingInstance = @{ settingDefinitionId = 'fw'; choiceSettingValue = @{ value = 'fw_true'; children = @(@{ settingDefinitionId = 'in'; choiceSettingValue = @{ value = 'in_1'; children = @() } }) } } }
      $s2 = @{ settingInstance = @{ settingDefinitionId = 'fw'; choiceSettingValue = @{ value = 'fw_true'; children = @(@{ settingDefinitionId = 'out'; choiceSettingValue = @{ value = 'out_0'; children = @() } }) } } }
      $r1 = @{ settingInstance = @{ settingDefinitionId = 'asr'; groupSettingCollectionValue = @(@{ children = @(@{ settingDefinitionId = 'asr_a'; choiceSettingValue = @{ value = 'asr_a_audit'; children = @() } }) }) } }
      $r2 = @{ settingInstance = @{ settingDefinitionId = 'asr'; groupSettingCollectionValue = @(@{ children = @(@{ settingDefinitionId = 'asr_b'; choiceSettingValue = @{ value = 'asr_b_audit'; children = @() } }) }) } }
      $r = Merge-IntuneSettings -Map ([ordered]@{}) -Settings @($s1, $s2, $r1, $r2)
      $r.conflicts.Count | Should -Be 0
      $r.map.Count | Should -Be 2
      Get-IntuneSettingLeaves @($r.map.Values) | Should -Be @('asr#=1', 'asr_a=asr_a_audit', 'asr_b=asr_b_audit', 'fw=fw_true', 'in=in_1', 'out=out_0')
    }
  }
  It 'reports a conflict when two settings choose different options' {
    InModuleScope Box.intune {
      $s1 = @{ settingInstance = @{ settingDefinitionId = 'a'; choiceSettingValue = @{ value = 'a_0'; children = @() } } }
      $s2 = @{ settingInstance = @{ settingDefinitionId = 'a'; choiceSettingValue = @{ value = 'a_1'; children = @() } } }
      @((Merge-IntuneSettings -Map ([ordered]@{}) -Settings @($s1, $s2)).conflicts).Count | Should -Be 1
    }
  }
  It 'compares properties, nested objects and ignores @odata.type' {
    InModuleScope Box.intune {
      $want = [ordered]@{ '@odata.type' = '#x'; a = $true; n = 3; s = @{ start = '08:00' } }
      Compare-IntuneProps $want ([pscustomobject]@{ a = $true; n = 3; s = [pscustomobject]@{ start = '08:00'; end = '17:00' } }) | Should -BeNullOrEmpty
      Compare-IntuneProps $want ([pscustomobject]@{ a = $false; n = 3; s = [pscustomobject]@{ start = '09:00' } }) | Should -Be @('a', 's.start')
    }
  }
  It 'turns assignment targets into keys and back' {
    InModuleScope Box.intune {
      Get-IntuneTargetKey (New-IntuneTarget 'group:g1') | Should -Be 'group:g1'
      Get-IntuneTargetKey (New-IntuneTarget 'exclude:g1') | Should -Be 'exclude:g1'
      Get-IntuneTargetKey (New-IntuneTarget 'allDevices') | Should -Be 'allDevices'
      Test-IntuneSameSet @('b', 'a') @('a', 'b') | Should -BeTrue
      Test-IntuneSameSet @('a') @() | Should -BeFalse
    }
  }
  It 'resolves placeholders and lists the ones it cannot' {
    InModuleScope Box.intune {
      $r = Resolve-WinPlaceholders '{"a":"{tenant:id}","b":"{config:devices.windows.minimum_os_version}","c":"{tenantid}"}' @{ '{tenant:id}' = '00000000-0000-0000-0000-000000000001'; '{config:devices.windows.minimum_os_version}' = $null }
      $r.json | Should -Match '"a":"00000000-0000-0000-0000-000000000001"'
      $r.json | Should -Match '"c":"\{tenantid\}"'
      $r.missing | Should -Be @('{config:devices.windows.minimum_os_version}')
    }
  }
  It 'builds the Edge sign-in pattern from verified domains' {
    InModuleScope Box.intune {
      ConvertTo-WinDomainPattern @('acme.example', 'Acme.onmicrosoft.com') | Should -Be '.*@(acme\.example|acme\.onmicrosoft\.com)$'
    }
  }
  It 'derives the Windows SID of an Entra object' {
    InModuleScope Box.intune {
      ConvertTo-IntuneSid '00000001-0002-0003-0400-000000000005' | Should -Be 'S-1-12-1-1-196610-4-83886080'
    }
  }
}
