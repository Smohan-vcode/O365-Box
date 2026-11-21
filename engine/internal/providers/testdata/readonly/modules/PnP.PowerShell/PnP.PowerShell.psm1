# Stands in for PnP.PowerShell in the audit-mode test: reads return a tenant
# the box did not build; every Set- cmdlet would write. Not shipped.
function Connect-PnPOnline { param($Url, $AccessToken, [switch] $ReturnConnection) if ($Url -notlike '*-admin.sharepoint.com') { throw "not the admin site: $Url" }; [pscustomobject]@{ Url = $Url } }
function Get-PnPTenant { param($Connection) [pscustomobject]@{ SharingCapability = 'ExternalUserSharingOnly'; DefaultSharingLinkType = 'Direct'; DefaultLinkPermission = 'View'
    ConditionalAccessPolicy = 'AllowFullAccess'; SharingDomainRestrictionMode = 'None'; SharingAllowedDomainList = ''; OneDriveSharingCapability = 'Disabled'; LegacyAuthProtocolsEnabled = $false } }
function Get-PnPTenantSite { param($Connection) @([pscustomobject]@{ Url = 'https://fernhilllabs.sharepoint.com/sites/a'; SharingCapability = 'ExternalUserAndGuestSharing'; DenyAddAndCustomizePages = 'Enabled' },
    [pscustomobject]@{ Url = 'https://fernhilllabs.sharepoint.com/sites/b'; SharingCapability = 'Disabled'; DenyAddAndCustomizePages = 'Enabled' }) }
function Get-PnPBrowserIdleSignout { param($Connection) [pscustomobject]@{ Enabled = $true; WarnAfter = [timespan]'00:55:00'; SignOutAfter = [timespan]'01:00:00' } }
function Get-PnPWeb { throw 'Access denied: a reader cannot open the site' }
function Set-PnPTenant { 'WROTE' }
function Set-PnPTenantSite { 'WROTE' }
function Set-PnPBrowserIdleSignout { 'WROTE' }
Export-ModuleMember -Function *
