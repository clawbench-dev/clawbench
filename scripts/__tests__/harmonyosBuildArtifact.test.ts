import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const workflow = readFileSync(resolve('.github/workflows/harmonyos.yml'), 'utf8')
const script = readFileSync(resolve('scripts/build-harmonyos.ps1'), 'utf8')

describe('HarmonyOS HAP artifact handoff', () => {
  it('uploads the unsigned HAP from the build script export directory', () => {
    const directory = /\$artifactDirectory\s*=\s*Join-Path\s+\$PSScriptRoot\s+'\.\.\/([^']+)'/.exec(script)?.[1]
    expect(directory).toBeTruthy()
    expect(script).toContain('$appName-$appVersion-$signature.hap')
    expect(script).toContain("$_.BaseName.EndsWith('-unsigned')")
    const upload = workflow.slice(workflow.indexOf('- uses: actions/upload-artifact@'))
    expect(upload).toContain(`path: ${directory}/*-unsigned.hap`)
    expect(upload).toContain('if-no-files-found: error')
  })

  it('keeps SDK execution limited to the explicitly enabled manual job', () => {
    const build = workflow.slice(workflow.indexOf('  hap-build:'))
    expect(build).toContain("if: github.event_name == 'workflow_dispatch' && inputs.build_hap")
    expect(build).toContain('./scripts/build-harmonyos.ps1 -DevEcoRoot $env:DEVECO_ROOT -Mode debug')
    expect(build).toContain('persist-credentials: false')
  })
})
