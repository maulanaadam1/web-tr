@echo off
REM Run web-tr with SQLite database
SET DATABASE_URL=file:streams.db
SET ADMIN_USER=admin
SET ADMIN_PASS=admin123
REM Add current directory to PATH for ffmpeg.exe
SET PATH=%CD%;%PATH%
echo Starting web-tr with SQLite database...
echo Database will be created at: streams.db
echo.
.\web-tr.exe
