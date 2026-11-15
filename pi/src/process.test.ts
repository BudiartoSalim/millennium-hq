import assert from "node:assert/strict";
import { test } from "node:test";
import { maxOutput, runProcess, tail } from "./process.ts";

const node = process.execPath;
const options = { cwd: ".", env: process.env, timeoutMs: 10_000 };

test("runProcess passes stdin and returns the exit status and output", async () => {
  const script = "let s='';process.stdin.on('data',d=>s+=d).on('end',()=>{process.stdout.write('got '+s);process.stderr.write('e');process.exit(3)})";
  const result = await runProcess(node, ["-e", script], { ...options, input: "hello" });
  assert.deepEqual(result, { code: 3, stdout: "got hello", stderr: "e" });
});

test("runProcess streams complete stdout lines", async () => {
  const lines: string[] = [];
  const script = "process.stdout.write('a\\n\\nb');setTimeout(()=>process.stdout.write('c\\nd'),20)";
  const result = await runProcess(node, ["-e", script], { ...options, onLine: (line) => lines.push(line) });
  assert.equal(result.code, 0);
  assert.deepEqual(lines, ["a", "bc", "d"]);
});

test("runProcess fails a program that cannot start", async () => {
  const result = await runProcess("/nonexistent/program", [], options);
  assert.equal(result.code, null);
  assert.match(result.failure ?? "", /could not start \/nonexistent\/program/);
});

test("runProcess kills a program that runs too long", async () => {
  const result = await runProcess(node, ["-e", "setTimeout(()=>{},60000)"], { ...options, timeoutMs: 200 });
  assert.equal(result.code, null);
  assert.equal(result.failure, "timed out after 0.2s");
});

test("runProcess stops when cancelled, before or during the run", async () => {
  const controller = new AbortController();
  const running = runProcess(node, ["-e", "setTimeout(()=>{},60000)"], { ...options, signal: controller.signal });
  setTimeout(() => controller.abort(), 100);
  assert.equal((await running).failure, "cancelled");
  const early = await runProcess(node, ["-e", "setTimeout(()=>{},60000)"], { ...options, signal: controller.signal });
  assert.equal(early.failure, "cancelled");
});

test("runProcess fails a program that writes too much", async () => {
  const script = `const b='x'.repeat(1<<20);for(let i=0;i<${String((maxOutput >> 20) + 2)};i++)process.stdout.write(b);process.stderr.write(b.repeat(9))`;
  const result = await runProcess(node, ["-e", script], options);
  assert.equal(result.failure, "too much output");
  assert.ok(result.stdout.length <= maxOutput);
});

test("tail keeps the end of long output", () => {
  assert.equal(tail("  short \n"), "short");
  assert.equal(tail("abcdef", 3), "…def");
});
