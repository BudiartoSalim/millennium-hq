import assert from "node:assert/strict";
import { test } from "node:test";
import { hookEvent, toolEvent, type Identity } from "./hook-event.ts";

const main: Identity = { sessionId: "s1", agentId: "", agentType: "" };
const tester: Identity = { sessionId: "s1", agentId: "qa-tester-1234", agentType: "qa-tester" };

test("hookEvent names the agent only for agents", () => {
  assert.deepEqual(hookEvent("Stop", "/repo", main), { session_id: "s1", cwd: "/repo", hook_event_name: "Stop" });
  assert.deepEqual(hookEvent("Stop", "/repo", tester), {
    session_id: "s1",
    cwd: "/repo",
    hook_event_name: "Stop",
    agent_id: "qa-tester-1234",
    agent_type: "qa-tester",
  });
});

test("toolEvent translates pi's tools into the engine's", () => {
  const cases: Array<[string, Record<string, unknown>, string, Record<string, string>]> = [
    ["read", { path: "src/a.ts" }, "Read", { file_path: "/repo/src/a.ts" }],
    ["edit", { path: "@.millennium/config.json", edits: [] }, "Edit", { file_path: "/repo/.millennium/config.json" }],
    ["write", { path: "/repo/x.go", content: "" }, "Write", { file_path: "/repo/x.go" }],
    ["grep", { pattern: "x", path: "services" }, "Grep", { path: "/repo/services" }],
    ["grep", { pattern: "x" }, "Grep", {}],
    ["find", { pattern: "**/*.ts", path: "." }, "Glob", { path: "/repo" }],
    ["ls", { path: "src" }, "Glob", { path: "/repo/src" }],
    ["ls", {}, "Glob", {}],
    ["bash", { command: "go test ./..." }, "Bash", { command: "go test ./..." }],
    ["powershell", { command: "Remove-Item .millennium" }, "Bash", { command: "Remove-Item .millennium" }],
    ["bash", {}, "Bash", { command: "" }],
  ];
  for (const [toolName, input, wantTool, wantInput] of cases) {
    const event = toolEvent({ toolName, toolCallId: "c1", input }, "/repo", "/home/u", tester);
    assert.equal(event?.tool_name, wantTool, toolName);
    assert.deepEqual(event?.tool_input, wantInput, toolName);
    assert.equal(event?.agent_type, "qa-tester");
  }
});

test("toolEvent rewrites the path it checked, so the tool uses the same file", () => {
  const input: Record<string, unknown> = { path: "~/../../repo/.pi/settings.json" };
  toolEvent({ toolName: "write", toolCallId: "c1", input }, "/repo", "/home/u", main);
  assert.equal(input["path"], "/repo/.pi/settings.json");
});

test("toolEvent leaves other tools alone, path argument included", () => {
  const input: Record<string, unknown> = { path: "@notes" };
  assert.equal(toolEvent({ toolName: "web_fetch", toolCallId: "c1", input }, "/repo", "/home/u", main), undefined);
  assert.equal(input["path"], "@notes");
});
