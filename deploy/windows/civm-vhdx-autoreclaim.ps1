<#
.SYNOPSIS
  Host-side V: auto-reclaim for the civm runner VHDX (no-SSH, conditional).
.DESCRIPTION
  The guest disk is already managed by the civmctl disk-watchdog; this reclaims
  the HOST V: that Hyper-V will not shrink online (UNMAP not honored on this box,
  even at 1MB block size). Runs daily off-hours: only when V: free is below
  -ThresholdGB AND a manual-safe headroom exists. Stop-VM -> Mount-VHD ReadOnly
  -> Optimize-VHD -Mode Full -> Start-VM, with a finally that ALWAYS restarts the
  VM. Effective only because the VHDX is now 1MB-block (see civm-runner-reliability).
  No SSH dependency (the scheduled-task account has no guest SSH).
#>
[CmdletBinding(SupportsShouldProcess)]
param(
  [string]$VMName = 'gha-ubuntu-2404',
  [string]$VhdxPath = 'V:\Hyper-V\gha-ubuntu-2404\Virtual Hard Disks\gha-ubuntu-2404.vhdx',
  [int]$ThresholdGB = 35,
  [int]$MinHeadroomGB = 8,
  [string]$LogPath = 'V:\civm-hyperv-maintenance.log'
)
$ErrorActionPreference = 'Stop'
function Log($m){ try{ "$((Get-Date).ToUniversalTime().ToString('o')) autoreclaim: $m" | Add-Content -LiteralPath $LogPath }catch{}; Write-Host $m }
function VFree(){ [math]::Round((Get-PSDrive V).Free/1GB,2) }
$before = VFree
if($before -ge $ThresholdGB){ Log "V: ${before}GB >= ${ThresholdGB}GB threshold; skip"; exit 0 }
if($before -lt $MinHeadroomGB){ Log "V: ${before}GB < ${MinHeadroomGB}GB headroom; refuse, manual reclaim needed"; exit 2 }
$started=$false; $mounted=$false
try {
  Log "V: ${before}GB below threshold; Stop-VM"
  Stop-VM -Name $VMName -ErrorAction Stop
  $dl=(Get-Date).AddSeconds(180); while((Get-Date)-lt $dl){ if((Get-VM -Name $VMName).State -eq 'Off'){break}; Start-Sleep 3 }
  if((Get-VM -Name $VMName).State -ne 'Off'){ throw "VM not Off in 180s" }
  try { Mount-VHD -Path $VhdxPath -ReadOnly -ErrorAction Stop; $mounted=$true } catch { Log "mount-ro skipped: $($_.Exception.Message)" }
  Optimize-VHD -Path $VhdxPath -Mode Full -ErrorAction Stop
  Log "optimized; V: now $(VFree)GB (was ${before}GB)"
} catch { Log "ERROR: $($_.Exception.Message)" }
finally {
  if($mounted){ try{ Dismount-VHD -Path $VhdxPath -ErrorAction Stop }catch{} }
  for($i=1;$i -le 3 -and -not $started;$i++){ try{ if((Get-VM -Name $VMName).State -ne 'Running'){ Start-VM -Name $VMName -ErrorAction Stop }; $d2=(Get-Date).AddSeconds(90); while((Get-Date)-lt $d2){ if((Get-VM -Name $VMName).State -eq 'Running'){$started=$true;break}; Start-Sleep 2 } }catch{ Start-Sleep 5 } }
  Log "done; vm_started=$started; V_final=$(VFree)GB"
}
