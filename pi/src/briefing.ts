// The session briefing: what the engine tells an opted-in session about its
// gates, plus how millennium's skills, written for Claude Code first, read
// in pi. It is a section of the system prompt of every run: a section, not
// a replaced prompt, so pi records it and providers keep their cache.
import type { Engine } from "./engine.ts";
import { field } from "./engine.ts";
import { hookEvent } from "./hook-event.ts";
import type { WhoIs } from "./guard.ts";
import type { BeforeAgentStartEvent, Context } from "./pi-api.ts";

/** sectionName tags the briefing in the system prompt. */
export const sectionName = "millennium";

/** piNotes maps the skills' Claude Code terms to pi. */
export function piNotes(engineCommand: string): string {
  return [
    "millennium in pi: millennium's skills are written for Claude Code first. Read them this way:",
    `- \`"\${CLAUDE_PLUGIN_ROOT}/scripts/millennium-code-qc"\` and \`millennium-code-qc\` mean the engine command ${engineCommand}.`,
    "- `$ARGUMENTS` means the request that follows the skill; `/millennium:<skill>` means `/skill:<skill>`.",
    "- An agent named `millennium:<agent>` runs through the `millennium_agent` tool. Several tasks in one call run in parallel, " +
      "each agent's result names its worktree and branch, and `resume` with an agent's id continues it with its context " +
      "(where the skills say SendMessage). Agents that work in a worktree branch from HEAD, so commit before starting them.",
    "- pi has no web tools: where the skills say WebFetch or WebSearch, use curl.",
  ].join("\n");
}

/** Briefing keeps the text for the session and adds it to each run's system prompt. */
export class Briefing {
  private readonly engine: Engine;
  private readonly whoIs: WhoIs;
  private readonly notes: string;
  private text: string;

  constructor(engine: Engine, whoIs: WhoIs) {
    this.engine = engine;
    this.whoIs = whoIs;
    this.notes = piNotes(engine.command());
    this.text = this.notes;
  }

  /** onSessionStart asks the engine for the briefing; it also fetches the engine on first use. */
  onSessionStart = async (_event: unknown, ctx: Context): Promise<void> => {
    const reply = await this.engine.hook(hookEvent("SessionStart", ctx.cwd, this.whoIs(ctx)));
    if (!reply.ok) {
      // The guard and the gates block on their own; say why now, once.
      if (ctx.hasUI) ctx.ui.notify(`millennium: the engine is not available: ${reply.message}`, "error");
      return;
    }
    const context = field(reply.output, "hookSpecificOutput", "additionalContext");
    this.text = context === "" ? this.notes : `${context}\n\n${this.notes}`;
  };

  onBeforeAgentStart = async (event: BeforeAgentStartEvent): Promise<undefined> => {
    event.systemPromptOptions.sections[sectionName] = this.text;
    return undefined;
  };
}
