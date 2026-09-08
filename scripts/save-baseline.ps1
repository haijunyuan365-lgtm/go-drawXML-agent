param([string]$Name = 'day01-original-20260908')

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if ($Name -notmatch '^[a-zA-Z0-9_-]+$') { throw 'Snapshot name must contain only letters, numbers, _ or -.' }
$destination = Join-Path $projectRoot "evals/baselines/$Name"
if (Test-Path -LiteralPath $destination) { throw "Snapshot already exists; refusing to overwrite: $destination" }

# Use an explicit source list: never collect .env, IDE settings, build caches or old results.
$sources = @()
foreach ($directory in @('cmd', 'internal', 'pkg', 'configs', 'deployments', 'frontend/src', 'frontend/public')) {
    $sourceDirectory = Join-Path $projectRoot $directory
    if (Test-Path -LiteralPath $sourceDirectory) {
        $sources += Get-ChildItem -LiteralPath $sourceDirectory -Recurse -File
    }
}
foreach ($file in @('go.mod', 'go.sum')) { $sources += Get-Item -LiteralPath (Join-Path $projectRoot $file) }
$sources += Get-ChildItem -LiteralPath (Join-Path $projectRoot 'frontend') -File |
    Where-Object { $_.Name -notmatch '^\.' -and ($_.Extension -in @('.json', '.ts', '.mjs', '.md', '.sh', '.yml') -or $_.Name -eq 'Dockerfile') }

$utf8 = [System.Text.UTF8Encoding]::new($false)
$manifest = @()
foreach ($source in ($sources | Sort-Object FullName -Unique)) {
    $relative = $source.FullName.Substring($projectRoot.Length + 1).Replace('\', '/')
    $target = Join-Path $destination "source/$relative"
    [System.IO.Directory]::CreateDirectory((Split-Path -Parent $target)) | Out-Null
    $redacted = $false
    if ($source.Extension -in @('.yaml', '.yml')) {
        $original = [System.IO.File]::ReadAllText($source.FullName)
        # Preserve environment placeholders. Replace literal credential values in YAML copies only.
        $safe = [regex]::Replace($original, '(?im)^(\s*(?:[\w-]*(?:password|secret|token|api[_-]?key|dsn)[\w-]*)\s*:\s*)([^\r\n]+)', {
            param($match)
            $value = $match.Groups[2].Value.Trim()
            if ($value.Contains('${')) { return $match.Value }
            return $match.Groups[1].Value + '"REDACTED_SET_LOCALLY"'
        })
        $redacted = $safe -cne $original
        [System.IO.File]::WriteAllText($target, $safe, $utf8)
    } else {
        Copy-Item -LiteralPath $source.FullName -Destination $target
    }
    $manifest += [ordered]@{
        path = $relative
        original_sha256 = (Get-FileHash -LiteralPath $source.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        snapshot_sha256 = (Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash.ToLowerInvariant()
        redacted = $redacted
    }
}
$metadata = [ordered]@{
    snapshot_id = $Name
    created_at = (Get-Date).ToUniversalTime().ToString('o')
    source_root = $projectRoot
    scope = 'Application source, prompt/config templates, Go/frontend dependencies; no .env, IDE data, caches or evaluation results.'
    restore = 'Copy source into a separate checkout, supply credentials locally, verify snapshot hashes. Do not overwrite your active working directory.'
    files = $manifest
}
[System.IO.File]::WriteAllText((Join-Path $destination 'manifest.json'), ($metadata | ConvertTo-Json -Depth 6), $utf8)
Write-Output "Snapshot saved: $destination ($($manifest.Count) files)."
