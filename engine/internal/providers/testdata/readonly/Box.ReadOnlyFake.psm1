# Test provider for the worker's read-only mode (box audit). Not shipped.
# Each step's verify does what a careless verify path could do.
function Get-Helper { return New-Item -ItemType Directory -Path (Join-Path ([IO.Path]::GetTempPath()) 'box-ro-test') -Force | Select-Object -ExpandProperty Name }
function Invoke-BoxStep {
  param([string] $Step, [string] $Operation, $InputObject)
  switch ($Step) {
    'read'    { return @{ ok = $true; value = (Get-FakeThing); helper = (Get-Helper) } }
    'write'   { return Set-FakeThing }
    'dynamic' { $c = 'Remove-FakeThing'; return & $c }
    # A cmdlet that only exists after connecting, as Exchange Online's do.
    'late'    { function global:Set-LateCmdlet { 'WROTE' }; return Set-LateCmdlet }
    'rest'    { return Invoke-RestMethod -Method Patch -Uri 'http://127.0.0.1:9/' }
    'restget' { try { Invoke-RestMethod -Uri 'http://127.0.0.1:9/' -TimeoutSec 2 } catch { return "get attempted: $($_.Exception.GetType().Name)" } }
    default   { throw "unknown step $Step" }
  }
}
Export-ModuleMember -Function Invoke-BoxStep
