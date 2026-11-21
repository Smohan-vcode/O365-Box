# Provider: identity. Entra users, groups, break-glass accounts, Conditional Access and authentication methods (T1).
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxProvider.psm1') -Force

# Step handlers, keyed by step id (see common/BoxProvider.psm1).
$script:Steps = [ordered]@{}

function Invoke-BoxStep {
  param([string] $Step, [string] $Operation, $InputObject)
  Invoke-BoxDispatch -Steps $script:Steps -Step $Step -Operation $Operation -InputObject $InputObject
}

function Get-BoxStepIds { @($script:Steps.Keys) }

function Get-BoxStepProblems { Test-BoxStepTable -Steps $script:Steps }

Export-ModuleMember -Function Invoke-BoxStep, Get-BoxStepIds, Get-BoxStepProblems
