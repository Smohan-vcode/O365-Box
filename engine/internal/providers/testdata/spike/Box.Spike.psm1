# Spike S-05 provider: connects a Microsoft module with an offline test
# token to force its identity assemblies to load. Not shipped.
function Invoke-BoxStep {
  param([string] $Step, [string] $Operation, $InputObject)
  $tok = (Get-BoxAuth 'test').access_token
  $sec = ConvertTo-SecureString $tok -AsPlainText -Force
  $r = @{ module = $Step; outcome = 'ok'; detail = '' }
  try {
    switch ($Step) {
      'Microsoft.Graph.Authentication' { Connect-MgGraph -AccessToken $sec -NoWelcome -ErrorAction Stop | Out-Null }
      'PnP.PowerShell' { Connect-PnPOnline -Url 'https://example.sharepoint.com' -AccessToken $tok -ErrorAction Stop | Out-Null }
      'ExchangeOnlineManagement' { Connect-ExchangeOnline -AccessToken $tok -Organization 'example.onmicrosoft.com' -ShowBanner:$false -ErrorAction Stop | Out-Null }
    }
  } catch {
    $r.outcome = $_.Exception.GetType().Name
    $r.detail = ($_.Exception.Message -split "`n")[0]
  }
  return $r
}
Export-ModuleMember -Function Invoke-BoxStep
