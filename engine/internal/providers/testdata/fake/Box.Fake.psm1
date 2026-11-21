# Test provider for the worker protocol. Not shipped.
function Invoke-BoxStep {
  param([string] $Step, [string] $Operation, $InputObject)
  switch ($Step) {
    'echo'     { return @{ step = $Step; operation = $Operation; input = $InputObject } }
    'progress' { 1..3 | ForEach-Object { Send-BoxProgress -Message "item $_" -Done $_ -Of 3 }; return @{ done = 3 } }
    'logs'     { Write-Host 'host line'; Write-Warning 'careful'; Write-Verbose 'hidden' -Verbose; Write-Information 'info line'; return 'ok' }
    'fail'     { throw 'step failed on purpose' }
    'leak'     { $t = (Get-BoxAuth 'graph').access_token; Write-Host "token is $t"; throw "bad token $t" }
    'console'  { [Console]::WriteLine('stray console output'); return 'after console' }
    'slow'     { Start-Sleep -Seconds 30; return 'late' }
    'auth'     { return @{ has = [bool](Get-BoxAuth 'graph').access_token } }
    default    { throw "unknown step $Step" }
  }
}
Export-ModuleMember -Function Invoke-BoxStep
