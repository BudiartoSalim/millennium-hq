import assert from "node:assert/strict";
import { test } from "node:test";
import { exited, fakeContext, fakeRun } from "./__tests__/fakes.ts";
import { Engine } from "./engine.ts";
import { guardToolCalls } from "./guard.ts";
import type { ProcessResult } from "./process.ts";

const launch = { shell: "/bin/sh", launcher: "/p/millennium-code-qc", env: {} };
const engineer = () => ({ sessionId: "s1", agentId: "software-engineer-1", agentType: "software-engineer" });

function guard(result: ProcessResult) {
  const fake = fakeRun(() => result);
  return { handler: guardToolCalls(new Engine(fake.run, launch), "/home/u", engineer), calls: fake.calls };
}

test("the guard blocks what the engine denies, with its reason", async () => {
  const deny = JSON.stringify({
    hookSpecificOutput: { hookEventName: "PreToolUse", permissionDecision: "deny", permissionDecisionReason: "millennium-code-qc: .millennium is protected" },
  });
  const { handler, calls } = guard(exited(deny));
  const result = await handler({ toolName: "write", toolCallId: "c", input: { path: ".millennium/config.json", content: "{}" } }, fakeContext());
  assert.deepEqual(result, { block: true, reason: "millennium-code-qc: .millennium is protected" });
  const sent = JSON.parse(calls[0]?.options.input ?? "");
  assert.equal(sent.tool_name, "Write");
  assert.equal(sent.agent_type, "software-engineer");
  assert.deepEqual(sent.tool_input, { file_path: "/repo/.millennium/config.json" });
});

test("the guard blocks a denial even without a reason", async () => {
  const { handler } = guard(exited('{"hookSpecificOutput":{"permissionDecision":"deny"}}'));
  const result = await handler({ toolName: "bash", toolCallId: "c", input: { command: "x" } }, fakeContext());
  assert.deepEqual(result, { block: true, reason: "millennium: the guard denied this call" });
});

test("the guard allows what the engine allows", async () => {
  const { handler } = guard(exited(""));
  assert.equal(await handler({ toolName: "read", toolCallId: "c", input: { path: "a.go" } }, fakeContext()), undefined);
});

test("the guard blocks every call it cannot check", async () => {
  const { handler } = guard(exited("", 2, "no engine release"));
  const result = await handler({ toolName: "read", toolCallId: "c", input: { path: "a.go" } }, fakeContext());
  assert.equal(result?.block, true);
  assert.match(result?.reason ?? "", /could not check this call, so it is blocked: no engine release/);
});

test("the guard does not ask about tools it does not cover", async () => {
  const { handler, calls } = guard(exited("", 2));
  assert.equal(await handler({ toolName: "millennium_agent", toolCallId: "c", input: {} }, fakeContext()), undefined);
  assert.equal(calls.length, 0);
});
