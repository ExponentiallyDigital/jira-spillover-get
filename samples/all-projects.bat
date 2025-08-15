@echo off
setlocal enabledelayedexpansion

set SCRIPT=c:\jira-tools\jira-spillover-get
set FROM_DATE=2022-01-01
set PROJECTS=EXPD EXPD-1 EXPD-2 SUP

for %%P in (%PROJECTS%) do (
    %SCRIPT% -Project %%P -FromDate %FROM_DATE% -OutputFileName Spillover-%FROM_DATE%.txt -Append -TokenFile "C:\Users\my-nme\api-tokens\Jira-API-token.txt" -url https://jira.company.com
)
