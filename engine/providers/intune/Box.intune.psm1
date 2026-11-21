# Provider: intune. Device enrolment, compliance and app protection (T1-04).
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxProvider.psm1') -Force
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxGraph.psm1') -Force

# Step handlers, keyed by step id (see common/BoxProvider.psm1).
$script:Steps = [ordered]@{}
# Policy steps (T14): shared helpers, then one file per area.
. (Join-Path $PSScriptRoot 'IntunePolicy.ps1')
. (Join-Path $PSScriptRoot 'Policy.Windows.ps1')

function Invoke-BoxStep {
  param([string] $Step, [string] $Operation, $InputObject)
  Invoke-BoxDispatch -Steps $script:Steps -Step $Step -Operation $Operation -InputObject $InputObject
}

function Get-BoxStepIds { @($script:Steps.Keys) }

function Get-BoxStepProblems { Test-BoxStepTable -Steps $script:Steps }

Export-ModuleMember -Function Invoke-BoxStep, Get-BoxStepIds, Get-BoxStepProblems
