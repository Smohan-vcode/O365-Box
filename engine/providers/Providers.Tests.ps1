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
    Mock -ModuleName BoxGraph Invoke-BoxGraph { @{ value = @() } }
    @(Get-BoxGraphAll '/x').Count | Should -Be 0
  }
  It 'returns every item across pages' {
    $script:page = 0
    Mock -ModuleName BoxGraph Invoke-BoxGraph {
      $script:page++
      if ($script:page -eq 1) { @{ value = @(@{ id = 'a' }); '@odata.nextLink' = 'https://graph.microsoft.com/v1.0/next' } }
      else { @{ value = @(@{ id = 'b' }) } }
    }
    $r = @(Get-BoxGraphAll '/x')
    $r.Count | Should -Be 2
    $r[1].id | Should -Be 'b'
  }
  It 'returns one item as a one-element array' {
    Mock -ModuleName BoxGraph Invoke-BoxGraph { @{ value = @(@{ id = 'only'; a = 1; b = 2 }) } }
    @(Get-BoxGraphAll '/x').Count | Should -Be 1
  }
}
