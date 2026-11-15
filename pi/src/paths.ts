// Resolves a tool's path argument the way pi's built-in tools do, so the
// guard decides on the file the tool will really touch.
import { isAbsolute, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// pi's tools read these as plain spaces (utils/paths.ts, UNICODE_SPACES).
const unicodeSpaces = /[  -   　]/g;

/**
 * resolveToolPath mirrors pi's resolveToCwd: unicode spaces become spaces, a
 * leading "@" is dropped, "~" is the home directory, file:// URLs are paths,
 * and relative paths start at cwd. The guard writes the result back into the
 * tool call, so whatever pi does with an absolute path, it is the path that
 * was checked.
 */
export function resolveToolPath(input: string, cwd: string, home: string): string {
  let p = input.replace(unicodeSpaces, " ");
  if (p.startsWith("@")) p = p.slice(1);
  if (p === "~") {
    p = home;
  } else if (p.startsWith("~/")) {
    p = join(home, p.slice(2));
  } else if (p.startsWith("file://")) {
    p = fileURLToPath(p);
  }
  return isAbsolute(p) ? resolve(p) : resolve(cwd, p);
}
