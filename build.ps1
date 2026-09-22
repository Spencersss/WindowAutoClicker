param(
    [switch]$RegenerateResources
)

$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    if ($RegenerateResources) {
        $windres = Get-Command windres -ErrorAction SilentlyContinue
        if ($null -eq $windres) {
            throw 'windres was not found. Install MinGW resource tools or omit -RegenerateResources to use the bundled resource file.'
        }

        Push-Location (Join-Path $PSScriptRoot 'cmd\spencer-clicker')
        try {
            & $windres.Source -i app.rc -O coff -o resource_windows_amd64.syso
            if ($LASTEXITCODE -ne 0) { throw 'windres resource generation failed.' }
        } finally {
            Pop-Location
        }
    }

    New-Item -ItemType Directory -Force -Path 'dist' | Out-Null
    go build -buildvcs=false -trimpath -ldflags '-s -w -H=windowsgui' -o dist/spencer-clicker.exe ./cmd/spencer-clicker
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }
    Copy-Item -LiteralPath 'dist/spencer-clicker.exe' -Destination 'spencer-clicker.exe' -Force
    Get-Item -LiteralPath 'dist/spencer-clicker.exe' | Select-Object FullName, Length
} finally {
    Pop-Location
}
