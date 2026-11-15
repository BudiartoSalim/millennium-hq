// The tool_call handler: every built-in tool call goes to the engine's guard
// first, and anything the guard cannot decide is blocked.
import type { Engine } from "./engine.ts";
import { field } from "./engine.ts";
import { toolEvent, type Identity } from "./hook-event.ts";
import type { Context, ToolCallEvent, ToolCallResult } from "./pi-api.ts";

export type WhoIs = (ctx: Context) => Identity;

/** guardToolCalls returns the tool_call handler. */
export function guardToolCalls(engine: Engine, home: string, whoIs: WhoIs) {
  return async (call: ToolCallEvent, ctx: Context): Promise<ToolCallResult | undefined> => {
    const event = toolEvent(call, ctx.cwd, home, whoIs(ctx));
    if (event === undefined) return undefined;
    const reply = await engine.hook(event);
    if (!reply.ok) {
      return { block: true, reason: `millennium: the guard could not check this call, so it is blocked: ${reply.message}` };
    }
    if (field(reply.output, "hookSpecificOutput", "permissionDecision") === "deny") {
      const reason = field(reply.output, "hookSpecificOutput", "permissionDecisionReason");
      return { block: true, reason: reason || "millennium: the guard denied this call" };
    }
    return undefined;
  };
}
