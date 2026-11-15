// Translates pi's events into the engine's hook protocol, which is Claude
// Code's hook event format: the engine decides, this extension only adapts.
import type { ToolCallEvent } from "./pi-api.ts";
import { resolveToolPath } from "./paths.ts";

export type HookEventName = "PreToolUse" | "Stop" | "SessionStart";

/** HookEvent is what `millennium-code-qc hook` reads on stdin. */
export interface HookEvent {
  session_id: string;
  cwd: string;
  hook_event_name: HookEventName;
  tool_name?: string;
  tool_input?: Record<string, string>;
  agent_id?: string;
  agent_type?: string;
}

/** Who is acting: the main session has an empty agent id and type. */
export interface Identity {
  sessionId: string;
  agentId: string;
  agentType: string;
}

// pi tools that name one file, as the engine's tool names.
const fileTools: Record<string, string> = { read: "Read", edit: "Edit", write: "Write" };
// pi tools that look through a directory. ls lists names, so it is guarded
// like a search: a black-box tester may not list a component's files.
const searchTools: Record<string, string> = { grep: "Grep", find: "Glob", ls: "Glob" };
const shellTools: Record<string, string> = { bash: "Bash", powershell: "Bash" };

/** hookEvent builds an event for the engine, with the agent's identity. */
export function hookEvent(name: HookEventName, cwd: string, who: Identity): HookEvent {
  const event: HookEvent = { session_id: who.sessionId, cwd, hook_event_name: name };
  if (who.agentId !== "") event.agent_id = who.agentId;
  if (who.agentType !== "") event.agent_type = who.agentType;
  return event;
}

/**
 * toolEvent translates a pi tool call into a PreToolUse event, or returns
 * undefined for a tool the guard does not cover. It rewrites the call's path
 * to the absolute path it checked, so the tool cannot resolve it differently.
 */
export function toolEvent(call: ToolCallEvent, cwd: string, home: string, who: Identity): HookEvent | undefined {
  const input = call.input;
  const shellTool = shellTools[call.toolName];
  if (shellTool !== undefined) {
    const command = typeof input["command"] === "string" ? input["command"] : "";
    return { ...hookEvent("PreToolUse", cwd, who), tool_name: shellTool, tool_input: { command } };
  }
  const fileTool = fileTools[call.toolName];
  const tool = fileTool ?? searchTools[call.toolName];
  if (tool === undefined) return undefined;
  let path: string | undefined;
  if (typeof input["path"] === "string") {
    path = resolveToolPath(input["path"], cwd, home);
    input["path"] = path;
  }
  const field = fileTool === undefined ? "path" : "file_path";
  return { ...hookEvent("PreToolUse", cwd, who), tool_name: tool, tool_input: path === undefined ? {} : { [field]: path } };
}
