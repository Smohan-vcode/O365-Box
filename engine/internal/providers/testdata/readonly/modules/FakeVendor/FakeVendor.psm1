# Stands in for a Microsoft module (PnP.PowerShell, MicrosoftTeams) in the
# read-only worker test. Not shipped.
function Get-FakeThing { return 'read' }
function Set-FakeThing { return 'WROTE' }
function Remove-FakeThing { return 'WROTE' }
Export-ModuleMember -Function Get-FakeThing, Set-FakeThing, Remove-FakeThing
