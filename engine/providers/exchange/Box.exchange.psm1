# Provider: exchange. Shared mailboxes, lists, aliases and automatic replies
# (T2-01), and the Exchange, Defender for Office 365 and Purview policy
# settings (T14). Connects with the pipeline app's token for
# outlook.office365.com, and for ps.compliance.protection.outlook.com
# (Security & Compliance PowerShell).
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxProvider.psm1') -Force
Import-Module (Join-Path $PSScriptRoot '..' 'common' 'BoxGraph.psm1') -Force

$script:LastToken = $null

function Connect-BoxExchange($In) {
  $t = (Get-BoxAuth 'app').access_token
  if ($t -ne $script:LastToken) {
    Connect-ExchangeOnline -AccessToken $t -Organization ([string]$In.tenant.initial_domain) -ShowBanner:$false -ErrorAction Stop | Out-Null
    $script:LastToken = $t
  }
}

# Security & Compliance (Purview) PowerShell, for the Purview and alert
# steps: a token for ps.compliance.protection.outlook.com under 'ipps'. box
# audit sends a reader-only delegated token (T16-06); module 3.9 or later
# takes -AccessToken here too. Its cmdlets are imported with the prefix SC
# so they never shadow the Exchange Online ones in the same worker
# (Get-Label is Get-SCLabel; Invoke-SC calls them by their usual names).
# A failed connection is remembered for the token, so a run of many
# settings does not retry it for each one.
$script:LastIppsToken = $null
$script:IppsError = $null
function Connect-BoxCompliance($In) {
  $t = (Get-BoxAuth 'ipps').access_token
  if ($t -ne $script:LastIppsToken) {
    $script:IppsError = $null
    try {
      Connect-IPPSSession -AccessToken $t -Organization ([string]$In.tenant.initial_domain) -Prefix SC -ShowBanner:$false -ErrorAction Stop | Out-Null
    } catch { $script:IppsError = "Security & Compliance PowerShell: $($_.Exception.Message)" }
    $script:LastIppsToken = $t
  }
  if (-not $script:IppsError -and -not (Get-Command Get-SCLabel -ErrorAction SilentlyContinue)) {
    $script:IppsError = 'Security & Compliance PowerShell connected but exposes no label, DLP or alert cmdlets: the account has no Security & Compliance role (Compliance Administrator for the pipeline app)'
  }
  if ($script:IppsError) { throw $script:IppsError }
}

# Calls a Security & Compliance cmdlet by its unprefixed name. The name is
# built at run time, so the worker's read-only guard cannot see it: in
# read-only mode (box audit) only Get cmdlets pass here.
function Invoke-SC([string] $Command, [hashtable] $Params = @{}) {
  $verb, $noun = $Command -split '-', 2
  if ($env:BOX_READ_ONLY -eq '1' -and $verb -ne 'Get') { throw "read-only (audit): $Command was refused; a verify must only read" }
  & "$verb-SC$noun" @Params -ErrorAction Stop
}

function Get-Domain($In) {
  $d = [string]$In.config.company.primary_domain
  if (-not $d) { $d = [string]$In.tenant.initial_domain }
  return $d.ToLower()
}
function Get-Recipient([string] $Identity) {
  return Get-EXORecipient -Identity $Identity -ErrorAction SilentlyContinue -Properties EmailAddresses, RecipientTypeDetails
}
function Get-Addresses($r) {
  return @($r.EmailAddresses | Where-Object { $_ -like 'smtp:*' } | ForEach-Object { ($_ -replace '^smtp:', '').ToLower() })
}
# Mailbox access is granted to the members of the access group: Exchange
# accepts only mail-enabled groups, and access groups are Entra security
# groups. The group stays the source of truth; every apply re-syncs.
function Get-GroupMemberUpns([string] $Group) {
  $g = @(Get-BoxGraphAll "/groups?`$filter=displayName eq '$(ConvertTo-BoxFilterValue $Group)'&`$select=id")
  if ($g.Count -ne 1) { throw "access group $Group not found (run identity.groups)" }
  return @(Get-BoxGraphAll "/groups/$($g[0].id)/members/microsoft.graph.user?`$select=userPrincipalName" | ForEach-Object { $_.userPrincipalName.ToLower() })
}

function Get-SharedChanges($In, [switch] $Apply) {
  Connect-BoxExchange $In
  $dom = Get-Domain $In
  $changes = @()
  foreach ($m in @($In.config.mail.shared)) {
    $addr = "$($m.name)@$dom"
    $r = Get-Recipient $addr
    if (-not $r) {
      $changes += New-BoxChange create "shared mailbox $addr"
      if ($Apply) {
        $dn = if ($m.display_name) { [string]$m.display_name } else { [string]$m.name }
        New-Mailbox -Shared -Name $dn -DisplayName $dn -Alias $m.name -PrimarySmtpAddress $addr -ErrorAction Stop | Out-Null
        for ($i = 0; $i -lt 20 -and -not $r; $i++) { Start-Sleep -Seconds 6; $r = Get-Recipient $addr }
        if (-not $r) { throw "mailbox $addr not visible after creation" }
      } else { continue }
    } elseif ($r.RecipientTypeDetails -ne 'SharedMailbox') {
      throw "$addr exists as $($r.RecipientTypeDetails), not a shared mailbox"
    }
    # A shared mailbox's account must not be able to sign in (SPEC §6) and holds no licence.
    $acct = Get-BoxGraphOne "/users/$([uri]::EscapeDataString($addr))?`$select=id,accountEnabled,assignedLicenses"
    if ($acct -and $acct.accountEnabled) {
      $changes += New-BoxChange update "block sign-in for $addr"
      if ($Apply) { Invoke-BoxGraph -Method PATCH -Uri "/users/$($acct.id)" -Body @{ accountEnabled = $false } | Out-Null }
    }
    if ($acct -and @($acct.assignedLicenses).Count -gt 0) { throw "shared mailbox $addr holds a licence; remove it (SPEC §6)" }
    $have = Get-Addresses $r
    foreach ($a in @($m.aliases | Where-Object { $_ })) {
      $full = "$a@$dom"
      if ($full -notin $have) {
        $changes += New-BoxChange update "alias $full on $addr"
        if ($Apply) { Set-Mailbox -Identity $addr -EmailAddresses @{ add = "smtp:$full" } -ErrorAction Stop }
      }
    }
    $mb = Get-EXOMailbox -Identity $addr -Properties MessageCopyForSentAsEnabled, MessageCopyForSendOnBehalfEnabled -ErrorAction Stop
    if (-not $mb.MessageCopyForSentAsEnabled) {
      $changes += New-BoxChange update "copy sent items on $addr"
      if ($Apply) { Set-Mailbox -Identity $addr -MessageCopyForSentAsEnabled $true -MessageCopyForSendOnBehalfEnabled $true -ErrorAction Stop }
    }
    $members = Get-GroupMemberUpns ([string]$m.access)
    $full = @(Get-EXOMailboxPermission -Identity $addr -ErrorAction Stop | Where-Object { $_.AccessRights -contains 'FullAccess' -and -not $_.IsInherited } | ForEach-Object { ([string]$_.User).ToLower() })
    $sendAs = @(Get-EXORecipientPermission -Identity $addr -ErrorAction Stop | Where-Object { $_.AccessRights -contains 'SendAs' } | ForEach-Object { ([string]$_.Trustee).ToLower() })
    foreach ($u in $members) {
      if ($u -notin $full) {
        $changes += New-BoxChange update "full access for $u on $addr" "via $($m.access)"
        if ($Apply) { Add-MailboxPermission -Identity $addr -User $u -AccessRights FullAccess -AutoMapping $true -Confirm:$false -ErrorAction Stop | Out-Null }
      }
      if ($u -notin $sendAs) {
        $changes += New-BoxChange update "send-as for $u on $addr" "via $($m.access)"
        if ($Apply) { Add-RecipientPermission -Identity $addr -Trustee $u -AccessRights SendAs -Confirm:$false -ErrorAction Stop | Out-Null }
      }
    }
  }
  return $changes
}

function Get-ListChanges($In, [switch] $Apply) {
  Connect-BoxExchange $In
  $dom = Get-Domain $In
  $users = @{}
  foreach ($u in @($In.config.users)) { $users[[string]$u.id] = "$($u.primary)@$dom" }
  $changes = @()
  foreach ($l in @($In.config.mail.lists)) {
    $addr = "$($l.name)@$dom"
    $want = @($l.members | Where-Object { $_ } | ForEach-Object { if ($users.ContainsKey([string]$_)) { $users[[string]$_] } else { "$_@$dom" } })
    $r = Get-Recipient $addr
    if (-not $r) {
      $changes += New-BoxChange create "list $addr" "$($want.Count) member(s)"
      if ($Apply) {
        $dn = if ($l.display_name) { [string]$l.display_name } else { [string]$l.name }
        New-DistributionGroup -Name $dn -DisplayName $dn -Alias $l.name -PrimarySmtpAddress $addr -Type Distribution -ErrorAction Stop | Out-Null
        for ($i = 0; $i -lt 20 -and -not $r; $i++) { Start-Sleep -Seconds 6; $r = Get-Recipient $addr }
      } else { continue }
    }
    $have = Get-Addresses $r
    foreach ($a in @($l.aliases | Where-Object { $_ })) {
      if ("$a@$dom" -notin $have) {
        $changes += New-BoxChange update "alias $a@$dom on $addr"
        if ($Apply) { Set-DistributionGroup -Identity $addr -EmailAddresses @{ add = "smtp:$a@$dom" } -ErrorAction Stop }
      }
    }
    $mem = @(Get-DistributionGroupMember -Identity $addr -ErrorAction Stop | ForEach-Object { ([string]$_.PrimarySmtpAddress).ToLower() })
    foreach ($w in $want) {
      if ($w.ToLower() -notin $mem) {
        $changes += New-BoxChange update "member $w of $addr"
        if ($Apply) { Add-DistributionGroupMember -Identity $addr -Member $w -BypassSecurityGroupManagerCheck -ErrorAction Stop }
      }
    }
    $requireAuth = -not [bool]$l.external_senders
    $dg = Get-DistributionGroup -Identity $addr -ErrorAction Stop
    if ($dg.RequireSenderAuthenticationEnabled -ne $requireAuth) {
      $changes += New-BoxChange update "external senders on $addr" "allowed=$([bool]$l.external_senders)"
      if ($Apply) { Set-DistributionGroup -Identity $addr -RequireSenderAuthenticationEnabled $requireAuth -ErrorAction Stop }
    }
  }
  return $changes
}

function Format-BoxText([string] $Text, $In) {
  return [regex]::Replace($Text, '\{company\.(\w+)\}', { param($m) [string]$In.config.company.($m.Groups[1].Value) })
}
function Get-AutoReplyChanges($In, [switch] $Apply) {
  Connect-BoxExchange $In
  $dom = Get-Domain $In
  $changes = @()
  $lines = if ($In.config.disclosures.auto_reply_lines.enabled) { "`n`n" + [string]$In.config.disclosures.auto_reply_lines.text } else { '' }
  foreach ($m in @($In.config.mail.shared | Where-Object { $_.auto_reply })) {
    $addr = "$($m.name)@$dom"
    $text = (Format-BoxText ([string]$In.config.mail.auto_replies.($m.auto_reply)) $In) + $lines
    $html = '<p>' + [System.Net.WebUtility]::HtmlEncode($text).Replace("`n", '<br>') + '</p>'
    if (-not (Get-Recipient $addr)) { $changes += New-BoxChange update "automatic reply on $addr" 'after the mailbox is created'; continue }
    $cur = Get-MailboxAutoReplyConfiguration -Identity $addr -ErrorAction Stop
    $same = $cur.AutoReplyState -eq 'Enabled' -and ($cur.ExternalMessage -replace '<[^>]+>|\s', '') -eq ($html -replace '<[^>]+>|\s', '')
    if (-not $same) {
      $changes += New-BoxChange update "automatic reply on $addr"
      if ($Apply) { Set-MailboxAutoReplyConfiguration -Identity $addr -AutoReplyState Enabled -InternalMessage $html -ExternalMessage $html -ExternalAudience All -ErrorAction Stop }
    }
  }
  return $changes
}

function Result-Changes($c) { return @{ changes = @($c | Where-Object { $null -ne $_ }) } }
function New-Evidence([string] $Kind, [string] $Summary) { return @{ items = @(@{ kind = $Kind; ref = "tenant:$Kind"; summary = $Summary }) } }
$ok = { param($In) @{ ok = $true } }

. (Join-Path $PSScriptRoot 'Box.exchange.policy.ps1')

$script:Steps = [ordered]@{
  'exchange.shared-mailboxes' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-SharedChanges $In) }
    apply = { param($In) Result-Changes (Get-SharedChanges $In -Apply) }
    verify = { param($In) $c = @(Get-SharedChanges $In | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'shared-mailboxes' "$(@($In.config.mail.shared).Count) shared mailbox(es)" }
  }
  'exchange.lists' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-ListChanges $In) }
    apply = { param($In) Result-Changes (Get-ListChanges $In -Apply) }
    verify = { param($In) $c = @(Get-ListChanges $In | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'lists' "$(@($In.config.mail.lists).Count) list(s)" }
  }
  'exchange.auto-replies' = @{
    test = $ok
    plan = { param($In) Result-Changes (Get-AutoReplyChanges $In) }
    apply = { param($In) Result-Changes (Get-AutoReplyChanges $In -Apply) }
    verify = { param($In) $c = @(Get-AutoReplyChanges $In | Where-Object { $_ }); @{ ok = ($c.Count -eq 0); drift = $c } }
    evidence = { param($In) New-Evidence 'auto-replies' 'automatic replies set' }
  }
  # Resolved policy settings (T14-04): Box.exchange.policy.ps1.
  'policy.exchange' = @{
    test = $ok
    plan = { param($In) Invoke-BoxPolicyOp 'policy.exchange' plan $In }
    apply = { param($In) Invoke-BoxPolicyOp 'policy.exchange' apply $In }
    verify = { param($In) Invoke-BoxPolicyOp 'policy.exchange' verify $In }
    evidence = { param($In) Invoke-BoxPolicyOp 'policy.exchange' evidence $In }
  }
  'policy.defender' = @{
    test = $ok
    plan = { param($In) Invoke-BoxPolicyOp 'policy.defender' plan $In }
    apply = { param($In) Invoke-BoxPolicyOp 'policy.defender' apply $In }
    verify = { param($In) Invoke-BoxPolicyOp 'policy.defender' verify $In }
    evidence = { param($In) Invoke-BoxPolicyOp 'policy.defender' evidence $In }
  }
  'policy.purview' = @{
    test = $ok
    plan = { param($In) Invoke-BoxPolicyOp 'policy.purview' plan $In }
    apply = { param($In) Invoke-BoxPolicyOp 'policy.purview' apply $In }
    verify = { param($In) Invoke-BoxPolicyOp 'policy.purview' verify $In }
    evidence = { param($In) Invoke-BoxPolicyOp 'policy.purview' evidence $In }
  }
  'policy.m365-admin.exchange' = @{
    test = $ok
    plan = { param($In) Invoke-BoxPolicyOp 'policy.m365-admin.exchange' plan $In }
    apply = { param($In) Invoke-BoxPolicyOp 'policy.m365-admin.exchange' apply $In }
    verify = { param($In) Invoke-BoxPolicyOp 'policy.m365-admin.exchange' verify $In }
    evidence = { param($In) Invoke-BoxPolicyOp 'policy.m365-admin.exchange' evidence $In }
  }
  'policy.m365-apps.exchange' = @{
    test = $ok
    plan = { param($In) Invoke-BoxPolicyOp 'policy.m365-apps.exchange' plan $In }
    apply = { param($In) Invoke-BoxPolicyOp 'policy.m365-apps.exchange' apply $In }
    verify = { param($In) Invoke-BoxPolicyOp 'policy.m365-apps.exchange' verify $In }
    evidence = { param($In) Invoke-BoxPolicyOp 'policy.m365-apps.exchange' evidence $In }
  }
}

function Invoke-BoxStep {
  param([string] $Step, [string] $Operation, $InputObject)
  Invoke-BoxDispatch -Steps $script:Steps -Step $Step -Operation $Operation -InputObject $InputObject
}
function Get-BoxStepIds { @($script:Steps.Keys) }
function Get-BoxStepProblems { Test-BoxStepTable -Steps $script:Steps }
Export-ModuleMember -Function Invoke-BoxStep, Get-BoxStepIds, Get-BoxStepProblems
