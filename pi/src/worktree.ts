// Creates the git worktree an agent works in, branched from HEAD.
import { join } from "node:path";
import { tail, type RunProcess } from "./process.ts";

/** worktreesDir is where agent worktrees go, relative to the repository root (git-ignored by `config apply`). */
export const worktreesDir = join(".pi", "worktrees");

export interface Worktree {
  path: string;
  branch: string;
}

/**
 * addWorktree creates a worktree for the agent id at
 * <repository>/.pi/worktrees/<id>, on a new branch millennium/<id> from HEAD.
 * The id is the extension's own, never a model's, and git reads no
 * repository config that could run a program (core.fsmonitor).
 */
export async function addWorktree(run: RunProcess, cwd: string, env: Record<string, string | undefined>, id: string): Promise<Worktree> {
  if (!/^[a-z][a-z0-9-]*$/.test(id)) throw new Error(`invalid agent id ${id}`);
  const top = (await git(run, cwd, env, ["rev-parse", "--show-toplevel"])).trim();
  const worktree = { path: join(top, worktreesDir, id), branch: `millennium/${id}` };
  await git(run, top, env, ["worktree", "add", "-b", worktree.branch, worktree.path, "HEAD"]);
  return worktree;
}

async function git(run: RunProcess, cwd: string, env: Record<string, string | undefined>, args: string[]): Promise<string> {
  const result = await run("git", ["-c", "core.fsmonitor=false", ...args], { cwd, env, timeoutMs: 60_000 });
  if (result.code !== 0) {
    throw new Error(`git ${args[0] ?? ""} failed: ${result.failure ?? tail(result.stderr, 500)}`);
  }
  return result.stdout;
}
