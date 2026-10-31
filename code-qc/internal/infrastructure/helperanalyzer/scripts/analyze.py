"""millennium Python analyzer. Run as `python3 -I -c <this file> <repo root>`.

Reads one JSON request per line on stdin, {"path", "content"} with a
repository-relative path, and answers each with one line, {"facts"} or
{"error"}. Standard library only; -I keeps the repository's own modules and
the PYTHON* environment out of the import path.
"""

import ast
import io
import json
import os
import re
import sys
import tokenize

ROOT = os.path.realpath(sys.argv[-1])
GENERATED = re.compile(r"@generated|Code generated .* DO NOT EDIT|DO NOT EDIT")


def main():
    if sys.version_info < (3, 10):
        fail_all("millennium's Python analyzer needs Python 3.10 or newer, found " + sys.version.split()[0])
        return
    for line in sys.stdin:
        try:
            request = json.loads(line)
            response = {"facts": analyze(request["path"], request["content"])}
        except SyntaxError as err:
            response = {"error": "parse %s:%s: %s" % (err.filename, err.lineno, err.msg)}
        except Exception as err:  # every failure must reach the engine, which blocks on it
            response = {"error": "%s: %s" % (type(err).__name__, err)}
        sys.stdout.write(json.dumps(response) + "\n")
        sys.stdout.flush()


def fail_all(message):
    for _ in sys.stdin:
        sys.stdout.write(json.dumps({"error": message}) + "\n")
        sys.stdout.flush()


def analyze(rel_path, content):
    tree = ast.parse(content, filename=rel_path)
    parents = {child: node for node in ast.walk(tree) for child in ast.iter_child_nodes(node)}
    name = os.path.basename(rel_path)
    return {
        "generated": bool(GENERATED.search("\n".join(content.splitlines()[:5]))),
        "test": name.startswith("test_") or name.endswith("_test.py") or "tests" in rel_path.split("/")[:-1],
        "imports": collect_imports(tree, rel_path),
        "functions": collect_functions(tree, parents),
        "comments": collect_comments(content),
        "constructs": collect_constructs(tree),
    }


# --- imports ---

def collect_imports(tree, rel_path):
    out = []
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            for alias in node.names:
                out.append(dict(path=alias.name, line=node.lineno, **classify(alias.name, rel_path)))
        elif isinstance(node, ast.ImportFrom):
            spec = "." * node.level + (node.module or "")
            out.append(dict(path=spec, line=node.lineno, **classify_from(node, rel_path)))
    return sorted(out, key=lambda imp: imp["line"])


def classify_from(node, rel_path):
    if node.level == 0:
        return classify(node.module, rel_path)
    base = os.path.dirname(rel_path)
    for _ in range(node.level - 1):
        base = os.path.dirname(base)
    parts = [base] + (node.module.split(".") if node.module else [])
    return {"kind": "project", "target": "/".join(p for p in parts if p)}


def classify(module, rel_path):
    top = module.split(".")[0]
    if top in sys.stdlib_module_names or top == "__future__":
        return {"kind": "standard", "target": ""}
    location = find_package(top, rel_path)
    if location is None:
        return {"kind": "external", "target": ""}
    rest = module.split(".")[1:]
    return {"kind": "project", "target": "/".join([location] + rest)}


def find_package(top, rel_path):
    """Walk up from the file towards the repository root looking for the
    top-level package as a directory or module, including a src/ layout."""
    current = os.path.dirname(rel_path)
    while True:
        for prefix in (current, join(current, "src")):
            candidate = join(prefix, top)
            absolute = os.path.join(ROOT, candidate)
            if os.path.isdir(absolute) or os.path.isfile(absolute + ".py"):
                return candidate
        if current == "":
            return None
        current = os.path.dirname(current)


def join(*parts):
    return "/".join(p for p in parts if p)


# --- functions ---

def collect_functions(tree, parents):
    collector = FunctionCollector(parents)
    for node in tree.body:
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            collector.record(node.name, node, "")
        elif isinstance(node, ast.ClassDef):
            collector.visit_class(node, node.name)
    return collector.functions


class FunctionCollector:
    """Measures module-level functions and class methods, keyed by name
    ("load", "Engine.run"); repeated keys get "#2", "#3"."""

    def __init__(self, parents):
        self.parents = parents
        self.functions = []
        self.seen = {}

    def record(self, key, node, receiver):
        self.seen[key] = self.seen.get(key, 0) + 1
        metrics = Metrics(node.name, receiver, self.parents)
        metrics.walk_all(node.body, 0)
        self.functions.append({
            "key": key if self.seen[key] == 1 else "%s#%d" % (key, self.seen[key]),
            "line": node.lineno,
            "lines": node.end_lineno - node.lineno + 1,
            "nesting": metrics.max_depth,
            "cognitive": metrics.cognitive,
        })

    def visit_class(self, cls, prefix):
        for member in cls.body:
            if isinstance(member, (ast.FunctionDef, ast.AsyncFunctionDef)):
                receiver = member.args.args[0].arg if member.args.args else ""
                self.record("%s.%s" % (prefix, member.name), member, receiver)
            elif isinstance(member, ast.ClassDef):
                self.visit_class(member, "%s.%s" % (prefix, member.name))


class Metrics:
    """SonarSource cognitive complexity, as the Go and TypeScript analyzers
    measure it: structures cost 1 plus their nesting level, elif and else cost
    1, each boolean operator sequence costs 1, direct recursion costs 1, and
    nested functions and lambdas add a nesting level."""

    STRUCTURES = (ast.For, ast.AsyncFor, ast.While, ast.Match)

    def __init__(self, name, receiver, parents):
        self.name = name
        self.receiver = receiver
        self.parents = parents
        self.cognitive = 0
        self.max_depth = 0

    def deepen(self, level):
        self.max_depth = max(self.max_depth, level)

    def structure(self, nesting):
        self.cognitive += 1 + nesting
        self.deepen(nesting + 1)

    def walk_all(self, nodes, nesting):
        for node in nodes:
            self.walk(node, nesting)

    def walk(self, node, nesting):
        if isinstance(node, ast.If):
            return self.if_statement(node, nesting, elif_=False)
        if isinstance(node, self.STRUCTURES):
            return self.loop_or_match(node, nesting)
        if isinstance(node, (ast.Try, getattr(ast, "TryStar", ast.Try))):
            return self.try_statement(node, nesting)
        if isinstance(node, ast.IfExp):
            self.structure(nesting)
            self.walk(node.test, nesting)
            return self.walk_all([node.body, node.orelse], nesting + 1)
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef, ast.Lambda, ast.ClassDef)):
            self.deepen(nesting + 1)
            return self.walk_all(ast.iter_child_nodes(node), nesting + 1)
        if isinstance(node, ast.BoolOp) and not self.continues_run(node):
            self.cognitive += 1
        if isinstance(node, ast.Call) and self.is_recursive(node):
            self.cognitive += 1
        self.walk_all(ast.iter_child_nodes(node), nesting)

    def if_statement(self, node, nesting, elif_):
        if elif_:
            self.cognitive += 1
        else:
            self.structure(nesting)
        self.deepen(nesting + 1)
        self.walk(node.test, nesting)
        self.walk_all(node.body, nesting + 1)
        if is_elif(node):
            return self.if_statement(node.orelse[0], nesting, elif_=True)
        if node.orelse:
            self.cognitive += 1
            self.walk_all(node.orelse, nesting + 1)

    def loop_or_match(self, node, nesting):
        self.structure(nesting)
        header = [node.subject] if isinstance(node, ast.Match) else [getattr(node, "iter", None) or node.test]
        self.walk_all([h for h in header if h is not None], nesting)
        body = [case for case in node.cases] if isinstance(node, ast.Match) else node.body
        self.walk_all(body, nesting + 1)
        if getattr(node, "orelse", None):
            self.cognitive += 1
            self.walk_all(node.orelse, nesting + 1)

    def try_statement(self, node, nesting):
        self.walk_all(node.body, nesting)
        for handler in node.handlers:
            self.structure(nesting)
            self.walk_all(handler.body, nesting + 1)
        self.walk_all(node.orelse + node.finalbody, nesting)

    def continues_run(self, node):
        parent = self.parents.get(node)
        return isinstance(parent, ast.BoolOp) and type(parent.op) is type(node.op)

    def is_recursive(self, call):
        func = call.func
        if isinstance(func, ast.Name):
            return self.receiver == "" and func.id == self.name
        return (isinstance(func, ast.Attribute) and isinstance(func.value, ast.Name)
                and self.receiver != "" and func.value.id == self.receiver and func.attr == self.name)


def is_elif(node):
    """ast writes `elif` and `else: if` the same way; an elif's If sits at the
    same column as the if it continues."""
    return (len(node.orelse) == 1 and isinstance(node.orelse[0], ast.If)
            and node.orelse[0].col_offset == node.col_offset)


# --- comments ---

def collect_comments(content):
    out = []
    tokens = tokenize.generate_tokens(io.StringIO(content).readline)
    for token in tokens:
        if token.type == tokenize.COMMENT:
            out.append({"line": token.start[0], "text": token.string})
    return out


# --- constructs ---

UNSAFE_LOADERS = {("pickle", "load"), ("pickle", "loads"), ("marshal", "load"), ("marshal", "loads"),
                  ("shelve", "open"), ("dill", "load"), ("dill", "loads")}


def collect_constructs(tree):
    out = []

    def add(kind, node, detail):
        out.append({"kind": kind, "line": node.lineno, "detail": detail})

    for node in ast.walk(tree):
        if isinstance(node, ast.Call):
            call_constructs(node, add)
        elif isinstance(node, ast.Assign):
            assign_constructs(node, add)
    return sorted(out, key=lambda c: c["line"])


def assign_constructs(node, add):
    disables_hostname_check = any(
        isinstance(target, ast.Attribute) and target.attr == "check_hostname" for target in node.targets)
    if disables_hostname_check and is_false(node.value):
        add("tls-verify-off", node, "check_hostname = False")


def call_constructs(call, add):
    name = dotted(call.func)
    if name in ("eval", "exec", "compile"):
        add("dynamic-code", call, name)
    if name in ("os.system", "os.popen"):
        add("shell-command", call, name)
    keywords = {kw.arg: kw.value for kw in call.keywords if kw.arg}
    if "shell" in keywords and is_true(keywords["shell"]):
        add("shell-command", call, "shell=True")
    if "verify" in keywords and is_false(keywords["verify"]):
        add("tls-verify-off", call, "verify=False")
    if name.endswith("_create_unverified_context"):
        add("tls-verify-off", call, name)
    if tuple(name.split(".")[-2:]) in UNSAFE_LOADERS:
        add("unsafe-deserialization", call, name)
    if name in ("yaml.load", "yaml.load_all") and "Safe" not in dotted(keywords.get("Loader")):
        add("unsafe-deserialization", call, name + " without a safe Loader")


def dotted(node):
    if isinstance(node, ast.Name):
        return node.id
    if isinstance(node, ast.Attribute):
        base = dotted(node.value)
        return base + "." + node.attr if base else node.attr
    return ""


def is_true(node):
    return isinstance(node, ast.Constant) and node.value is True


def is_false(node):
    return isinstance(node, ast.Constant) and node.value is False


main()
