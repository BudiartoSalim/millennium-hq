// Fakes shared by the tests: a process runner that records calls and answers
// from a function, and a pi context that records notifications.
import type { Context } from "../pi-api.ts";
import type { ProcessResult, RunOptions, RunProcess } from "../process.ts";

export interface Call {
  command: string;
  args: readonly string[];
  options: RunOptions;
}

export function fakeRun(answer: (call: Call) => ProcessResult | Promise<ProcessResult>): { run: RunProcess; calls: Call[] } {
  const calls: Call[] = [];
  const run: RunProcess = async (command, args, options) => {
    const call = { command, args, options };
    calls.push(call);
    return answer(call);
  };
  return { run, calls };
}

export function exited(stdout = "", code: number | null = 0, stderr = ""): ProcessResult {
  return { code, stdout, stderr };
}

export interface FakeContext extends Context {
  notes: Array<[string, string | undefined]>;
}

export function fakeContext(cwd = "/repo", hasUI = true): FakeContext {
  const notes: Array<[string, string | undefined]> = [];
  return {
    cwd,
    hasUI,
    notes,
    ui: { notify: (message, type) => notes.push([message, type]) },
    sessionManager: { getSessionId: () => "session-1" },
    model: { provider: "anthropic", id: "claude-opus-5-5" },
  };
}
