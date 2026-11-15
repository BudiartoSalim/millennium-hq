// The part of pi's extension API this extension uses, written out here so the
// extension needs no package of pi's: pi passes these objects at runtime, and
// TypeScript checks them structurally. Field names follow pi's
// @earendil-works/pi-coding-agent types (dist/core/extensions/types.d.ts).

/** A built-in or extension tool call before it runs; `input` is mutable. */
export interface ToolCallEvent {
  toolName: string;
  toolCallId: string;
  input: Record<string, unknown>;
}

/** What a tool_call handler returns to block the call. */
export interface ToolCallResult {
  block?: boolean;
  reason?: string;
}

/** A message an extension adds to the session at a boundary. */
export interface CustomMessageDraft {
  type: "custom_message";
  customType: string;
  content: string;
  display: boolean;
}

/** The last point where an extension can keep the agent working. */
export interface SettleEvent {
  entries: unknown[];
  outcome: "completed" | "aborted" | "error";
}

/** `continue: true` asks pi for one more model request. */
export interface SettleResult {
  entries?: unknown[];
  continue?: boolean;
}

/** Before a run: `sections` are XML-tagged system prompt sections, mutable in place. */
export interface BeforeAgentStartEvent {
  systemPromptOptions: { sections: Record<string, string> };
}

export interface Model {
  provider: string;
  id: string;
}

/** The context pi passes to every handler and tool. */
export interface Context {
  cwd: string;
  hasUI: boolean;
  ui: { notify(message: string, type?: "info" | "warning" | "error"): void };
  sessionManager: { getSessionId(): string };
  model?: Model | undefined;
}

export interface TextContent {
  type: "text";
  text: string;
}

export interface ToolResult {
  content: TextContent[];
  details: unknown;
}

/** A tool the extension adds; `parameters` is a JSON Schema. */
export interface ToolDefinition<P> {
  name: string;
  label: string;
  description: string;
  promptSnippet?: string;
  parameters: object;
  execute(
    toolCallId: string,
    params: P,
    signal: AbortSignal | undefined,
    onUpdate: unknown,
    ctx: Context,
  ): Promise<ToolResult>;
}

export interface PiAPI {
  on(event: "session_start", handler: (event: unknown, ctx: Context) => Promise<void>): void;
  on(event: "before_agent_start", handler: (event: BeforeAgentStartEvent, ctx: Context) => Promise<undefined>): void;
  on(event: "tool_call", handler: (event: ToolCallEvent, ctx: Context) => Promise<ToolCallResult | undefined>): void;
  on(event: "agent_before_settle", handler: (event: SettleEvent, ctx: Context) => Promise<SettleResult | undefined>): void;
  registerTool<P>(tool: ToolDefinition<P>): void;
}
