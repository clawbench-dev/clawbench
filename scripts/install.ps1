# ClawBench 一键安装脚本 — Windows (PowerShell)
#
# 做两件事：
#   1. 准备 Node.js（本机已有 20 及以上就直接用，否则下载一份到自己的目录）
#   2. 用 npm 把 clawbench 装到这个目录里
#
# 不写系统目录，不需要管理员权限。国内默认走 npmmirror，连不上自动换官方源。
#
# 用法（PowerShell 窗口里执行）：
#   irm https://clawbench.pages.dev/install.ps1 | iex
#
# 安装位置（可用环境变量 CLAWBENCH_HOME 覆盖）：
#   %LOCALAPPDATA%\clawbench\node   Node.js
#   %LOCALAPPDATA%\clawbench\npm    全局 npm 前缀（clawbench 在这里）

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

# 老系统（Win 8.1 / 2012）默认不启用 TLS 1.2，不设会下载失败
try {
  [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
} catch {
  # 新系统已经是 TLS 1.2+，设不进去也无所谓
}

$ClawbenchHome = if ($env:CLAWBENCH_HOME) {
  $env:CLAWBENCH_HOME
} elseif ($env:LOCALAPPDATA) {
  Join-Path $env:LOCALAPPDATA 'clawbench'
} else {
  Join-Path $env:USERPROFILE 'AppData\Local\clawbench'
}
$NodeDir = Join-Path $ClawbenchHome 'node'
$NpmPrefix = Join-Path $ClawbenchHome 'npm'
$NodeMinMajor = 20
$NpmPackage = '@xulongzhe/clawbench'

function Write-Step([string]$Message) {
  Write-Host ''
  Write-Host "==> $Message" -ForegroundColor Cyan
}

function Write-Info([string]$Message) {
  Write-Host "    $Message"
}

function Write-Ok([string]$Message) {
  Write-Host "    " -NoNewline
  Write-Host "OK" -ForegroundColor Green -NoNewline
  Write-Host " $Message"
}

function Fail([string]$Message) {
  # 用 throw 而不是 exit：脚本通常以 `irm | iex` 在当前窗口里跑，
  # exit 会把这个窗口一起关掉，用户就看不到错误原因了。
  throw "ClawBench 安装失败：$Message"
}

# ---------- 平台 ----------

$arch = $env:PROCESSOR_ARCHITECTURE
if ($env:PROCESSOR_ARCHITEW6432) {
  # 32 位 PowerShell 跑在 64 位系统上时，PROCESSOR_ARCHITECTURE 是 x86
  $arch = $env:PROCESSOR_ARCHITEW6432
}

$forceOwnNode = $false
switch ($arch) {
  'AMD64' { $nodeArch = 'x64' }
  'ARM64' {
    # ClawBench 的 npm 包只有 win32-x64，没有 win32-arm64；
    # Windows 11 ARM 能透明模拟运行 x64 程序，所以这里装 x64 版。
    $nodeArch = 'x64'
    $forceOwnNode = $true
    Write-Info 'Windows ARM64：安装 x64 版本（系统会自动模拟运行）'
  }
  default { Fail "不支持的 CPU 架构：$arch" }
}

# ---------- 镜像 ----------

Write-Step '检查网络'
$MirrorBase = 'https://registry.npmmirror.com/-/binary/node'
$MirrorAlt = 'https://nodejs.org/dist'
$NpmRegistry = 'https://registry.npmmirror.com'

$useMirror = $true
try {
  Invoke-WebRequest -Uri "$NpmRegistry/-/ping" -UseBasicParsing -TimeoutSec 8 | Out-Null
} catch {
  $useMirror = $false
}

if ($useMirror) {
  Write-Ok '使用国内镜像 npmmirror'
} else {
  $MirrorBase = $MirrorAlt
  $NpmRegistry = 'https://registry.npmjs.org'
  Write-Info '镜像不可达，使用官方源'
}

# ---------- Node.js ----------

Write-Step '准备 Node.js'

$nodeExe = $null
$npmCmd = $null

if (Test-Path (Join-Path $NodeDir 'node.exe')) {
  $nodeExe = Join-Path $NodeDir 'node.exe'
  $npmCmd = Join-Path $NodeDir 'npm.cmd'
  Write-Info "已有本脚本装的 Node.js $(& $nodeExe -v)，直接使用"
} elseif (-not $forceOwnNode) {
  # ARM64 上不复用系统 Node：原生 arm64 的 npm 会去装不存在的 win32-arm64 包
  $systemNode = Get-Command node.exe -ErrorAction SilentlyContinue
  $systemNpm = Get-Command npm.cmd -ErrorAction SilentlyContinue
  if ($systemNode -and $systemNpm) {
    $current = (& $systemNode.Source -v).Trim()
    $major = 0
    $majorText = $current.TrimStart('v').Split('.')[0]
    if ([int]::TryParse($majorText, [ref]$major) -and $major -ge $NodeMinMajor) {
      $nodeExe = $systemNode.Source
      $npmCmd = $systemNpm.Source
      Write-Ok "已安装 Node.js $current，直接使用"
    } else {
      Write-Info "已安装 Node.js $current，低于 $NodeMinMajor，另装一份"
    }
  }
}

if (-not $nodeExe) {
  Write-Info '查询最新 LTS 版本...'

  $index = $null
  try {
    $index = Invoke-RestMethod -Uri "$MirrorBase/index.json" -UseBasicParsing -TimeoutSec 30
  } catch {
    if ($MirrorBase -ne $MirrorAlt) {
      Write-Info '镜像取不到版本列表，换官方源'
      $MirrorBase = $MirrorAlt
      $index = Invoke-RestMethod -Uri "$MirrorBase/index.json" -UseBasicParsing -TimeoutSec 30
    } else {
      Fail '拿不到 Node.js 版本列表，请检查网络'
    }
  }

  $lts = $index | Where-Object { $_.lts } | Select-Object -First 1
  if (-not $lts) { Fail '解析 Node.js 版本列表失败' }

  $version = $lts.version
  $file = "node-$version-win-$nodeArch.zip"
  $url = "$MirrorBase/$version/$file"
  Write-Info "下载 Node.js $version（win-$nodeArch，约 30MB）"

  $tmp = Join-Path $env:TEMP ("clawbench-install-" + [guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Path $tmp | Out-Null

  try {
    $zip = Join-Path $tmp $file
    try {
      (New-Object System.Net.WebClient).DownloadFile($url, $zip)
    } catch {
      if ($MirrorBase -ne $MirrorAlt) {
        Write-Info '镜像下载失败，换官方源重试'
        $MirrorBase = $MirrorAlt
        $url = "$MirrorBase/$version/$file"
        (New-Object System.Net.WebClient).DownloadFile($url, $zip)
      } else {
        throw
      }
    }

    # 校验和能拿到就校验；拿不到不阻断（镜像偶尔抽风）
    $expected = $null
    try {
      $sums = (Invoke-WebRequest -Uri "$MirrorBase/$version/SHASUMS256.txt" -UseBasicParsing -TimeoutSec 30).Content
      $line = ($sums -split "`n") | Where-Object { $_ -match [regex]::Escape($file) } | Select-Object -First 1
      if ($line) {
        $expected = ($line -split '\s+')[0].Trim().ToLower()
      }
    } catch {
      $expected = $null
    }
    if ($expected) {
      $actual = (Get-FileHash -Path $zip -Algorithm SHA256).Hash.ToLower()
      if ($expected -ne $actual) { Fail 'Node.js 安装包校验失败（下载不完整），请重试' }
    }

    Expand-Archive -Path $zip -DestinationPath $tmp -Force
    $inner = Join-Path $tmp "node-$version-win-$nodeArch"
    if (-not (Test-Path $inner)) { Fail '解压后没找到 Node.js 目录' }

    New-Item -ItemType Directory -Path $ClawbenchHome -Force | Out-Null
    if (Test-Path $NodeDir) {
      try {
        Remove-Item -Path $NodeDir -Recurse -Force
      } catch {
        Fail "旧的 Node.js 目录删不掉（可能正被占用）：$NodeDir"
      }
    }
    Move-Item -Path $inner -Destination $NodeDir

    $nodeExe = Join-Path $NodeDir 'node.exe'
    $npmCmd = Join-Path $NodeDir 'npm.cmd'
    Write-Ok "Node.js $(& $nodeExe -v) 已就位：$NodeDir"
  } finally {
    Remove-Item -Path $tmp -Recurse -Force -ErrorAction SilentlyContinue
  }
}

$env:Path = "$NodeDir;$env:Path"

if (-not (Test-Path $npmCmd)) { Fail "找不到 npm：$npmCmd" }

# ---------- ClawBench ----------

Write-Step '安装 ClawBench（npm 全局包）'
Write-Info '下载量约 150MB，国内镜像下通常一两分钟'
New-Item -ItemType Directory -Path $NpmPrefix -Force | Out-Null

& $npmCmd install -g "$NpmPackage@latest" `
  --prefix $NpmPrefix `
  --registry $NpmRegistry `
  --no-fund `
  --no-audit

if ($LASTEXITCODE -ne 0) {
  Fail "npm 安装失败（退出码 $LASTEXITCODE），请把上面的输出发出来"
}

$clawbenchCmd = Join-Path $NpmPrefix 'clawbench.cmd'
if (-not (Test-Path $clawbenchCmd)) {
  Fail "npm 跑完了但没找到 $clawbenchCmd，请把上面的输出发出来"
}

$env:Path = "$NpmPrefix;$env:Path"
$versionText = (& $clawbenchCmd --version 2>$null | Select-Object -First 1)
if (-not $versionText) {
  Fail 'clawbench 装上了但跑不起来（多半是平台包没装上）。请把上面的 npm 输出发出来'
}
Write-Ok "clawbench $versionText"

# ---------- PATH ----------

Write-Step '把命令加进 PATH'

function Add-UserPath([string]$Dir) {
  $current = [Environment]::GetEnvironmentVariable('Path', 'User')
  if (-not $current) { $current = '' }
  $parts = @($current.Split(';') | Where-Object { $_ })
  if ($parts -notcontains $Dir) {
    [Environment]::SetEnvironmentVariable('Path', (($parts + $Dir) -join ';'), 'User')
    Write-Info "已写入用户 PATH：$Dir"
  } else {
    Write-Info "用户 PATH 里已有：$Dir"
  }
  if (($env:Path.Split(';') -notcontains $Dir)) {
    $env:Path = "$Dir;$env:Path"
  }
}

# 改完用户 PATH 还要通知一下资源管理器：从「开始菜单 / 桌面」新开的终端是
# 继承 Explorer 的环境块的，不广播 WM_SETTINGCHANGE 的话，新窗口里仍然
# 找不到 clawbench（要等注销重登才生效）。失败不影响已写入的 PATH。
function Send-EnvironmentChanged {
  try {
    if (-not ('Clawbench.NativeMethods' -as [type])) {
      Add-Type -Namespace Clawbench -Name NativeMethods -MemberDefinition @'
[System.Runtime.InteropServices.DllImport("user32.dll", SetLastError = true, CharSet = System.Runtime.InteropServices.CharSet.Auto)]
public static extern System.IntPtr SendMessageTimeout(System.IntPtr hWnd, uint Msg, System.UIntPtr wParam, string lParam, uint fuFlags, uint uTimeout, out System.UIntPtr lpdwResult);
'@
    }
    $result = [System.UIntPtr]::Zero
    [void][Clawbench.NativeMethods]::SendMessageTimeout(
      [System.IntPtr]0xffff, 0x001A, [System.UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$result)
  } catch {
    # 受限环境（AppLocker 等）编译不了 C#：PATH 已经写进注册表了，只是要注销重登才生效
  }
}

Add-UserPath $NodeDir
Add-UserPath $NpmPrefix
Send-EnvironmentChanged

# ---------- 收尾 ----------

Write-Host ''
Write-Host 'ClawBench 装好了。' -ForegroundColor Green
Write-Host ''
Write-Host '启动（新开一个 PowerShell 窗口，然后执行）：'
Write-Host '    clawbench'
Write-Host ''
Write-Host '然后在浏览器打开：'
Write-Host '    http://localhost:20000'
Write-Host ''
Write-Host '首次启动会把随机密码打印在窗口里，请保存好。'
Write-Host ''
Write-Host '（万一新窗口里提示找不到 clawbench，注销重登一次或重启电脑即可。）'
Write-Host ''
Write-Host 'AI 智能体不在这里装。启动后打开网页，欢迎界面里每个智能体都有「安装」按钮，'
Write-Host '点一下就在这台机器上装好（同样走国内镜像）。'
