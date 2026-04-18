$ErrorActionPreference = "Stop"

$w64Dir = "$PSScriptRoot\w64devkit"
$gccPath = "$w64Dir\bin\gcc.exe"

if (Get-Command gcc -ErrorAction SilentlyContinue) {
    Write-Host "GCC is already available in PATH." -ForegroundColor Green
}
else {
    if (-not (Test-Path $gccPath)) {
        Write-Host "GCC not found. Downloading portable compiler (w64devkit ~80MB)..." -ForegroundColor Yellow
        $zipPath = "$PSScriptRoot\w64devkit.zip"
        
        # Download w64devkit
        Invoke-WebRequest -Uri "https://github.com/skeeto/w64devkit/releases/download/v1.23.0/w64devkit-1.23.0.zip" -OutFile $zipPath
        
        Write-Host "Extracting compiler... Please wait." -ForegroundColor Yellow
        Expand-Archive -Path $zipPath -DestinationPath $PSScriptRoot -Force
        
        # Cleanup zip
        Remove-Item $zipPath -Force
    }
    
    Write-Host "Adding compiler to current session PATH..." -ForegroundColor Green
    $env:Path = "$w64Dir\bin;" + $env:Path
}

Write-Host "Checking GCC version..."
gcc --version

Write-Host "`nSetting CGO_ENABLED=1..." -ForegroundColor Green
$env:CGO_ENABLED = "1"

Write-Host "Building RTSP2go Gateway (this might take 1-3 minutes for the first time)..." -ForegroundColor Yellow
go build -ldflags="-H windowsgui" -o RTSP2go-Gateway.exe .

if ($?) {
    Write-Host "`n=========================================" -ForegroundColor Green
    Write-Host "BUILD SUCCESSFUL! " -ForegroundColor Green
    Write-Host "You can now run RTSP2go-Gateway.exe" -ForegroundColor Green
    Write-Host "=========================================" -ForegroundColor Green
}
else {
    Write-Host "`nBuild failed. Please check the errors above." -ForegroundColor Red
}
