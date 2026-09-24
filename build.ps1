param(
    [switch]$RegenerateResources
)

$ErrorActionPreference = 'Stop'

function Assert-WindowsGuiExecutable {
    param([Parameter(Mandatory = $true)][string]$Path)

    $stream = [System.IO.File]::OpenRead((Resolve-Path -LiteralPath $Path).Path)
    $reader = [System.IO.BinaryReader]::new($stream)
    try {
        if ($stream.Length -lt 0x40) { throw "Executable is too small to contain a PE header: $Path" }
        $stream.Position = 0x3c
        $peOffset = $reader.ReadInt32()
        if ($peOffset -lt 0x40 -or ([long]$peOffset + 24) -gt $stream.Length) {
            throw "Executable has an invalid PE header: $Path"
        }

        $stream.Position = $peOffset
        if ($reader.ReadUInt32() -ne 0x00004550) { throw "Executable has an invalid PE signature: $Path" }
        $stream.Position = [long]$peOffset + 4 + 16
        $optionalHeaderSize = $reader.ReadUInt16()
        if ($optionalHeaderSize -lt 70 -or ([long]$peOffset + 24 + $optionalHeaderSize) -gt $stream.Length) {
            throw "Executable has an invalid optional header size: $Path"
        }
        $stream.Position = [long]$peOffset + 24
        $optionalHeaderMagic = $reader.ReadUInt16()
        if ($optionalHeaderMagic -ne 0x010b -and $optionalHeaderMagic -ne 0x020b) {
            throw "Executable has an unsupported PE optional header: $Path"
        }

        $stream.Position = [long]$peOffset + 24 + 68
        $subsystem = $reader.ReadUInt16()
        if ($subsystem -ne 2) {
            throw "Refusing to publish ${Path}: PE subsystem $subsystem is not Windows GUI (2). Build with -H=windowsgui."
        }
        Write-Host "Verified Windows GUI subsystem: $Path"
    } finally {
        $reader.Dispose()
        $stream.Dispose()
    }
}

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
    $buildId = [guid]::NewGuid().ToString('N')
    $pendingExecutable = "dist/spencer-clicker.$buildId.pending.exe"
    $pendingRootExecutable = "spencer-clicker.$buildId.pending.exe"
    try {
        go build -buildvcs=false -trimpath -ldflags '-s -w -H=windowsgui' -o $pendingExecutable ./cmd/spencer-clicker
        if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }
        Assert-WindowsGuiExecutable -Path $pendingExecutable
        Move-Item -LiteralPath $pendingExecutable -Destination 'dist/spencer-clicker.exe' -Force

        Copy-Item -LiteralPath 'dist/spencer-clicker.exe' -Destination $pendingRootExecutable
        Assert-WindowsGuiExecutable -Path $pendingRootExecutable
        Move-Item -LiteralPath $pendingRootExecutable -Destination 'spencer-clicker.exe' -Force
        Assert-WindowsGuiExecutable -Path 'spencer-clicker.exe'
        Get-Item -LiteralPath 'dist/spencer-clicker.exe' | Select-Object FullName, Length
    } finally {
        foreach ($temporaryExecutable in @($pendingExecutable, $pendingRootExecutable)) {
            if (Test-Path -LiteralPath $temporaryExecutable) { Remove-Item -LiteralPath $temporaryExecutable -Force }
        }
    }
} finally {
    Pop-Location
}
