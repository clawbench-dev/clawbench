// File type detection
export interface FileType {
    exts: string[]
    lang: string
    label: string
    color: string
    isMarkdown: boolean
    isHtml?: boolean
    isImage?: boolean
    isPdf?: boolean
    isAudio?: boolean
    isVideo?: boolean
}

const FILE_TYPES: FileType[] = [
    { exts: ['.md', '.markdown'], lang: 'markdown', label: 'MD', color: '#4a90d9', isMarkdown: true },
    { exts: ['.json', '.jsonc', '.json5'], lang: 'json', label: 'JSON', color: '#e0a030', isMarkdown: false },
    { exts: ['.yaml', '.yml'], lang: 'yaml', label: 'YAML', color: '#cb6f1e', isMarkdown: false },
    { exts: ['.toml'], lang: 'toml', label: 'TOML', color: '#9c4122', isMarkdown: false },
    { exts: ['.xml', '.plist'], lang: 'xml', label: 'XML', color: '#e44d26', isMarkdown: false },
    { exts: ['.ini', '.properties', '.conf', '.cfg'], lang: 'ini', label: 'INI', color: '#8b8b8b', isMarkdown: false },
    { exts: ['.go', '.mod', '.sum'], lang: 'go', label: 'Go', color: '#00acd7', isMarkdown: false },
    { exts: ['.py', '.pyi'], lang: 'python', label: 'PY', color: '#3572a5', isMarkdown: false },
    { exts: ['.rs'], lang: 'rust', label: 'RS', color: '#ce412b', isMarkdown: false },
    { exts: ['.js', '.mjs', '.cjs', '.jsx'], lang: 'javascript', label: 'JS', color: '#f7df1e', isMarkdown: false },
    { exts: ['.ts', '.tsx', '.mts', '.cts'], lang: 'typescript', label: 'TS', color: '#3178c6', isMarkdown: false },
    { exts: ['.java'], lang: 'java', label: 'Java', color: '#b07219', isMarkdown: false },
    { exts: ['.cs'], lang: 'csharp', label: 'C#', color: '#68217a', isMarkdown: false },
    { exts: ['.rb'], lang: 'ruby', label: 'RB', color: '#cc342d', isMarkdown: false },
    { exts: ['.php'], lang: 'php', label: 'PHP', color: '#4f5d95', isMarkdown: false },
    { exts: ['.swift'], lang: 'swift', label: 'Swift', color: '#f05138', isMarkdown: false },
    { exts: ['.kt', '.kts'], lang: 'kotlin', label: 'Kotlin', color: '#7f52ff', isMarkdown: false },
    { exts: ['.scala'], lang: 'scala', label: 'Scala', color: '#dc322f', isMarkdown: false },
    { exts: ['.c', '.h'], lang: 'c', label: 'C', color: '#555555', isMarkdown: false },
    { exts: ['.cpp', '.hpp', '.cc', '.cxx'], lang: 'cpp', label: 'C++', color: '#f34b7d', isMarkdown: false },
    { exts: ['.lua'], lang: 'lua', label: 'Lua', color: '#000080', isMarkdown: false },
    { exts: ['.r', '.R'], lang: 'r', label: 'R', color: '#198ce7', isMarkdown: false },
    { exts: ['.pl', '.pm'], lang: 'perl', label: 'Perl', color: '#cc99cc', isMarkdown: false },
    { exts: ['.sh', '.bash', '.zsh', '.fish', '.ksh', '.ash'], lang: 'bash', label: 'SH', color: '#89e051', isMarkdown: false },
    { exts: ['.ps1', '.psm1'], lang: 'powershell', label: 'PS', color: '#012456', isMarkdown: false },
    { exts: ['.sql'], lang: 'sql', label: 'SQL', color: '#e38c00', isMarkdown: false },
    { exts: ['.graphql', '.gql'], lang: 'graphql', label: 'GraphQL', color: '#e10098', isMarkdown: false },
    { exts: ['.html', '.htm', '.xhtml'], lang: 'xml', label: 'HTML', color: '#e44d26', isMarkdown: false, isHtml: true },
    { exts: ['.css', '.scss', '.sass', '.less', '.styl'], lang: 'css', label: 'CSS', color: '#563d7c', isMarkdown: false },
    { exts: ['.vue', '.svelte'], lang: 'vue', label: 'Vue', color: '#41b883', isMarkdown: false },
    { exts: ['.dockerfile', '.dockerignore', 'dockerfile'], lang: 'dockerfile', label: 'Docker', color: '#384d54', isMarkdown: false },
    { exts: ['.makefile', '.mak'], lang: 'makefile', label: 'Make', color: '#6d8086', isMarkdown: false },
    { exts: ['.nginx'], lang: 'nginx', label: 'Nginx', color: '#009639', isMarkdown: false },
    { exts: ['.gitignore', '.gitattributes', '.gitconfig', '.editorconfig', '.ignore'], lang: 'plaintext', label: 'Config', color: '#6d8086', isMarkdown: false },
    { exts: ['.env', '.env.example', '.env.local'], lang: 'bash', label: 'ENV', color: '#ecd53f', isMarkdown: false },
    { exts: ['.txt', '.text', '.log'], lang: 'plaintext', label: 'TXT', color: '#8b8b8b', isMarkdown: false },
    { exts: ['.diff', '.patch'], lang: 'diff', label: 'Diff', color: '#8b8b8b', isMarkdown: false },
    { exts: ['.csv', '.tsv'], lang: 'plaintext', label: 'CSV', color: '#237f4a', isMarkdown: false },
    { exts: ['.tex'], lang: 'latex', label: 'LaTeX', color: '#3d6118', isMarkdown: false },
    { exts: ['.pem', '.crt', '.key', '.pub'], lang: 'plaintext', label: 'Cert', color: '#8b8b8b', isMarkdown: false },
    { exts: ['.regex', '.regexp'], lang: 'regex', label: 'Regex', color: '#8b8b8b', isMarkdown: false },
    { exts: ['.png'], lang: 'image', label: 'PNG', color: '#a855f7', isMarkdown: false, isImage: true },
    { exts: ['.jpg', '.jpeg'], lang: 'image', label: 'JPG', color: '#a855f7', isMarkdown: false, isImage: true },
    { exts: ['.gif'], lang: 'image', label: 'GIF', color: '#a855f7', isMarkdown: false, isImage: true },
    { exts: ['.webp'], lang: 'image', label: 'WEBP', color: '#a855f7', isMarkdown: false, isImage: true },
    { exts: ['.svg'], lang: 'image', label: 'SVG', color: '#a855f7', isMarkdown: false, isImage: true },
    { exts: ['.bmp'], lang: 'image', label: 'BMP', color: '#a855f7', isMarkdown: false, isImage: true },
    { exts: ['.ico'], lang: 'image', label: 'ICO', color: '#a855f7', isMarkdown: false, isImage: true },
    { exts: ['.tiff', '.tif'], lang: 'image', label: 'TIFF', color: '#a855f7', isMarkdown: false, isImage: true },
    { exts: ['.avif'], lang: 'image', label: 'AVIF', color: '#a855f7', isMarkdown: false, isImage: true },
    { exts: ['.pdf'], lang: 'pdf', label: 'PDF', color: '#e53e3e', isMarkdown: false, isPdf: true },
    { exts: ['.mp3'], lang: 'audio', label: 'MP3', color: '#22c55e', isMarkdown: false, isAudio: true },
    { exts: ['.wav'], lang: 'audio', label: 'WAV', color: '#22c55e', isMarkdown: false, isAudio: true },
    { exts: ['.ogg'], lang: 'audio', label: 'OGG', color: '#22c55e', isMarkdown: false, isAudio: true },
    { exts: ['.m4a'], lang: 'audio', label: 'M4A', color: '#22c55e', isMarkdown: false, isAudio: true },
    { exts: ['.aac'], lang: 'audio', label: 'AAC', color: '#22c55e', isMarkdown: false, isAudio: true },
    { exts: ['.flac'], lang: 'audio', label: 'FLAC', color: '#22c55e', isMarkdown: false, isAudio: true },
    { exts: ['.wma'], lang: 'audio', label: 'WMA', color: '#22c55e', isMarkdown: false, isAudio: true },
    { exts: ['.opus'], lang: 'audio', label: 'OPUS', color: '#22c55e', isMarkdown: false, isAudio: true },
    { exts: ['.mp4'], lang: 'video', label: 'MP4', color: '#ef4444', isMarkdown: false, isVideo: true },
    { exts: ['.mkv'], lang: 'video', label: 'MKV', color: '#ef4444', isMarkdown: false, isVideo: true },
    { exts: ['.avi'], lang: 'video', label: 'AVI', color: '#ef4444', isMarkdown: false, isVideo: true },
    { exts: ['.mov'], lang: 'video', label: 'MOV', color: '#ef4444', isMarkdown: false, isVideo: true },
    { exts: ['.webm'], lang: 'video', label: 'WEBM', color: '#ef4444', isMarkdown: false, isVideo: true },
    { exts: ['.flv'], lang: 'video', label: 'FLV', color: '#ef4444', isMarkdown: false, isVideo: true },
    { exts: ['.wmv'], lang: 'video', label: 'WMV', color: '#ef4444', isMarkdown: false, isVideo: true },
    { exts: ['.m4v'], lang: 'video', label: 'M4V', color: '#ef4444', isMarkdown: false, isVideo: true },
    { exts: ['.3gp'], lang: 'video', label: '3GP', color: '#ef4444', isMarkdown: false, isVideo: true },
    { exts: ['.m3u8'], lang: 'video', label: 'M3U8', color: '#ef4444', isMarkdown: false, isVideo: true },
]

export function getFileType(name: string): FileType {
    const lower = name.toLowerCase()
    for (const ft of FILE_TYPES) {
        for (const ext of ft.exts) {
            if (lower.endsWith(ext)) return ft
        }
    }
    return { exts: [], lang: 'plaintext', label: 'TXT', color: '#8b8b8b', isMarkdown: false }
}

export function formatFileSize(bytes: number): string {
    if (bytes < 1024) return bytes + ' B'
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
    return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
}
