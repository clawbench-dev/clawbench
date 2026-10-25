/**
 * Agent SVG icon data mapping — sourced from @lobehub/icons-static-svg npm package.
 * Maps backend ID → { svg: inner HTML for <svg>, viewBox }
 * Backends without a logo fall back to the initial letter in AgentIcon.vue.
 *
 * Most icons use ?raw imports from the npm package. VeCLI is not available
 * in lobe-icons and is kept as an inline fallback.
 *
 * Monochrome icons (fill="currentColor") inherit CSS `color` property,
 * which adapts to light/dark theme automatically via CSS variables.
 */

// Color icons (have baked-in brand colors)
import claudeIcon from '@lobehub/icons-static-svg/icons/claude-color.svg?raw'
import codebuddyIcon from '@lobehub/icons-static-svg/icons/codebuddy-color.svg?raw'
import codexIcon from '@lobehub/icons-static-svg/icons/codex-color.svg?raw'
import copilotIcon from '@lobehub/icons-static-svg/icons/copilot-color.svg?raw'
import deepseekIcon from '@lobehub/icons-static-svg/icons/deepseek-color.svg?raw'
import qoderIcon from '@lobehub/icons-static-svg/icons/qoder-color.svg?raw'
import mimoIcon from '@lobehub/icons-static-svg/icons/xiaomimimo.svg?raw'
import antigravityIcon from '@lobehub/icons-static-svg/icons/antigravity-color.svg?raw'
import openclawIcon from '@lobehub/icons-static-svg/icons/openclaw-color.svg?raw'

// Monochrome icons (fill="currentColor" — inherit CSS `color`, theme-aware)
import opencodeIcon from '@lobehub/icons-static-svg/icons/opencode.svg?raw'
import piIcon from '@lobehub/icons-static-svg/icons/pi.svg?raw'
import grokIcon from '@lobehub/icons-static-svg/icons/grok.svg?raw'
import hermesIcon from '@lobehub/icons-static-svg/icons/hermesagent.svg?raw'
import cursorIcon from '@lobehub/icons-static-svg/icons/cursor.svg?raw'
import kimiIcon from '@lobehub/icons-static-svg/icons/kimi.svg?raw'


// Command Code (Langbase) is not in lobe-icons. Its official mark ships inside
// the CLI's own VS Code extension (vsix/commandcode-vscode.vsix →
// extension/icons/icon-light.svg) as a monochrome square knot; the fixed fills
// (#424242 / white) are rewritten to currentColor so the shared mono treatment
// handles both themes.
const commandcodeIcon = '<path fill-rule="evenodd" clip-rule="evenodd" d="M73.1399 5.87207H70.4648C53.6901 5.87207 41.6998 5.88454 32.5877 7.10962C23.6446 8.312 18.3583 10.5847 14.4715 14.4715C10.5847 18.3583 8.312 23.6446 7.10962 32.5877C5.88454 41.6998 5.87207 53.6901 5.87207 70.4648V73.1398C5.87207 89.9145 5.88454 101.905 7.10962 111.017C8.312 119.96 10.5847 125.246 14.4715 129.133C18.3583 133.02 23.6446 135.293 32.5877 136.495C41.6998 137.72 53.6901 137.733 70.4648 137.733H73.1398C89.9145 137.733 101.905 137.72 111.017 136.495C119.96 135.293 125.246 133.02 129.133 129.133C133.02 125.246 135.293 119.96 136.495 111.017C137.72 101.905 137.733 89.9146 137.733 73.1399V70.4648C137.733 53.6901 137.72 41.6998 136.495 32.5877C135.293 23.6446 133.02 18.3583 129.133 14.4715C125.246 10.5847 119.96 8.312 111.017 7.10962C101.905 5.88454 89.9146 5.87207 73.1399 5.87207ZM10.3193 10.3193C0 20.6387 0 37.2474 0 70.4648V73.1398C0 106.357 0 122.966 10.3193 133.285C20.6387 143.605 37.2474 143.605 70.4648 143.605H73.1398C106.357 143.605 122.966 143.605 133.285 133.285C143.605 122.966 143.605 106.357 143.605 73.1399V70.4648C143.605 37.2474 143.605 20.6387 133.285 10.3193C122.966 0 106.357 0 73.1399 0H70.4648C37.2474 0 20.6387 0 10.3193 10.3193Z" fill="currentColor"/> <path d="M98.8049 27.6163C89.3295 27.6163 81.6214 35.3243 81.6214 44.7997V52.1641H61.9832V44.7997C61.9832 35.3243 54.2752 27.6163 44.7997 27.6163C35.3243 27.6163 27.6163 35.3243 27.6163 44.7997C27.6163 54.2752 35.3243 61.9832 44.7997 61.9832H52.1641V81.6214H44.7997C35.3243 81.6214 27.6163 89.3295 27.6163 98.8049C27.6163 108.28 35.3243 115.988 44.7997 115.988C54.2752 115.988 61.9832 108.28 61.9832 98.8049V91.4406H81.6214V98.8049C81.6214 108.28 89.3295 115.988 98.8049 115.988C108.28 115.988 115.988 108.28 115.988 98.8049C115.988 89.3295 108.28 81.6214 98.8049 81.6214H91.4406V61.9832H98.8049C108.28 61.9832 115.988 54.2752 115.988 44.7997C115.988 35.3243 108.28 27.6163 98.8049 27.6163ZM91.4406 52.1641V44.7997C91.4406 40.7248 94.73 37.4354 98.8049 37.4354C102.88 37.4354 106.169 40.7248 106.169 44.7997C106.169 48.8747 102.88 52.1641 98.8049 52.1641H91.4406ZM44.7997 52.1641C40.7248 52.1641 37.4354 48.8747 37.4354 44.7997C37.4354 40.7248 40.7248 37.4354 44.7997 37.4354C48.8747 37.4354 52.1641 40.7248 52.1641 44.7997V52.1641H44.7997ZM61.9832 81.6214V61.9832H81.6214V81.6214H61.9832ZM98.8049 106.169C94.73 106.169 91.4406 102.88 91.4406 98.8049V91.4406H98.8049C102.88 91.4406 106.169 94.73 106.169 98.8049C106.169 102.88 102.88 106.169 98.8049 106.169ZM44.7997 106.169C40.7248 106.169 37.4354 102.88 37.4354 98.8049C37.4354 94.73 40.7248 91.4406 44.7997 91.4406H52.1641V98.8049C52.1641 102.88 48.8747 106.169 44.7997 106.169Z" fill="currentColor"/>'

import { extractSvgInner, extractViewBox } from '@/utils/providerIcons'

interface AgentSvgData {
    svg: string
    viewBox: string
    /** If true, the icon needs a contrasting background for visibility */
    needsBg?: boolean
    /** CSS class name for monochrome icons that need theme-aware color */
    monoCssClass?: string
}

const agentSvgMap: Record<string, AgentSvgData> = {
    // Color icons
    claude: { svg: extractSvgInner(claudeIcon), viewBox: extractViewBox(claudeIcon) },
    codebuddy: { svg: extractSvgInner(codebuddyIcon), viewBox: extractViewBox(codebuddyIcon) },
    codex: { svg: extractSvgInner(codexIcon), viewBox: extractViewBox(codexIcon) },
    copilot: { svg: extractSvgInner(copilotIcon), viewBox: extractViewBox(copilotIcon) },
    qoder: { svg: extractSvgInner(qoderIcon), viewBox: extractViewBox(qoderIcon) },
    deepseek: { svg: extractSvgInner(deepseekIcon), viewBox: extractViewBox(deepseekIcon) },
    // DeepSeek Harness is DeepSeek's official agent, so it shares the DeepSeek mark.
    dsh: { svg: extractSvgInner(deepseekIcon), viewBox: extractViewBox(deepseekIcon) },
    // OpenClaw's official mark is the red lobster; lobe-icons ships it as a
    // color SVG with baked-in gradient fills.
    openclaw: { svg: extractSvgInner(openclawIcon), viewBox: extractViewBox(openclawIcon) },

    // Monochrome icons (currentColor — CSS `color` controls theme adaptation)
    opencode: { svg: extractSvgInner(opencodeIcon), viewBox: extractViewBox(opencodeIcon), needsBg: true, monoCssClass: 'mono-opencode' },
    pi: { svg: extractSvgInner(piIcon), viewBox: extractViewBox(piIcon), needsBg: true, monoCssClass: 'mono-pi' },
    mimo: { svg: extractSvgInner(mimoIcon), viewBox: extractViewBox(mimoIcon), needsBg: true, monoCssClass: 'mono-mimo' },
    grok: { svg: extractSvgInner(grokIcon), viewBox: extractViewBox(grokIcon), needsBg: true, monoCssClass: 'mono-grok' },
    antigravity: { svg: extractSvgInner(antigravityIcon), viewBox: extractViewBox(antigravityIcon) },
    // Hermes Agent's mark is monochrome (fill="currentColor"), like the other
    // mono backends, so it carries a theme-aware color class.
    hermes: { svg: extractSvgInner(hermesIcon), viewBox: extractViewBox(hermesIcon), needsBg: true, monoCssClass: 'mono-hermes' },
    // Cursor's mark is monochrome (fill="currentColor"), so it needs the same
    // theme-aware color treatment as the other mono backends.
    cursor: { svg: extractSvgInner(cursorIcon), viewBox: extractViewBox(cursorIcon), needsBg: true, monoCssClass: 'mono-cursor' },
    // Kimi's "-color" variant paints the K glyph pure white on the assumption
    // of a blue tile behind it; lobe-icons ships no such tile, so on a light
    // background the glyph vanishes (only the blue dot survives). The mono
    // variant in Kimi's own brand blue is legible on both themes, so it needs
    // no contrasting backdrop.
    kimi: { svg: extractSvgInner(kimiIcon), viewBox: extractViewBox(kimiIcon), monoCssClass: 'mono-kimi' },

    // Command Code's mark is monochrome, so it gets the theme-aware mono class.
    commandcode: { svg: commandcodeIcon, viewBox: '0 0 144 144', needsBg: true, monoCssClass: 'mono-commandcode' },

    // VeCLI not in lobe-icons — kept as inline fallback (explicit fill colors)
    vecli: {
        svg: '<path d="M19.44 10.153l-2.936 11.586a.215.215 0 00.214.261h5.87a.215.215 0 00.214-.261l-2.95-11.586a.214.214 0 00-.412 0zM3.28 12.778l-2.275 8.96A.214.214 0 001.22 22h4.532a.212.212 0 00.214-.165.214.214 0 000-.097l-2.276-8.96a.214.214 0 00-.41 0z" fill="#00E5E5"/><path d="M7.29 5.359L3.148 21.738a.215.215 0 00.203.261h8.29a.214.214 0 00.215-.261L7.7 5.358a.214.214 0 00-.41 0z" fill="#006EFF"/><path d="M14.44.15a.214.214 0 00-.41 0L8.366 21.739a.214.214 0 00.214.261H19.9a.216.216 0 00.171-.078.214.214 0 00.044-.183L14.439.15z" fill="#006EFF"/><path d="M10.278 7.741L6.685 21.736a.214.214 0 00.214-.264h7.17a.215.215 0 00.214-.264L10.688 7.741a.214.214 0 00-.41 0z" fill="#00E5E5"/>',
        viewBox: '0 0 24 24',
    },

    // User-defined ACP agents (issue #557) all share the `custom-acp` backend
    // id, so no single brand mark can stand for them — and without an entry
    // here every one of them fell back to an initial letter, which reads as a
    // broken/missing icon rather than "your own agent". This is a generic
    // terminal prompt (`>_`): what these agents actually are is a spawn command
    // speaking ACP over stdio. Deliberately monochrome via currentColor so it
    // adapts to all 36 themes instead of inventing a brand colour for a
    // concept that has none. Each agent's own name still rides alongside the
    // icon in every surface that shows it, and a user-configured avatar (if
    // any) outranks this in AgentIcon.
    'custom-acp': {
        svg: '<path d="M3.6 4.8h16.8a1.2 1.2 0 011.2 1.2v12a1.2 1.2 0 01-1.2 1.2H3.6a1.2 1.2 0 01-1.2-1.2V6a1.2 1.2 0 011.2-1.2zm0 1.8a.3.3 0 00-.3.3v9.4a.3.3 0 00.3.3h16.8a.3.3 0 00.3-.3V6.9a.3.3 0 00-.3-.3H3.6z" fill="currentColor"/><path d="M6.34 8.86a.9.9 0 011.27.06l2.2 2.42a.9.9 0 010 1.2l-2.2 2.42a.9.9 0 11-1.33-1.2l1.79-1.97-1.79-1.97a.9.9 0 01.06-1.27z" fill="currentColor"/><rect x="11.6" y="13.3" width="5" height="1.8" rx=".9" fill="currentColor"/>',
        viewBox: '0 0 24 24',
        monoCssClass: 'mono-custom-acp',
    },
}

/**
 * Get SVG icon data for a backend ID.
 * Returns null if no logo is available (caller should fall back to initial letter).
 */
export function getAgentSvg(backendId: string): AgentSvgData | null {
    return agentSvgMap[backendId] ?? null
}

// Re-export the AgentSvgData type for backward compatibility
export type { AgentSvgData }
