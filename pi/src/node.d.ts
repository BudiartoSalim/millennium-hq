// The Node.js APIs this extension and its tests use, declared here instead of
// depending on @types/node: the package has no dependencies beyond the
// TypeScript compiler. Signatures are narrowed to the calls the code makes;
// the tests run them against the real Node.js.

interface ImportMeta {
  url: string;
}

interface AbortSignal {
  readonly aborted: boolean;
  addEventListener(type: "abort", listener: () => void, options?: { once?: boolean }): void;
  removeEventListener(type: "abort", listener: () => void): void;
}

declare class AbortController {
  readonly signal: AbortSignal;
  abort(): void;
}

declare class URL {
  constructor(url: string, base?: string);
  readonly href: string;
}

declare class TextDecoder {
  decode(input?: Uint8Array, options?: { stream?: boolean }): string;
}

declare function setTimeout(callback: () => void, ms: number): unknown;
declare function clearTimeout(timer: unknown): void;

declare const process: {
  env: Record<string, string | undefined>;
  platform: string;
  execPath: string;
  argv: string[];
};

declare module "node:child_process" {
  interface Readable {
    on(event: "data", listener: (chunk: Uint8Array) => void): this;
  }
  interface Writable {
    on(event: "error", listener: (error: Error) => void): this;
    end(data: string): void;
  }
  interface ChildProcess {
    readonly stdin: Writable | null;
    readonly stdout: Readable | null;
    readonly stderr: Readable | null;
    kill(signal: "SIGKILL"): boolean;
    on(event: "error", listener: (error: Error) => void): this;
    on(event: "close", listener: (code: number | null) => void): this;
  }
  export function spawn(
    command: string,
    args: readonly string[],
    options: { cwd: string; env: Record<string, string | undefined>; stdio: ["pipe", "pipe", "pipe"]; windowsHide: boolean },
  ): ChildProcess;
}

declare module "node:fs" {
  export function existsSync(path: string): boolean;
  export function readFileSync(path: string, encoding: "utf8"): string;
  export function writeFileSync(path: string, data: string, options?: { mode?: number }): void;
  export function mkdirSync(path: string, options: { recursive: true }): void;
  export function mkdtempSync(prefix: string): string;
  export function rmSync(path: string, options: { recursive: true; force: true }): void;
  export function chmodSync(path: string, mode: number): void;
}

declare module "node:path" {
  export function isAbsolute(path: string): boolean;
  export function join(...paths: string[]): string;
  export function resolve(...paths: string[]): string;
  export function dirname(path: string): string;
}

declare module "node:url" {
  export function fileURLToPath(url: string): string;
  export function pathToFileURL(path: string): { href: string };
}

declare module "node:os" {
  export function homedir(): string;
  export function tmpdir(): string;
}

declare module "node:crypto" {
  export function randomUUID(): string;
}

declare module "node:test" {
  export function test(name: string, fn: () => void | Promise<void>): void;
  export function after(fn: () => void): void;
}

declare module "node:assert/strict" {
  interface Assert {
    equal(actual: unknown, expected: unknown, message?: string): void;
    notEqual(actual: unknown, expected: unknown, message?: string): void;
    deepEqual(actual: unknown, expected: unknown, message?: string): void;
    ok(value: unknown, message?: string): void;
    match(value: string, pattern: RegExp, message?: string): void;
    doesNotMatch(value: string, pattern: RegExp, message?: string): void;
    throws(fn: () => unknown, pattern?: RegExp): void;
    rejects(promise: Promise<unknown>, pattern?: RegExp): Promise<void>;
  }
  const assert: Assert;
  export default assert;
}
