# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Configures the otelcol-google Windows service to run strictly from the
# googlecontrolplane xDS config source (without config.yaml). When -OffGCP is
# set, it also stops the service, blocks outbound traffic from
# otelcol-google.exe to the GCE metadata server (169.254.169.254), installs the
# filtered hosts file so metadata.google.internal resolves to 127.0.0.1, and
# sets GOOGLE_APPLICATION_CREDENTIALS and GOOGLE_CLOUD_PROJECT on the Windows
# service for X.509 WIF and googleclientauth/default_gcp_destination.

param(
    [Parameter(Mandatory = $true)][string]$ControlPlaneConfig,
    [Parameter(Mandatory = $true)][string]$LogDir,
    [switch]$OffGCP,
    [string]$WifCredConfig = "",
    [string]$Project = "",
    [string]$FilteredHostsPath = ""
)

$ErrorActionPreference = 'Stop'
$serviceName = 'otelcol-google'
$regKey = "HKLM:\SYSTEM\CurrentControlSet\Services\$serviceName"
$exePath = 'C:\Program Files\Google\OpenTelemetry Collector\bin\otelcol-google.exe'

if (-not (Test-Path -LiteralPath $exePath)) {
    throw "Collector binary not found at $exePath"
}

New-Item -ItemType Directory -Force -Path $LogDir | Out-Null
Stop-Service -Force -Name $serviceName -ErrorAction SilentlyContinue

$newImagePath = "`"$exePath`" $ControlPlaneConfig"
Set-ItemProperty -Path $regKey -Name ImagePath -Value $newImagePath

if ($OffGCP) {
    $ruleName = 'TangoOffGCPBlockMetadata'
    Remove-NetFirewallRule -DisplayName $ruleName -ErrorAction SilentlyContinue
    New-NetFirewallRule `
        -DisplayName $ruleName `
        -Direction Outbound `
        -Action Block `
        -RemoteAddress '169.254.169.254' `
        -Program $exePath | Out-Null

    if ($FilteredHostsPath -ne "") {
        $systemHosts = "$env:SystemRoot\System32\drivers\etc\hosts"
        Copy-Item -Force -LiteralPath $FilteredHostsPath -Destination $systemHosts
        Clear-DnsClientCache
    }

    [string[]]$svcEnv = @(
        "GOOGLE_APPLICATION_CREDENTIALS=$WifCredConfig",
        "GOOGLE_CLOUD_PROJECT=$Project"
    )
    Set-ItemProperty -Path $regKey -Name Environment -Type MultiString -Value $svcEnv
}

(Get-ItemProperty -Path $regKey).ImagePath
