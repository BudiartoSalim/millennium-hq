// Runs `millennium-code-qc hook` through the plugin's launcher, which
// verifies the locked engine release before running it, and reads its answer.
import type { HookEvent } from "./hook-event.ts";
import { tail, type RunProcess } from "./process.ts";

/** How long each kind of event may take, as in plugin/hooks/hooks.json. */
export const timeouts = { PreToolUse: 30_000, Stop: 600_000, SessionStart: 120_000 } as const;

/** Launch says how to start the launcher: a POSIX shell, the script, its environment. */
export interface Launch {
  shell: string;
  launcher: string;
  env: Record<string, string | undefined>;
}

/** EngineReply is the engine's JSON answer (absent when it had nothing to say), or why there is none. */
export type EngineReply = { ok: true; output: Record<string, unknown> | undefined } | { ok: false; message: string };

/** Engine sends hook events to the engine. */
export class Engine {
  private readonly run: RunProcess;
  private readonly launch: Launch;

  constructor(run: RunProcess, launch: Launch) {
    this.run = run;
    this.launch = launch;
  }

  /** command is the engine command for agents' shells, quoted for POSIX shells. */
  command(): string {
    return shellQuote(this.launch.launcher);
  }

  async hook(event: HookEvent, signal?: AbortSignal): Promise<EngineReply> {
    const result = await this.run(this.launch.shell, [this.launch.launcher, "hook"], {
      cwd: event.cwd,
      env: this.launch.env,
      input: JSON.stringify(event),
      timeoutMs: timeouts[event.hook_event_name],
      signal,
    });
    if (result.failure !== undefined) return { ok: false, message: `the engine ${result.failure}` };
    if (result.code !== 0) {
      return { ok: false, message: tail(result.stderr) || `the engine exited with status ${String(result.code)}` };
    }
    return parseReply(result.stdout);
  }
}

function parseReply(stdout: string): EngineReply {
  const text = stdout.trim();
  if (text === "") return { ok: true, output: undefined };
  try {
    const output: unknown = JSON.parse(text);
    if (isRecord(output)) return { ok: true, output };
  } catch {
    // reported below, with the output
  }
  return { ok: false, message: `the engine answered with unexpected output: ${tail(text, 300)}` };
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** field reads a string field of a JSON object, "" when it is missing. */
export function field(object: unknown, ...keys: string[]): string {
  let value: unknown = object;
  for (const key of keys) value = isRecord(value) ? value[key] : undefined;
  return typeof value === "string" ? value : "";
}

/** shellQuote quotes a word for POSIX shells. */
export function shellQuote(word: string): string {
  return "'" + word.replaceAll("'", `'\\''`) + "'";
}
