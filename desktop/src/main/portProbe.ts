import { createServer } from 'node:net'

/** Resolve true when nothing is listening on 127.0.0.1:port right now. */
export function isPortFree(port: number, host = '127.0.0.1'): Promise<boolean> {
  return new Promise((resolve) => {
    const srv = createServer()
    srv.unref()
    srv.once('error', () => resolve(false))
    srv.listen({ port, host, exclusive: true }, () => {
      srv.close(() => resolve(true))
    })
  })
}

/** Every port in `ports` that is currently taken. Checked sequentially so a
 *  batch of probes cannot exhaust the ephemeral range at once. */
export async function portsInUse(ports: readonly number[]): Promise<number[]> {
  const out: number[] = []
  for (const p of ports) {
    if (!(await isPortFree(p))) out.push(p)
  }
  return out
}
