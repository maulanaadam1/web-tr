@echo off
chcp 65001 >nul
echo.
echo ╔══════════════════════════════════════════╗
echo ║         PUSH TO GITHUB - web-tr          ║
echo ╚══════════════════════════════════════════╝
echo.

cd /d "%~dp0"

:: Cek status
git status --short
echo.

:: Minta pesan commit
set /p COMMIT_MSG="Masukkan pesan commit (atau tekan Enter untuk default): "
if "%COMMIT_MSG%"=="" set COMMIT_MSG=Update: %DATE% %TIME%

echo.
echo [1/3] Menambahkan semua perubahan...
git add .

echo [2/3] Membuat commit: %COMMIT_MSG%
git commit -m "%COMMIT_MSG%"

echo [3/3] Push ke GitHub (origin/main)...
git push origin main

echo.
if %ERRORLEVEL%==0 (
    echo ✅ BERHASIL! Kode sudah di-push ke GitHub.
    echo    Sekarang klik Redeploy di Easypanel untuk update VPS.
) else (
    echo ❌ GAGAL! Periksa koneksi atau log error di atas.
)

echo.
pause
