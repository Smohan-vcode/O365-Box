<#
  Shared helpers for provider modules (SPEC §14.1). A provider keeps a
  table of step handlers:

    $script:Steps = @{
      'identity.users' = @{
        test     = { param($In) @{ ok = $true } }
        plan     = { param($In) @{ changes = @() } }
        apply    = { param($In) @{ changes = @() } }
        verify   = { param($In) @{ ok = $true } }
        evidence = { param($In) @{ items = @() } }
      }
    }

  and exports Invoke-BoxStep by calling Invoke-BoxDispatch.
#>

$script:Operations = @('test', 'plan', 'apply', 'verify', 'evidence')

function Invoke-BoxDispatch {
  param(
    [Parameter(Mandatory)] [System.Collections.IDictionary] $Steps,
    [Parameter(Mandatory)] [string] $Step,
    [Parameter(Mandatory)] [string] $Operation,
    $InputObject
  )
  if ($Operation -notin $script:Operations) { throw "unknown operation '$Operation'" }
  if (-not $Steps.Contains($Step)) { throw "unknown step '$Step'" }
  $handler = $Steps[$Step]
  if (-not $handler.Contains($Operation)) { throw "step '$Step' does not implement '$Operation'" }
  return & $handler[$Operation] $InputObject
}

function New-BoxChange {
  param(
    [Parameter(Mandatory)] [ValidateSet('create', 'update', 'delete')] [string] $Action,
    [Parameter(Mandatory)] [string] $Target,
    [string] $Detail = ''
  )
  return @{ action = $Action; target = $Target; detail = $Detail }
}

function Test-BoxStepTable {
  # Returns problems with a step table: missing operations.
  param([Parameter(Mandatory)] [System.Collections.IDictionary] $Steps)
  foreach ($id in $Steps.Keys) {
    foreach ($op in $script:Operations) {
      if (-not $Steps[$id].Contains($op)) { "step '$id' does not implement '$op'" }
    }
  }
}

Export-ModuleMember -Function Invoke-BoxDispatch, New-BoxChange, Test-BoxStepTable
