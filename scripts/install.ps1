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
#   # 固定版本（默认装最新版；需要可复现的安装时指定版本号）
#   $env:CLAWBENCH_VERSION = '0.130.0'; irm https://clawbench.pages.dev/install.ps1 | iex
#
#   # 校验脚本本身后再跑（sha256 在 GitHub Release 的说明里给出）
#   irm https://clawbench.pages.dev/install.ps1 -OutFile install.ps1
#   (Get-FileHash .\install.ps1 -Algorithm SHA256).Hash
#
#   # 卸载（删安装目录 + 从用户 PATH 移除）
#   & ([scriptblock]::Create((irm https://clawbench.pages.dev/install.ps1))) -Uninstall
#
# 安装位置（可用环境变量 CLAWBENCH_HOME 覆盖）：
#   %LOCALAPPDATA%\clawbench\node   Node.js
#   %LOCALAPPDATA%\clawbench\npm    全局 npm 前缀（clawbench 在这里）

param(
  # 卸载：删安装目录、从用户 PATH 移除、清 npm 缓存链接。任何一步失败只警告。
  [switch]$Uninstall,
  # 固定版本（例如 0.130.0）；等价于设置环境变量 CLAWBENCH_VERSION。
  [string]$Version,
  # 显示帮助后退出。
  [switch]$Help
)

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
# 固定版本（不带 v 前缀，与 npm 语义化版本一致）。留空 = 装最新版。
# 优先取 -Version 参数，其次取环境变量（`irm | iex` 没法传参数）。
$FixedVersion = if ($Version) { $Version } elseif ($env:CLAWBENCH_VERSION) { $env:CLAWBENCH_VERSION } else { '' }

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

function Write-Warn([string]$Message) {
  Write-Host "警告: $Message" -ForegroundColor Yellow
}

function Fail([string]$Message) {
  # 用 throw 而不是 exit：脚本通常以 `irm | iex` 在当前窗口里跑，
  # exit 会把这个窗口一起关掉，用户就看不到错误原因了。
  throw "ClawBench 安装失败：$Message"
}

# ---------- 帮助 / 卸载 ----------

function Show-Help {
  @'
ClawBench 一键安装脚本（Windows / PowerShell）

用法（先在 PowerShell 窗口里执行）：
  irm https://clawbench.pages.dev/install.ps1 | iex          安装
  $env:CLAWBENCH_VERSION = '0.130.0'; irm ... | iex           固定版本安装
  .\install.ps1 -Uninstall                                    卸载（删安装目录 + 移出用户 PATH）
  .\install.ps1 -Help                                         显示这段帮助

环境变量：
  CLAWBENCH_HOME      安装目录，默认 %LOCALAPPDATA%\clawbench
  CLAWBENCH_VERSION   固定版本，例如 0.130.0；留空装最新版

说明：
  只装 win32-x64 平台包。32 位 Windows 与未启用 x64 模拟的 ARM64 设备不支持。
  卸载不会动用户数据（会话、配置、数据目录）。
'@ | Write-Host
}

function Remove-FromUserPath([string]$Dir) {
  # 只删我们自己加的那一项，PATH 里其他内容原样保留。
  try {
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not $current) { return $false }
    $parts = @($current.Split(';') | Where-Object { $_ })
    # Windows 路径不区分大小写，写成 -contains 会漏掉大小写不同的同一目录
    $kept = @($parts | Where-Object { $_.TrimEnd('\') -ne $Dir.TrimEnd('\') })
    if ($kept.Count -eq $parts.Count) { return $false }
    [Environment]::SetEnvironmentVariable('Path', ($kept -join ';'), 'User')
    return $true
  } catch {
    Write-Warn "没能从用户 PATH 移除 $Dir（$($_.Exception.Message)），请手动删除"
    return $false
  }
}

function Uninstall-ClawBench {
  Write-Step '卸载 ClawBench'

  if (Test-Path $ClawbenchHome) {
    try {
      Remove-Item -Path $ClawbenchHome -Recurse -Force -ErrorAction Stop
      Write-Ok "已删除安装目录 $ClawbenchHome"
    } catch {
      # 目录里可能正有 clawbench 在跑（文件被占用），也可能有权限问题。
      # 卸载要尽量做完其余步骤，不要停在这里。
      Write-Warn "删不掉 $ClawbenchHome（$($_.Exception.Message)）。先关掉正在运行的 clawbench，再执行：Remove-Item -Recurse -Force '$ClawbenchHome'"
    }
  } else {
    Write-Info "安装目录不存在（$ClawbenchHome），跳过"
  }

  if (Remove-FromUserPath $NodeDir) { Write-Ok "已从用户 PATH 移除 $NodeDir" } else { Write-Info "用户 PATH 里没有 $NodeDir，跳过" }
  if (Remove-FromUserPath $NpmPrefix) { Write-Ok "已从用户 PATH 移除 $NpmPrefix" } else { Write-Info "用户 PATH 里没有 $NpmPrefix，跳过" }

  Write-Host ''
  Write-Host '卸载完成。' -ForegroundColor Green
  Write-Host ''
  Write-Host '用户数据（会话、配置、.clawbench 数据目录）没有动，还在原来的位置。'
  Write-Host '已经开着的 PowerShell 窗口里 PATH 还是旧的，重开一个窗口即可。'
}

if ($Help) { Show-Help; return }
if ($Uninstall) { Uninstall-ClawBench; return }

# ---------- 平台 ----------

$arch = $env:PROCESSOR_ARCHITECTURE
if ($env:PROCESSOR_ARCHITEW6432) {
  # 32 位 PowerShell 跑在 64 位系统上时，PROCESSOR_ARCHITECTURE 是 x86
  $arch = $env:PROCESSOR_ARCHITEW6432
}

$forceOwnNode = $false
$checkEmulation = $false
switch ($arch) {
  'AMD64' { $nodeArch = 'x64' }
  'ARM64' {
    # ClawBench 的 npm 包只有 win32-x64，没有 win32-arm64；
    # Windows 11 ARM 能透明模拟运行 x64 程序，所以这里装 x64 版。
    # 但模拟可以被组策略/注册表关掉（企业环境常见），那时 node.exe 根本起不来，
    # 所以下面装完要真跑一次 -v 来确认，而不是假设备注成立。
    $nodeArch = 'x64'
    $forceOwnNode = $true
    $checkEmulation = $true
    Write-Info 'Windows ARM64：安装 x64 版本（系统会自动模拟运行）'
  }
  default {
    if ($arch -eq 'x86') {
      Fail @"
这是 32 位系统（$arch），而 ClawBench 只发布 64 位平台包（win32-x64）。
  请换用 64 位 Windows；如果是 64 位系统上开了 32 位 PowerShell，
  改用「Windows PowerShell」或 PowerShell 7 的 64 位版本重跑本脚本。
  也可以用安装包：https://github.com/clawbench-dev/clawbench/releases
"@
    }
    Fail "不支持的 CPU 架构：$arch。ClawBench 只发布 win32-x64 平台包"
  }
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

# 跑一次 node.exe -v 并判断输出是不是真的版本号。返回值是版本字符串；
# 跑不起来（缺 DLL、架构不对、模拟被关）返回 $null。
# 为什么非要看输出而不是看退出码：Windows 上「不是有效的 Win32 应用程序」
# 这类错误 exit code 可能是 0，而 $ErrorActionPreference='Stop' 时
# 原生程序的 stderr 又会被当成异常——两种情况都不能当作「装好了」。
function Get-NodeVersion([string]$Exe) {
  try {
    $out = (& $Exe -v 2>&1 | Out-String).Trim()
  } catch {
    return $null
  }
  if ($LASTEXITCODE -ne 0) { return $null }
  if ($out -match '^v\d+\.\d+\.\d+') { return ($out -split '\s+')[0] }
  return $null
}

if (Test-Path (Join-Path $NodeDir 'node.exe')) {
  $nodeExe = Join-Path $NodeDir 'node.exe'
  $npmCmd = Join-Path $NodeDir 'npm.cmd'
  $bundled = Get-NodeVersion $nodeExe
  if (-not $bundled) {
    Fail "已有的 Node.js 起不来：$nodeExe`n  可以删掉 $NodeDir 后重跑本脚本，或用安装包：https://github.com/clawbench-dev/clawbench/releases"
  }
  Write-Info "已有本脚本装的 Node.js $bundled，直接使用"
} elseif (-not $forceOwnNode) {
  # ARM64 上不复用系统 Node：原生 arm64 的 npm 会去装不存在的 win32-arm64 包
  $systemNode = Get-Command node.exe -ErrorAction SilentlyContinue
  $systemNpm = Get-Command npm.cmd -ErrorAction SilentlyContinue
  if ($systemNode -and $systemNpm) {
    $current = Get-NodeVersion $systemNode.Source
    $major = 0
    $majorText = if ($current) { $current.TrimStart('v').Split('.')[0] } else { '0' }
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

  # 取最新 LTS，而不是「数组里第一条带 lts 的」：Invoke-RestMethod 解析出来的
  # 顺序不是接口契约（换实现、换代理、换 CDN 都可能变），按顺序取会静默装到旧版。
  # 判据只看接口给的 `lts` 字段（值是真名或 $false），不靠「偶数码」猜：
  # v24 在进入 LTS 线之前同样是 false。之后再按**版本号语义**取最大，
  # 不能用字符串比较（"22.9" > "22.20"）。
  $ltsVersions = @($index | Where-Object {
      $_.version -and
      $_.lts -and
      ([int]($_.version.TrimStart('v').Split('.')[0]) -ge $NodeMinMajor)
    })
  if (-not $ltsVersions) { Fail '解析 Node.js 版本列表失败（没有找到 LTS 版本）' }

  $lts = $ltsVersions | Sort-Object -Property {
    $parts = @($_.version.TrimStart('v').Split('.') | ForEach-Object { [int]$_ })
    [version](($parts + @(0, 0))[0..2] -join '.')
  } -Descending | Select-Object -First 1

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
    $installed = Get-NodeVersion $nodeExe
    if (-not $installed) {
      # 解压出来的 node.exe 跑不起来：在 ARM64 上最常见的原因是 x64 模拟被关掉；
      # 在 AMD64 上通常是缺 VC++ 运行库。两种都给用户指路，不要静默报成功。
      $hint = if ($checkEmulation) {
        @"
这通常是 Windows ARM64 上没启用 x64 模拟（WOW64 / Prism）。
  本脚本装的是 Node.js 的 win-x64 版本，模拟关掉时它起不来。
  请在「设置 → 系统 → 系统信息」确认系统类型，或由管理员检查
  HKLM\SYSTEM\CurrentControlSet\Control\Session Manager\Image File Execution Options
  下的 IgnoreWow64 策略；也可以改用安装包：
  https://github.com/clawbench-dev/clawbench/releases
"@
      } else {
        "这通常是缺少「Microsoft Visual C++ 可再发行组件」。安装后再执行：npm install -g $NpmPackage"
      }
      Fail "Node.js 解压完成但启动不了：`n  node.exe -v 没有输出有效版本号`n  $hint"
    }
    if ($checkEmulation) {
      Write-Ok "x64 模拟可用（node $installed）"
    }
    Write-Ok "Node.js $installed 已就位：$NodeDir"
  } finally {
    Remove-Item -Path $tmp -Recurse -Force -ErrorAction SilentlyContinue
  }
}

$env:Path = "$NodeDir;$env:Path"

if (-not (Test-Path $npmCmd)) { Fail "找不到 npm：$npmCmd" }

# ---------- ClawBench ----------

Write-Step '安装 ClawBench（npm 全局包）'

# 默认 latest（每次安装装到的版本可能不同——脚本不保证可复现）；
# 要复现就设 CLAWBENCH_VERSION 或传 -Version。npm 会自校验 tarball 的
# dist.integrity，脚本这里不重复做。
if ($FixedVersion) {
  $npmSpec = "$NpmPackage@$FixedVersion"
  Write-Info "固定版本：$FixedVersion"
} else {
  $npmSpec = "$NpmPackage@latest"
  Write-Info '下载量约 150MB，国内镜像下通常一两分钟（装最新版；要可复现请设 CLAWBENCH_VERSION）'
}
New-Item -ItemType Directory -Path $NpmPrefix -Force | Out-Null

& $npmCmd install -g $npmSpec `
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

# stderr 必须一起收：平台包缺失、DLL 缺失时，真正的原因全在 stderr 里，
# 只留一句「多半是平台包没装上」等于把用户唯一能自助的线索扔掉。
# 2>&1 在 $ErrorActionPreference='Stop' 下会把 stderr 变成异常，所以要包一层：
# 先临时放开，读完退出码再恢复。
$versionText = $null
$versionErr = $null
try {
  $oldEap = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  try {
    $versionText = (& $clawbenchCmd --version 2>&1 | Out-String).Trim()
    $versionCode = $LASTEXITCODE
  } finally {
    $ErrorActionPreference = $oldEap
  }
} catch {
  $versionErr = $_.Exception.Message
}

if ($versionErr) {
  Fail "clawbench 装上了但跑不起来：`n`n$versionErr`n`n  上面这段就是它自己的报错。请把上面的 npm 输出一起发出来"
}
if ($versionCode -ne 0 -or -not $versionText) {
  Fail "clawbench 装上了但跑不起来（退出码 $versionCode）：`n`n$versionText`n`n  上面这段就是它自己的报错。常见原因：平台包没装上（npm 输出里找 EBADPLATFORM / EBADENGINE），或者缺少 VC++ 运行库。"
}
$actualVersion = ($versionText -split "\r?\n")[0].Trim()
# 实际装上的版本号：npm 可能解析到比预期更新的版本（或镜像滞后），
# 打印二进制自己报的版本，而不是我们请求的那个。
Write-Ok "clawbench $actualVersion"

# ---------- PATH ----------

Write-Step '把命令加进 PATH'

function Add-UserPath([string]$Dir) {
  try {
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not $current) { $current = '' }
    $parts = @($current.Split(';') | Where-Object { $_ })
    if ($parts -notcontains $Dir) {
      [Environment]::SetEnvironmentVariable('Path', (($parts + $Dir) -join ';'), 'User')
      Write-Info "已写入用户 PATH：$Dir"
    } else {
      Write-Info "用户 PATH 里已有：$Dir"
    }
  } catch {
    # 组策略可能锁住了用户环境变量。这时不能把整个安装判为失败——
    # 东西已经装好了，只是要用户自己加 PATH。
    Write-Warn "写不进用户 PATH（$($_.Exception.Message)）。请手动把下面两个目录加到 PATH："
    Write-Host "    $Dir"
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
Write-Host "ClawBench $actualVersion 装好了。" -ForegroundColor Green
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
Write-Host ''
Write-Host '卸载：irm https://clawbench.pages.dev/install.ps1 存成文件后执行 .\install.ps1 -Uninstall'
