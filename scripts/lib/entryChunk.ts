/**
 * Locating the live entry chunk in a Vite build output.
 *
 * `.clawbench-web/` is never emptied (`emptyOutDir: false`, deliberate — it
 * protects the vendored Excalidraw assets), so every build leaves its
 * `main-<hash>.js` behind. Measured on a working checkout: 7 of them, all
 * ~1.2MB and several byte-identical. Scanning the directory therefore returns
 * whichever entry the filesystem happens to list first, which only produced the
 * right answer by luck — once a stale chunk diverges in size, a size gate would
 * silently measure a file no browser loads, passing on an old bundle while the
 * real one grew.
 *
 * index.html is the authority: it names the chunk the browser will fetch.
 */
export function parseEntryChunkName(html: string): string | null {
    const m = html.match(/<script[^>]+type="module"[^>]+src="\/?([^"]+\.js)"/)
        || html.match(/src="\/?(main-[^"]+\.js)"/)
    return m ? m[1] : null
}
