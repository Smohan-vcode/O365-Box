<#
  Microsoft Graph helpers for providers that call Graph with the token the
  orchestrator sends (Get-BoxAuth 'graph'). Uses Microsoft.Graph.Authentication.
#>

$script:LastToken = $null

function Connect-BoxGraph {
  $t = (Get-BoxAuth 'graph').access_token
  if ($t -ne $script:LastToken) {
    Connect-MgGraph -AccessToken (ConvertTo-SecureString $t -AsPlainText -Force) -NoWelcome -ErrorAction Stop | Out-Null
    $script:LastToken = $t
  }
}

function Invoke-BoxGraph {
  param([string] $Method = 'GET', [Parameter(Mandatory)] [string] $Uri, $Body)
  Connect-BoxGraph
  $u = if ($Uri -like 'https://*') { $Uri } else { 'https://graph.microsoft.com/v1.0' + $Uri }
  $p = @{ Method = $Method; Uri = $u; OutputType = 'Hashtable'; ErrorAction = 'Stop' }
  if ($null -ne $Body) { $p.Body = ($Body | ConvertTo-Json -Depth 20 -Compress); $p.ContentType = 'application/json' }
  for ($i = 0; ; $i++) {
    try { return Invoke-MgGraphRequest @p }
    catch {
      $msg = $_.Exception.Message
      if ($_.ErrorDetails -and $_.ErrorDetails.Message) {
        try { $e = ($_.ErrorDetails.Message | ConvertFrom-Json).error; $msg = "$msg $($e.code): $($e.message)" } catch { $msg = "$msg $($_.ErrorDetails.Message)" }
      }
      # Replication delays after creating an object show up as 404/400 for a few seconds.
      if ($i -lt 6 -and $Method -ne 'GET' -and ($msg -match 'NotFound|does not exist|Request_ResourceNotFound')) { Start-Sleep -Seconds (3 + 2 * $i); continue }
      throw "$Method $Uri failed: $msg"
    }
  }
}

function Get-BoxGraphAll {
  param([Parameter(Mandatory)] [string] $Uri)
  $out = [System.Collections.Generic.List[object]]::new()
  $next = $Uri
  while ($next) {
    $page = Invoke-BoxGraph -Uri $next
    foreach ($v in @($page.value)) { if ($null -ne $v) { $out.Add($v) } }
    $next = $page['@odata.nextLink']
  }
  # Callers wrap the call in @() to get an array, empty or not.
  return $out.ToArray()
}

function Get-BoxGraphOne {
  # Returns the object, or $null on 404.
  param([Parameter(Mandatory)] [string] $Uri)
  try { return Invoke-BoxGraph -Uri $Uri }
  catch { if ($_.Exception.Message -match 'NotFound|404|does not exist') { return $null }; throw }
}

function ConvertTo-BoxFilterValue([string] $s) { return $s.Replace("'", "''") }

Export-ModuleMember -Function Connect-BoxGraph, Invoke-BoxGraph, Get-BoxGraphAll, Get-BoxGraphOne, ConvertTo-BoxFilterValue
