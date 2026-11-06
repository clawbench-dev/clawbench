param(
  [string]$DevEcoRoot = 'F:\DevEco Studio',
  [ValidateSet('debug', 'release')][string]$Mode = 'debug'
)
$ErrorActionPreference = 'Stop'
$project = Join-Path $PSScriptRoot '../harmony'
$env:DEVECO_SDK_HOME = Join-Path $DevEcoRoot 'sdk'
$env:JAVA_HOME = Join-Path $DevEcoRoot 'jbr'
$env:NODE_HOME = Join-Path $DevEcoRoot 'tools/node'
$env:Path = "$env:NODE_HOME;$env:JAVA_HOME/bin;$env:Path"
Push-Location $project
try {
  & (Join-Path $DevEcoRoot 'tools/ohpm/bin/ohpm.bat') install
  if ($LASTEXITCODE -ne 0) { throw 'ohpm install failed' }
  & (Join-Path $DevEcoRoot 'tools/hvigor/bin/hvigorw.bat') --mode module -p product=default -p module=entry@default -p buildMode=$Mode assembleHap --no-daemon
  if ($LASTEXITCODE -ne 0) { throw 'HAP build failed' }
  $appManifest = Get-Content -LiteralPath 'AppScope/app.json5' -Encoding UTF8 -Raw | ConvertFrom-Json
  $appName = [string]$appManifest.app.label
  if ($appName.StartsWith('$string:')) {
    $nameResource = $appName.Substring(8)
    $strings = Get-Content -LiteralPath 'AppScope/resources/base/element/string.json' -Encoding UTF8 -Raw | ConvertFrom-Json
    $appName = [string]($strings.string | Where-Object { $_.name -eq $nameResource } | Select-Object -First 1).value
  }
  $appName = [regex]::Replace($appName, '[<>:"/\\|?*\x00-\x1F]', '_').Trim().TrimEnd('.')
  $appVersion = [string]$appManifest.app.versionName
  if (-not $appName -or $appVersion -notmatch '^[A-Za-z0-9._+-]+$') {
    throw 'App name or version cannot be used as an artifact filename'
  }
  $artifactDirectory = Join-Path $PSScriptRoot '../release'
  New-Item -ItemType Directory -Path $artifactDirectory -Force | Out-Null
  $packages = @(Get-ChildItem -LiteralPath 'entry/build/default/outputs/default' -Filter 'entry-default-*.hap')
  if ($packages.Count -eq 0) { throw 'No built HAP found' }
  $packages | ForEach-Object {
      $signature = if ($_.BaseName.EndsWith('-unsigned')) { 'unsigned' } else { 'signed' }
      $artifactPath = Join-Path $artifactDirectory "$appName-$appVersion-$signature.hap"
      Copy-Item -LiteralPath $_.FullName -Destination $artifactPath -Force
      $artifact = Get-Item -LiteralPath $artifactPath
      [pscustomobject]@{ Path=$artifact.FullName; Bytes=$artifact.Length; Sha256=(Get-FileHash -LiteralPath $artifact.FullName).Hash.ToLowerInvariant() }
    } |
    ConvertTo-Json
} finally {
  Pop-Location
}
