@echo off
rem Windows counterpart of build.sh - one batch file, no PowerShell, no other
rem script to call.
rem
rem   build.bat install   build pin.exe for this machine and relink the shims
rem                       (i works too, like pin's own short verbs)
rem   build.bat           dist\: five targets + SHA256SUMS
rem
rem Only plain ASCII here on purpose: a .bat is read with the console codepage,
rem and non-ASCII comments turn into mojibake in a cp936/cp437 window.
setlocal EnableExtensions EnableDelayedExpansion

pushd "%~dp0" || exit /b 1

where go >nul 2>nul
if errorlevel 1 (
  1>&2 echo build: go is not on PATH
  goto :fail
)

rem A checkout without tags must still produce a usable version string, so a
rem failing "git describe" is not an error: VERSION keeps its fallback.
set "VERSION=dev"
for /f "delims=" %%v in ('git describe --tags --always --dirty 2^>nul') do set "VERSION=%%v"
set "LDFLAGS=-s -w -X main.version=%VERSION%"
set "CGO_ENABLED=0"

if /i "%~1"=="install" goto :install
if /i "%~1"=="i" goto :install
if not "%~1"=="" goto :usage

rem ---------- cross-compilation ----------

if not exist dist mkdir dist || goto :fail
call :build linux   amd64 pin-linux-amd64        || goto :fail
call :build linux   arm64 pin-linux-arm64        || goto :fail
call :build windows amd64 pin-windows-amd64.exe  || goto :fail
call :build windows arm64 pin-windows-arm64.exe  || goto :fail
call :build darwin  arm64 pin-darwin-arm64       || goto :fail

rem SHA256SUMS: only the binaries, LF endings, so "sha256sum -c" on Linux and
rem macOS accepts the file. Batch can only echo CRLF, and a CRLF checksum file
rem makes sha256sum look for "pin-linux-amd64<CR>" - hence the one-byte LF file
rem decoded out of hex. certutil already prints lowercase hex.
if exist "dist\SHA256SUMS" del /f /q "dist\SHA256SUMS"
type nul > "dist\SHA256SUMS"
set "LF=%TEMP%\pin-lf.bin"
set "LFMODE="
> "%TEMP%\pin-lf.hex" echo 0a
certutil -decodehex "%TEMP%\pin-lf.hex" "%LF%" >nul 2>nul
if not errorlevel 1 if exist "%LF%" set "LFMODE=1"
del /f /q "%TEMP%\pin-lf.hex" >nul 2>nul
if not defined LFMODE (1>&2 echo build: warning: no LF helper; SHA256SUMS will have CRLF line endings)
for %%f in (pin-linux-amd64 pin-linux-arm64 pin-windows-amd64.exe pin-windows-arm64.exe pin-darwin-arm64) do call :sum "dist\%%f" "dist\SHA256SUMS" || goto :fail

echo.
dir /b dist
echo.
echo built dist\ from %VERSION%
popd
exit /b 0

rem ---------- install ----------

:install
if not defined PIN_BIN_DIR set "PIN_BIN_DIR=%USERPROFILE%\bin"
if not exist "%PIN_BIN_DIR%" mkdir "%PIN_BIN_DIR%" || goto :fail
set "OUT=%PIN_BIN_DIR%\pin.exe"

go build -trimpath -ldflags "%LDFLAGS%" -o "%OUT%" . || goto :fail
echo installed %OUT%

if defined PIN_HOME (set "SHIMDIR=%PIN_HOME%\bin") else (set "SHIMDIR=%USERPROFILE%\.pin\bin")

rem Shims are hard links, so they need the binary on the same volume. Compare
rem drive letters rather than volume names: that is what a user controls.
for %%d in ("%OUT%") do set "OUTDRIVE=%%~dd"
for %%d in ("%SHIMDIR%") do set "SHIMDRIVE=%%~dd"
if /i not "%OUTDRIVE%"=="%SHIMDRIVE%" (1>&2 echo build: warning: %PIN_BIN_DIR% and %SHIMDIR% are on different drives; shims become copies, not links)

rem "pin s" relinks the pinned tools. Exit 1 means "something to look at" (a
rem shim shadowed on PATH, say), which is not a reason to fail the build;
rem 2 and up are real errors.
"%OUT%" s
if errorlevel 2 goto :fail

rem ... but pin.exe in the shim directory is not in the registry, so "pin s"
rem leaves it alone. That directory sits on PATH, so a stale link there would
rem keep answering "pin version" for the old build.
set "SHIMPIN=%SHIMDIR%\pin.exe"
if exist "%SHIMDIR%" if /i not "%SHIMPIN%"=="%OUT%" call :link "%OUT%" "%SHIMPIN%" || goto :fail

rem Trust but verify: a rebuild that keeps the same version string is
rem indistinguishable from a stale shim to "pin doctor" (it compares build
rem stamps), so compare bytes instead of believing the stamp. fc /b is the
rem "are these the same file" test that exists in every Windows.
if not exist "%SHIMDIR%" goto :installed
set "NSTALE=0"
for %%f in ("%SHIMDIR%\*.exe") do (
  fc /b "%OUT%" "%%~ff" >nul 2>nul
  if errorlevel 1 (
    set /a NSTALE+=1
    del /f /q "%%~ff" >nul 2>nul
  )
)
if !NSTALE! gtr 0 (
  echo relinking !NSTALE! shim^(s^) still running an older build
  "%OUT%" d --fix
  if errorlevel 2 goto :fail
  set "LEFT=0"
  for %%f in ("%SHIMDIR%\*.exe") do (
    fc /b "%OUT%" "%%~ff" >nul 2>nul
    if errorlevel 1 set /a LEFT+=1
  )
  if !LEFT! gtr 0 (1>&2 echo build: warning: !LEFT! shim^(s^) still stale)
)

:installed
popd
exit /b 0

rem ---------- helpers ----------

:build
rem go build for one GOOS/GOARCH pair, into dist\<out>.
set "GOOS=%~1"
set "GOARCH=%~2"
go build -trimpath -ldflags "%LDFLAGS%" -o "dist\%~3" .
if errorlevel 1 (
  1>&2 echo build: %~1/%~2 failed
  exit /b 1
)
echo built dist\%~3
exit /b 0

:sum
rem "<hash>  <name>" plus a bare LF, appended to the checksum file. The hash
rem line is the only line in certutil's output that is pure hex, which makes
rem this work in every display language.
set "HASH="
for /f "delims=" %%h in ('certutil -hashfile "%~1" SHA256 2^>nul ^| findstr /r /x "[0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F]*"') do if not defined HASH set "HASH=%%h"
if not defined HASH (
  1>&2 echo build: could not hash %~1
  exit /b 1
)
if defined LFMODE (
  <nul set /p "=%HASH%  %~nx1">>"%~2"
  type "%LF%" >>"%~2"
) else (
  >>"%~2" echo %HASH%  %~nx1
)
exit /b 0

:link
rem Hard-link <src> to <dst>, replacing whatever was there. A freshly written
rem binary is briefly locked on Windows (virus scanner, indexer, a shell running
rem the old shim), so retry before falling back to a copy.
set "TRYLINK=0"
:link.try
set /a TRYLINK+=1
if exist "%~2" del /f /q "%~2" >nul 2>nul
mklink /H "%~2" "%~1" >nul 2>nul
if not errorlevel 1 (
  echo linked %~2
  exit /b 0
)
if %TRYLINK% lss 8 (
  ping -n 2 127.0.0.1 >nul
  goto :link.try
)
copy /y "%~1" "%~2" >nul 2>nul
if errorlevel 1 (
  1>&2 echo build: could not update %~2 - a running shim still holds it, close it and rerun
  exit /b 1
)
echo copied %~2 ^(hard link failed^)
exit /b 0

:usage
1>&2 echo usage: build.bat [install^|i]
1>&2 echo    install  build pin.exe for this machine and relink the shims
1>&2 echo    (none)   cross-compile: dist\ with five targets and SHA256SUMS
goto :fail

:fail
popd
exit /b 1
