# End-to-end test provider. Resources live in $env:BOX_PROVIDER_DEMO_STORE.
function Read-Store {
  $p = $env:BOX_PROVIDER_DEMO_STORE
  if (-not (Test-Path $p)) { return @{} }
  return Get-Content $p -Raw | ConvertFrom-Json -AsHashtable
}
function Write-Store($s) { $s | ConvertTo-Json -Depth 8 | Set-Content -Path $env:BOX_PROVIDER_DEMO_STORE }

function Get-Desired([string] $Step, $In) {
  switch ($Step) {
    'demo.groups' { return @($In.config.groups | ForEach-Object { $_.name }) }
    'demo.sites'  { return @($In.config.structure.sites | ForEach-Object { $_.url }) }
    default       { return @($Step) }
  }
}

function Invoke-BoxStep {
  param([string] $Step, [string] $Operation, $InputObject)
  if ($Step -eq 'demo.gate-check') {
    return @{ ok = [bool](Test-Path $env:BOX_PROVIDER_DEMO_GATE) }
  }
  $store = Read-Store
  $have = @(if ($store.ContainsKey($Step)) { $store[$Step] })
  $want = @(Get-Desired $Step $InputObject)
  $changes = @()
  foreach ($w in $want) { if ($w -notin $have) { $changes += @{ action = 'create'; target = "$Step/$w" } } }
  foreach ($h in $have) { if ($h -notin $want) { $changes += @{ action = 'delete'; target = "$Step/$h" } } }
  switch ($Operation) {
    'test'     { return @{ ok = $true } }
    'plan'     { return @{ changes = $changes } }
    'apply'    {
      if ($Step -eq 'demo.fragile' -and $env:BOX_PROVIDER_DEMO_FAIL -eq '1') { throw 'fragile step failed' }
      $i = 0
      foreach ($c in $changes) { $i++; Send-BoxProgress -Message $c.target -Done $i -Of $changes.Count }
      $store[$Step] = $want
      Write-Store $store
      return @{ changes = $changes }
    }
    'verify'   { return @{ ok = ($changes.Count -eq 0); drift = $changes } }
    'evidence' { return @{ items = @(@{ kind = 'export'; ref = "evidence/$Step.json"; summary = "$($want.Count) item(s)" }) } }
  }
}
Export-ModuleMember -Function Invoke-BoxStep
