// Polyfill Promise.withResolvers for older browsers.
// pdfjs-dist v5 relies on this ES2024 API; without it opening PDFs throws
// "Promise.withResolvers is not a function". Native impl used when available.
export function installPromiseWithResolversPolyfill() {
  if (typeof Promise.withResolvers === 'function') return

  // @ts-expect-error - adding a static method not in older TS lib typings
  Promise.withResolvers = function withResolvers() {
    let resolve
    let reject
    const promise = new Promise((res, rej) => {
      resolve = res
      reject = rej
    })
    return { promise, resolve, reject }
  }
}
