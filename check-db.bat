@echo off
REM Check SQLite database contents
echo Checking streams.db contents...
echo.

REM Try different methods to query SQLite

REM Method 1: Using sqlite3.exe (if installed)
where sqlite3.exe >nul 2>nul
if %ERRORLEVEL% EQU 0 (
    echo === Method 1: sqlite3.exe ===
    sqlite3.exe streams.db "SELECT name, backend, url FROM streams;"
    echo.
) else (
    echo sqlite3.exe not found, trying PowerShell...
)

REM Method 2: Using PowerShell
echo === Method 2: PowerShell ===
powershell -Command "$db = [System.Data.SQLite.SQLiteConnection]::new('Data Source=streams.db'); try { $db.Open(); $cmd = $db.CreateCommand(); $cmd.CommandText = 'SELECT name, backend, url FROM streams'; $reader = $cmd.ExecuteReader(); while ($reader.Read()) { Write-Host \"Name: $($reader['name']), Backend: $($reader['backend']), URL: $($reader['url'])\"; } } catch { Write-Host 'Error: SQLite library not available. Install System.Data.SQLite or use sqlite3.exe'; } finally { if ($db) { $db.Close(); } }"

echo.
echo === Database File Info ===
dir streams.db

pause
