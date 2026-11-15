// The agent_before_settle handler: when an agent finishes, the engine runs
// the fast gates, and while they fail the agent is sent back with the
// findings. The engine counts the attempts and gives up after three, so this
// cannot loop.
import type { Engine } from "./engine.ts";
import { field } from "./engine.ts";
import { hookEvent } from "./hook-event.ts";
import type { WhoIs } from "./guard.ts";
import type { Context, CustomMessageDraft, SettleEvent, SettleResult } from "./pi-api.ts";

export const messageType = "millennium-gates";

/** checkBeforeSettle returns the agent_before_settle handler. */
export function checkBeforeSettle(engine: Engine, whoIs: WhoIs) {
  return async (event: SettleEvent, ctx: Context): Promise<SettleResult | undefined> => {
    if (event.outcome !== "completed") return undefined; // aborted or failed: nothing to check
    const reply = await engine.hook(hookEvent("Stop", ctx.cwd, whoIs(ctx)));
    if (!reply.ok) {
      return warn(event, ctx, `millennium: the gates could not run, so this work is NOT checked: ${reply.message}`);
    }
    if (field(reply.output, "decision") === "block") {
      return { entries: [...event.entries, message(field(reply.output, "reason"))], continue: true };
    }
    const warning = field(reply.output, "systemMessage");
    return warning === "" ? undefined : warn(event, ctx, warning);
  };
}

// warn tells the user, and records the warning in the session so a tech lead
// reading an agent's transcript sees it too.
function warn(event: SettleEvent, ctx: Context, text: string): SettleResult {
  if (ctx.hasUI) ctx.ui.notify(text, "warning");
  return { entries: [...event.entries, message(text)] };
}

function message(content: string): CustomMessageDraft {
  return { type: "custom_message", customType: messageType, content, display: true };
}
