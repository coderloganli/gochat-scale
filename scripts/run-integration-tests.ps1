# PowerShell script to run integration tests on Windows
param(
    [switch]$SkipBuild,
    [switch]$KeepRunning
)

$ErrorActionPreference = "Stop"

Write-Host "=== GoChat Integration Tests ===" -ForegroundColor Cyan

# Navigate to project root
$projectRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $projectRoot

# Start services
if (-not $SkipBuild) {
    Write-Host "`n[1/4] Starting services with build..." -ForegroundColor Yellow
    docker compose -f docker-compose.yml -f deployments/docker-compose.test.yml up -d --build
} else {
    Write-Host "`n[1/4] Starting services (skip build)..." -ForegroundColor Yellow
    docker compose -f docker-compose.yml -f deployments/docker-compose.test.yml up -d
}

# Wait for services
Write-Host "`n[2/4] Waiting for services to be ready..." -ForegroundColor Yellow
$maxWait = 60
$waited = 0
$ready = $false

while ($waited -lt $maxWait) {
    try {
        # Check API service
        $response = Invoke-WebRequest -Uri "http://localhost:7070/" -TimeoutSec 2 -ErrorAction SilentlyContinue
        if ($response.StatusCode -lt 500) {
            $ready = $true
            break
        }
    } catch {
        # Service not ready yet
    }
    Start-Sleep -Seconds 2
    $waited += 2
    Write-Host "  Waiting... ($waited/$maxWait seconds)" -ForegroundColor Gray
}

if (-not $ready) {
    Write-Host "  Services may not be fully ready, proceeding anyway..." -ForegroundColor Yellow
}
Write-Host "  Services ready!" -ForegroundColor Green

# Set environment variables
Write-Host "`n[3/4] Running integration tests..." -ForegroundColor Yellow
$env:TEST_API_URL = "http://localhost:7070"
$env:TEST_WS_URL = "ws://localhost:7000/ws"
$env:TEST_TCP_ADDR = "localhost:7001"
$env:TEST_REDIS_ADDR = "localhost:6379"
$env:TEST_ETCD_ADDR = "localhost:2379"

# Run tests and capture output
$testResult = 0
$failedTests = @()
$passedTests = @()
$skippedTests = @()

try {
    # Run go test with JSON output for accurate parsing
    $jsonFile = "$env:TEMP\gotest_json.txt"
    $verboseFile = "$env:TEMP\gotest_verbose.txt"

    # Run with both -json and tee to verbose file for display
    $process = Start-Process -FilePath "go" -ArgumentList "test", "-v", "-json", "-timeout", "10m", "./tests/integration/..." `
        -NoNewWindow -PassThru -RedirectStandardOutput $jsonFile -RedirectStandardError "$env:TEMP\gotest_stderr.txt"

    # Wait for process to complete
    $process.WaitForExit()
    $testResult = $process.ExitCode

    # Parse JSON output
    if (Test-Path $jsonFile) {
        $jsonLines = Get-Content $jsonFile -ErrorAction SilentlyContinue

        foreach ($line in $jsonLines) {
            try {
                $event = $line | ConvertFrom-Json -ErrorAction SilentlyContinue
                if ($event) {
                    # Display output in real-time style
                    if ($event.Action -eq "output" -and $event.Output) {
                        Write-Host $event.Output -NoNewline
                    }

                    # Track test results (only for actual tests, not package-level)
                    if ($event.Test) {
                        switch ($event.Action) {
                            "pass" { $passedTests += $event.Test }
                            "fail" { $failedTests += $event.Test }
                            "skip" { $skippedTests += $event.Test }
                        }
                    }
                }
            } catch {
                # Skip malformed JSON lines
            }
        }
    }

    # Show stderr if any
    if (Test-Path "$env:TEMP\gotest_stderr.txt") {
        $stderr = Get-Content "$env:TEMP\gotest_stderr.txt" -Raw -ErrorAction SilentlyContinue
        if ($stderr) {
            Write-Host $stderr -ForegroundColor Red
        }
    }

    # Filter to only show leaf tests (most specific failures)
    $leafFailedTests = @()
    foreach ($test in $failedTests) {
        $isLeaf = $true
        foreach ($other in $failedTests) {
            if ($other -ne $test -and $other.StartsWith("$test/")) {
                $isLeaf = $false
                break
            }
        }
        if ($isLeaf) {
            $leafFailedTests += $test
        }
    }
    $failedTests = $leafFailedTests

    # Filter passed/skipped to leaf only for accurate count
    $leafPassedTests = @()
    foreach ($test in $passedTests) {
        $isLeaf = $true
        foreach ($other in $passedTests) {
            if ($other -ne $test -and $other.StartsWith("$test/")) {
                $isLeaf = $false
                break
            }
        }
        if ($isLeaf) {
            $leafPassedTests += $test
        }
    }
    $passedTests = $leafPassedTests

} catch {
    $testResult = 1
    Write-Host "Tests failed with error: $_" -ForegroundColor Red
} finally {
    # Cleanup temp files
    Remove-Item "$env:TEMP\gotest_json.txt" -ErrorAction SilentlyContinue
    Remove-Item "$env:TEMP\gotest_stderr.txt" -ErrorAction SilentlyContinue
}

# Stop services unless KeepRunning is specified
if (-not $KeepRunning) {
    Write-Host "`n[4/4] Stopping services..." -ForegroundColor Yellow
    docker compose -f docker-compose.yml -f deployments/docker-compose.test.yml down
} else {
    Write-Host "`n[4/4] Keeping services running (-KeepRunning was specified)" -ForegroundColor Yellow
    Write-Host "  To stop: docker compose -f docker-compose.yml -f deployments/docker-compose.test.yml down" -ForegroundColor Gray
}

# Summary
Write-Host "`n"
Write-Host "=============================================" -ForegroundColor Cyan
Write-Host "           TEST SUMMARY                      " -ForegroundColor Cyan
Write-Host "=============================================" -ForegroundColor Cyan

$totalTests = $passedTests.Count + $failedTests.Count + $skippedTests.Count

Write-Host "`nTotal: $totalTests tests" -ForegroundColor White
Write-Host "  Passed:  $($passedTests.Count)" -ForegroundColor Green
Write-Host "  Failed:  $($failedTests.Count)" -ForegroundColor $(if ($failedTests.Count -gt 0) { "Red" } else { "Green" })
Write-Host "  Skipped: $($skippedTests.Count)" -ForegroundColor Yellow

if ($failedTests.Count -gt 0) {
    Write-Host "`n----- FAILED TESTS -----" -ForegroundColor Red
    foreach ($test in $failedTests) {
        Write-Host "  X $test" -ForegroundColor Red
    }
    Write-Host "`nTip: Run a specific failed test with:" -ForegroundColor Gray
    Write-Host "  go test -v -run '$($failedTests[0])' ./tests/integration/..." -ForegroundColor Gray
}

if ($skippedTests.Count -gt 0 -and $skippedTests.Count -le 10) {
    Write-Host "`n----- SKIPPED TESTS -----" -ForegroundColor Yellow
    foreach ($test in $skippedTests) {
        Write-Host "  - $test" -ForegroundColor Yellow
    }
}

Write-Host "`n=============================================" -ForegroundColor Cyan
if ($testResult -eq 0) {
    Write-Host "  All tests PASSED" -ForegroundColor Green
} else {
    Write-Host "  Tests FAILED (exit code: $testResult)" -ForegroundColor Red
}
Write-Host "=============================================" -ForegroundColor Cyan

exit $testResult
