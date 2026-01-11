@echo off
setlocal EnableDelayedExpansion

echo === GoChat Integration Tests ===
echo.

cd /d "%~dp0.."

echo [1/4] Starting services...
docker compose -f docker-compose.yml -f deployments/docker-compose.test.yml up -d --build
if errorlevel 1 (
    echo Failed to start services
    exit /b 1
)

echo.
echo [2/4] Waiting for services to be ready (30 seconds)...
timeout /t 30 /nobreak > nul

echo.
echo [3/4] Running integration tests...
set TEST_API_URL=http://localhost:7070
set TEST_WS_URL=ws://localhost:7000/ws
set TEST_TCP_ADDR=localhost:7001
set TEST_REDIS_ADDR=localhost:6379
set TEST_ETCD_ADDR=localhost:2379

set "TEMP_OUTPUT=%TEMP%\gotest_output.txt"
go test -v -timeout 10m ./tests/integration/... > "%TEMP_OUTPUT%" 2>&1
set TEST_RESULT=%errorlevel%

type "%TEMP_OUTPUT%"

:: Parse test results
set PASSED_COUNT=0
set FAILED_COUNT=0
set SKIPPED_COUNT=0
set "FAILED_TESTS="

for /f "tokens=*" %%a in ('findstr /r /c:"^--- PASS:" "%TEMP_OUTPUT%"') do (
    set /a PASSED_COUNT+=1
)
for /f "tokens=*" %%a in ('findstr /r /c:"^--- SKIP:" "%TEMP_OUTPUT%"') do (
    set /a SKIPPED_COUNT+=1
)
:: Collect all failed tests first
set "ALL_FAILED="
for /f "tokens=2" %%a in ('findstr /r /c:"^--- FAIL:" "%TEMP_OUTPUT%"') do (
    set "ALL_FAILED=!ALL_FAILED!%%a;"
)

:: Filter to leaf tests only (tests that contain "/" are more specific)
set "FAILED_TESTS="
for /f "tokens=2" %%a in ('findstr /r /c:"^--- FAIL:" "%TEMP_OUTPUT%"') do (
    set "TEST_NAME=%%a"
    set "IS_LEAF=1"
    :: Check if this test is a parent of another failed test
    for %%b in (!ALL_FAILED!) do (
        set "OTHER=%%b"
        if not "!OTHER!"=="!TEST_NAME!" (
            echo !OTHER! | findstr /c:"!TEST_NAME!/" >nul 2>&1
            if not errorlevel 1 set "IS_LEAF=0"
        )
    )
    if "!IS_LEAF!"=="1" (
        set /a FAILED_COUNT+=1
        set "FAILED_TESTS=!FAILED_TESTS!  X !TEST_NAME!
"
    )
)

del "%TEMP_OUTPUT%" 2>nul

echo.
echo [4/4] Stopping services...
docker compose -f docker-compose.yml -f deployments/docker-compose.test.yml down

echo.
echo =============================================
echo            TEST SUMMARY
echo =============================================
echo.
set /a TOTAL_COUNT=%PASSED_COUNT%+%FAILED_COUNT%+%SKIPPED_COUNT%
echo Total: %TOTAL_COUNT% tests
echo   Passed:  %PASSED_COUNT%
echo   Failed:  %FAILED_COUNT%
echo   Skipped: %SKIPPED_COUNT%

if %FAILED_COUNT% gtr 0 (
    echo.
    echo ----- FAILED TESTS -----
    echo !FAILED_TESTS!
)

echo.
echo =============================================
if %TEST_RESULT% equ 0 (
    echo   All tests PASSED
) else (
    echo   Tests FAILED [exit code: %TEST_RESULT%]
)
echo =============================================

exit /b %TEST_RESULT%
