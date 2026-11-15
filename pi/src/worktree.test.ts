import assert from "node:assert/strict";
import { after, test } from "node:test";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { exited, fakeRun } from "./__tests__/fakes.ts";
import { runProcess } from "./process.ts";
import { addWorktree } from "./worktree.ts";

// git with the user's and the system's config left out.
const env = { ...process.env, GIT_CONFIG_GLOBAL: "/dev/null", GIT_CONFIG_NOSYSTEM: "1", GIT_AUTHOR_NAME: "t", GIT_AUTHOR_EMAIL: "t@example.com", GIT_COMMITTER_NAME: "t", GIT_COMMITTER_EMAIL: "t@example.com" };
const dirs: string[] = [];
after(() => {
  for (const dir of dirs) rmSync(dir, { recursive: true, force: true });
});

async function git(cwd: string, ...args: string[]): Promise<string> {
  const result = await runProcess("git", args, { cwd, env, timeoutMs: 30_000 });
  assert.equal(result.code, 0, result.stderr);
  return result.stdout.trim();
}

test("addWorktree branches the agent's worktree from HEAD under .pi/worktrees", async () => {
  const repo = mkdtempSync(join(tmpdir(), "millennium-wt-"));
  dirs.push(repo);
  await git(repo, "init", "-q", "-b", "main");
  writeFileSync(join(repo, "a.txt"), "a\n");
  await git(repo, "add", "a.txt");
  await git(repo, "commit", "-q", "-m", "first");
  const head = await git(repo, "rev-parse", "HEAD");
  const worktree = await addWorktree(runProcess, join(repo), env, "software-engineer-1a2b");
  const top = await git(repo, "rev-parse", "--show-toplevel");
  assert.deepEqual(worktree, { path: join(top, ".pi", "worktrees", "software-engineer-1a2b"), branch: "millennium/software-engineer-1a2b" });
  assert.equal(await git(worktree.path, "rev-parse", "HEAD"), head);
  assert.equal(await git(worktree.path, "branch", "--show-current"), "millennium/software-engineer-1a2b");
});

test("addWorktree reports git's failure", async () => {
  const outside = mkdtempSync(join(tmpdir(), "millennium-nogit-"));
  dirs.push(outside);
  await assert.rejects(addWorktree(runProcess, outside, env, "qa-tester-1"), /git rev-parse failed: .*not a git repository/i);
  const { run } = fakeRun(() => ({ code: null, stdout: "", stderr: "", failure: "timed out after 60s" }));
  await assert.rejects(addWorktree(run, "/repo", env, "qa-tester-1"), /git rev-parse failed: timed out after 60s/);
});

test("addWorktree refuses an id git could misread", async () => {
  const { run, calls } = fakeRun(() => exited("/repo\n"));
  await assert.rejects(addWorktree(run, "/repo", env, "-b"), /invalid agent id -b/);
  await assert.rejects(addWorktree(run, "/repo", env, "../x"), /invalid agent id/);
  assert.equal(calls.length, 0);
});
