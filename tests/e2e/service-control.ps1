[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('start', 'stop')][string]$Action,
    [switch]$AllowFailedStart
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
# Avoid cmdlets' implicit waiting and release every SCM handle before MSI
# removes/recreates the service. No installation/registration is done here.
$controller = New-Object System.ServiceProcess.ServiceController('wininit')
try {
    if ($Action -eq 'stop') {
        if ($controller.Status -eq [System.ServiceProcess.ServiceControllerStatus]::Stopped) { return }
        if ($controller.Status -ne [System.ServiceProcess.ServiceControllerStatus]::StopPending) { $controller.Stop() }
        $controller.WaitForStatus([System.ServiceProcess.ServiceControllerStatus]::Stopped, [TimeSpan]::FromSeconds(30))
    }
    else {
        try { $controller.Start() }
        catch {
            if (-not $AllowFailedStart) { throw }
            Write-Host 'SCM start returned an error; require a fresh failed checkpoint before accepting the negative case.'
        }
        if (-not $AllowFailedStart) {
            $controller.WaitForStatus([System.ServiceProcess.ServiceControllerStatus]::Running, [TimeSpan]::FromSeconds(30))
        }
    }
}
finally { $controller.Dispose() }
