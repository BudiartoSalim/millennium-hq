import assert from "node:assert/strict";
import { test } from "node:test";
import { exited, fakeContext, fakeRun } from "./__tests__/fakes.ts";
import { Briefing, piNotes, sectionName } from "./briefing.ts";
import { Engine } from "./engine.ts";
import type { ProcessResult } from "./process.ts";

const launch = { shell: "/bin/sh", launcher: "/p/scripts/millennium-code-qc", env: {} };
const main = () => ({ sessionId: "s1", agentId: "", agentType: "" });

// section runs onBeforeAgentStart and returns the briefing section it set.
async function section(b: Briefing): Promise<string | undefined> {
  const event = { systemPromptOptions: { sections: { preamble: "base" } as Record<string, string> } };
  assert.equal(await b.onBeforeAgentStart(event), undefined);
  assert.equal(event.systemPromptOptions.sections["preamble"], "base");
  return event.systemPromptOptions.sections[sectionName];
}

function briefing(result: ProcessResult) {
  const fake = fakeRun(() => result);
  return { briefing: new Briefing(new Engine(fake.run, launch), main), calls: fake.calls };
}

test("an opted-in session gets the engine's briefing and the pi notes", async () => {
  const context = JSON.stringify({ hookSpecificOutput: { hookEventName: "SessionStart", additionalContext: "millennium is active." } });
  const { briefing: b, calls } = briefing(exited(context));
  await b.onSessionStart(undefined, fakeContext());
  assert.equal(await section(b), `millennium is active.\n\n${piNotes("'/p/scripts/millennium-code-qc'")}`);
  assert.equal(JSON.parse(calls[0]?.options.input ?? "").hook_event_name, "SessionStart");
});

test("a repository that is not opted in gets only the pi notes", async () => {
  const { briefing: b } = briefing(exited(""));
  await b.onSessionStart(undefined, fakeContext());
  assert.equal(await section(b), piNotes("'/p/scripts/millennium-code-qc'"));
});

test("an engine that cannot run is reported once, at session start", async () => {
  const { briefing: b } = briefing(exited("", 2, "this plugin version has no engine release yet"));
  const ctx = fakeContext();
  await b.onSessionStart(undefined, ctx);
  assert.deepEqual(ctx.notes, [["millennium: the engine is not available: this plugin version has no engine release yet", "error"]]);
  await b.onSessionStart(undefined, fakeContext("/repo", false));
  assert.match((await section(b)) ?? "", /^millennium in pi/);
});

test("the pi notes name the engine command and the agent tool", () => {
  const notes = piNotes("'/x/millennium-code-qc'");
  assert.match(notes, /mean the engine command '\/x\/millennium-code-qc'/);
  assert.match(notes, /`millennium_agent` tool/);
  assert.match(notes, /use curl/);
});
