import { describe, expect, it } from 'vitest'
import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Guard for the one-line installers (`scripts/install.sh` / `install.ps1`).
 *
 * These two files are the first thing a new user runs, and they are shipped
 * from the landing site rather than from the npm package — so nothing else in
 * the repo exercises them. A silent break here (a typo in the mirror URL, a
 * missing `--prefix`, a shell syntax error) only surfaces when a stranger
 * pipes the script into their shell.
 *
 * The checks are deliberately split:
 *   - install.sh gets a real `bash -n` parse (cheap, catches the whole class of
 *     syntax breakage that a text assertion cannot).
 *   - Everything else is a text invariant: the exact strings the install
 *     depends on (mirror host, package name, prefix flag) and the wiring in the
 *     two landing workflows that publishes them.
 */

function repoPath(rel: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    const p = resolve(base, rel)
    if (existsSync(p)) return p
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

const INSTALL_SH = repoPath('scripts/install.sh')
const INSTALL_PS1 = repoPath('scripts/install.ps1')

function read(rel: string): string {
  return readFileSync(repoPath(rel), 'utf8')
}

/** Slice out the `Stage site` step body of a landing workflow. */
function stageSiteStep(workflow: string): string {
  const src = read(workflow)
  const start = src.indexOf('- name: Stage site')
  expect(start, `${workflow} 里没有 Stage site 步骤`).toBeGreaterThan(-1)
  const rest = src.slice(start)
  const next = rest.indexOf('\n      - ', 1)
  return next === -1 ? rest : rest.slice(0, next)
}

describe('install.sh', () => {
  it('通过 bash 语法检查', () => {
    expect(() => execFileSync('bash', ['-n', INSTALL_SH], { stdio: 'pipe' })).not.toThrow()
  })

  it('失败必须中断，不能把半截安装当成功', () => {
    const src = readFileSync(INSTALL_SH, 'utf8')
    expect(src).toContain('set -euo pipefail')
  })

  it('Node.js 和 clawbench 都装进自己的目录，不写系统目录', () => {
    const src = readFileSync(INSTALL_SH, 'utf8')
    // npm 全局前缀必须是显式的：不传 --prefix 就会往系统 node 的前缀里装，
    // 非 root 用户大概率 EACCES，而我们的 PATH 里也找不到它。
    expect(src).toContain('--prefix "$NPM_PREFIX"')
    expect(src).toContain('@xulongzhe/clawbench')
    expect(src).toContain('XDG_DATA_HOME')
    // 不能真的调用 sudo（这个词只应出现在「别用 sudo」的警告文案里）
    expect(src).not.toMatch(/^\s*sudo\s/m)
  })

  it('国内镜像和官方源都留了退路', () => {
    const src = readFileSync(INSTALL_SH, 'utf8')
    expect(src).toContain('registry.npmmirror.com')
    expect(src).toContain('https://nodejs.org/dist')
    expect(src).toContain('https://registry.npmjs.org')
  })

  it('PATH 只写一次（重复执行不会把 rc 文件撑爆）', () => {
    const src = readFileSync(INSTALL_SH, 'utf8')
    // 写入前先 grep 同一个标记；两处用的是同一个常量字符串
    expect(src).toContain('>>> clawbench installer >>>')
    expect(src).toContain("grep -q '>>> clawbench installer >>>'")
  })
})

describe('install.ps1', () => {
  it('带 UTF-8 BOM，否则 PowerShell 5.1 会把中文按 GBK 解码', () => {
    // Windows PowerShell 5.1 读没有 BOM 的脚本时用系统 ANSI 代码页（中文机器上
    // 是 GBK）。UTF-8 的中文按 GBK 两两配对时会把后面的引号、花括号一起吃掉，
    // 报出来的是「switch 语句的子句中缺少语句块」这种跟中文毫无关系的语法错。
    // 带 BOM 时 5.1 才会按 UTF-8 解码；`irm | iex` 那条路 StreamReader 也会
    // 识别 BOM 并剥掉，所以两边都安全。
    const buf = readFileSync(INSTALL_PS1)
    expect([...buf.subarray(0, 3)]).toEqual([0xef, 0xbb, 0xbf])
  })

  it('出错就停，且不关掉用户的窗口', () => {
    const src = readFileSync(INSTALL_PS1, 'utf8')
    expect(src).toContain("$ErrorActionPreference = 'Stop'")
    // `irm | iex` 在当前窗口里执行，exit 会把窗口一起关掉，用户看不到报错
    expect(src).toMatch(/function Fail[\s\S]*?throw /)
  })

  it('同样装进用户目录并使用显式 npm 前缀', () => {
    const src = readFileSync(INSTALL_PS1, 'utf8')
    expect(src).toContain('LOCALAPPDATA')
    expect(src).toContain('--prefix $NpmPrefix')
    expect(src).toContain('@xulongzhe/clawbench')
    expect(src).toContain('registry.npmmirror.com')
  })

  it('走用户级 PATH，不碰系统环境变量', () => {
    const src = readFileSync(INSTALL_PS1, 'utf8')
    expect(src).toContain("[Environment]::SetEnvironmentVariable('Path', (($parts + $Dir) -join ';'), 'User')")
  })
})

describe('landing 站点发布这两个脚本', () => {
  for (const workflow of [
    '.github/workflows/deploy-landing.yml',
    '.github/workflows/deploy-landing-pages.yml',
  ]) {
    it(`${workflow} 的 Stage site 复制安装脚本`, () => {
      const step = stageSiteStep(workflow)
      expect(step).toContain('scripts/install.sh')
      expect(step).toContain('scripts/install.ps1')
    })

    it(`${workflow} 在脚本变更时重新部署`, () => {
      const src = read(workflow)
      expect(src).toContain("'scripts/install.sh'")
      expect(src).toContain("'scripts/install.ps1'")
    })
  }

  it('install.ps1 声明 charset=utf-8（否则 PowerShell 5.1 会把中文解码成乱码）', () => {
    const headers = read('web/landing/_headers')
    expect(headers).toMatch(/\/install\.ps1[\s\S]*?Content-Type: text\/plain; charset=utf-8/)
  })
})
