import assert from "node:assert/strict";
import { test } from "node:test";
import { exited, fakeContext, fakeRun } from "./__tests__/fakes.ts";
import { Engine } from "./engine.ts";
import type { ProcessResult } from "./process.ts";
import { checkBeforeSettle, messageType } from "./settle.ts";

const launch = { shell: "/bin/sh", launcher: "/p/millennium-code-qc", env: {} };
const main = () => ({ sessionId: "s1", agentId: "", agentType: "" });
const earlier = { type: "custom", customType: "other" };

function settle(result: ProcessResult) {
  const fake = fakeRun(() => result);
  return { handler: checkBeforeSettle(new Engine(fake.run, launch), main), calls: fake.calls };
}

test("failing gates send the agent back with the findings", async () => {
  const { handler, calls } = settle(exited(JSON.stringify({ decision: "block", reason: "2 findings; fix them before stopping." })));
  const result = await handler({ entries: [earlier], outcome: "completed" }, fakeContext());
  assert.deepEqual(result, {
    entries: [earlier, { type: "custom_message", customType: messageType, content: "2 findings; fix them before stopping.", display: true }],
    continue: true,
  });
  assert.deepEqual(JSON.parse(calls[0]?.options.input ?? ""), { session_id: "s1", cwd: "/repo", hook_event_name: "Stop" });
});

test("after the last attempt the user is warned and the agent stops", async () => {
  const { handler } = settle(exited(JSON.stringify({ systemMessage: "still failing; the work is NOT done" })));
  const ctx = fakeContext();
  const result = await handler({ entries: [], outcome: "completed" }, ctx);
  assert.equal(result?.continue, undefined);
  assert.deepEqual(ctx.notes, [["still failing; the work is NOT done", "warning"]]);
  assert.equal(result?.entries?.length, 1);
});

test("passing gates let the agent finish", async () => {
  const { handler } = settle(exited(""));
  assert.equal(await handler({ entries: [], outcome: "completed" }, fakeContext()), undefined);
});

test("gates that cannot run leave the work marked unchecked", async () => {
  const { handler } = settle(exited("", 2, "no engine"));
  const ctx = fakeContext("/repo", false);
  const result = await handler({ entries: [], outcome: "completed" }, ctx);
  assert.equal(result?.continue, undefined);
  assert.match(JSON.stringify(result?.entries), /NOT checked: no engine/);
  assert.deepEqual(ctx.notes, []);
});

test("an aborted or failed run is not checked", async () => {
  const { handler, calls } = settle(exited(""));
  assert.equal(await handler({ entries: [], outcome: "aborted" }, fakeContext()), undefined);
  assert.equal(await handler({ entries: [], outcome: "error" }, fakeContext()), undefined);
  assert.equal(calls.length, 0);
});
