# Pester tests for the pure helpers of policy.intune-macos, policy.intune-mobile
# and policy.m365-apps (IntunePolicy.Mobile.ps1). Fictional data only.
BeforeAll {
  $script:Steps = [ordered]@{}
  . (Join-Path $PSScriptRoot 'IntunePolicy.Mobile.ps1')
  . (Join-Path $PSScriptRoot 'Policy.Mac.ps1')
  . (Join-Path $PSScriptRoot 'Policy.Mobile.ps1')
  function Get-ImGroupId([string] $Name) { return "id-$Name" }
  $script:In = @{ tenant = @{ id = '00000000-0000-0000-0000-00000000acme' }; config = @{ devices = @{ ios = @{ minimum_version = '17.0' } } }
    policy = @{ rollout = @{ stage = 'audit'; pilot_group = 'grp-pilot' }; settings = @() } }
  function choice($id, $v) { [ordered]@{ '@odata.type' = '#microsoft.graph.deviceManagementConfigurationChoiceSettingInstance'; settingDefinitionId = $id; choiceSettingValue = [ordered]@{ value = $v; children = @() } } }
}

Describe 'placeholders' {
  It 'fills the tenant id and a configuration value' {
    $m = [System.Collections.Generic.List[string]]::new()
    $r = Resolve-ImValue ([ordered]@{ a = '{tenant:id}'; b = @('{config:devices.ios.minimum_version}') }) $In $m
    $r.a | Should -Be '00000000-0000-0000-0000-00000000acme'
    $r.b[0] | Should -Be '17.0'
    $m.Count | Should -Be 0
  }
  It 'reports values the company must supply' {
    $m = [System.Collections.Generic.List[string]]::new()
    Resolve-ImValue ([ordered]@{ a = '{text:escrow}'; b = '{config:devices.android.minimum_version}'; c = 'base64(script)'; d = 'decision needed: x' }) $In $m | Out-Null
    $m.Count | Should -Be 4
  }
  It 'collects group names from a payload' {
    Get-ImPayloadGroups ([ordered]@{ assignments = @(@{ target = @{ groupId = '{group:grp-staff}' } }) }) | Should -Be @('grp-staff')
  }
}

Describe 'comparison' {
  It 'finds no difference when the tenant has the desired values and more' {
    $want = [ordered]@{ '@odata.type' = '#x'; displayName = 'Box x'; passcodeRequired = $true; allowed = @('b', 'a'); nested = [ordered]@{ blocked = $false } }
    $have = [pscustomobject]@{ passcodeRequired = $true; allowed = @('a', 'b'); nested = [pscustomobject]@{ blocked = $false; other = 1 }; extra = 'x' }
    @(Compare-ImProps $want $have).Count | Should -Be 0
  }
  It 'reports a changed value' {
    $d = @(Compare-ImProps ([ordered]@{ minimumPinLength = 6 }) ([pscustomobject]@{ minimumPinLength = 4 }))
    $d.Count | Should -Be 1
    $d[0] | Should -Match 'minimumPinLength 4 -> 6'
  }
  It 'flattens settings the same with or without template references and ids' {
    $a = @(ConvertTo-ImSetting (choice 'x_enable' 'x_enable_1'))
    $b = @([pscustomobject]@{ id = '0'; settingInstance = [pscustomobject]@{ '@odata.type' = '#microsoft.graph.deviceManagementConfigurationChoiceSettingInstance'; settingDefinitionId = 'x_enable'
          settingInstanceTemplateReference = $null; choiceSettingValue = [pscustomobject]@{ value = 'x_enable_1'; children = @(); settingValueTemplateReference = $null } } })
    (Get-ImFlatSettings $a) -join '|' | Should -Be ((Get-ImFlatSettings $b) -join '|')
  }
  It 'merges the children of one group collection from two settings' {
    $g = { param($kids) [ordered]@{ '@odata.type' = '#microsoft.graph.deviceManagementConfigurationGroupSettingCollectionInstance'; settingDefinitionId = 'g'; groupSettingCollectionValue = @([ordered]@{ children = $kids }) } }
    $m = Merge-ImSettings (@(, @((& $g @((choice 'g_a' 'g_a_1'))))) + @(, @((& $g @((choice 'g_b' 'g_b_0'))))))
    $flat = Get-ImFlatSettings $m
    $flat | Should -Contain 'g[0]/g_a=g_a_1'
    $flat | Should -Contain 'g[0]/g_b=g_b_0'
  }
  It 'keys assignment targets' {
    Get-ImTargetKey @{ '@odata.type' = '#microsoft.graph.groupAssignmentTarget'; groupId = 'g1' } | Should -Be 'group:g1'
    Get-ImTargetKey @{ '@odata.type' = '#microsoft.graph.exclusionGroupAssignmentTarget'; groupId = 'g2' } | Should -Be 'exclude:g2'
  }
}

Describe 'targets' {
  It 'assigns to the pilot group in the audit stage and excludes break-glass' {
    $t = Get-ImTargets $In @{ kind = 'compliance'; groups = @('grp-staff') }
    @($t | ForEach-Object { Get-ImTargetKey $_ }) | Should -Be @('group:id-grp-pilot', 'exclude:id-grp-break-glass')
  }
  It 'assigns to the payload groups in the enforce stage' {
    $e = @{ policy = @{ rollout = @{ stage = 'enforce'; pilot_group = 'grp-pilot' } } }
    $t = Get-ImTargets $e @{ kind = 'shellScript'; groups = @('grp-staff', 'all-devices') }
    @($t | ForEach-Object { Get-ImTargetKey $_ }) | Should -Be @('group:id-grp-staff', 'allDevices')
  }
}

Describe 'settings from definitions' {
  It 'picks an option by suffix, by name and by a yes/no description' {
    $def = [pscustomobject]@{ id = 'd'; options = @([pscustomobject]@{ itemId = 'd_0'; displayName = 'No'; name = 'No' }, [pscustomobject]@{ itemId = 'd_1'; displayName = 'Yes'; name = 'Yes' }) }
    Resolve-ImOption $def '_0' | Should -Be 'd_0'
    Resolve-ImOption $def 'Yes' | Should -Be 'd_1'
    Resolve-ImOption $def 'show notification' | Should -Be 'd_1'
    $tf = [pscustomobject]@{ id = 'c'; options = @([pscustomobject]@{ itemId = 'c_0'; displayName = 'False' }, [pscustomobject]@{ itemId = 'c_1'; displayName = 'True' }) }
    Resolve-ImOption $tf '_true' | Should -Be 'c_1'
    { Resolve-ImOption $tf 'maybe' } | Should -Throw
  }
}

Describe 'profiles' {
  It 'builds the same configuration profile on every run' {
    $a = New-ImMobileConfig 'com.example.x' 'X' 'com.apple.loginwindow' ([ordered]@{ DisableAutoLoginClient = $true })
    $a | Should -Be (New-ImMobileConfig 'com.example.x' 'X' 'com.apple.loginwindow' ([ordered]@{ DisableAutoLoginClient = $true }))
    [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($a)) | Should -Match '<key>DisableAutoLoginClient</key><true/>'
  }
}

Describe 'builders' {
  It 'routes phone settings to one object per platform' {
    $s = @(
      [pscustomobject]@{ id = 'MOB-003'; payload = [pscustomobject]@{ ios = [pscustomobject]@{ '@odata.type' = '#microsoft.graph.iosCompliancePolicy'; securityBlockJailbrokenDevices = $true }
          androidWorkProfile = [pscustomobject]@{ '@odata.type' = '#microsoft.graph.androidWorkProfileCompliancePolicy'; securityBlockJailbrokenDevices = $true } } }
      [pscustomobject]@{ id = 'MOB-004'; payload = [pscustomobject]@{ ios = [pscustomobject]@{ '@odata.type' = '#microsoft.graph.iosCompliancePolicy'; osMinimumVersion = '{config:devices.ios.minimum_version}' }
          androidWorkProfile = [pscustomobject]@{ '@odata.type' = '#microsoft.graph.androidWorkProfileCompliancePolicy'; osMinimumVersion = '{config:devices.android.minimum_version}' } } }
      [pscustomobject]@{ id = 'MOB-015'; payload = [pscustomobject]@{ ios = [pscustomobject]@{ '@odata.type' = '#microsoft.graph.iosManagedAppProtection'; dataBackupBlocked = $true } } }
    )
    $in = @{ tenant = $In.tenant; config = $In.config; policy = @{ rollout = $In.policy.rollout; settings = $s } }
    $b = Get-ImMobileObjects $in
    $ios = @($b.objects | Where-Object { $_.name -eq 'Box iOS compliance' })[0]
    $ios.body.osMinimumVersion | Should -Be '17.0'
    $ios.body.scheduledActionsForRule | Should -Not -BeNullOrEmpty
    @($b.objects | Where-Object { $_.name -eq 'Box Android work profile compliance' })[0].body.Contains('osMinimumVersion') | Should -BeFalse
    @($b.skipped | ForEach-Object { $_.id }) | Should -Be @('MOB-004 (androidWorkProfile)')
    @($b.objects | Where-Object { $_.kind -eq 'iosAppProtection' })[0].body.appGroupType | Should -Be 'allCoreMicrosoftApps'
  }
  It 'makes the macOS automatic login profile concrete and skips the enrolment profile' {
    $s = @(
      [pscustomobject]@{ id = 'MAC-014'; payload = [pscustomobject]@{ '@odata.type' = '#microsoft.graph.macOSCustomConfiguration'; displayName = 'Box macOS disable automatic login'; payloadName = 'Disable automatic login'; payloadFileName = 'box-loginwindow.mobileconfig'; payload = 'base64(described)' } }
      [pscustomobject]@{ id = 'MAC-023'; payload = [pscustomobject]@{ depMacOSEnrollmentProfile = [pscustomobject]@{} } }
    )
    $b = Get-ImMacObjects @{ tenant = $In.tenant; config = @{}; policy = @{ rollout = $In.policy.rollout; settings = $s } }
    $b.objects.Count | Should -Be 1
    [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($b.objects[0].body.payload)) | Should -Match 'DisableAutoLoginClient'
    $b.skipped[0].id | Should -Be 'MAC-023'
  }
}
