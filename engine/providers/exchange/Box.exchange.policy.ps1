# Policy steps of the exchange provider (T14-04, T14-08, T14-10, T14-11,
# T14-12): policy.exchange, policy.defender, policy.purview,
# policy.m365-admin.exchange and policy.m365-apps.exchange. Each step
# receives its resolved settings in $In.policy.settings; the mapping files
# (expected-state/setting-ids/*.json) are the spec of what each handler
# calls. Dot-sourced by Box.exchange.psm1.
#
# Security & Compliance cmdlets are called through Invoke-SC after
# Connect-BoxCompliance (Box.exchange.psm1), with the prefix SC.

# ---------- pure helpers (Pester-tested) ----------

function ConvertTo-BoxHash($o) {
  if ($null -eq $o) { return @{} }
  if ($o -is [System.Collections.IDictionary]) { return $o }
  return ($o | ConvertTo-Json -Depth 20 | ConvertFrom-Json -AsHashtable)
}

# A value in a form two settings can be compared by: case and order do not
# matter; lists compare as sets.
function ConvertTo-BoxComparable($v) {
  if ($null -eq $v) { return '' }
  if ($v -is [string]) { return $v.Trim().ToLowerInvariant() }
  if ($v -is [bool]) { return $v.ToString().ToLowerInvariant() }
  if ($v -is [System.Collections.IDictionary]) { return (($v.Keys | Sort-Object | ForEach-Object { "$_=$(ConvertTo-BoxComparable $v[$_])" }) -join ';') }
  if ($v -is [System.Collections.IEnumerable]) { return ((@($v) | Where-Object { $null -ne $_ } | ForEach-Object { ConvertTo-BoxComparable $_ } | Where-Object { $_ -ne '' } | Sort-Object -Unique) -join "`n") }
  return ([string]$v).Trim().ToLowerInvariant()
}

# Lists the desired properties whose current value differs, as
# "Name: have -> want". Identity is a selector, not a property.
function Compare-BoxProperties($Current, [System.Collections.IDictionary] $Desired) {
  $out = @()
  foreach ($k in @($Desired.Keys | Sort-Object)) {
    if ($k -eq 'Identity') { continue }
    $have = if ($null -eq $Current) { $null } else { $Current.$k }
    $a = ConvertTo-BoxComparable $have
    $b = ConvertTo-BoxComparable $Desired[$k]
    if ($a -ne $b) { $out += "${k}: $($a -replace "`n", ',') -> $($b -replace "`n", ',')" }
  }
  return $out
}

# Get-LabelPolicy and Get-Label return Settings as "[key, value]" strings.
function ConvertFrom-BoxSettingList($Settings) {
  $h = @{}
  foreach ($e in @($Settings)) {
    if ($e -is [System.Collections.IDictionary]) { foreach ($k in $e.Keys) { $h[([string]$k).ToLowerInvariant()] = [string]$e[$k] }; continue }
    $m = [regex]::Match([string]$e, '^\[\s*([^,]+?)\s*,\s*(.*?)\s*\]$')
    if ($m.Success) { $h[$m.Groups[1].Value.ToLowerInvariant()] = $m.Groups[2].Value }
  }
  return $h
}

# A label's LabelActions (JSON strings) flattened to "type[/subtype].key" = value.
function ConvertFrom-BoxLabelActions($Actions) {
  $h = @{}
  foreach ($a in @($Actions)) {
    if (-not $a) { continue }
    try { $o = if ($a -is [string]) { $a | ConvertFrom-Json } else { $a } } catch { continue }
    $t = ([string]$o.Type).ToLowerInvariant()
    if ($o.SubType) { $t += '/' + ([string]$o.SubType).ToLowerInvariant() }
    $h[$t] = 'on'
    foreach ($s in @($o.Settings)) { if ($s.Key) { $h["$t." + ([string]$s.Key).ToLowerInvariant()] = [string]$s.Value } }
  }
  return $h
}

# The domains a sharing policy keeps: free/busy only, never anonymous.
function Get-BoxSharingDomains([string[]] $Domains, [bool] $IsDefault) {
  $keep = @($Domains | Where-Object { $_ -and $_ -notlike 'Anonymous:*' -and $_ -like '*:CalendarSharingFreeBusySimple' })
  if ($IsDefault -and -not ($keep -contains '*:CalendarSharingFreeBusySimple')) { $keep = @('*:CalendarSharingFreeBusySimple') + $keep }
  return $keep
}

# DLP-09: a recipient pattern matching a user's own consumer address
# (given, family, given.family, family.given, optional digits). Built at
# plan time from the configuration; never stored.
function Get-BoxOwnAddressPattern([string] $Given, [string] $Family, [string[]] $Domains) {
  $g = [regex]::Escape($Given.ToLowerInvariant()); $f = [regex]::Escape($Family.ToLowerInvariant())
  $d = ($Domains | ForEach-Object { [regex]::Escape($_.ToLowerInvariant()) }) -join '|'
  return "^($g|$f|$g[._-]?$f|$f[._-]?$g)[._-]?[0-9]*@($d)$"
}

# ---------- results ----------

function New-BoxNote([string] $Id, [string] $Text) { return @{ note = "${Id}: $Text" } }
function Test-AuditMode($s) { return ([string]$s.mode) -in @('audit', 'report') }

# Runs one handler per resolved setting and gathers changes, notes and
# errors. A setting that fails does not stop the others; the errors are
# raised together at the end.
function Invoke-BoxPolicy($In, [hashtable] $Handlers, [switch] $Apply) {
  if ($In.policy_error) { throw "policy settings could not be resolved: $($In.policy_error)" }
  Connect-BoxExchange $In
  $settings = @($In.policy.settings | Where-Object { $_ } | Sort-Object { [string]$_.id })
  $script:PolicyCtx = @{ in = $In; ids = @($settings | ForEach-Object { [string]$_.id }); labels = $null; skippedLabels = @() }
  $changes = @(); $notes = @(); $errors = @()
  foreach ($s in $settings) {
    $h = $Handlers[[string]$s.id]
    if (-not $h) { $errors += "$($s.id): no handler in the exchange provider"; continue }
    try {
      foreach ($r in @(& $h $In $s $Apply.IsPresent)) {
        if ($null -eq $r) { continue }
        if ($r.note) { $notes += $r.note } elseif ($r.action) { $r.target = "$($s.id) $($r.target)"; $changes += $r }
      }
    } catch { $errors += "$($s.id): $($_.Exception.Message)" }
  }
  return @{ changes = $changes; notes = $notes; errors = $errors; count = $settings.Count }
}

function Result-Policy($r) {
  if ($r.errors.Count) { throw ("{0} setting(s) failed: {1}" -f $r.errors.Count, ($r.errors -join ' | ')) }
  return @{ changes = @($r.changes); notes = @($r.notes) }
}
function Verify-Policy($r) {
  return @{ ok = ($r.changes.Count -eq 0 -and $r.errors.Count -eq 0); drift = @($r.changes); notes = @($r.notes); errors = @($r.errors) }
}
function Evidence-Policy($r, [string] $Step) {
  $items = @(@{ kind = $Step; ref = "tenant:$Step"; summary = "$($r.count) resolved setting(s); drift $($r.changes.Count); errors $($r.errors.Count)" })
  foreach ($n in $r.notes) { $items += @{ kind = "$Step-note"; ref = "tenant:$Step"; summary = $n } }
  return @{ items = $items }
}

# Compares one object with the desired properties and, when applying,
# calls $SyncSet with the desired hashtable.
function Sync-BoxSetting([string] $SyncTarget, $SyncCurrent, [System.Collections.IDictionary] $SyncDesired, [scriptblock] $SyncSet, [bool] $SyncApply) {
  $syncDiff = @(Compare-BoxProperties $SyncCurrent $SyncDesired)
  if ($syncDiff.Count -eq 0) { return }
  if ($SyncApply) { $syncParams = @{}; foreach ($syncKey in $SyncDesired.Keys) { if ($syncKey -ne 'Identity') { $syncParams[$syncKey] = $SyncDesired[$syncKey] } }; & $SyncSet $syncParams }
  return New-BoxChange update $SyncTarget ($syncDiff -join '; ')
}

function Get-Part($s, [string] $Name) { return ConvertTo-BoxHash ($s.payload.$Name) }
function Get-Mailbox-Address($In, [string] $Name) { return "$Name@$(Get-Domain $In)" }

# ---------- policy.exchange ----------

# Role assignment and sharing policies can be changed only once the
# organization is customizable (a one-time, tenant-wide switch). Returns a
# change while it is not.
function Use-BoxOrgCustomization([bool] $Apply) {
  if (-not (Get-OrganizationConfig -ErrorAction Stop).IsDehydrated) { return }
  if ($Apply) { Enable-OrganizationCustomization -ErrorAction Stop | Out-Null }
  New-BoxChange update 'organization customization' 'enable (needed to edit role assignment and sharing policies)'
}

function Sync-SharingPolicies([bool] $Apply, [switch] $DefaultOnly) {
  $c = Use-BoxOrgCustomization $Apply
  if ($c) { $c }
  foreach ($p in @(Get-SharingPolicy -ErrorAction Stop | Where-Object { $_.Enabled -or $_.Default })) {
    if ($DefaultOnly -and -not $p.Default) { continue }
    $want = if ($DefaultOnly) { @('*:CalendarSharingFreeBusySimple') } else { Get-BoxSharingDomains @($p.Domains | ForEach-Object { [string]$_ }) ([bool]$p.Default) }
    if (-not $p.Default -and $want.Count -eq 0) { $want = $null }
    $desired = if ($null -eq $want) { @{ Enabled = $false } } else { @{ Domains = $want } }
    $cur = [pscustomobject]@{ Domains = @($p.Domains | ForEach-Object { [string]$_ }); Enabled = $p.Enabled }
    Sync-BoxSetting "sharing policy $($p.Name)" $cur $desired { param($d) Set-SharingPolicy -Identity $p.Identity @d -Confirm:$false -ErrorAction Stop } $Apply
  }
}

function Sync-AddinRoles([string[]] $Roles, [bool] $Apply) {
  $c = Use-BoxOrgCustomization $Apply
  if ($c) { $c }
  foreach ($pol in @(Get-RoleAssignmentPolicy -ErrorAction Stop)) {
    $have = @($pol.AssignedRoles | ForEach-Object { [string]$_ } | Where-Object { $_ -in $Roles })
    if ($have.Count -eq 0) { continue }
    if ($Apply) {
      Get-ManagementRoleAssignment -RoleAssignee $pol.Name -ErrorAction Stop | Where-Object { [string]$_.Role -in $Roles } |
        ForEach-Object { Remove-ManagementRoleAssignment -Identity $_.Identity -Confirm:$false -ErrorAction Stop }
    }
    New-BoxChange update "role assignment policy $($pol.Name)" "remove $($have -join ', ')"
  }
}

function Get-SmtpAuthUseNote($s) {
  try {
    $since = (Get-Date).ToUniversalTime().AddDays(-30).ToString('yyyy-MM-ddTHH:mm:ssZ')
    $n = @(Get-BoxGraphAll "/auditLogs/signIns?`$filter=clientAppUsed eq 'Authenticated SMTP' and createdDateTime ge $since&`$select=userPrincipalName" | ForEach-Object { $_.userPrincipalName } | Sort-Object -Unique).Count
    return New-BoxNote $s.id "audit: no change; $n account(s) used SMTP AUTH in the last 30 days"
  } catch { return New-BoxNote $s.id "audit: no change; SMTP AUTH sign-ins not readable ($($_.Exception.Message))" }
}

$script:ExchangeHandlers = @{
  'EXO-001' = { param($In, $s, $Apply)
    if (Test-AuditMode $s) { return Get-SmtpAuthUseNote $s }
    Sync-BoxSetting 'transport config' (Get-TransportConfig -ErrorAction Stop) (Get-Part $s 'Set-TransportConfig') { param($d) Set-TransportConfig @d -ErrorAction Stop } $Apply
  }
  'EXO-002' = { param($In, $s, $Apply)
    foreach ($p in @(Get-CASMailboxPlan -ErrorAction Stop)) {
      Sync-BoxSetting "mailbox plan $($p.Name)" $p @{ PopEnabled = $false; ImapEnabled = $false } { param($d) Set-CASMailboxPlan -Identity $p.Identity @d -ErrorAction Stop } $Apply
    }
  }
  'EXO-003' = { param($In, $s, $Apply)
    if (Test-AuditMode $s) { return New-BoxNote $s.id 'audit: no change; direct-send senders are reviewed from message trace before enforce' }
    Sync-BoxSetting 'organization config' (Get-OrganizationConfig -ErrorAction Stop) (Get-Part $s 'Set-OrganizationConfig') { param($d) Set-OrganizationConfig @d -ErrorAction Stop } $Apply
  }
  'EXO-004' = { param($In, $s, $Apply)
    Sync-BoxSetting 'remote domain Default' (Get-RemoteDomain -Identity Default -ErrorAction Stop) @{ AutoForwardEnabled = $false } { param($d) Set-RemoteDomain -Identity Default @d -ErrorAction Stop } $Apply
  }
  'EXO-005' = { param($In, $s, $Apply)
    $want = Get-Part $s 'New-TransportRule'
    $name = [string]$want.Name
    $r = Get-TransportRule -ErrorAction Stop | Where-Object { $_.Name -eq $name }
    if (-not $r) {
      if ($Apply) { New-TransportRule @want -ErrorAction Stop | Out-Null }
      return New-BoxChange create "transport rule '$name'"
    }
    $props = @{}; foreach ($k in 'FromScope', 'SentToScope', 'MessageTypeMatches', 'RejectMessageEnhancedStatusCode', 'RejectMessageReasonText', 'Mode') { $props[$k] = $want[$k] }
    Sync-BoxSetting "transport rule '$name'" $r $props { param($d) Set-TransportRule -Identity $r.Identity @d -ErrorAction Stop } $Apply
    if ([string]$r.State -ne 'Enabled') {
      if ($Apply) { Enable-TransportRule -Identity $r.Identity -Confirm:$false -ErrorAction Stop }
      New-BoxChange update "transport rule '$name'" 'enable'
    }
  }
  'EXO-007' = { param($In, $s, $Apply)
    $cur = @(Get-ExternalInOutlook -ErrorAction Stop)[0]
    Sync-BoxSetting 'external sender tag' $cur @{ Enabled = $true } { param($d) Set-ExternalInOutlook @d -ErrorAction Stop | Out-Null } $Apply
  }
  'EXO-008' = { param($In, $s, $Apply)
    Sync-BoxSetting 'organization config (mail tips)' (Get-OrganizationConfig -ErrorAction Stop) (Get-Part $s 'Set-OrganizationConfig') { param($d) Set-OrganizationConfig @d -ErrorAction Stop } $Apply
  }
  'EXO-009' = { param($In, $s, $Apply)
    Sync-BoxSetting 'organization config (mailbox audit)' (Get-OrganizationConfig -ErrorAction Stop) @{ AuditDisabled = $false } { param($d) Set-OrganizationConfig @d -ErrorAction Stop } $Apply
    foreach ($m in @(Get-EXOMailbox -ResultSize Unlimited -RecipientTypeDetails UserMailbox, SharedMailbox -Properties AuditEnabled -ErrorAction Stop | Where-Object { -not $_.AuditEnabled })) {
      if ($Apply) { Set-Mailbox -Identity $m.PrimarySmtpAddress -AuditEnabled $true -ErrorAction Stop }
      New-BoxChange update "mailbox audit on $($m.PrimarySmtpAddress)"
    }
  }
  # Shared mailbox sign-in and access are applied by exchange.shared-mailboxes;
  # the policy step runs the same comparison so drift shows here too.
  'EXO-010' = { param($In, $s, $Apply) New-BoxNote $s.id 'checked with EXO-011 (exchange.shared-mailboxes)' }
  'EXO-011' = { param($In, $s, $Apply) Get-SharedChanges $In -Apply:$Apply }
  'EXO-012' = { param($In, $s, $Apply)
    foreach ($p in @(Get-OwaMailboxPolicy -ErrorAction Stop)) {
      Sync-BoxSetting "OWA policy $($p.Name)" $p @{ AdditionalStorageProvidersAvailable = $false } { param($d) Set-OwaMailboxPolicy -Identity $p.Identity @d -ErrorAction Stop } $Apply
    }
  }
  'EXO-013' = { param($In, $s, $Apply)
    foreach ($p in @(Get-OwaMailboxPolicy -ErrorAction Stop)) {
      Sync-BoxSetting "OWA policy $($p.Name)" $p @{ ConditionalAccessPolicy = 'ReadOnly' } { param($d) Set-OwaMailboxPolicy -Identity $p.Identity @d -ErrorAction Stop } $Apply
    }
  }
  'EXO-014' = { param($In, $s, $Apply) Sync-AddinRoles @($s.payload.remove_roles_from_default_role_assignment_policy) $Apply }
  'EXO-015' = { param($In, $s, $Apply) Sync-SharingPolicies $Apply }
  'EXO-016' = { param($In, $s, $Apply)
    foreach ($dom in @(Get-AcceptedDomain -ErrorAction Stop | Where-Object { $_.DomainName -notlike '*.onmicrosoft.com' -and [string]$_.DomainType -eq 'Authoritative' })) {
      $name = [string]$dom.DomainName
      $c = Get-DkimSigningConfig -ErrorAction Stop | Where-Object { [string]$_.Domain -eq $name }
      if (-not $c) {
        if ($Apply) { New-DkimSigningConfig -DomainName $name -KeySize 2048 -Enabled $false -ErrorAction Stop | Out-Null }
        New-BoxChange create "DKIM keys for $name" 'disabled until the selector CNAMEs are published (T2-03)'
        continue
      }
      if ([int]$c.Selector1KeySize -lt 2048) {
        if ($Apply) { Rotate-DkimSigningConfig -Identity $name -KeySize 2048 -ErrorAction Stop }
        New-BoxChange update "DKIM key size for $name" '2048'
      }
      if (-not $c.Enabled) {
        if ($Apply) {
          try { Set-DkimSigningConfig -Identity $name -Enabled $true -ErrorAction Stop }
          catch { New-BoxNote $s.id "DKIM for $name waits for its selector CNAMEs ($($_.Exception.Message))"; continue }
        }
        New-BoxChange update "enable DKIM for $name"
      }
    }
  }
  # Enhanced tier. The allow list is company data with no configuration key
  # yet, and an empty list would block every EWS client.
  'EXO-018' = { param($In, $s, $Apply) New-BoxNote $s.id 'skipped: the EWS allow list has no configuration key yet (an empty list would block every EWS client)' }
  'EXO-019' = { param($In, $s, $Apply)
    Sync-BoxSetting 'organization config (modern auth)' (Get-OrganizationConfig -ErrorAction Stop) (Get-Part $s 'Set-OrganizationConfig') { param($d) Set-OrganizationConfig @d -ErrorAction Stop } $Apply
  }
}

$script:AdminHandlers = @{
  'ADM-008' = { param($In, $s, $Apply) Sync-SharingPolicies $Apply -DefaultOnly }
}

# ---------- policy.defender ----------

function Get-PresetNote($s, [string] $What) {
  return New-BoxNote $s.id "skipped: $What exists only after the preset security policies are first turned on in the Defender portal (gate DEF-009)"
}

$script:DefenderHandlers = @{
  'DEF-012' = { param($In, $s, $Apply)
    $want = ConvertTo-BoxHash $s.payload
    $id = [string]$want.Identity
    Sync-BoxSetting "anti-phishing policy $id" (Get-AntiPhishPolicy -Identity $id -ErrorAction Stop) $want { param($d) Set-AntiPhishPolicy -Identity $id @d -ErrorAction Stop } $Apply
    foreach ($p in @(Get-AntiPhishPolicy -ErrorAction Stop | Where-Object { [string]$_.RecommendedPolicyType -in 'Standard', 'Strict' -and -not $_.HonorDmarcPolicy })) {
      New-BoxChange update "preset anti-phishing policy $($p.Name)" 'HonorDmarcPolicy is false (preset policies cannot be edited)'
    }
  }
  'DEF-013' = { param($In, $s, $Apply)
    $p = @(Get-SafeAttachmentPolicy -ErrorAction Stop | Where-Object { [string]$_.RecommendedPolicyType -eq 'Standard' })
    if ($p.Count -eq 0) { return Get-PresetNote $s 'the Standard preset Safe Attachments policy' }
    $want = [string](ConvertTo-BoxHash $s.payload).Action
    if ([string]$p[0].Action -ne $want -or -not $p[0].Enable) { New-BoxChange update "preset Safe Attachments policy $($p[0].Name)" "Action $($p[0].Action), expected $want (preset policies cannot be edited)" }
    $r = Get-ATPProtectionPolicyRule -ErrorAction SilentlyContinue | Where-Object { $_.Name -eq 'Standard Preset Security Policy' }
    if (-not $r -or [string]$r.State -ne 'Enabled') { New-BoxNote $s.id 'the Standard preset rule is not enabled yet (gate DEF-009)' }
  }
  'DEF-014' = { param($In, $s, $Apply)
    Sync-BoxSetting 'Safe Attachments for SharePoint, OneDrive and Teams' (Get-AtpPolicyForO365 -ErrorAction Stop) (ConvertTo-BoxHash $s.payload) { param($d) Set-AtpPolicyForO365 @d -ErrorAction Stop } $Apply
  }
  'DEF-015' = { param($In, $s, $Apply)
    $p = @(Get-SafeLinksPolicy -ErrorAction Stop | Where-Object { [string]$_.RecommendedPolicyType -in 'Standard', 'Strict' })
    if ($p.Count -eq 0) { return Get-PresetNote $s 'the Standard and Strict preset Safe Links policies' }
    foreach ($x in $p) {
      $d = @(Compare-BoxProperties $x @{ EnableSafeLinksForEmail = $true; EnableSafeLinksForTeams = $true; EnableSafeLinksForOffice = $true; AllowClickThrough = $false })
      if ($d.Count) { New-BoxChange update "preset Safe Links policy $($x.Name)" (($d -join '; ') + ' (preset policies cannot be edited)') }
    }
  }
  'DEF-016' = { param($In, $s, $Apply)
    Sync-BoxSetting 'anti-malware policy Default' (Get-MalwareFilterPolicy -Identity Default -ErrorAction Stop) (Get-Part $s 'Set-MalwareFilterPolicy') { param($d) Set-MalwareFilterPolicy -Identity Default @d -ErrorAction Stop } $Apply
    Sync-BoxSetting 'anti-spam policy Default' (Get-HostedContentFilterPolicy -Identity Default -ErrorAction Stop) (Get-Part $s 'Set-HostedContentFilterPolicy') { param($d) Set-HostedContentFilterPolicy -Identity Default @d -ErrorAction Stop } $Apply
  }
  'DEF-017' = { param($In, $s, $Apply)
    Sync-BoxSetting 'anti-spam policy Default (high-confidence phish)' (Get-HostedContentFilterPolicy -Identity Default -ErrorAction Stop) (Get-Part $s 'Set-HostedContentFilterPolicy') { param($d) Set-HostedContentFilterPolicy -Identity Default @d -ErrorAction Stop } $Apply
    Sync-BoxSetting 'anti-malware policy Default (quarantine)' (Get-MalwareFilterPolicy -Identity Default -ErrorAction Stop) (Get-Part $s 'Set-MalwareFilterPolicy') { param($d) Set-MalwareFilterPolicy -Identity Default @d -ErrorAction Stop } $Apply
  }
  'DEF-018' = { param($In, $s, $Apply)
    Sync-BoxSetting 'outbound spam policy Default (forwarding)' (Get-HostedOutboundSpamFilterPolicy -Identity Default -ErrorAction Stop) (ConvertTo-BoxHash $s.payload) { param($d) Set-HostedOutboundSpamFilterPolicy -Identity Default @d -ErrorAction Stop } $Apply
  }
  'DEF-019' = { param($In, $s, $Apply)
    Sync-BoxSetting 'outbound spam policy Default (limits)' (Get-HostedOutboundSpamFilterPolicy -Identity Default -ErrorAction Stop) (Get-Part $s 'Set-HostedOutboundSpamFilterPolicy') { param($d) Set-HostedOutboundSpamFilterPolicy -Identity Default @d -ErrorAction Stop } $Apply
    $alerts = Get-Mailbox-Address $In 'alerts'
    Connect-BoxCompliance $In
    $a = Invoke-SC 'Get-ProtectionAlert' @{ Identity = 'User restricted from sending email' }
    $have = @($a.NotifyUser | ForEach-Object { ([string]$_).ToLowerInvariant() })
    if ($alerts -notin $have -and $a.IsSystemRule) {
      # Set-ProtectionAlert answers "no rule matching identity" for the built-in
      # alert under app-only sign-in (by name, Guid and ImmutableId alike).
      return New-BoxNote $s.id "not applied: the built-in alert '$($a.Name)' cannot be changed app-only; it notifies $($a.NotifyUser -join ', ') (portal: Alert policies)"
    }
    if ($alerts -notin $have) {
      if ($Apply) { Invoke-SC 'Set-ProtectionAlert' @{ Identity = $a.Identity; NotifyUser = @($have + $alerts | Sort-Object -Unique) } | Out-Null }
      New-BoxChange update "alert policy '$($a.Name)'" "notify $alerts"
    }
  }
  'DEF-020' = { param($In, $s, $Apply)
    $sec = Get-Mailbox-Address $In 'security'
    $want = Get-Part $s 'Set-ReportSubmissionPolicy'
    foreach ($k in @($want.Keys)) { if ($want[$k] -is [System.Collections.IList]) { $want[$k] = @($want[$k] | ForEach-Object { if ($_ -eq '{mailbox:security}') { $sec } else { $_ } }) } }
    $id = [string]$want.Identity
    Sync-BoxSetting "report submission policy" (Get-ReportSubmissionPolicy -ErrorAction Stop) $want { param($d) Set-ReportSubmissionPolicy -Identity $id @d -ErrorAction Stop } $Apply
    $rule = @(Get-ReportSubmissionRule -ErrorAction Stop)[0]
    if (-not $rule) {
      if ($Apply) { New-ReportSubmissionRule -Name DefaultReportSubmissionRule -ReportSubmissionPolicy $id -SentTo $sec -ErrorAction Stop | Out-Null }
      return New-BoxChange create 'report submission rule' "to $sec"
    }
    Sync-BoxSetting 'report submission rule' $rule @{ SentTo = @($sec) } { param($d) Set-ReportSubmissionRule -Identity $rule.Identity @d -ErrorAction Stop } $Apply
    if ([string]$rule.State -ne 'Enabled') {
      if ($Apply) { Enable-ReportSubmissionRule -Identity $rule.Identity -Confirm:$false -ErrorAction Stop }
      New-BoxChange update 'report submission rule' 'enable'
    }
  }
  'DEF-021' = { param($In, $s, $Apply)
    $exp = (Get-Date).ToUniversalTime().Date.AddDays(30)
    foreach ($t in 'Sender', 'Url', 'FileHash') {
      foreach ($e in @(Get-TenantAllowBlockListItems -ListType $t -ErrorAction Stop)) {
        $limit = if ($e.LastModifiedDateTime) { ([datetime]$e.LastModifiedDateTime).AddDays(31) } else { $exp.AddDays(1) }
        if ($e.ExpirationDate -and ([datetime]$e.ExpirationDate) -le $limit) { continue }
        if ($Apply) { Set-TenantAllowBlockListItems -ListType $t -Ids $e.Identity -ExpirationDate $exp -ErrorAction Stop | Out-Null }
        New-BoxChange update "$t allow/block entry $($e.Value)" "expires $($exp.ToString('yyyy-MM-dd'))"
      }
    }
  }
}

# ---------- policy.purview ----------

# Engine texts for the labels (not company data).
$script:LabelTooltips = @{
  'Public' = 'Approved for anyone, inside or outside the company.'
  'Internal' = 'For people in the company. The default for new documents and mail.'
  'Confidential' = 'For named teams; sharing outside the company needs a reason.'
  'Confidential-Sales' = 'Confidential to the sales team.'
  'Confidential-Legal' = 'Confidential to the legal team.'
  'Confidential-People' = 'Confidential to people (HR) matters.'
  'Restricted' = 'Encrypted: only people in the company can open it, and mail cannot be forwarded.'
}

# Built-in sensitive information types per operating jurisdiction (T14-11
# catalogue data). Names not offered by the tenant are dropped at apply.
$script:JurisdictionTypes = @{
  'us' = @{ bank = @('U.S. Bank Account Number', 'ABA Routing Number'); personal = @('U.S. Social Security Number (SSN)', 'U.S. / U.K. Passport Number', "U.S. Driver's License Number", 'U.S. Individual Taxpayer Identification Number (ITIN)') }
  'gb' = @{ bank = @(); personal = @('U.K. National Insurance Number (NINO)', 'U.S. / U.K. Passport Number', "U.K. Driver's License Number", 'U.K. Unique Taxpayer Reference Number') }
  'eu' = @{ bank = @(); personal = @('EU Passport Number', 'EU National Identification Number', 'EU Tax Identification Number (TIN)', "EU Driver's License Number") }
  'de' = @{ bank = @(); personal = @('German Passport Number', 'Germany Identity Card Number', 'Germany Tax Identification Number') }
}

# 'All credential types' is not offered as one type on every tenant; these
# built-in credential types stand in for it (names the tenant lacks drop out).
$script:CredentialTypes = @('General Password', 'Client secret / API key', 'Http authorization header', 'Amazon S3 Client Secret Access Key',
  'GitHub Personal Access Token', 'Slack access token', 'Azure DocumentDB Auth Key', 'Azure IAAS Database Connection String and Azure SQL Connection String',
  'Azure IoT Connection String', 'Azure Publish Setting Password', 'Azure Redis Cache Connection String', 'Azure SAS', 'Azure Service Bus Connection String',
  'Azure Storage Account Key', 'Azure Storage Account Key (Generic)', 'SQL Server Connection String')

function Get-BoxJurisdictionTypes($In, [string] $Kind) {
  $j = @($In.config.jurisdictions.operating) + @($In.config.jurisdictions.registered) | Where-Object { $_ } | ForEach-Object { ([string]$_).ToLowerInvariant() } | Sort-Object -Unique
  $names = @(foreach ($x in $j) { if ($script:JurisdictionTypes.ContainsKey($x)) { $script:JurisdictionTypes[$x][$Kind] } })
  $known = Get-BoxTenantTypes
  return @($names | Sort-Object -Unique | Where-Object { $null -eq $known -or $_ -in $known })
}
function Get-BoxTenantTypes {
  if (-not $script:PolicyCtx.ContainsKey('sits')) {
    $script:PolicyCtx.sits = try { @(Invoke-SC 'Get-DlpSensitiveInformationType' | ForEach-Object { [string]$_.Name }) } catch { $null }
  }
  return $script:PolicyCtx.sits
}

function Get-BoxLabels([switch] $Refresh) {
  if ($Refresh -or $null -eq $script:PolicyCtx.labels) {
    $h = @{}
    foreach ($l in @(Invoke-SC 'Get-Label')) { $h[[string]$l.Name] = $l }
    $script:PolicyCtx.labels = $h
  }
  return $script:PolicyCtx.labels
}
function Get-BoxLabelId([string] $Name) {
  $l = (Get-BoxLabels)[$Name]
  if (-not $l) { return $null }
  if ($l.ImmutableId) { return [string]$l.ImmutableId }
  return [string]$l.Guid
}

# The essentials of a label read back from LabelActions, in the New-Label
# parameter names, for comparison with the payload.
function Get-BoxLabelState($l) {
  $a = ConvertFrom-BoxLabelActions $l.LabelActions
  $st = [ordered]@{
    DisplayName = [string]$l.DisplayName
    ContentType = (([string]$l.ContentType) -split '\s*,\s*' | Where-Object { $_ } | Sort-Object) -join ','
    ApplyContentMarkingHeaderEnabled = $a.ContainsKey('applycontentmarking/header')
    ApplyContentMarkingFooterEnabled = $a.ContainsKey('applycontentmarking/footer')
    ApplyWaterMarkingEnabled = $a.ContainsKey('applywatermarking')
    EncryptionEnabled = $a.ContainsKey('encrypt') -and $a['encrypt.disabled'] -ne 'true'
  }
  if ($a['applycontentmarking/header.text']) { $st.ApplyContentMarkingHeaderText = $a['applycontentmarking/header.text'] }
  if ($a['applycontentmarking/footer.text']) { $st.ApplyContentMarkingFooterText = $a['applycontentmarking/footer.text'] }
  if ($a['applywatermarking.text']) { $st.ApplyWaterMarkingText = $a['applywatermarking.text'] }
  return [pscustomobject]$st
}

function Get-BoxLabelWant([hashtable] $Payload, $In) {
  $w = @{}
  foreach ($k in $Payload.Keys) { $w[$k] = $Payload[$k] }
  if ($w.ContainsKey('ContentType')) { $w.ContentType = (([string]$w.ContentType) -split '\s*,\s*' | Sort-Object) -join ',' }
  if ($w.EncryptionRightsDefinitions) {
    # Rights only for domains the tenant owns: never grant a domain the
    # company has not added to this tenant.
    $accepted = @(Get-AcceptedDomain -ErrorAction Stop | ForEach-Object { ([string]$_.DomainName).ToLowerInvariant() })
    $doms = @([string]$In.config.company.primary_domain, [string]$In.tenant.initial_domain) | Where-Object { $_ } | ForEach-Object { $_.ToLowerInvariant() }
    $rights = ([string]$w.EncryptionRightsDefinitions -split ';')[0] -replace '^(\{[^}]+\}|[^:]+):', ''
    $w.EncryptionRightsDefinitions = (($doms | Sort-Object -Unique | Where-Object { $_ -in $accepted }) | ForEach-Object { "${_}:$rights" }) -join ';'
  }
  return $w
}

function Sync-BoxLabel($In, [hashtable] $Payload, [bool] $Apply) {
  $want = Get-BoxLabelWant $Payload $In
  $name = [string]$want.Name
  $labels = Get-BoxLabels
  $parentName = $null
  if ($want.ParentId) { $parentName = ([string]$want.ParentId) -replace '^\{label:(.+)\}$', '$1' }
  $l = $labels[$name]
  if (-not $l) {
    if ($Apply) {
      $p = @{}; foreach ($k in $want.Keys) { if ($k -notin 'Priority', 'ParentId') { $p[$k] = $want[$k] } }
      $p.Tooltip = $script:LabelTooltips[$name]
      if ($parentName) { $p.ParentId = Get-BoxLabelId $parentName }
      $p.ContentType = $want.ContentType -replace ',', ', '
      Invoke-SC 'New-Label' $p | Out-Null
      Get-BoxLabels -Refresh | Out-Null
    }
    return New-BoxChange create "sensitivity label $name"
  }
  $cmp = @{}
  foreach ($k in $want.Keys) { if ($k -notin 'Name', 'Priority', 'ParentId', 'EncryptionProtectionType', 'EncryptionRightsDefinitions', 'EncryptionContentExpiredOnDateInDaysOrNever', 'EncryptionOfflineAccessDays') { $cmp[$k] = $want[$k] } }
  $d = @(Compare-BoxProperties (Get-BoxLabelState $l) $cmp)
  if ($want.EncryptionRightsDefinitions) {
    $a = ConvertFrom-BoxLabelActions $l.LabelActions
    $haveRights = (ConvertTo-BoxComparable (([string]$a['encrypt.rightsdefinitions']) -replace '[\[\]"{}]|Identity:|Rights:', ''))
    foreach ($dom in ([string]$want.EncryptionRightsDefinitions -split ';' | ForEach-Object { ($_ -split ':')[0] })) { if ($haveRights -notmatch [regex]::Escape($dom)) { $d += "EncryptionRightsDefinitions: missing $dom" } }
  }
  if ($d.Count -eq 0) { return }
  if ($Apply) {
    $p = @{ Identity = $l.Guid.ToString() }
    foreach ($k in $want.Keys) { if ($k -notin 'Name', 'Priority', 'ParentId') { $p[$k] = $want[$k] } }
    $p.ContentType = $want.ContentType -replace ',', ', '
    Invoke-SC 'Set-Label' $p | Out-Null
    Get-BoxLabels -Refresh | Out-Null
  }
  return New-BoxChange update "sensitivity label $name" ($d -join '; ')
}

function Get-BoxGroupUpns([string] $Group) { return @(Get-GroupMemberUpns $Group) }

function Sync-BoxLabelPolicy($In, [hashtable] $Want, [bool] $Apply, [switch] $SettingsOnly) {
  $name = [string]$Want.Name
  # Settings (default label, mandatory, justification) and AdvancedSettings
  # (Outlook default) are separate parameters; Get-LabelPolicy reads both
  # back in Settings.
  $set = @{}; $adv = @{}; $all = [ordered]@{}
  foreach ($pair in @(@($Want.Settings, $set), @($Want.AdvancedSettings, $adv))) {
    if (-not $pair[0]) { continue }
    foreach ($k in $pair[0].Keys) { $v = [string]$pair[0][$k]; if ($v -match '^\{label:(.+)\}$') { $v = Get-BoxLabelId $Matches[1] }; $pair[1][$k] = $v; $all[$k] = $v }
  }
  $setParams = @{}
  if ($set.Count) { $setParams.Settings = $set }
  if ($adv.Count) { $setParams.AdvancedSettings = $adv }
  $p = Invoke-SC 'Get-LabelPolicy' | Where-Object { $_.Name -eq $name }
  if ($SettingsOnly) {
    if (-not $p) { throw "label policy '$name' does not exist yet: run policy.purview first" }
  } elseif (-not $p) {
    if ($Apply) {
      $loc = Get-BoxLabelPolicyLocation $In $Want
      $labels = @($Want.Labels | Where-Object { [string]$_ -notin $script:PolicyCtx.skippedLabels } | ForEach-Object { Get-BoxLabelId ([string]$_) } | Where-Object { $_ })
      Invoke-SC 'New-LabelPolicy' (@{ Name = $name; Labels = $labels; ExchangeLocation = $loc } + $setParams) | Out-Null
    }
    return New-BoxChange create "label policy '$name'" "published to $(if ([string]$Want.ExchangeLocation -eq 'All') { 'all users' } else { 'the pilot group' })"
  }
  $changes = @()
  $have = ConvertFrom-BoxSettingList $p.Settings
  $diff = @(foreach ($k in $all.Keys) { if ((ConvertTo-BoxComparable $have[$k.ToLowerInvariant()]) -ne (ConvertTo-BoxComparable $all[$k])) { "${k}: $($have[$k.ToLowerInvariant()]) -> $($all[$k])" } })
  if ($diff.Count) {
    if ($Apply) { Invoke-SC 'Set-LabelPolicy' (@{ Identity = $p.Guid.ToString() } + $setParams) | Out-Null }
    $changes += New-BoxChange update "label policy '$name' settings" ($diff -join '; ')
  }
  if ($SettingsOnly) { return $changes }
  $Want.Labels = @($Want.Labels | Where-Object { [string]$_ -notin $script:PolicyCtx.skippedLabels })
  $haveLabels = @($p.Labels | ForEach-Object { [string]$_ })
  $byName = @{}; foreach ($l in (Get-BoxLabels).Values) { $byName[[string]$l.Name] = $l }
  $missing = @($Want.Labels | Where-Object { $l = $byName[[string]$_]; -not $l -or -not (@($haveLabels) | Where-Object { $_ -in @([string]$l.Name, [string]$l.Guid, [string]$l.ImmutableId) }) })
  if ($missing.Count) {
    if ($Apply) { Invoke-SC 'Set-LabelPolicy' @{ Identity = $p.Guid.ToString(); AddLabels = @($missing | ForEach-Object { Get-BoxLabelId ([string]$_) } | Where-Object { $_ }) } | Out-Null }
    $changes += New-BoxChange update "label policy '$name' labels" "add $($missing -join ', ')"
  }
  $wantLoc = @(Get-BoxLabelPolicyLocation $In $Want | ForEach-Object { ([string]$_).ToLowerInvariant() })
  $haveLoc = @($p.ExchangeLocation | ForEach-Object { $x = $_; $n = if ($x.PrimarySmtpAddress) { $x.PrimarySmtpAddress } elseif ($x.Name) { $x.Name } else { $x }; ([string]$n).ToLowerInvariant() })
  $add = @($wantLoc | Where-Object { $_ -notin $haveLoc }); $remove = @($haveLoc | Where-Object { $_ -notin $wantLoc })
  if ($add.Count -or $remove.Count) {
    if ($Apply) {
      $q = @{ Identity = $p.Guid.ToString() }
      if ($remove.Count) { $q.RemoveExchangeLocation = $remove }
      if ($add.Count) { $q.AddExchangeLocation = $add }
      Invoke-SC 'Set-LabelPolicy' $q | Out-Null
    }
    $changes += New-BoxChange update "label policy '$name' scope" "add $($add.Count), remove $($remove.Count)"
  }
  return $changes
}

# The label policy's user scope: All in enforce, the pilot group's members
# in audit (label policies have no audit mode; Exchange takes users or
# mail-enabled groups, so the access group is expanded on every apply).
function Get-BoxLabelPolicyLocation($In, [hashtable] $Want) {
  $loc = [string]$Want.ExchangeLocation
  if ($loc -eq 'All') { return @('All') }
  $g = [string]$In.policy.rollout.pilot_group
  if (-not $g) { throw 'policy.rollout.pilot_group is empty' }
  $u = @(Get-BoxGroupUpns $g)
  if ($u.Count -eq 0) { throw "pilot group $g has no members" }
  return $u
}

# ---- data loss prevention ----

function Get-BoxSiteUrls($In, [object[]] $Names) {
  $have = @($In.config.structure.sites | ForEach-Object { [string]$_.url })
  return @($Names | ForEach-Object { ([string]$_) -replace '^\{site:(.+)\}$', '$1' } | Where-Object { $_ -in $have } | ForEach-Object { "$($In.tenant.sharepoint)/sites/$_" })
}

# Replaces placeholders in a DLP rule payload; returns $null for a rule
# that has nothing to match (for example no types for the jurisdictions).
function Resolve-BoxDlpRule($In, [hashtable] $Rule, [bool] $Apply) {
  $r = @{}
  foreach ($k in $Rule.Keys) { $r[$k] = $Rule[$k] }
  foreach ($k in 'GenerateAlert', 'GenerateIncidentReport') {
    if ($r[$k]) { $r[$k] = @(@($r[$k]) | ForEach-Object { ([string]$_) -replace '^\{mailbox:(.+)\}$', ('$1@' + (Get-Domain $In)) }) }
  }
  if ($r.NotifyPolicyTipCustomText -eq '<engine text>') { $r.NotifyPolicyTipCustomText = 'This looks like financial account data. Check the recipients before you share it outside the company.' }
  if ($r.ContainsKey('NotifyAllowOverride') -and $null -eq $r.NotifyAllowOverride) { $r.Remove('NotifyAllowOverride') }
  # S&C: an override needs NotifyUser; per-user blocking needs an override,
  # so a rule without one blocks everyone outside.
  if ($r.NotifyAllowOverride -and -not $r.NotifyUser) { $r.NotifyUser = @('LastModifier', 'Owner') }
  if ($r.BlockAccessScope -eq 'PerUser' -and -not $r.NotifyAllowOverride) { $r.BlockAccessScope = 'All' }
  # Every rule needs a content condition; a site-scoped rule matches every file.
  # Every rule needs a content condition. For a site-scoped rule meant to
  # match every file there is none on SharePoint: DocumentNameMatchesPatterns
  # is Exchange-only and DocumentSizeOver starts at 10 KB (tested). Such a
  # rule is left out until a condition is decided.
  $conditions = 'ContentContainsSensitiveInformation', 'RecipientDomainIs', 'From', 'AnyOfRecipientAddressMatchesPatterns', 'DocumentSizeOver', 'DocumentNameMatchesPatterns'
  if (-not @($conditions | Where-Object { $r.ContainsKey($_) }).Count) {
    $script:PolicyCtx.skipReason = 'a SharePoint DLP rule needs a content condition and none matches every file (DocumentNameMatchesPatterns is Exchange-only; DocumentSizeOver starts at 10 KB): decision needed'
    return $null
  }
  $c = $r.ContentContainsSensitiveInformation
  if ($c) {
    foreach ($grp in @($c.groups)) {
      if ($grp.labels) {
        $grp.labels = @($grp.labels | Where-Object { (([string]$_.name) -replace '^\{label:(.+)\}$', '$1') -notin $script:PolicyCtx.skippedLabels } | ForEach-Object { $n = ([string]$_.name) -replace '^\{label:(.+)\}$', '$1'; $id = Get-BoxLabelId $n; if (-not $id) { if ($Apply) { throw "label $n does not exist yet" }; $id = "(label $n, created by this step)" }; @{ name = $id; type = 'Sensitivity' } })
      }
      if ($grp.sensitivetypes) {
        $grp.sensitivetypes = @(foreach ($t in @($grp.sensitivetypes)) {
            $n = [string]$t.name
            $names = switch -regex ($n) {
              '^\{jurisdictions:bank_account_sits\}$' { Get-BoxJurisdictionTypes $In 'bank' }
              '^\{jurisdictions:personal_data_sits\}$' { Get-BoxJurisdictionTypes $In 'personal' }
              '^\{sit:' { @() }
              '^All credential types$' { $known = Get-BoxTenantTypes; @($script:CredentialTypes | Where-Object { $null -eq $known -or $_ -in $known }) }
              default { $known = Get-BoxTenantTypes; if ($null -eq $known -or $n -in $known) { @($n) } else { @() } }
            }
            foreach ($x in $names) { $o = @{}; foreach ($k in $t.Keys) { $o[$k] = $t[$k] }; $o.name = $x; $o }
          })
        if ($grp.sensitivetypes.Count -eq 0) { $script:PolicyCtx.skipReason = 'no sensitive information types for the configured jurisdictions'; return $null }
      }
    }
  }
  return $r
}

# Rule properties compared after apply (conditions are compared by the
# names of the types and labels they match).
$script:DlpRuleProps = @('AccessScope', 'BlockAccess', 'BlockAccessScope', 'NotifyUser', 'NotifyAllowOverride', 'GenerateAlert', 'GenerateIncidentReport', 'IncidentReportContent', 'ReportSeverityLevel', 'RecipientDomainIs', 'From', 'AnyOfRecipientAddressMatchesPatterns')

function Get-BoxKey($h, [string] $Key) {
  if ($null -eq $h) { return $null }
  if ($h -is [System.Collections.IDictionary]) { foreach ($k in $h.Keys) { if ([string]$k -ieq $Key) { return $h[$k] } }; return $null }
  return $h.$Key
}

# The types and labels a DLP condition matches, with their counts, whether
# it is written as groups (as the payload is) or flat (as the tenant may
# return it). A missing count and -1 both mean "any".
function Get-BoxDlpConditionKey($c) {
  if (-not $c) { return '' }
  $names = [System.Collections.Generic.List[string]]::new()
  $walk = {
    param($o)
    foreach ($x in @($o)) {
      if ($null -eq $x) { continue }
      $g = Get-BoxKey $x 'groups'; if ($g) { & $walk $g; continue }
      $st = Get-BoxKey $x 'sensitivetypes'; $lb = Get-BoxKey $x 'labels'
      if ($st -or $lb) { if ($st) { & $walk $st }; foreach ($l in @($lb)) { if ($l) { $names.Add("label:$(Get-BoxKey $l 'name')") } }; continue }
      $n = Get-BoxKey $x 'name'
      if ($n) {
        $min = [string](Get-BoxKey $x 'mincount'); if (-not $min) { $min = '1' }
        $max = [string](Get-BoxKey $x 'maxcount'); if ($max -eq '-1') { $max = '' }
        $names.Add("sit:${n}:${min}:${max}")
      }
    }
  }
  & $walk (ConvertTo-BoxHash $c)
  return ConvertTo-BoxComparable @($names)
}

function Sync-BoxDlpPolicy($In, $s, [bool] $Apply, [object[]] $Rules) {
  $pol = Get-Part $s 'New-DlpCompliancePolicy'
  $name = [string]$pol.Name
  # The rules travel in both forms of the payload; only the policy's Mode differs.
  $rules = if ($Rules) { $Rules } else { @((ConvertTo-BoxHash $s.payload).'New-DlpComplianceRule' | Where-Object { $_ }) }
  if ($rules.Count -eq 0) { throw "no DLP rules in the payload of $($s.id)" }
  # PVW-020: every rule reports to the security mailbox and raises one alert per match.
  if ('PVW-020' -in $script:PolicyCtx.ids) {
    $sec = Get-Mailbox-Address $In 'security'
    foreach ($r in $rules) {
      $r.GenerateIncidentReport = '{mailbox:security}'; $r.IncidentReportContent = 'All'
      $r.GenerateAlert = @(@($r.GenerateAlert) + '{mailbox:security}' | Where-Object { $_ } | Sort-Object -Unique)
      $r.AlertProperties = @{ AggregationType = 'None' }
    }
  }
  $resolved = @(foreach ($r in $rules) { Resolve-BoxDlpRule $In $r $Apply })
  if (@($resolved | Where-Object { $_ }).Count -eq 0) {
    $why = if ($script:PolicyCtx.skipReason) { $script:PolicyCtx.skipReason } else { 'no sensitive information types for the configured jurisdictions' }
    $script:PolicyCtx.skipReason = $null
    return New-BoxNote $s.id "not applied: $why"
  }
  foreach ($k in 'SharePointLocation') { if ($pol[$k] -is [System.Collections.IList]) { $pol[$k] = Get-BoxSiteUrls $In $pol[$k] } }
  $out = @()
  $p = Invoke-SC 'Get-DlpCompliancePolicy' | Where-Object { $_.Name -eq $name }
  if (-not $p) {
    if ($Apply) { Invoke-SC 'New-DlpCompliancePolicy' $pol | Out-Null }
    $out += New-BoxChange create "DLP policy '$name'" "mode $($pol.Mode)"
  } else {
    if ([string]$p.Mode -ne [string]$pol.Mode) {
      if ($Apply) { Invoke-SC 'Set-DlpCompliancePolicy' @{ Identity = $p.Guid.ToString(); Mode = $pol.Mode } | Out-Null }
      $out += New-BoxChange update "DLP policy '$name'" "mode $($p.Mode) -> $($pol.Mode)"
    }
    foreach ($k in 'ExchangeLocation', 'SharePointLocation', 'OneDriveLocation') {
      if (-not $pol.ContainsKey($k)) { continue }
      $want = @(@($pol[$k]) | ForEach-Object { ([string]$_).ToLowerInvariant().TrimEnd('/') })
      $have = @(@($p.$k) | ForEach-Object { $x = $_; $n = if ($x.Name) { $x.Name } else { $x }; ([string]$n).ToLowerInvariant().TrimEnd('/') })
      $add = @($want | Where-Object { $_ -notin $have })
      if ($add.Count) {
        if ($Apply) { Invoke-SC 'Set-DlpCompliancePolicy' @{ Identity = $p.Guid.ToString(); "Add$k" = @($pol[$k] | Where-Object { ([string]$_).ToLowerInvariant().TrimEnd('/') -in $add }) } | Out-Null }
        $out += New-BoxChange update "DLP policy '$name'" "add $k $($add -join ', ')"
      }
    }
  }
  $haveRules = @{}
  if ($p) { foreach ($x in @(Invoke-SC 'Get-DlpComplianceRule' @{ Policy = $p.Guid.ToString() })) { $haveRules[[string]$x.Name] = $x } }
  foreach ($r in $resolved) {
    if (-not $r) { continue }
    $rn = [string]$r.Name
    $x = $haveRules[$rn]
    if (-not $x) {
      if ($Apply) { $q = @{}; foreach ($k in $r.Keys) { $q[$k] = $r[$k] }; $q.Policy = $name; Invoke-SC 'New-DlpComplianceRule' $q | Out-Null }
      $out += New-BoxChange create "DLP rule '$rn'"
      continue
    }
    $cmp = @{}; foreach ($k in $script:DlpRuleProps) { if ($r.ContainsKey($k)) { $cmp[$k] = $r[$k] } }
    $d = @(Compare-BoxProperties $x $cmp)
    if ($r.ContentContainsSensitiveInformation -and (Get-BoxDlpConditionKey $x.ContentContainsSensitiveInformation) -ne (Get-BoxDlpConditionKey $r.ContentContainsSensitiveInformation)) { $d += 'ContentContainsSensitiveInformation' }
    if ($x.Disabled) { $d += 'Disabled: true -> false' }
    if ($d.Count) {
      if ($Apply) { $q = @{ Identity = $x.Guid.ToString(); Disabled = $false }; foreach ($k in $r.Keys) { if ($k -ne 'Name') { $q[$k] = $r[$k] } }; Invoke-SC 'Set-DlpComplianceRule' $q | Out-Null }
      $out += New-BoxChange update "DLP rule '$rn'" ($d -join '; ')
    }
  }
  return $out
}

function Sync-BoxConsumerDlp($In, $s, [bool] $Apply) {
  $doms = @($In.config.policy.consumer_mail_domains | Where-Object { $_ })
  if ($doms.Count -eq 0) { return New-BoxNote $s.id 'skipped: policy.consumer_mail_domains is empty' }
  if ($s.id -eq 'PVW-018') {
    $users = @($In.config.users | Where-Object { $_.given_name -and $_.family_name })
    $dom = Get-Domain $In
    $rules = @(foreach ($u in $users) {
        @{ Name = "DLP-09 $($u.id)"; From = @("$($u.primary)@$dom"); AnyOfRecipientAddressMatchesPatterns = @(Get-BoxOwnAddressPattern ([string]$u.given_name) ([string]$u.family_name) $doms)
          BlockAccess = $true; NotifyAllowOverride = 'WithJustification'; GenerateAlert = @('{mailbox:alerts}'); GenerateIncidentReport = @('{mailbox:security}') }
      })
  } else {
    $rules = @(@{ Name = 'DLP-08 justify'; RecipientDomainIs = $doms; DocumentSizeOver = 1; BlockAccess = $true; NotifyUser = @('LastModifier'); NotifyAllowOverride = 'WithJustification'; GenerateIncidentReport = @('{mailbox:security}'); IncidentReportContent = 'All' })
  }
  return Sync-BoxDlpPolicy $In $s $Apply $rules
}

function Sync-BoxAlert($In, $s, [bool] $Apply) {
  $want = Get-Part $s 'New-ProtectionAlert'
  $name = [string]$want.Name
  $want.NotifyUser = @(Get-Mailbox-Address $In 'alerts')
  $out = @()
  if (@($want.Operation).Count -gt 1) { $want.LogicalOperationName = ($name -replace '[^A-Za-z0-9]', '') }
  if ($want.ContainsKey('Filter')) { $want.Remove('Filter'); $out += New-BoxNote $s.id 'the alert fires on every label downgrade or removal: a filter on the old label is not set (syntax unconfirmed)' }
  $a = Invoke-SC 'Get-ProtectionAlert' | Where-Object { $_.Name -eq $name }
  if (-not $a) {
    if ($Apply) { Invoke-SC 'New-ProtectionAlert' $want | Out-Null }
    $out += New-BoxChange create "alert policy '$name'"
    return $out
  }
  $cmp = @{ Operation = $want.Operation; NotifyUser = $want.NotifyUser; AggregationType = $want.AggregationType; Severity = $want.Severity; Disabled = $false }
  $d = @(Compare-BoxProperties $a $cmp)
  if ($d.Count) {
    if ($Apply) { Invoke-SC 'Set-ProtectionAlert' @{ Identity = $a.Identity; Operation = $want.Operation; NotifyUser = $want.NotifyUser; AggregationType = $want.AggregationType; Severity = $want.Severity; Disabled = $false } | Out-Null }
    $out += New-BoxChange update "alert policy '$name'" ($d -join '; ')
  }
  return $out
}

$script:PurviewHandlers = @{
  'PVW-001' = { param($In, $s, $Apply)
    $want = Get-Part $s 'Set-AdminAuditLogConfig'
    $cur = Get-AdminAuditLogConfig -ErrorAction Stop
    try { Connect-BoxCompliance $In; $cur = Invoke-SC 'Get-AdminAuditLogConfig' } catch { New-BoxNote $s.id "read from Exchange Online (Security & Compliance not available: $($_.Exception.Message))" }
    Sync-BoxSetting 'unified audit log' $cur $want { param($d) Set-AdminAuditLogConfig @d -ErrorAction Stop } $Apply
  }
  'PVW-003' = { param($In, $s, $Apply) Connect-BoxCompliance $In; Sync-BoxLabel $In (Get-Part $s 'New-Label') $Apply }
  'PVW-004' = { param($In, $s, $Apply) Connect-BoxCompliance $In; Sync-BoxLabel $In (Get-Part $s 'New-Label') $Apply }
  'PVW-005' = { param($In, $s, $Apply)
    Connect-BoxCompliance $In
    Sync-BoxLabel $In (Get-Part $s 'New-Label(parent)') $Apply
    $parent = (Get-BoxLabels)[[string](Get-Part $s 'New-Label(parent)').Name]
    $subs = @((ConvertTo-BoxHash $s.payload).'New-Label(sub-labels)')
    if ($parent -and -not $parent.IsLabelGroup) {
      # The tenant uses the modern label scheme: sub-labels sit only under a
      # label group, which carries no marking and cannot label sites. The
      # setting's Confidential label with a header and site protection
      # cannot also be the sub-labels' parent; decision recorded in the report.
      foreach ($sub in $subs) { $script:PolicyCtx.skippedLabels += [string]$sub.Name }
      return New-BoxNote $s.id "not applied: sub-labels $(($subs | ForEach-Object { $_.Name }) -join ', ') need a label group parent in the modern label scheme; Confidential is a label with markings (decision needed)"
    }
    foreach ($sub in $subs) { Sync-BoxLabel $In $sub $Apply }
  }
  'PVW-006' = { param($In, $s, $Apply) Connect-BoxCompliance $In; Sync-BoxLabel $In (Get-Part $s 'New-Label') $Apply }
  'PVW-007' = { param($In, $s, $Apply) Connect-BoxCompliance $In; Sync-BoxLabelPolicy $In (Get-Part $s 'New-LabelPolicy') $Apply }
  'PVW-008' = { param($In, $s, $Apply)
    Connect-BoxCompliance $In
    $pl = ConvertTo-BoxHash $s.payload
    foreach ($n in 'Internal', 'Confidential', 'Restricted') {
      $want = $pl["Set-Label($n)"]
      $l = (Get-BoxLabels)[$n]
      if (-not $l) { if ($Apply) { throw "label $n does not exist" }; New-BoxChange update "sensitivity label $n" 'site and group protection (after the label is created)'; continue }
      $set = ConvertFrom-BoxSettingList $l.Settings
      $a = ConvertFrom-BoxLabelActions $l.LabelActions
      $have = [pscustomobject]@{
        SiteAndGroupProtectionEnabled = $a.ContainsKey('protectgroup') -or $a.ContainsKey('protectsite')
        SiteAndGroupProtectionPrivacy = $a['protectgroup.privacy']
        SiteAndGroupProtectionAllowAccessToGuestUsers = $a['protectgroup.allowaccesstoguestusers']
        SiteExternalSharingControlType = $a['protectsite.externalsharingcontroltype']
        SiteAndGroupProtectionBlockAccess = $a['protectsite.blockaccess']
      }
      $d = @(Compare-BoxProperties $have $want)
      if ($d.Count) {
        if ($Apply) { $p = @{ Identity = $l.Guid.ToString() }; foreach ($k in $want.Keys) { $p[$k] = $want[$k] }; Invoke-SC 'Set-Label' $p | Out-Null }
        New-BoxChange update "sensitivity label $n" ("site and group protection: " + ($d -join '; '))
      }
      $null = $set
    }
    Get-BoxLabels -Refresh | Out-Null
    New-BoxNote $s.id 'not applied here: Entra Group.Unified EnableMIPLabels (needs Directory.ReadWrite.All, not granted) and SharePoint EnableAIPIntegration (sharepoint provider)'
  }
  'PVW-009' = { param($In, $s, $Apply)
    Sync-BoxSetting 'IRM configuration' (Get-IRMConfiguration -ErrorAction Stop) (Get-Part $s 'Set-IRMConfiguration') { param($d) Set-IRMConfiguration @d -ErrorAction Stop } $Apply
  }
  'PVW-020' = { param($In, $s, $Apply) New-BoxNote $s.id 'applied with each DLP rule (incident report to the security mailbox; one alert per match)' }
}
foreach ($id in 'PVW-010', 'PVW-011', 'PVW-012', 'PVW-013', 'PVW-014', 'PVW-015', 'PVW-016') {
  $script:PurviewHandlers[$id] = { param($In, $s, $Apply) Connect-BoxCompliance $In; Sync-BoxDlpPolicy $In $s $Apply }
}
foreach ($id in 'PVW-017', 'PVW-018') {
  $script:PurviewHandlers[$id] = { param($In, $s, $Apply) Connect-BoxCompliance $In; Sync-BoxConsumerDlp $In $s $Apply }
}
$script:PurviewHandlers['PVW-019'] = { param($In, $s, $Apply)
  $terms = @($In.dictionary.company.marker_terms | Where-Object { $_ })
  if ($terms.Count -eq 0) { return New-BoxNote $s.id 'skipped: Dictionary company.marker_terms is empty' }
  New-BoxNote $s.id 'skipped: the marker-terms keyword type (New-DlpKeywordDictionary and a rule package) is not built yet'
}
foreach ($id in 'PVW-021', 'PVW-022', 'PVW-023', 'PVW-024', 'PVW-025', 'PVW-026') {
  $script:PurviewHandlers[$id] = { param($In, $s, $Apply) Connect-BoxCompliance $In; Sync-BoxAlert $In $s $Apply }
}

$script:AppsHandlers = @{
  'OFC-009' = { param($In, $s, $Apply) Sync-AddinRoles @($s.payload.remove_roles_from_default_role_assignment_policy) $Apply }
  'OFC-013' = { param($In, $s, $Apply)
    Connect-BoxCompliance $In
    $w = Get-Part $s 'Set-LabelPolicy'
    Sync-BoxLabelPolicy $In @{ Name = $w.Identity; Settings = $w.Settings } $Apply -SettingsOnly
  }
}

# The handler table of each policy step.
$script:PolicySteps = @{
  'policy.exchange' = $script:ExchangeHandlers
  'policy.defender' = $script:DefenderHandlers
  'policy.purview' = $script:PurviewHandlers
  'policy.m365-admin.exchange' = $script:AdminHandlers
  'policy.m365-apps.exchange' = $script:AppsHandlers
}
function Invoke-BoxPolicyOp([string] $Step, [string] $Op, $In) {
  $h = $script:PolicySteps[$Step]
  switch ($Op) {
    'plan' { return Result-Policy (Invoke-BoxPolicy $In $h) }
    'apply' { return Result-Policy (Invoke-BoxPolicy $In $h -Apply) }
    'verify' { return Verify-Policy (Invoke-BoxPolicy $In $h) }
    'evidence' { return Evidence-Policy (Invoke-BoxPolicy $In $h) $Step }
  }
}
