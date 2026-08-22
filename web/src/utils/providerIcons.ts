/**
 * Provider icon mapping and model-to-provider detection.
 *
 * Icons sourced from @lobehub/icons-static-svg npm package.
 * - "-color" variants: have baked-in brand colors, used directly
 * - Monochrome variants (fill="currentColor"): inherit CSS `color` property,
 *   which adapts to light/dark theme automatically via CSS variables.
 *   Dark-colored monochrome icons (black, dark-gray) are assigned a
 *   `monoCssClass` so CSS can flip their color in dark mode for visibility.
 *
 * getModelProvider() uses string pattern matching on model names to detect
 * the provider. Returns null for unrecognized models (caller should fall back
 * to initial letter).
 */

// ---- Types ----

export interface ProviderIconData {
    /** Raw SVG string from the npm package */
    raw: string
    /** If true, the icon needs a contrasting background for visibility */
    needsBg?: boolean
    /** CSS class name for monochrome icons that need theme-aware color.
     *  Applied to the <svg> element so `currentColor` inherits the right color
     *  in both light and dark themes. */
    monoCssClass?: string
}

// ---- Provider slug → icon import (with ?raw for inline SVG string) ----

// Color icons (baked-in brand colors)
import claudeIcon from '@lobehub/icons-static-svg/icons/claude-color.svg?raw'
import deepseekIcon from '@lobehub/icons-static-svg/icons/deepseek-color.svg?raw'
import kimiIcon from '@lobehub/icons-static-svg/icons/kimi-color.svg?raw'
import minimaxIcon from '@lobehub/icons-static-svg/icons/minimax-color.svg?raw'
import mistralIcon from '@lobehub/icons-static-svg/icons/mistral-color.svg?raw'
import googleIcon from '@lobehub/icons-static-svg/icons/google-color.svg?raw'
import geminiIcon from '@lobehub/icons-static-svg/icons/gemini-color.svg?raw'
import metaaiIcon from '@lobehub/icons-static-svg/icons/metaai-color.svg?raw'
import huggingfaceIcon from '@lobehub/icons-static-svg/icons/huggingface-color.svg?raw'
import openrouterIcon from '@lobehub/icons-static-svg/icons/openrouter-color.svg?raw'
import cloudflareIcon from '@lobehub/icons-static-svg/icons/cloudflare-color.svg?raw'
import fireworksIcon from '@lobehub/icons-static-svg/icons/fireworks-color.svg?raw'
import cerebrasIcon from '@lobehub/icons-static-svg/icons/cerebras-color.svg?raw'
import bedrockIcon from '@lobehub/icons-static-svg/icons/bedrock-color.svg?raw'
import azureaiIcon from '@lobehub/icons-static-svg/icons/azureai-color.svg?raw'
import qwenIcon from '@lobehub/icons-static-svg/icons/qwen-color.svg?raw'
import doubaoIcon from '@lobehub/icons-static-svg/icons/doubao-color.svg?raw'
import hunyuanIcon from '@lobehub/icons-static-svg/icons/hunyuan-color.svg?raw'
import codexIcon from '@lobehub/icons-static-svg/icons/codex-color.svg?raw'
import copilotIcon from '@lobehub/icons-static-svg/icons/copilot-color.svg?raw'
import codebuddyIcon from '@lobehub/icons-static-svg/icons/codebuddy-color.svg?raw'
import mimoIcon from '@lobehub/icons-static-svg/icons/xiaomimimo.svg?raw'
import qoderIcon from '@lobehub/icons-static-svg/icons/qoder-color.svg?raw'
import zhipuIcon from '@lobehub/icons-static-svg/icons/zhipu-color.svg?raw'

// Monochrome icons (fill="currentColor" — inherit CSS `color`, theme-aware)
import openaiIcon from '@lobehub/icons-static-svg/icons/openai.svg?raw'
import anthropicIcon from '@lobehub/icons-static-svg/icons/anthropic.svg?raw'
import groqIcon from '@lobehub/icons-static-svg/icons/groq.svg?raw'
import xaiIcon from '@lobehub/icons-static-svg/icons/xai.svg?raw'
import opencodeIcon from '@lobehub/icons-static-svg/icons/opencode.svg?raw'
import piIcon from '@lobehub/icons-static-svg/icons/pi.svg?raw'

// ---- Provider icon registry ----

const providerIconMap: Record<string, ProviderIconData> = {
    // LLM providers — color icons
    claude: { raw: claudeIcon },
    google: { raw: googleIcon },
    gemini: { raw: geminiIcon },
    deepseek: { raw: deepseekIcon },
    mistral: { raw: mistralIcon },
    meta: { raw: metaaiIcon },
    metaai: { raw: metaaiIcon },
    qwen: { raw: qwenIcon },
    doubao: { raw: doubaoIcon },
    hunyuan: { raw: hunyuanIcon },
    minimax: { raw: minimaxIcon },
    kimi: { raw: kimiIcon },
    xiaomi: { raw: mimoIcon },
    mimo: { raw: mimoIcon },
    cerebras: { raw: cerebrasIcon },
    huggingface: { raw: huggingfaceIcon },
    openrouter: { raw: openrouterIcon },
    cloudflare: { raw: cloudflareIcon },
    fireworks: { raw: fireworksIcon },
    bedrock: { raw: bedrockIcon },
    azure: { raw: azureaiIcon },
    azureai: { raw: azureaiIcon },

    // LLM providers — monochrome icons
    // Dark-colored ones need needsBg + monoCssClass for theme-aware visibility
    // Bright-colored ones (Anthropic, Groq) only need monoCssClass, no background
    openai: { raw: openaiIcon, needsBg: true, monoCssClass: 'mono-openai' },
    anthropic: { raw: anthropicIcon, monoCssClass: 'mono-anthropic' },
    groq: { raw: groqIcon, monoCssClass: 'mono-groq' },
    xai: { raw: xaiIcon, needsBg: true, monoCssClass: 'mono-xai' },

    // CLI backend tools — color icons
    codex: { raw: codexIcon },
    copilot: { raw: copilotIcon },
    codebuddy: { raw: codebuddyIcon },
    qoder: { raw: qoderIcon },
    zhipu: { raw: zhipuIcon },
    chatglm: { raw: zhipuIcon },
    glm: { raw: zhipuIcon },

    // CLI backend tools — monochrome icons
    opencode: { raw: opencodeIcon, needsBg: true, monoCssClass: 'mono-opencode' },
    pi: { raw: piIcon, needsBg: true, monoCssClass: 'mono-pi' },

    // VeCLI not in lobe-icons — inline fallback (explicit fill colors, not currentColor)
    vecli: { raw: '<svg fill-rule="evenodd" height="1em" style="flex:none;line-height:1" viewBox="0 0 24 24" width="1em" xmlns="http://www.w3.org/2000/svg"><title>VeCLI</title><path d="M19.44 10.153l-2.936 11.586a.215.215 0 00.214.261h5.87a.215.215 0 00.214-.261l-2.95-11.586a.214.214 0 00-.412 0zM3.28 12.778l-2.275 8.96A.214.214 0 001.22 22h4.532a.212.212 0 00.214-.165.214.214 0 000-.097l-2.276-8.96a.214.214 0 00-.41 0z" fill="#00E5E5"/><path d="M7.29 5.359L3.148 21.738a.215.215 0 00.203.261h8.29a.214.214 0 00.215-.261L7.7 5.358a.214.214 0 00-.41 0z" fill="#006EFF"/><path d="M14.44.15a.214.214 0 00-.41 0L8.366 21.739a.214.214 0 00.214.261H19.9a.216.216 0 00.171-.078.214.214 0 00.044-.183L14.439.15z" fill="#006EFF"/><path d="M10.278 7.741L6.685 21.736a.214.214 0 00.214-.264h7.17a.215.215 0 00.214-.264L10.688 7.741a.214.214 0 00-.41 0z" fill="#00E5E5"/></svg>' },
}

// ---- Model name → provider pattern matching ----

/**
 * Pattern rules for detecting provider from model name.
 * Each entry: [regex_pattern, provider_id]
 * Tested against lowercase model name. First match wins.
 */
const modelPatterns: [RegExp, string][] = [
    // Anthropic family
    [/^claude/, 'claude'],
    [/^anthropic/, 'anthropic'],

    // OpenAI family
    [/^gpt/, 'openai'],
    [/^o[1-4]/, 'openai'],         // o1, o2, o3, o4 reasoning models
    [/^chatgpt/, 'openai'],
    [/^dall-e/, 'openai'],

    // Google family
    [/^gemini/, 'gemini'],
    [/^gemma/, 'google'],

    // DeepSeek
    [/^deepseek/, 'deepseek'],

    // Mistral
    [/^mistral/, 'mistral'],
    [/^codestral/, 'mistral'],
    [/^pixtral/, 'mistral'],

    // Meta
    [/^llama/, 'meta'],
    [/^meta-llama/, 'meta'],

    // Qwen (Alibaba)
    [/^qwen/, 'qwen'],

    // Doubao (ByteDance)
    [/^doubao/, 'doubao'],

    // Hunyuan (Tencent)
    [/^hunyuan/, 'hunyuan'],

    // MiniMax
    [/^minimax/, 'minimax'],
    [/^abab/, 'minimax'],

    // Kimi / Moonshot
    [/^moonshot/, 'kimi'],
    [/^kimi/, 'kimi'],

    // Xiaomi / MiMo
    [/^mimo/, 'xiaomi'],

    // Zhipu / GLM / ChatGLM
    [/^glm/, 'zhipu'],
    [/^chatglm/, 'chatglm'],
    [/^zhipu/, 'zhipu'],

    // Groq (models from Groq-hosted endpoints)
    [/^groq/, 'groq'],

    // xAI / Grok
    [/^grok/, 'xai'],

    // Cerebras
    [/^cerebras/, 'cerebras'],

    // Hugging Face
    [/^huggingface/, 'huggingface'],

    // OpenRouter (prefix in Pi models)
    [/^openrouter/, 'openrouter'],

    // Cloudflare
    [/^cloudflare/, 'cloudflare'],

    // Fireworks
    [/^fireworks/, 'fireworks'],

    // Amazon Bedrock
    [/^bedrock/, 'bedrock'],
    [/^amazon/, 'bedrock'],

    // Azure
    [/^azure/, 'azure'],

]

/**
 * Detect the provider ID from a model name using string pattern matching.
 * For "provider/model-name" format (e.g. "azure/gpt-4o"), the provider prefix
 * is stripped and the model-name part is matched first. If the model-name
 * yields no match, the provider prefix is used as a fallback.
 * Returns null for unrecognized model names.
 */
export function getModelProvider(modelName: string): string | null {
    if (!modelName) return null
    const lower = modelName.toLowerCase()

    // Handle "provider/model-name" format: strip provider prefix,
    // match the model-name part first, fall back to provider prefix.
    const slashIdx = lower.indexOf('/')
    if (slashIdx > 0) {
        const modelPart = lower.slice(slashIdx + 1)
        const providerPart = lower.slice(0, slashIdx)

        // Try matching the model-name part (e.g. "azure/gpt-4o" -> "gpt-4o" -> "openai")
        for (const [pattern, providerId] of modelPatterns) {
            if (pattern.test(modelPart)) return providerId
        }

        // Fall back to provider prefix (e.g. "openrouter/auto" -> "openrouter")
        if (providerIconMap[providerPart]) return providerPart
    }

    for (const [pattern, providerId] of modelPatterns) {
        if (pattern.test(lower)) return providerId
    }

    return null
}

/**
 * Get icon data for a provider ID.
 * Returns null if no icon is available (caller should fall back to initial letter).
 */
export function getProviderIcon(providerId: string): ProviderIconData | null {
    return providerIconMap[providerId] ?? null
}

/**
 * Extract the inner content of an SVG (everything inside the <svg> tag).
 * If the <svg> tag has fill="currentColor", propagates it to child elements
 * that don't have their own fill attribute — so `currentColor` still works
 * when rendered as v-html inside a Vue-controlled <svg> wrapper.
 *
 * Covers shape elements (<path>, <circle>, etc.) and container elements
 * (<g>, <defs>) that may use fill="currentColor" as group-level defaults.
 */
export function extractSvgInner(rawSvg: string): string {
    const svgTagMatch = rawSvg.match(/<svg[^>]*>([\s\S]*)<\/svg>/)
    let inner = svgTagMatch?.[1]?.trim() ?? ''

    // Check if the <svg> tag itself has fill="currentColor"
    const svgTag = rawSvg.match(/<svg[^>]*>/)?.[0] ?? ''
    const svgHasCurrentColorFill = /fill="currentColor"/.test(svgTag)

    if (svgHasCurrentColorFill) {
        // Propagate fill="currentColor" to child elements that inherit it.
        // Shape elements and group/def containers that may set group-level fill.
        inner = inner.replace(/<(path|circle|rect|ellipse|polygon|polyline|g|defs)([^>]*)>/g, (match, tag, attrs) => {
            if (/fill="/.test(attrs)) return match
            return `<${tag} fill="currentColor"${attrs}>`
        })
    }

    // Remove <title> elements from inner content — the Vue-controlled <svg>
    // already has aria-label for accessibility, so <title> creates conflict.
    inner = inner.replace(/<title>[^<]*<\/title>/g, '')

    return inner
}

/**
 * Extract viewBox from raw SVG string.
 */
export function extractViewBox(rawSvg: string): string {
    const match = rawSvg.match(/viewBox="([^"]+)"/)
    return match?.[1] ?? '0 0 24 24'
}

/**
 * Convenience: get processed SVG inner HTML for a provider.
 * Returns null if provider not found.
 */
export function getProviderSvgHtml(providerId: string): string | null {
    const data = getProviderIcon(providerId)
    if (!data) return null
    return extractSvgInner(data.raw)
}

/**
 * Build a complete SVG string for a provider, with injected attributes.
 * Returns the full <svg>...</svg> tag ready for v-html on a <span>.
 * This avoids SVG-namespace issues with v-html on an <svg> element
 * (some mobile WebViews fail to render innerHTML inside SVG).
 */
export function getProviderFullSvg(providerId: string, size: number, cssClasses: string[] = [], ariaLabel?: string): string | null {
    const data = getProviderIcon(providerId)
    if (!data) return null
    const raw = data.raw

    // Build class list
    const classList = ['provider-icon-svg']
    if (data.needsBg) classList.push('provider-icon-bg')
    if (data.monoCssClass) classList.push(data.monoCssClass)
    classList.push(...cssClasses)

    // Remove existing style attribute (has flex:none;line-height:1 from lobe-icons)
    // and replace with our own. Also remove width/height attributes.
    const svgTag = raw.match(/<svg[^>]*>/)?.[0] ?? ''
    const innerMatch = raw.match(/<svg[^>]*>([\s\S]*)<\/svg>/)
    if (!innerMatch) return null

    let inner = innerMatch[1]

    // Propagate fill="currentColor" if needed (same as extractSvgInner)
    const svgHasCurrentColorFill = /fill="currentColor"/.test(svgTag)
    if (svgHasCurrentColorFill) {
        inner = inner.replace(/<(path|circle|rect|ellipse|polygon|polyline|g|defs)([^>]*)>/g, (match, tag, attrs) => {
            if (/fill="/.test(attrs)) return match
            return `<${tag} fill="currentColor"${attrs}>`
        })
    }

    // Remove <title> elements
    inner = inner.replace(/<title>[^<]*<\/title>/g, '')

    // Extract viewBox from original SVG
    const viewBoxMatch = svgTag.match(/viewBox="([^"]+)"/)
    const viewBox = viewBoxMatch ? viewBoxMatch[1] : '0 0 24 24'

    // Build the final SVG tag with our attributes
    const classAttr = classList.length > 0 ? ` class="${classList.join(' ')}"` : ''
    const styleAttr = ` style="width:${size}px;height:${size}px"`
    const viewBoxAttr = ` viewBox="${viewBox}"`
    const roleAttr = ' role="img"'
    const ariaAttr = ariaLabel ? ` aria-label="${ariaLabel}"` : ''

    return `<svg${classAttr}${styleAttr}${viewBoxAttr}${roleAttr}${ariaAttr} xmlns="http://www.w3.org/2000/svg">${inner}</svg>`
}

/**
 * Convenience: get viewBox for a provider icon.
 * Returns default viewBox if provider not found.
 */
export function getProviderViewBox(providerId: string): string {
    const data = getProviderIcon(providerId)
    if (!data) return '0 0 24 24'
    return extractViewBox(data.raw)
}
