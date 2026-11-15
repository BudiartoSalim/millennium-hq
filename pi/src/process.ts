// Runs a program without a shell, with explicit arguments, bounded output,
// a timeout and cancellation: the one place the extension starts processes.
import { spawn } from "node:child_process";

/** Output past this is dropped and the run fails: hooks and agents answer in far less. */
export const maxOutput = 8 << 20;

export interface RunOptions {
  cwd: string;
  env: Record<string, string | undefined>;
  /** Written to stdin, which is then closed. */
  input?: string;
  timeoutMs: number;
  signal?: AbortSignal | undefined;
  /** Called with each complete stdout line, for programs that stream JSONL. */
  onLine?: (line: string) => void;
}

export interface ProcessResult {
  /** The exit status; null when the program did not exit normally. */
  code: number | null;
  stdout: string;
  stderr: string;
  /** Why the run failed before an exit status: start failure, timeout, cancel, too much output. */
  failure?: string;
}

export type RunProcess = (command: string, args: readonly string[], options: RunOptions) => Promise<ProcessResult>;

/** runProcess is the real RunProcess. */
export const runProcess: RunProcess = (command, args, options) =>
  new Promise((resolve) => {
    const child = spawn(command, args, { cwd: options.cwd, env: options.env, stdio: ["pipe", "pipe", "pipe"], windowsHide: true });
    const out = new Collector(options.onLine);
    const err = new Collector(undefined);
    let failure: string | undefined;
    const stop = (why: string) => {
      failure ??= why;
      child.kill("SIGKILL");
    };
    const timer = setTimeout(() => stop(`timed out after ${options.timeoutMs / 1000}s`), options.timeoutMs);
    const onAbort = () => stop("cancelled");
    options.signal?.addEventListener("abort", onAbort, { once: true });
    if (options.signal?.aborted) onAbort();
    child.stdout?.on("data", (chunk) => out.add(chunk) || stop("too much output"));
    child.stderr?.on("data", (chunk) => err.add(chunk) || stop("too much output"));
    child.stdin?.on("error", () => undefined); // a program that exits without reading its input
    child.stdin?.end(options.input ?? "");
    child.on("error", (error) => {
      failure ??= `could not start ${command}: ${error.message}`;
    });
    child.on("close", (code) => {
      clearTimeout(timer);
      options.signal?.removeEventListener("abort", onAbort);
      out.flush();
      const result: ProcessResult = { code: failure === undefined ? code : null, stdout: out.text, stderr: err.text };
      if (failure !== undefined) result.failure = failure;
      resolve(result);
    });
  });

/** Collector keeps a stream's text up to maxOutput and splits out lines. */
class Collector {
  text = "";
  private pending = "";
  private readonly decoder = new TextDecoder();
  private readonly onLine: ((line: string) => void) | undefined;

  constructor(onLine: ((line: string) => void) | undefined) {
    this.onLine = onLine;
  }

  /** add returns false once the output is too large. */
  add(chunk: Uint8Array): boolean {
    const text = this.decoder.decode(chunk, { stream: true });
    if (this.text.length + text.length > maxOutput) return false;
    this.text += text;
    if (this.onLine) this.split(text);
    return true;
  }

  flush(): void {
    const rest = this.decoder.decode();
    this.text += rest;
    if (this.onLine) this.split(rest + "\n");
  }

  private split(text: string): void {
    const lines = (this.pending + text).split("\n");
    this.pending = lines.pop() ?? "";
    for (const line of lines) if (line.trim() !== "") this.onLine?.(line);
  }
}

/** tail is the end of a program's output, for messages. */
export function tail(text: string, max = 2000): string {
  const trimmed = text.trim();
  return trimmed.length <= max ? trimmed : "…" + trimmed.slice(-max);
}
