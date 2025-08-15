@echo off
REM Build script with automatic version synchronization
REM Extracts version from Go source and updates versioninfo.json before building

setlocal enabledelayedexpansion

if "%1"=="--help" goto :help
if "%1"=="-help" goto :help
if "%1"=="-?" goto :help
if "%1"=="/?" goto :help

echo Building jira-spillover-get...

REM Check if we're in the right directory
if not exist "jira-spillover-get.go" (
  echo Error: Must be run from the project root directory
  exit /b 1
)

REM Extract version from Go file
echo Extracting version from Go source...
for /f "tokens=2 delims==" %%a in ('findstr "programVersion.*=" jira-spillover-get.go') do (
  set version_line=%%a
)

REM Clean up the version string (remove quotes and spaces)
set version_line=!version_line: =!
set version_line=!version_line:"=!
set current_version=!version_line!

if "!current_version!"=="" (
  echo Error: Could not extract version from jira-spillover-get.go
  exit /b 1
)

echo Detected version: !current_version!

REM Parse version components
for /f "tokens=1,2,3 delims=." %%a in ("!current_version!") do (
  set major=%%a
  set minor=%%b
  set patch=%%c
)

echo Updating versioninfo.json to version !current_version!...
powershell -Command "& { $v = Get-Content 'versioninfo.json' | ConvertFrom-Json; $v.FixedFileInfo.FileVersion.Major = !major!; $v.FixedFileInfo.FileVersion.Minor = !minor!; $v.FixedFileInfo.FileVersion.Patch = !patch!; $v.FixedFileInfo.ProductVersion.Major = !major!; $v.FixedFileInfo.ProductVersion.Minor = !minor!; $v.FixedFileInfo.ProductVersion.Patch = !patch!; $v.StringFileInfo.FileVersion = '!current_version!.0'; $v.StringFileInfo.ProductVersion = '!current_version!.0'; $v | ConvertTo-Json -Depth 10 | Out-File 'versioninfo.json' -Encoding ASCII }"
if errorlevel 1 (
  echo Error: Failed to update versioninfo.json
  exit /b 1
)
echo Updated versioninfo.json with version !current_version!

echo Running go generate...
go generate
if errorlevel 1 (
  echo Error: go generate failed
  exit /b 1
)
echo go generate completed successfully

REM Create build output folder
set BUILD_DIR=..\build
if not exist "%BUILD_DIR%" (
  mkdir "%BUILD_DIR%"
)

echo Building Windows binary...
go build -o %BUILD_DIR%\jira-spillover-get.exe
if errorlevel 1 (
  echo Error: Windows build failed
  exit /b 1
)

echo Building macOS binary...
set GOOS=darwin
set GOARCH=amd64
go build -o "%BUILD_DIR%\jira-spillover-get-macos" jira-spillover-get.go
if errorlevel 1 (
  echo Error: macOS build failed
  exit /b 1
)

echo Building Linux binary...
set GOOS=linux
set GOARCH=amd64
go build -o "%BUILD_DIR%\jira-spillover-get-linux" jira-spillover-get.go
if errorlevel 1 (
  echo Error: Linux build failed
  exit /b 1
)

REM Reset GOOS/GOARCH
set GOOS=
set GOARCH=

echo All builds completed successfully!
echo Output folder: %BUILD_DIR%
dir %BUILD_DIR%

echo Build process completed!
goto :end

:help
echo.
echo Build script for jira-spillover-get
echo.
echo Usage: build.bat [options]
echo.
echo Options:
echo  --help, -help, -?  Show this help message
echo.
echo This script will:
echo  1. Extract version from jira-spillover-get.go
echo  2. Update versioninfo.json with the extracted version
echo  3. Run go generate to update version resources
echo  4. Build Windows, macOS, and Linux binaries
echo  5. Output all binaries to ..\build
echo.

:end
