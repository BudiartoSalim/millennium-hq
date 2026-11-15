// The millennium_agent tool: pi has no subagents, so each millennium agent
// runs as a separate pi process with fresh context, its own tools and
// instructions, its role (which the guard enforces), and for the agents that
// need one, its own git worktree. Several tasks in one call run in parallel.
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import { agentNames, isAgentName, loadAgent, systemPrompt, type AgentName } from "./agents.ts";
import { field, isRecord } from "./engine.ts";
import type { Context, ToolDefinition, ToolResult } from "./pi-api.ts";
import { tail, type RunProcess } from "./process.ts";
import { addWorktree } from "./worktree.ts";

export const toolName = "millennium_agent";
const maxTasks = 4;
// An agent may work for a long time; this only stops one that hangs.
const agentTimeoutMs = 3 * 60 * 60 * 1000;

export interface Task {
  agent?: string;
  task: string;
  resume?: string;
}

export interface Params {
  tasks: Task[];
}

/** AgentRun is a started agent, kept so the tech lead can resume it. */
interface AgentRun {
  id: string;
  agent: AgentName;
  cwd: string;
  sessionFile: string;
  promptFile: string;
  tools: string[];
  branch?: string;
}

/** Spawner holds what starting a pi process needs. */
export interface Spawner {
  run: RunProcess;
  /** The pi command: how this pi process was started. */
  pi: { command: string; args: string[] };
  /** This extension's entry file, loaded into every agent so the guard and the gates apply there too. */
  extension: string;
  pluginRoot: string;
  stateDir: string;
  env: Record<string, string | undefined>;
}

const parameters = {
  type: "object",
  additionalProperties: false,
  required: ["tasks"],
  properties: {
    tasks: {
      type: "array",
      minItems: 1,
      maxItems: maxTasks,
      description: "Agents to run in parallel. Each is a new agent (agent and task) or a follow-up to one (resume and task).",
      items: {
        type: "object",
        additionalProperties: false,
        required: ["task"],
        properties: {
          agent: { type: "string", enum: [...agentNames], description: "The agent to start." },
          task: { type: "string", minLength: 1, description: "Everything the agent needs: it sees nothing of this session." },
          resume: { type: "string", description: "The id of an agent this session started, to continue it with its context." },
        },
      },
    },
  },
};

/** AgentTool starts and resumes millennium's agents. */
export class AgentTool {
  private readonly spawner: Spawner;
  private readonly runs = new Map<string, AgentRun>();
  // Agents working right now: two processes must not share one session.
  private readonly busy = new Set<string>();

  constructor(spawner: Spawner) {
    this.spawner = spawner;
  }

  definition(): ToolDefinition<Params> {
    return {
      name: toolName,
      label: "millennium agent",
      description:
        "Runs millennium's agents (" + agentNames.join(", ") + ") as separate pi processes with fresh context. " +
        "Each gets only the task text. Agents that write code work in their own git worktree branched from HEAD; " +
        "the result names it. Up to " + String(maxTasks) + " tasks run in parallel.",
      promptSnippet: "Run millennium agents (engineers, reviewers, QA) with fresh context, in parallel",
      parameters,
      execute: (_id, params, signal, _onUpdate, ctx) => this.execute(params, signal, ctx),
    };
  }

  async execute(params: Params, signal: AbortSignal | undefined, ctx: Context): Promise<ToolResult> {
    const results = await Promise.all(params.tasks.map((task) => this.runTask(task, signal, ctx).catch((error: unknown) => `failed: ${message(error)}`)));
    const text = results.map((result, i) => `## Task ${String(i + 1)}\n${result}`).join("\n\n");
    return { content: [{ type: "text", text }], details: undefined };
  }

  private async runTask(task: Task, signal: AbortSignal | undefined, ctx: Context): Promise<string> {
    const run = task.resume === undefined ? await this.start(task, ctx) : this.resume(task);
    if (this.busy.has(run.id)) throw new Error(`agent ${run.id} is still working on another task`);
    this.busy.add(run.id);
    try {
      return await this.runAgent(run, task.task, signal, ctx);
    } finally {
      this.busy.delete(run.id);
    }
  }

  private async runAgent(run: AgentRun, task: string, signal: AbortSignal | undefined, ctx: Context): Promise<string> {
    const args = [...this.spawner.pi.args, "--mode", "json", "--print", "--session", run.sessionFile];
    args.push("--tools", run.tools.join(","), "--append-system-prompt", run.promptFile, "--extension", this.spawner.extension);
    if (ctx.model !== undefined) args.push("--model", `${ctx.model.provider}/${ctx.model.id}`);
    // A message starting with "-" or "@" would be read as an option or a file.
    args.push("--", `Task:\n${task}`);
    let answer = "";
    const result = await this.spawner.run(this.spawner.pi.command, args, {
      cwd: run.cwd,
      env: { ...this.spawner.env, MILLENNIUM_AGENT: run.agent, MILLENNIUM_AGENT_ID: run.id },
      timeoutMs: agentTimeoutMs,
      signal,
      onLine: (line) => {
        answer = finalAnswer(line) ?? answer;
      },
    });
    const where = run.branch === undefined ? `in ${run.cwd}` : `in worktree ${run.cwd} (branch ${run.branch})`;
    const head = `agent ${run.agent}, id ${run.id}, ${where}`;
    if (result.code !== 0) {
      const why = result.failure ?? `exited with status ${String(result.code)}`;
      return `${head}\nFAILED: the agent ${why}. ${tail(result.stderr, 1500)}\nLast answer:\n${answer || "(none)"}`;
    }
    return `${head}\n\n${answer || "(the agent gave no answer)"}`;
  }

  private async start(task: Task, ctx: Context): Promise<AgentRun> {
    const agentName = task.agent ?? "";
    if (!isAgentName(agentName)) throw new Error(`unknown agent "${agentName}"; use one of ${agentNames.join(", ")}`);
    const agent = loadAgent(this.spawner.pluginRoot, agentName);
    const id = `${agentName}-${randomUUID().slice(0, 8)}`;
    const dir = join(this.spawner.stateDir, "agents");
    mkdirSync(dir, { recursive: true });
    const run: AgentRun = {
      id,
      agent: agentName,
      cwd: ctx.cwd,
      sessionFile: join(dir, `${id}.jsonl`),
      promptFile: join(dir, `${id}.prompt.md`),
      tools: agent.tools,
    };
    writeFileSync(run.promptFile, systemPrompt(this.spawner.pluginRoot, agent), { mode: 0o600 });
    if (agent.worktree) {
      const worktree = await addWorktree(this.spawner.run, ctx.cwd, this.spawner.env, id);
      run.cwd = worktree.path;
      run.branch = worktree.branch;
    }
    this.runs.set(id, run);
    return run;
  }

  private resume(task: Task): AgentRun {
    const run = this.runs.get(task.resume ?? "");
    if (run === undefined) throw new Error(`no agent with id "${task.resume ?? ""}" was started in this session`);
    if (task.agent !== undefined && task.agent !== run.agent) throw new Error(`agent ${run.id} is a ${run.agent}, not a ${task.agent}`);
    return run;
  }
}

/** finalAnswer reads the text of an assistant message from one line of pi's JSON output. */
export function finalAnswer(line: string): string | undefined {
  let event: unknown;
  try {
    event = JSON.parse(line);
  } catch {
    return undefined; // not an event line
  }
  if (!isRecord(event) || event["type"] !== "message_end" || field(event, "message", "role") !== "assistant") return undefined;
  const content = isRecord(event["message"]) ? event["message"]["content"] : undefined;
  if (!Array.isArray(content)) return undefined;
  const text = content.filter((part) => field(part, "type") === "text").map((part) => field(part, "text")).join("\n").trim();
  return text === "" ? undefined : text;
}

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
