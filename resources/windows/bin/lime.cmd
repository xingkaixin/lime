@echo off
setlocal
call :check_help %*
if errorlevel 1 exit /b 0

set "EXE=%~dp0..\Lime.exe"
if not exist "%EXE%" (
  echo lime: Lime executable not found at "%EXE%" 1>&2
  exit /b 1
)

start "" "%EXE%" %*
exit /b 0

:check_help
if "%~1"=="" exit /b 0
if "%~1"=="--" exit /b 0
if "%~1"=="-h" goto :usage
if "%~1"=="--help" goto :usage
shift
goto :check_help

:usage
echo Usage: lime [OPTIONS] [PATHS...]
echo.
echo Examples:
echo   lime                       Simply opens Lime
echo   lime path-to-your-project  Open your project in Lime
echo   lime -n path-to-file       Open file/folder in a new window
echo.
echo Options:
echo   -n, --new   Open in a new window
echo   -h, --help  Show this help message
exit /b 1
