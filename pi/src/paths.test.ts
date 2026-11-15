import assert from "node:assert/strict";
import { test } from "node:test";
import { resolveToolPath } from "./paths.ts";

test("resolveToolPath reads paths the way pi's tools do", () => {
  const cases: Array<[string, string]> = [
    ["src/a.ts", "/repo/src/a.ts"],
    ["./src/../.millennium/config.json", "/repo/.millennium/config.json"],
    ["@.millennium/config.json", "/repo/.millennium/config.json"],
    ["/etc/hosts", "/etc/hosts"],
    ["/repo//x/./y", "/repo/x/y"],
    ["~", "/home/u"],
    ["~/notes.md", "/home/u/notes.md"],
    ["~user/x", "/repo/~user/x"],
    ["file:///repo/.pi/settings.json", "/repo/.pi/settings.json"],
    ["a b　c", "/repo/a b c"],
  ];
  for (const [input, want] of cases) {
    assert.equal(resolveToolPath(input, "/repo", "/home/u"), want, input);
  }
});
