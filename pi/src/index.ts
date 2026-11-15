// millennium's pi extension. It wires pi's events to the engine through the
// plugin's launcher (the same one Claude Code's hooks run):
//   tool_call            -> the guard; a call it cannot check is blocked
//   agent_before_settle  -> the fast gates; failing work goes back to the agent
//   session_start        -> the engine's briefing, added to every system prompt
// and adds the millennium_agent tool, pi's stand-in for Claude Code subagents.
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { AgentTool, type Spawner } from "./agent-tool.ts";
import { Briefing } from "./briefing.ts";
import { Engine, type Launch } from "./engine.ts";
import { guardToolCalls, type WhoIs } from "./guard.ts";
import type { Identity } from "./hook-event.ts";
import type { Context, PiAPI } from "./pi-api.ts";
import { runProcess } from "./process.ts";
import { checkBeforeSettle } from "./settle.ts";

/** Setup is everything the wiring needs from the machine. */
export interface Setup {
  launch: Launch;
  spawner: Spawner;
  home: string;
  env: Record<string, string | undefined>;
}

/** wire registers millennium's handlers and tool. */
export function wire(pi: PiAPI, setup: Setup): void {
  const engine = new Engine(setup.spawner.run, setup.launch);
  const whoIs: WhoIs = (ctx) => identity(ctx, setup.env);
  const briefing = new Briefing(engine, whoIs);
  pi.on("session_start", briefing.onSessionStart);
  pi.on("before_agent_start", briefing.onBeforeAgentStart);
  pi.on("tool_call", guardToolCalls(engine, setup.home, whoIs));
  pi.on("agent_before_settle", checkBeforeSettle(engine, whoIs));
  // Agents started by the tool get no tool to start agents of their own.
  if (setup.env["MILLENNIUM_AGENT"] === undefined) pi.registerTool(new AgentTool(setup.spawner).definition());
}

/** identity is who acts: an agent the tool started says so in its environment. */
export function identity(ctx: Context, env: Record<string, string | undefined>): Identity {
  return {
    sessionId: ctx.sessionManager.getSessionId(),
    agentId: env["MILLENNIUM_AGENT_ID"] ?? "",
    agentType: env["MILLENNIUM_AGENT"] ?? "",
  };
}

/** Process is the part of node's process object setup reads. */
export interface ProcessInfo {
  env: Record<string, string | undefined>;
  platform: string;
  execPath: string;
  argv: string[];
}

/**
 * setupFor finds the plugin from this file's location (pi/src/ in the
 * package), keeps the hooks' state in pi's agent directory, and runs the
 * launcher with a POSIX shell: /bin/sh, or Git Bash on Windows, as Claude
 * Code does.
 */
export function setupFor(entry: string, proc: ProcessInfo, home: string): Setup {
  const pluginRoot = join(dirname(entry), "..", "..", "plugin");
  const agentDir = expandHome(proc.env["PI_CODING_AGENT_DIR"] ?? join(home, ".pi", "agent"), home);
  const stateDir = proc.env["MILLENNIUM_STATE_DIR"] ?? join(agentDir, "millennium", "state");
  const env = { ...proc.env, MILLENNIUM_PLUGIN_ROOT: pluginRoot, MILLENNIUM_STATE_DIR: stateDir };
  const script = proc.argv[1];
  // pi runs as `node <cli.js>` (start it the same way) or as a compiled binary.
  const pi = { command: proc.execPath, args: script !== undefined && existsSync(script) ? [script] : [] };
  return {
    launch: { shell: posixShell(proc), launcher: join(pluginRoot, "scripts", "millennium-code-qc"), env },
    spawner: { run: runProcess, pi, extension: entry, pluginRoot, stateDir, env },
    home,
    env: proc.env,
  };
}

function posixShell(proc: ProcessInfo): string {
  if (proc.platform !== "win32") return "/bin/sh";
  return join(proc.env["ProgramFiles"] ?? "C:\\Program Files", "Git", "bin", "bash.exe");
}

function expandHome(p: string, home: string): string {
  return p === "~" || p.startsWith("~/") ? join(home, p.slice(1)) : p;
}

/**
 * The extension entry point pi calls, on every load and /reload. Agents get
 * this file with --extension as well as from your settings; pi loads a path
 * given both ways once.
 */
export default function millennium(pi: PiAPI): void {
  wire(pi, setupFor(fileURLToPath(import.meta.url), process, homedir()));
}
