set PROJECT_DIR=%~dp0
REM powershell's exit codes are apparently completely broken with -File
powershell.exe -Command " & '%PROJECT_DIR%build.ps1'"
exit %ERRORLEVEL%
