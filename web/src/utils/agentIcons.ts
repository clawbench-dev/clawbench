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
import kimiIcon from '@lobehub/icons-static-svg/icons/kimi-color.svg?raw'
import deepseekIcon from '@lobehub/icons-static-svg/icons/deepseek-color.svg?raw'
import qoderIcon from '@lobehub/icons-static-svg/icons/qoder-color.svg?raw'
import mimoIcon from '@lobehub/icons-static-svg/icons/xiaomimimo.svg?raw'

// Monochrome icons (fill="currentColor" — inherit CSS `color`, theme-aware)
import opencodeIcon from '@lobehub/icons-static-svg/icons/opencode.svg?raw'
import piIcon from '@lobehub/icons-static-svg/icons/pi.svg?raw'
import grokIcon from '@lobehub/icons-static-svg/icons/grok.svg?raw'

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
    kimi: { svg: extractSvgInner(kimiIcon), viewBox: extractViewBox(kimiIcon) },
    deepseek: { svg: extractSvgInner(deepseekIcon), viewBox: extractViewBox(deepseekIcon) },

    // Monochrome icons (currentColor — CSS `color` controls theme adaptation)
    opencode: { svg: extractSvgInner(opencodeIcon), viewBox: extractViewBox(opencodeIcon), needsBg: true, monoCssClass: 'mono-opencode' },
    pi: { svg: extractSvgInner(piIcon), viewBox: extractViewBox(piIcon), needsBg: true, monoCssClass: 'mono-pi' },
    mimo: { svg: extractSvgInner(mimoIcon), viewBox: extractViewBox(mimoIcon), needsBg: true, monoCssClass: 'mono-mimo' },
    grok: { svg: extractSvgInner(grokIcon), viewBox: extractViewBox(grokIcon), needsBg: true, monoCssClass: 'mono-grok' },

    // VeCLI not in lobe-icons — kept as inline fallback (explicit fill colors)
    vecli: {
        svg: '<path d="M19.44 10.153l-2.936 11.586a.215.215 0 00.214.261h5.87a.215.215 0 00.214-.261l-2.95-11.586a.214.214 0 00-.412 0zM3.28 12.778l-2.275 8.96A.214.214 0 001.22 22h4.532a.212.212 0 00.214-.165.214.214 0 000-.097l-2.276-8.96a.214.214 0 00-.41 0z" fill="#00E5E5"/><path d="M7.29 5.359L3.148 21.738a.215.215 0 00.203.261h8.29a.214.214 0 00.215-.261L7.7 5.358a.214.214 0 00-.41 0z" fill="#006EFF"/><path d="M14.44.15a.214.214 0 00-.41 0L8.366 21.739a.214.214 0 00.214.261H19.9a.216.216 0 00.171-.078.214.214 0 00.044-.183L14.439.15z" fill="#006EFF"/><path d="M10.278 7.741L6.685 21.736a.214.214 0 00.214-.264h7.17a.215.215 0 00.214-.264L10.688 7.741a.214.214 0 00-.41 0z" fill="#00E5E5"/>',
        viewBox: '0 0 24 24',
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
