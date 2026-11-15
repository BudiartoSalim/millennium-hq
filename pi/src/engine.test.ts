import assert from "node:assert/strict";
import { test } from "node:test";
import { exited, fakeRun } from "./__tests__/fakes.ts";
import { Engine, field, shellQuote, timeouts } from "./engine.ts";
import type { ProcessResult } from "./process.ts";

const launch = { shell: "/bin/sh", launcher: "/p/plugin/scripts/millennium-code-qc", env: { A: "1" } };
const event = { session_id: "s", cwd: "/repo", hook_event_name: "PreToolUse" as const, tool_name: "Read" };

test("Engine.hook runs the launcher with the event on stdin", async () => {
  const { run, calls } = fakeRun(() => exited(""));
  assert.deepEqual(await new Engine(run, launch).hook(event), { ok: true, output: undefined });
  const call = calls[0];
  assert.equal(call?.command, "/bin/sh");
  assert.deepEqual(call?.args, [launch.launcher, "hook"]);
  assert.equal(call?.options.cwd, "/repo");
  assert.deepEqual(call?.options.env, { A: "1" });
  assert.deepEqual(JSON.parse(call?.options.input ?? ""), event);
  assert.equal(call?.options.timeoutMs, timeouts.PreToolUse);
});

test("Engine.hook reads the answer, and anything else is a failure", async () => {
  const cases: Array<[ProcessResult, string]> = [
    [exited('{"decision":"block"}'), "ok"],
    [{ code: null, stdout: "", stderr: "", failure: "timed out after 30s" }, "the engine timed out after 30s"],
    [exited("", 2, "millennium-code-qc: BLOCKED: no engine\n"), "millennium-code-qc: BLOCKED: no engine"],
    [exited("", 1, ""), "the engine exited with status 1"],
    [exited("not json"), "the engine answered with unexpected output: not json"],
    [exited("[1]"), "the engine answered with unexpected output: [1]"],
  ];
  for (const [result, want] of cases) {
    const reply = await new Engine(fakeRun(() => result).run, launch).hook(event);
    assert.equal(reply.ok ? "ok" : reply.message, want);
  }
});

test("Engine.command quotes the launcher for agents' shells", () => {
  const engine = new Engine(fakeRun(() => exited()).run, { ...launch, launcher: "/it's here/millennium-code-qc" });
  assert.equal(engine.command(), `'/it'\\''s here/millennium-code-qc'`);
  assert.equal(shellQuote("a b"), "'a b'");
});

test("field reads nested strings only", () => {
  const output = { a: { b: "x", n: 1 } };
  assert.equal(field(output, "a", "b"), "x");
  assert.equal(field(output, "a", "n"), "");
  assert.equal(field(output, "a", "missing", "deeper"), "");
  assert.equal(field(undefined, "a"), "");
});
