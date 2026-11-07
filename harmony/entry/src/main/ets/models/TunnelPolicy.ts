export type TunnelTransport = 'ssh' | 'h2';
export type TunnelDirection = 'forward' | 'reverse';

export interface TunnelMapping {
  port: number;
  targetPort: number;
  host: string;
  direction: TunnelDirection;
}

export interface TunnelSettings {
  transport: TunnelTransport;
  mappings: TunnelMapping[];
}

export interface TunnelCommand {
  method: string;
  transport?: TunnelTransport;
  origin?: string;
  cookie?: string;
  password?: string;
  username?: string;
  hostKey?: string;
  sshPort?: number;
  port?: number;
  targetPort?: number;
  host?: string;
  direction?: TunnelDirection;
}

export interface TunnelResult {
  ok: boolean;
  connected?: boolean;
  reachable?: boolean;
  port?: number;
  reason?: string;
  error?: string;
  errorType?: string;
  fingerprint?: string;
  transport?: string;
}

export interface TunnelAddResult {
  ok: boolean;
  port?: number;
  reason?: string;
}

export interface TunnelDriver {
  execute(command: TunnelCommand): Promise<TunnelResult>;
  cancel(): void;
  prepare(origin: string, transport: TunnelTransport, isCurrent: () => boolean): Promise<TunnelCommand>;
  approveHostKey(command: TunnelCommand, fingerprint: string, isCurrent: () => boolean): Promise<boolean>;
  persist(origin: string, settings: TunnelSettings): Promise<void>;
}

export class TunnelFailure extends Error {
  code: string;

  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}

interface MappingEntry {
  mapping: TunnelMapping;
  revision: number;
}

export function validTunnelPort(port: number): boolean {
  return Number.isInteger(port) && port >= 1 && port <= 65535;
}

export function tunnelMapping(port: number, targetPort: number, host: string,
  direction: TunnelDirection): TunnelMapping {
  const normalizedHost = host.trim() || '127.0.0.1';
  if (!validTunnelPort(port) || !validTunnelPort(targetPort) || normalizedHost.length > 253 ||
    !/^[a-zA-Z0-9.:-]+$/.test(normalizedHost) || (direction !== 'forward' && direction !== 'reverse')) {
    throw new Error('Invalid port mapping');
  }
  return { port, targetPort, host: normalizedHost, direction };
}

export function validHostFingerprint(value: string): boolean {
  return /^[0-9a-f]{64}$/.test(value);
}

export function retryTunnelAutomatically(errorType: string): boolean {
  return errorType === '' || errorType === 'network' || errorType === 'capacity' || errorType === 'unknown';
}

function mappingKey(mapping: TunnelMapping): string {
  return mapping.direction + ':' + mapping.port;
}

function copyMapping(mapping: TunnelMapping): TunnelMapping {
  return { port: mapping.port, targetPort: mapping.targetPort, host: mapping.host, direction: mapping.direction };
}

class Generation {
  cancelled: boolean = false;
  private waiters: (() => void)[] = [];

  cancel(): void {
    this.cancelled = true;
    const waiters = this.waiters;
    this.waiters = [];
    waiters.forEach((waiter) => waiter());
  }

  // UI consent and credential loading can outlive the page. Their late values
  // are discarded, while submitted native operations remain serialised.
  wait<T>(operation: Promise<T>): Promise<T | undefined> {
    return new Promise((resolve, reject) => {
      let settled = false;
      const finish = (value: T | undefined): void => {
        if (settled) {
          return;
        }
        settled = true;
        this.waiters = this.waiters.filter((waiter) => waiter !== cancel);
        resolve(value);
      };
      const cancel = (): void => finish(undefined);
      operation.then((value: T) => finish(this.cancelled ? undefined : value), (error: Error) => {
        if (settled) {
          return;
        }
        if (this.cancelled) {
          finish(undefined);
          return;
        }
        settled = true;
        this.waiters = this.waiters.filter((waiter) => waiter !== cancel);
        reject(error);
      });
      if (this.cancelled) {
        cancel();
      } else {
        this.waiters.push(cancel);
      }
    });
  }
}

export class TunnelPolicy {
  private driver: TunnelDriver;
  private rebound: (requested: number, actual: number) => void;
  private origin: string = '';
  private transport: TunnelTransport = 'ssh';
  private activeTransport: string = '';
  private foreground: boolean = true;
  private closed: boolean = false;
  private generation: Generation = new Generation();
  private queue: Promise<void> = Promise.resolve();
  private persisted: Promise<void> = Promise.resolve();
  private connecting?: Promise<boolean>;
  private connectionGeneration?: Generation;
  private reconnecting?: Promise<boolean>;
  private reconnectGeneration?: Generation;
  private preparing: Map<number, Promise<number>> = new Map();
  private polling?: Promise<void>;
  private revision: number = 0;
  private desired: Map<string, MappingEntry> = new Map();
  private active: Map<string, TunnelMapping> = new Map();
  private connected: boolean = false;
  private error: string = '';
  private errorType: string = '';
  private retryAt: number = 0;
  private failures: number = 0;

  constructor(driver: TunnelDriver, rebound: (requested: number, actual: number) => void = () => {}) {
    this.driver = driver;
    this.rebound = rebound;
  }

  setServer(origin: string, settings: TunnelSettings): void {
    this.invalidate();
    this.origin = origin;
    this.transport = settings.transport === 'h2' ? 'h2' : 'ssh';
    this.desired.clear();
    for (const mapping of settings.mappings.slice(0, 32)) {
      try {
        const parsed = tunnelMapping(mapping.port, mapping.targetPort, mapping.host, mapping.direction);
        this.desired.set(mappingKey(parsed), { mapping: parsed, revision: ++this.revision });
      } catch (_) {}
    }
    this.error = '';
    this.errorType = '';
    this.retryAt = 0;
    this.failures = 0;
  }

  setForeground(value: boolean): void {
    if (value === this.foreground) {
      return;
    }
    this.foreground = value;
    if (!value && !this.connected) {
      this.invalidate();
    }
    this.retryAt = 0;
  }

  shutdown(): void {
    this.closed = true;
    this.invalidate();
  }

  getForwardedPorts(): string {
    return JSON.stringify(this.mappings());
  }

  getTunnelError(): string {
    return this.error;
  }

  getTunnelErrorType(): string {
    return this.errorType;
  }

  getTunnelTransport(): TunnelTransport {
    return this.transport;
  }

  getActiveTunnelTransport(): string {
    return this.connected ? this.activeTransport : '';
  }

  getTunnelTransportH2Enabled(): boolean {
    return this.transport === 'h2';
  }

  async setTunnelTransportH2Enabled(enabled: boolean): Promise<void> {
    this.transport = enabled ? 'h2' : 'ssh';
    await this.persist();
  }

  async isTunnelConnected(): Promise<boolean> {
    await this.refreshStatus();
    return this.connected && this.usable(this.generation);
  }

  async testPortReachable(port: number): Promise<boolean> {
    const generation = this.generation;
    if (!validTunnelPort(port) || !this.active.has('forward:' + port) || !this.usable(generation)) {
      return false;
    }
    return await this.serial(async (): Promise<boolean> => {
      if (!this.usable(generation) || !this.active.has('forward:' + port)) {
        return false;
      }
      const result = await this.execute({ method: 'probe', port });
      return this.usable(generation) && result.ok && result.reachable === true;
    });
  }

  prepareForwardedPort(port: number): Promise<number> {
    const generation = this.generation;
    if (!this.foreground || !this.usable(generation)) {
      return Promise.reject(new TunnelFailure('cancelled', 'Return to the app before opening a forwarded port'));
    }
    const pending = this.preparing.get(port);
    if (pending) {
      return pending;
    }
    const entry = this.desired.get('forward:' + port);
    if (!validTunnelPort(port) || !entry) {
      return Promise.reject(new TunnelFailure('invalid', 'This forwarded port is not configured'));
    }
    const operation = this.preparePort(generation, entry);
    this.preparing.set(port, operation);
    void operation.then(() => {
      if (this.preparing.get(port) === operation) {
        this.preparing.delete(port);
      }
    }, () => {
      if (this.preparing.get(port) === operation) {
        this.preparing.delete(port);
      }
    });
    return operation;
  }

  async addForwardedPort(port: number, targetPort: number, host: string): Promise<TunnelAddResult> {
    return await this.add(port, targetPort, host, 'forward');
  }

  async addReverseForwardedPort(port: number, targetPort: number, host: string): Promise<boolean> {
    return (await this.add(port, targetPort, host, 'reverse')).ok;
  }

  async removeForwardedPort(port: number): Promise<void> {
    await this.remove(port, 'forward');
  }

  async removeReverseForwardedPort(port: number): Promise<void> {
    await this.remove(port, 'reverse');
  }

  async forgetForwardedPorts(): Promise<void> {
    this.desired.clear();
    this.invalidate();
    await this.persist();
  }

  stopWhenIdle(): void {
    if (this.desired.size === 0) {
      this.invalidate();
    }
  }

  reconnectTunnel(): Promise<boolean> {
    if (!this.foreground) {
      return Promise.resolve(false);
    }
    if (this.reconnecting && this.reconnectGeneration === this.generation) {
      return this.reconnecting;
    }
    this.invalidate();
    this.retryAt = 0;
    this.failures = 0;
    const generation = this.generation;
    const operation = this.reconnect(generation);
    this.reconnecting = operation;
    this.reconnectGeneration = generation;
    void operation.then(() => {
      if (this.reconnecting === operation) {
        this.reconnecting = undefined;
        this.reconnectGeneration = undefined;
      }
    }, () => {
      if (this.reconnecting === operation) {
        this.reconnecting = undefined;
        this.reconnectGeneration = undefined;
      }
    });
    return operation;
  }

  private async reconnect(generation: Generation): Promise<boolean> {
    return await this.ensureConnected(true) && await this.restore(generation);
  }

  async tick(now: number = Date.now()): Promise<void> {
    if (this.polling) {
      return await this.polling;
    }
    const operation = this.tickOnce(now);
    this.polling = operation;
    try {
      await operation;
    } finally {
      if (this.polling === operation) {
        this.polling = undefined;
      }
    }
  }

  private async tickOnce(now: number): Promise<void> {
    const generation = this.generation;
    if (!this.foreground || !this.usable(generation) || this.desired.size === 0) {
      return;
    }
    await this.refreshStatus();
    if (!this.foreground || !this.usable(generation)) {
      return;
    }
    if (this.connected) {
      await this.restore(generation);
    } else if (now >= this.retryAt && retryTunnelAutomatically(this.errorType) && await this.ensureConnected(false)) {
      await this.restore(generation);
    }
  }

  private invalidate(): void {
    this.generation.cancel();
    this.generation = new Generation();
    this.connected = false;
    this.activeTransport = '';
    this.active.clear();
    this.preparing.clear();
    this.driver.cancel();
  }

  private usable(generation: Generation): boolean {
    return generation === this.generation && !generation.cancelled && !this.closed &&
      this.origin.length > 0;
  }

  private mappings(): TunnelMapping[] {
    return Array.from(this.desired.values()).map((entry) => copyMapping(entry.mapping));
  }

  private persist(): Promise<void> {
    const origin = this.origin;
    const settings: TunnelSettings = { transport: this.transport, mappings: this.mappings() };
    const operation = this.persisted.then(async (): Promise<void> => {
      if (origin) {
        await this.driver.persist(origin, settings);
      }
    });
    this.persisted = operation.catch(() => {});
    return operation;
  }

  private serial<T>(operation: () => Promise<T>): Promise<T> {
    const result = this.queue.then(operation);
    this.queue = result.then(() => {}, () => {});
    return result;
  }

  private async execute(command: TunnelCommand): Promise<TunnelResult> {
    try {
      return await this.driver.execute(command);
    } catch (_) {
      return { ok: false, error: 'Tunnel operation failed', errorType: 'network' };
    }
  }

  private fail(result: TunnelResult): void {
    this.connected = false;
    this.active.clear();
    this.error = result.error || 'Tunnel connection failed';
    this.errorType = result.errorType || 'network';
    this.failures++;
    this.retryAt = Date.now() + Math.min(30000, 1000 * Math.pow(2, Math.min(this.failures, 5)));
  }

  private ensureConnected(allowPrompt: boolean): Promise<boolean> {
    const generation = this.generation;
    if (!this.usable(generation)) {
      return Promise.resolve(false);
    }
    if (this.connected) {
      return Promise.resolve(true);
    }
    if (!this.foreground) {
      return Promise.resolve(false);
    }
    if (this.connecting && this.connectionGeneration === generation) {
      return this.connecting;
    }
    const origin = this.origin;
    const transport = this.transport;
    const operation = this.serial(async (): Promise<boolean> => {
      if (!this.foreground || !this.usable(generation)) {
        return false;
      }
      let command: TunnelCommand | undefined;
      try {
        command = await generation.wait(this.driver.prepare(origin, transport, () => this.usable(generation)));
      } catch (error) {
        if (this.usable(generation)) {
          this.fail({ ok: false, error: (error as Error).message || 'Tunnel information unavailable',
            errorType: error instanceof TunnelFailure ? error.code : 'network' });
        }
        return false;
      }
      if (!command || !this.usable(generation)) {
        if (this.usable(generation)) {
          this.fail({ ok: false, error: 'Tunnel credentials or server information unavailable', errorType: 'auth' });
        }
        return false;
      }
      let result = await this.execute(command);
      if (!this.usable(generation)) {
        this.driver.cancel();
        return false;
      }
      if (!result.ok && transport === 'ssh' && result.errorType === 'hostkey' &&
        validHostFingerprint(result.fingerprint || '') && allowPrompt) {
        const fingerprint = result.fingerprint || '';
        let approved = false;
        try {
          approved = await generation.wait(this.driver.approveHostKey(command, fingerprint,
            () => this.usable(generation))) === true;
        } catch (_) {}
        if (!this.usable(generation)) {
          return false;
        }
        if (approved === true) {
          command.hostKey = fingerprint;
          result = await this.execute(command);
          if (!this.usable(generation)) {
            this.driver.cancel();
            return false;
          }
        }
      }
      if (!result.ok || result.connected !== true) {
        this.fail(result);
        return false;
      }
      this.connected = true;
      this.activeTransport = transport;
      this.error = '';
      this.errorType = '';
      this.failures = 0;
      this.retryAt = 0;
      return true;
    });
    this.connecting = operation;
    this.connectionGeneration = generation;
    void operation.then(() => {
      if (this.connecting === operation) {
        this.connecting = undefined;
        this.connectionGeneration = undefined;
      }
    }, () => {
      if (this.connecting === operation) {
        this.connecting = undefined;
        this.connectionGeneration = undefined;
      }
    });
    return operation;
  }

  private async refreshStatus(): Promise<void> {
    const generation = this.generation;
    if (!this.connected || !this.usable(generation)) {
      return;
    }
    await this.serial(async (): Promise<void> => {
      if (!this.connected || !this.usable(generation)) {
        return;
      }
      const result = await this.execute({ method: 'status' });
      if (this.usable(generation) && (!result.ok || result.connected !== true)) {
        this.fail(result);
      }
    });
  }

  private async add(port: number, targetPort: number, host: string,
    direction: TunnelDirection): Promise<TunnelAddResult> {
    let mapping: TunnelMapping;
    try {
      mapping = tunnelMapping(port, targetPort, host, direction);
    } catch (_) {
      return { ok: false, reason: 'unreachable' };
    }
    const generation = this.generation;
    if (!this.foreground || !this.usable(generation)) {
      return { ok: false, reason: 'unreachable' };
    }
    const key = mappingKey(mapping);
    const existing = this.desired.get(key);
    if (existing && (existing.mapping.targetPort !== targetPort || existing.mapping.host !== mapping.host)) {
      return { ok: false, reason: 'conflict' };
    }
    if (!existing && this.desired.size >= 32) {
      return { ok: false, reason: 'conflict' };
    }
    const entry: MappingEntry = existing || { mapping, revision: ++this.revision };
    this.desired.set(key, entry);
    try {
      await this.persist();
    } catch (_) {
      if (this.desired.get(key) === entry && !existing) {
        this.desired.delete(key);
      }
      return { ok: false, reason: 'unreachable' };
    }
    if (!this.usable(generation) || this.desired.get(key) !== entry || !await this.ensureConnected(true)) {
      return { ok: false, reason: 'unreachable' };
    }
    return await this.bind(generation, entry);
  }

  private bind(generation: Generation, entry: MappingEntry, notifyRebound: boolean = false): Promise<TunnelAddResult> {
    return this.serial(async (): Promise<TunnelAddResult> => {
      const mapping = entry.mapping;
      const key = mappingKey(mapping);
      if (!this.foreground || !this.usable(generation) || !this.connected || this.desired.get(key) !== entry) {
        return { ok: false, reason: 'unreachable' };
      }
      if (this.active.has(key)) {
        return { ok: true, port: mapping.port };
      }
      const command: TunnelCommand = {
        method: 'add', port: mapping.port, targetPort: mapping.targetPort, host: mapping.host, direction: mapping.direction
      };
      const result = await this.execute(command);
      if (!this.usable(generation)) {
        this.driver.cancel();
        return { ok: false, reason: 'unreachable' };
      }
      if (this.desired.get(key) !== entry) {
        if (result.ok && validTunnelPort(result.port || 0)) {
          await this.execute({ method: 'remove', port: result.port, direction: mapping.direction });
        }
        return { ok: false, reason: 'unreachable' };
      }
      if (!result.ok || !validTunnelPort(result.port || 0)) {
        return { ok: false, reason: result.reason === 'conflict' ? 'conflict' : 'unreachable' };
      }
      const actual = copyMapping(mapping);
      actual.port = result.port || mapping.port;
      if (actual.port !== mapping.port) {
        const collision = this.desired.get(mappingKey(actual));
        if (collision && collision !== entry) {
          await this.execute({ method: 'remove', port: actual.port, direction: actual.direction });
          return { ok: false, reason: 'conflict' };
        }
        const updatedEntry: MappingEntry = { mapping: actual, revision: entry.revision };
        this.desired.delete(key);
        this.desired.set(mappingKey(actual), updatedEntry);
        try {
          await this.persist();
        } catch (_) {
          if (this.usable(generation) && this.desired.get(mappingKey(actual)) === updatedEntry) {
            await this.execute({ method: 'remove', port: actual.port, direction: actual.direction });
            if (this.usable(generation) && this.desired.get(mappingKey(actual)) === updatedEntry) {
              this.desired.delete(mappingKey(actual));
            }
          }
          return { ok: false, reason: 'unreachable' };
        }
        if (!this.usable(generation)) {
          this.driver.cancel();
          return { ok: false, reason: 'unreachable' };
        }
        if (this.desired.get(mappingKey(actual)) !== updatedEntry) {
          await this.execute({ method: 'remove', port: actual.port, direction: actual.direction });
          return { ok: false, reason: 'unreachable' };
        }
      }
      this.active.set(mappingKey(actual), actual);
      if (notifyRebound && actual.direction === 'forward' && actual.port !== mapping.port) {
        try { this.rebound(mapping.port, actual.port); } catch (_) {}
      }
      return { ok: true, port: actual.port };
    });
  }

  private async remove(port: number, direction: TunnelDirection): Promise<void> {
    if (!validTunnelPort(port)) {
      return;
    }
    const key = direction + ':' + port;
    this.desired.delete(key);
    this.active.delete(key);
    const generation = this.generation;
    const persistence = this.persist();
    await this.serial(async (): Promise<void> => {
      if (this.usable(generation) && this.connected && !this.desired.has(key)) {
        await this.execute({ method: 'remove', port, direction });
      }
    });
    await persistence;
  }

  private assertPreparing(generation: Generation): void {
    if (!this.foreground || !this.usable(generation)) {
      throw new TunnelFailure('cancelled', 'Opening the forwarded port was cancelled');
    }
  }

  private async preparePort(generation: Generation, entry: MappingEntry): Promise<number> {
    await this.refreshStatus();
    this.assertPreparing(generation);
    if (!await this.ensureConnected(true)) {
      this.assertPreparing(generation);
      throw new TunnelFailure(this.errorType || 'network', this.error || 'The tunnel could not be connected');
    }
    this.assertPreparing(generation);
    await this.restore(generation);
    this.assertPreparing(generation);
    const current = Array.from(this.desired.values()).find((item) => item.revision === entry.revision);
    if (!current) {
      throw new TunnelFailure('cancelled', 'The forwarded port was removed');
    }
    const actual = current.mapping.port;
    if (!this.active.has('forward:' + actual) || !await this.testPortReachable(actual)) {
      this.assertPreparing(generation);
      throw new TunnelFailure('network', 'The forwarded port is not ready');
    }
    this.assertPreparing(generation);
    if (this.desired.get('forward:' + actual) !== current || !this.active.has('forward:' + actual)) {
      throw new TunnelFailure('cancelled', 'The forwarded port changed while opening');
    }
    return actual;
  }

  private async restore(generation: Generation): Promise<boolean> {
    const entries = Array.from(this.desired.values());
    let restored = true;
    for (const entry of entries) {
      if (!this.foreground || !this.usable(generation)) {
        return false;
      }
      if (!(await this.bind(generation, entry, true)).ok) {
        restored = false;
      }
    }
    return restored && this.connected && this.usable(generation);
  }
}
