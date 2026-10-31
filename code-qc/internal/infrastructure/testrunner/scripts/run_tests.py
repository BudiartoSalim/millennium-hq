"""millennium Python test runner. Run as
`python3 -I -c <this file> <project root> <report path>`.

Discovers and runs the project's unittest tests from its root (tests/ when
present) and writes a JSON report: whether anything failed, a summary, and
line coverage of the project's own files. Coverage uses sys.monitoring on
Python 3.12 and newer, and coverage.py, when installed, before that.
Standard library only otherwise.
"""

import io
import json
import os
import sys
import unittest

ROOT = os.path.realpath(sys.argv[-2])
REPORT = sys.argv[-1]


def main():
    sys.path.insert(0, ROOT)
    os.chdir(ROOT)
    try:
        tracer = Tracer() if sys.version_info >= (3, 12) else CoveragePy()
        tracer.start()
        result, errors = run_suite()
        tracer.stop()
    except Exception as err:  # the engine blocks on a failed report, never on silence
        write({"failed": True, "summary": "the tests could not run: %s: %s" % (type(err).__name__, err), "coverage": {}})
        return
    failures = [str(test) for test, _ in result.failures + result.errors] + errors
    write({
        "failed": bool(failures),
        "summary": summarize(result, failures),
        "coverage": tracer.coverage(),
    })


def run_suite():
    """Discover from tests/ when it exists. A tests/ without __init__.py is
    not a package, so it is its own top level; the project root is on
    sys.path either way, so tests can import the project."""
    tests = os.path.join(ROOT, "tests")
    start = tests if os.path.isdir(tests) else ROOT
    top = ROOT if start == ROOT or os.path.isfile(os.path.join(tests, "__init__.py")) else tests
    loader = unittest.TestLoader()
    suite = loader.discover(start, top_level_dir=top)
    runner = unittest.TextTestRunner(stream=io.StringIO(), verbosity=0)
    return runner.run(suite), list(loader.errors)


def summarize(result, failures):
    if failures:
        return "%d of %d tests failed or errored: %s" % (len(failures), result.testsRun, "; ".join(failures[:10]))
    return "%d tests passed" % result.testsRun


def write(report):
    with open(REPORT, "w", encoding="utf-8") as out:
        json.dump(report, out)


def project_file(filename):
    path = os.path.realpath(filename)
    return path.startswith(ROOT + os.sep) and path.endswith(".py")


def relative(path):
    return os.path.relpath(os.path.realpath(path), ROOT).replace(os.sep, "/")


class Tracer:
    """Records executed lines with sys.monitoring, disabling each location
    after its first hit so tracing stays cheap."""

    def __init__(self):
        self.hits = {}
        self.tool = sys.monitoring.COVERAGE_ID

    def start(self):
        sys.monitoring.use_tool_id(self.tool, "millennium")
        sys.monitoring.register_callback(self.tool, sys.monitoring.events.LINE, self.on_line)
        sys.monitoring.set_events(self.tool, sys.monitoring.events.LINE)

    def on_line(self, code, line):
        if project_file(code.co_filename):
            self.hits.setdefault(os.path.realpath(code.co_filename), set()).add(line)
        return sys.monitoring.DISABLE

    def stop(self):
        sys.monitoring.set_events(self.tool, 0)
        sys.monitoring.free_tool_id(self.tool)

    def coverage(self):
        report = {}
        for path in project_files():
            executed = self.hits.get(path, set())
            report[relative(path)] = {str(line): int(line in executed) for line in executable_lines(path)}
        return report


class CoveragePy:
    """coverage.py, for Python older than 3.12."""

    def __init__(self):
        try:
            import coverage
        except ImportError:
            write({"failed": True, "summary": "Python %s has no sys.monitoring; install coverage.py (pip install coverage) so test coverage can be measured" % sys.version.split()[0], "coverage": {}})
            sys.exit(0)
        self.cov = coverage.Coverage(source=[ROOT], data_file=None)

    def start(self):
        self.cov.start()

    def stop(self):
        self.cov.stop()

    def coverage(self):
        report = {}
        for path in project_files():
            _, statements, _, missing, _ = self.cov.analysis2(path)
            report[relative(path)] = {str(line): int(line not in missing) for line in statements}
        return report


def project_files():
    for directory, dirs, files in os.walk(ROOT):
        dirs[:] = [d for d in dirs if not d.startswith(".") and d not in ("node_modules", "__pycache__", "venv", ".venv")]
        for name in files:
            if name.endswith(".py"):
                yield os.path.join(directory, name)


def executable_lines(path):
    with open(path, encoding="utf-8") as source:
        code = compile(source.read(), path, "exec")
    lines = set()
    stack = [code]
    while stack:
        current = stack.pop()
        lines.update(line for _, _, line in current.co_lines() if line is not None)
        stack.extend(const for const in current.co_consts if hasattr(const, "co_lines"))
    lines.discard(0)
    return sorted(lines)


main()
