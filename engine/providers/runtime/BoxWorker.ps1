<#
.SYNOPSIS
  Provider worker: one pwsh process per provider, driven by the Go
  orchestrator over JSON lines (SPEC §3, spike S-05).

.DESCRIPTION
  Protocol. The orchestrator writes one JSON request per line on stdin:
    {"id":"r1","op":"hello"}
    {"id":"r2","op":"auth","data":{"graph":{"access_token":"..."}}}
    {"id":"r3","op":"invoke","data":{"step":"x","operation":"plan","input":{...}}}
    {"id":"r5","op":"steps"}            (step ids the provider implements)
    {"id":"r4","op":"shutdown"}
  The worker answers with lines that start with the marker U+001E "BOX:"
  followed by one JSON event:
    {"id":"r3","ev":"progress","msg":"...","done":1,"of":3}
    {"id":"r3","ev":"log","level":"warning","msg":"..."}
    {"id":"r3","ev":"result","data":{...}}
    {"id":"r3","ev":"error","error":{"type":"...","message":"..."}}
  Lines without the marker are stray console output from a module; the
  orchestrator records them as raw log lines.

  Credentials arrive only in an "auth" request on stdin. They are kept in
  memory, never put in arguments, environment variables or files, and every
  value is redacted from log, progress and error text.
#>
param(
  [Parameter(Mandatory)] [string] $Provider,     # path to the provider module (.psm1 or .psd1)
  [string[]] $Modules = @()                      # Microsoft modules to import at start
)

Set-StrictMode -Version 3.0
# -File passes "-Modules a,b" as one string; accept both forms.
$Modules = @($Modules | ForEach-Object { $_ -split ',' } | Where-Object { $_ })
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$InformationPreference = 'Continue'

$script:Marker = [char]0x1E + 'BOX:'
$script:Out = [Console]::Out
$script:Secrets = [System.Collections.Generic.List[string]]::new()
$script:BoxAuth = @{}
$script:CurrentId = $null

# Read-only mode (box audit, SPEC §26): set by the orchestrator through
# BOX_READ_ONLY=1, never by a request. Only test, verify and evidence run;
# in the provider module, every module cmdlet whose verb could change the
# tenant is replaced by a function that refuses, and Graph and REST calls
# other than GET are refused (common/BoxGraph.psm1 and the REST wrappers
# below). Handlers reach the tenant only through cmdlets and REST, so a
# verify path that would write fails instead of writing.
$script:ReadOnly = ($env:BOX_READ_ONLY -eq '1')
$script:ReadOnlyOperations = @('test', 'verify', 'evidence')
# Verbs that only read. 'Test' is left out: Exchange's Test- cmdlets send mail.
$script:ReadVerbs = @('Get', 'Connect', 'Disconnect', 'Find', 'Search', 'Select', 'Measure', 'Compare', 'Format', 'ConvertTo', 'ConvertFrom', 'Resolve', 'Read')

# Command names the provider may call that could write: every command the
# Microsoft modules export with another verb, and every such name the
# provider's own files call (Exchange Online's cmdlets only exist after
# Connect-ExchangeOnline). PowerShell's own cmdlets and the provider's own
# functions are not included.
function Get-BoxWriteCommandNames([string] $ProviderPath, [string[]] $MicrosoftModules) {
  $names = [System.Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
  $isWrite = { param([string] $n) $v, $rest = $n -split '-', 2; $rest -and ($v -notin $script:ReadVerbs) }
  foreach ($m in $MicrosoftModules) {
    foreach ($mod in @(Get-Module -Name $m)) {
      foreach ($n in $mod.ExportedCommands.Keys) { if (& $isWrite $n) { [void]$names.Add($n) } }
    }
  }
  $dir = Split-Path -Parent $ProviderPath
  $common = Join-Path $dir '..' 'common'
  $files = @(Get-ChildItem -Path (Join-Path $dir '*.psm1'), (Join-Path $dir '*.ps1'), (Join-Path $common '*.psm1') -File -ErrorAction SilentlyContinue |
      Where-Object { $_.Name -notlike '*.Tests.ps1' })
  $own = [System.Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
  $called = @()
  foreach ($f in $files) {
    $ast = [System.Management.Automation.Language.Parser]::ParseFile($f.FullName, [ref]$null, [ref]$null)
    foreach ($fd in $ast.FindAll({ $args[0] -is [System.Management.Automation.Language.FunctionDefinitionAst] }, $true)) { [void]$own.Add($fd.Name) }
    $called += @($ast.FindAll({ $args[0] -is [System.Management.Automation.Language.CommandAst] }, $true) | ForEach-Object { $_.GetCommandName() } | Where-Object { $_ })
  }
  foreach ($n in $called) {
    if ($own.Contains($n) -or -not (& $isWrite $n)) { continue }
    $c = Get-Command -Name $n -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($c -and ([string]$c.ModuleName -like 'Microsoft.PowerShell.*' -or $c.CommandType -eq 'Alias')) { continue }
    [void]$names.Add($n)
  }
  return @($names)
}

# Defines the refusing functions in the provider module's scope, where they
# take precedence over the cmdlets for every call the provider makes,
# including '& $name' with a name built at run time.
function Install-BoxReadOnlyGuard($ProviderModule, [string[]] $Names) {
  & $ProviderModule {
    param([string[]] $Names)
    foreach ($n in $Names) {
      Set-Item -Path "function:script:$n" -Value ([scriptblock]::Create("throw 'read-only (audit): $n was refused; a verify must only read'"))
    }
    # REST calls keep every parameter of the cmdlet; only GET and HEAD pass.
    foreach ($n in @('Invoke-RestMethod', 'Invoke-WebRequest')) {
      $meta = [System.Management.Automation.CommandMetadata]::new((Get-Command "Microsoft.PowerShell.Utility\$n"))
      $body = [System.Management.Automation.ProxyCommand]::GetCmdletBindingAttribute($meta) +
        "`nparam(" + [System.Management.Automation.ProxyCommand]::GetParamBlock($meta) + ")`n" +
        "`$m = if (`$PSBoundParameters.ContainsKey('Method')) { [string]`$PSBoundParameters['Method'] } else { 'Get' }`n" +
        "if (`$m -notin @('Get', 'Head', 'Default')) { throw `"read-only (audit): $n -Method `$m was refused`" }`n" +
        "Microsoft.PowerShell.Utility\$n @PSBoundParameters"
      Set-Item -Path "function:script:$n" -Value ([scriptblock]::Create($body))
    }
  } $Names
}

function Hide-BoxSecret([string] $Text) {
  if ($null -eq $Text) { return $Text }
  foreach ($s in $script:Secrets) {
    if ($s.Length -ge 6) { $Text = $Text.Replace($s, '[redacted]') }
  }
  return $Text
}

function Write-BoxEvent([hashtable] $Event) {
  foreach ($k in @('msg')) {
    if ($Event.ContainsKey($k)) { $Event[$k] = Hide-BoxSecret ([string]$Event[$k]) }
  }
  $json = ConvertTo-Json -InputObject $Event -Depth 32 -Compress
  $script:Out.WriteLine($script:Marker + (Hide-BoxSecret $json))
  $script:Out.Flush()
}

# Available to provider modules.
function global:Send-BoxProgress {
  param([Parameter(Mandatory)][string] $Message, [int] $Done = 0, [int] $Of = 0)
  Write-BoxEvent @{ id = $script:CurrentId; ev = 'progress'; msg = $Message; done = $Done; of = $Of }
}

function global:Get-BoxAuth {
  param([Parameter(Mandatory)][string] $Name)
  if (-not $script:BoxAuth.ContainsKey($Name)) { throw "no credential '$Name' was provided to this worker" }
  return $script:BoxAuth[$Name]
}

function Add-BoxSecretValues($Value) {
  if ($Value -is [string]) { if ($Value.Length -ge 6) { $script:Secrets.Add($Value) } ; return }
  if ($Value -is [System.Collections.IDictionary]) { foreach ($v in $Value.Values) { Add-BoxSecretValues $v } ; return }
  if ($Value -is [System.Collections.IEnumerable]) { foreach ($v in $Value) { Add-BoxSecretValues $v } }
}

function Write-BoxRecord($Record) {
  $level = switch ($Record.GetType().Name) {
    'WarningRecord'     { 'warning' }
    'VerboseRecord'     { 'verbose' }
    'DebugRecord'       { 'debug' }
    'ErrorRecord'       { 'error' }
    'InformationRecord' { 'info' }
    default             { $null }
  }
  if ($null -eq $level) { return $false }
  $text = if ($Record -is [System.Management.Automation.InformationRecord]) { [string]$Record.MessageData } else { [string]$Record }
  Write-BoxEvent @{ id = $script:CurrentId; ev = 'log'; level = $level; msg = $text }
  return $true
}

function Invoke-BoxRequest($Request) {
  switch ($Request.op) {
    'hello' {
      $mods = @(Get-Module | Where-Object { $_.Name -notlike 'Microsoft.PowerShell.*' } |
        ForEach-Object { @{ name = $_.Name; version = $_.Version.ToString() } })
      return @{ pid = $PID; pwsh = $PSVersionTable.PSVersion.ToString(); provider = $script:ProviderName; modules = $mods }
    }
    'auth' {
      $stored = @()
      foreach ($p in $Request.data.PSObject.Properties) {
        $script:BoxAuth[$p.Name] = $p.Value
        Add-BoxSecretValues ($p.Value | ConvertTo-Json -Depth 8 | ConvertFrom-Json -AsHashtable)
        $stored += $p.Name
      }
      return @{ stored = $stored }
    }
    'steps' {
      $cmd = Get-Command Get-BoxStepIds -Module $script:ProviderName -ErrorAction SilentlyContinue
      if (-not $cmd) { return @() }
      return @(& $cmd)
    }
    'invoke' {
      $d = $Request.data
      if ($script:ReadOnly -and [string]$d.operation -notin $script:ReadOnlyOperations) {
        throw "read-only (audit): operation '$($d.operation)' was refused; only test, verify and evidence run"
      }
      $stepInput = if ($d.PSObject.Properties['input']) { $d.input } else { $null }
      $results = [System.Collections.Generic.List[object]]::new()
      & { Invoke-BoxStep -Step $d.step -Operation $d.operation -InputObject $stepInput } *>&1 | ForEach-Object {
        if (-not (Write-BoxRecord $_)) { $results.Add($_) }
      }
      if ($results.Count -eq 0) { return $null }
      if ($results.Count -eq 1) { return $results[0] }
      return $results.ToArray()
    }
    default { throw "unknown op '$($Request.op)'" }
  }
}

# Start-up: import requested Microsoft modules and the provider module.
try {
  foreach ($m in $Modules) { Import-Module $m -ErrorAction Stop *>&1 | Out-Null }
  $mod = Import-Module $Provider -PassThru -Force -ErrorAction Stop
  $script:ProviderName = $mod.Name
  if (-not (Get-Command Invoke-BoxStep -Module $mod.Name -ErrorAction SilentlyContinue)) {
    throw "provider module $($mod.Name) does not export Invoke-BoxStep"
  }
  if ($script:ReadOnly) {
    Install-BoxReadOnlyGuard $mod (Get-BoxWriteCommandNames $mod.Path $Modules)
  }
} catch {
  Write-BoxEvent @{ id = 'start'; ev = 'error'; error = @{ type = $_.Exception.GetType().FullName; message = $_.Exception.Message } }
  exit 2
}
Write-BoxEvent @{ id = 'start'; ev = 'ready'; data = @{ pid = $PID } }

while ($true) {
  $line = [Console]::In.ReadLine()
  if ($null -eq $line) { break }
  if ([string]::IsNullOrWhiteSpace($line)) { continue }
  try {
    $req = $line | ConvertFrom-Json
  } catch {
    Write-BoxEvent @{ id = ''; ev = 'error'; error = @{ type = 'protocol'; message = 'request is not JSON' } }
    continue
  }
  $script:CurrentId = [string]$req.id
  if ($req.op -eq 'shutdown') {
    Write-BoxEvent @{ id = $script:CurrentId; ev = 'result'; data = @{ bye = $true } }
    break
  }
  try {
    $data = Invoke-BoxRequest $req
    Write-BoxEvent @{ id = $script:CurrentId; ev = 'result'; data = $data }
  } catch {
    $where = ''
    $ii = $_.InvocationInfo
    if ($ii -and $ii.ScriptName) { $where = " (at $([IO.Path]::GetFileName($ii.ScriptName)):$($ii.ScriptLineNumber) $($ii.MyCommand))" }
    Write-BoxEvent @{ id = $script:CurrentId; ev = 'error'; error = @{ type = $_.Exception.GetType().FullName; message = (Hide-BoxSecret ($_.Exception.Message + $where)) } }
  }
}
exit 0
