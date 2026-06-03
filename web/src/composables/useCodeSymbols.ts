import { apiGet } from '@/utils/api'

export interface CodeSymbol {
  name: string
  kind: string  // "function", "class", "method", "struct", "interface", "type", "enum", "variable", "constant", etc.
  line: number
  endLine: number
  level: number
}

interface SymbolResult {
  lang: string
  symbols: CodeSymbol[]
}

/**
 * Fetch code symbols from the backend tree-sitter API.
 * Returns null on failure (caller should fallback to regex-based extraction).
 */
export async function fetchCodeSymbols(path: string): Promise<SymbolResult | null> {
  try {
    const result = await apiGet<SymbolResult>(`/api/file/symbols?path=${encodeURIComponent(path)}`)
    if (result && result.symbols && result.symbols.length > 0) {
      return result
    }
    return null
  } catch {
    return null
  }
}
