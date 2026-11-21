# Stands in for MicrosoftTeams in the audit-mode test: reads return a tenant
# the box did not build; every Set- cmdlet would write. Not shipped.
function Connect-MicrosoftTeams { param([string[]] $AccessTokens) if ($AccessTokens.Count -ne 2) { throw 'two tokens expected' } }
function Get-CsTeamsMeetingPolicy { param($Identity) [pscustomobject]@{ Identity = 'Global'; AutoAdmittedUsers = 'Everyone'; AllowPSTNUsersToBypassLobby = $false; AllowAnonymousUsersToStartMeeting = $false } }
function Get-CsTeamsAppPermissionPolicy { param($Identity) [pscustomobject]@{ Identity = 'Global'; GlobalCatalogAppsType = 'BlockedAppList'; GlobalCatalogApps = @(); PrivateCatalogAppsType = 'AllowedAppList'; PrivateCatalogApps = @() } }
function Get-CsTeamsAppSetupPolicy { param($Identity) [pscustomobject]@{ Identity = 'Global'; AllowSideLoading = $false; AllowUserPinning = $true } }
function Get-CsTeamsSettingsCustomApp { [pscustomobject]@{ isSideloadedAppsInteractionEnabled = $false } }
function Get-CsTeamsMessagingPolicy { param($Identity) [pscustomobject]@{ Identity = 'Global'; AllowSecurityEndUserReporting = $true } }
function Set-CsTeamsMeetingPolicy { 'WROTE' }
function Set-CsTeamsAppPermissionPolicy { 'WROTE' }
function Set-CsTeamsAppSetupPolicy { 'WROTE' }
function Set-CsTeamsMessagingPolicy { 'WROTE' }
Export-ModuleMember -Function *
