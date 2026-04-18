# Windows L2TP/IPsec NAT Traversal Fix
# Required for connecting to a VPN Server that is behind a NAT.

Write-Host "Applying Windows L2TP NAT Traversal Fix..."

New-ItemProperty -Path "HKLM:\System\CurrentControlSet\Services\Rasman\Parameters" -Name "AssumeUDPEncapsulationContextOnSendRule" -PropertyType DWord -Value 2 -Force

Write-Host "Restarting IPsec Policy Agent and Remote Access Connection Manager services..."
Restart-Service -Name "PolicyAgent" -Force
Restart-Service -Name "RasMan" -Force

Write-Host "Done! You can now connect to WebTR-VPN using Web-TR Gateway."
Write-Host "Press any key to close..."
$Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")
