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
