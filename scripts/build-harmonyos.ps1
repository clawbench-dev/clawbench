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
  Get-ChildItem -LiteralPath 'entry/build/default/outputs/default' -Filter '*.hap' |
    ForEach-Object { [pscustomobject]@{ Path=$_.FullName; Bytes=$_.Length; Sha256=(Get-FileHash -LiteralPath $_.FullName).Hash.ToLowerInvariant() } } |
    ConvertTo-Json
} finally {
  Pop-Location
}
