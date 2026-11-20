'use strict';
// millennium TypeScript analyzer. Run as `node -e <this file> -- <repo root>`.
// Reads one JSON request per line on stdin, {"path", "content"} with a
// repository-relative path, and answers each with one line, {"facts"} or
// {"error"}. It parses with the project's own `typescript` package (the
// classic compiler API, TypeScript 6 or older) and needs nothing else.

const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');
const readline = require('node:readline');
const { createRequire, builtinModules } = require('node:module');

const root = path.resolve(process.argv[process.argv.length - 1]);
const builtins = new Set(builtinModules);
const compilers = new Map();
const compilerOptions = new Map();

function main() {
  const lines = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
  lines.on('line', (line) => {
    let response;
    try {
      const request = JSON.parse(line);
      response = { facts: analyze(request.path, request.content, request.runtime === true) };
    } catch (err) {
      response = { error: err instanceof Error ? err.message : String(err) };
    }
    process.stdout.write(JSON.stringify(response) + '\n');
  });
}

// analyze returns the facts of one file. The runtime code costs several
// times the rest (a transpile, a second tree, a token walk), and only the
// tests gate reads it, so it is computed only when the request asks. Its
// first half, the emit, comes before the file's own tree: the transpile's
// peak memory is then not added to the tree's.
function analyze(relPath, content, withRuntime) {
  const file = path.join(root, relPath);
  const ts = compilerFor(file, relPath);
  const options = optionsFor(ts, file);
  const emitted = withRuntime ? emittedCode(ts, file, options, content) : null;
  // The compiler reads extensions case-insensitively, so Foo.TSX holds JSX.
  const kind = /\.tsx$/i.test(relPath) ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
  const source = ts.createSourceFile(file, content, ts.ScriptTarget.Latest, true, kind);
  if (source.parseDiagnostics && source.parseDiagnostics.length > 0) {
    const diag = source.parseDiagnostics[0];
    const line = source.getLineAndCharacterOfPosition(diag.start || 0).line + 1;
    throw new Error(`parse ${relPath}:${line}: ${ts.flattenDiagnosticMessageText(diag.messageText, ' ')}`);
  }
  const ctx = { ts, source, file, relPath, options };
  const imports = collectImports(ctx);
  return {
    generated: /@generated|Code generated .* DO NOT EDIT/.test(content.split('\n').slice(0, 5).join('\n')),
    test: /\.(test|spec)\.[cm]?tsx?$/.test(relPath) || relPath.split('/').includes('__tests__'),
    imports,
    functions: collectFunctions(ctx),
    comments: collectComments(ctx),
    constructs: collectConstructs(ctx, imports),
    runtime: emitted && runtimeCode(ctx, content, emitted),
  };
}

// compilerFor loads `typescript` from the nearest package.json at or above
// the file, inside the repository: the project's own, already vetted copy.
function compilerFor(file, relPath) {
  const projectDir = findUp(path.dirname(file), 'package.json') || root;
  if (compilers.has(projectDir)) return compilers.get(projectDir);
  let ts;
  try {
    ts = createRequire(path.join(projectDir, 'package.json'))('typescript');
  } catch {
    throw new Error(`the typescript package cannot be loaded for ${relPath} from ${projectDir}; install the project's dependencies (npm ci)`);
  }
  if (typeof ts.createSourceFile !== 'function') {
    throw new Error(`typescript ${ts.version} in ${projectDir} has no classic compiler API (TypeScript 7 replaced it); millennium's TypeScript analyzer needs TypeScript 6 or older`);
  }
  compilers.set(projectDir, ts);
  return ts;
}

function findUp(dir, name) {
  for (let current = dir; isWithinRoot(current); current = path.dirname(current)) {
    if (fs.existsSync(path.join(current, name))) return current;
    if (current === root) break;
  }
  return null;
}

function isWithinRoot(p) {
  const rel = path.relative(root, p);
  return rel === '' || (!rel.startsWith('..') && !path.isAbsolute(rel));
}

function optionsFor(ts, file) {
  const dir = findUp(path.dirname(file), 'tsconfig.json');
  if (!dir) return {};
  if (compilerOptions.has(dir)) return compilerOptions.get(dir);
  const configPath = path.join(dir, 'tsconfig.json');
  const read = ts.readConfigFile(configPath, ts.sys.readFile);
  const options = read.error ? {} : ts.parseJsonConfigFileContent(read.config, ts.sys, dir).options;
  compilerOptions.set(dir, options);
  return options;
}

function lineOf(ctx, pos) {
  return ctx.source.getLineAndCharacterOfPosition(pos).line + 1;
}

function toRepoPath(abs) {
  return path.relative(root, abs).split(path.sep).join('/');
}

// --- imports ---

function collectImports(ctx) {
  const { ts } = ctx;
  const out = [];
  const add = (literal) => out.push({ path: literal.text, line: lineOf(ctx, literal.getStart(ctx.source)), ...classify(ctx, literal.text) });
  const visit = (node) => {
    if ((ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) && node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier)) {
      add(node.moduleSpecifier);
    } else if (ts.isImportEqualsDeclaration(node) && ts.isExternalModuleReference(node.moduleReference) && ts.isStringLiteral(node.moduleReference.expression)) {
      add(node.moduleReference.expression);
    } else if (ts.isCallExpression(node) && node.arguments.length >= 1 && ts.isStringLiteral(node.arguments[0]) && isImportCall(ts, node)) {
      add(node.arguments[0]);
    }
    ts.forEachChild(node, visit);
  };
  visit(ctx.source);
  return out;
}

function isImportCall(ts, call) {
  return call.expression.kind === ts.SyntaxKind.ImportKeyword || (ts.isIdentifier(call.expression) && call.expression.text === 'require');
}

// classify resolves an import with TypeScript's own module resolution, so
// tsconfig paths and baseUrl aliases are followed like the compiler does.
function classify(ctx, spec) {
  if (spec.startsWith('node:') || builtins.has(spec) || builtins.has(spec.split('/')[0])) {
    return { kind: 'standard', target: '' };
  }
  const resolved = ctx.ts.resolveModuleName(spec, ctx.file, ctx.options, ctx.ts.sys).resolvedModule;
  if (resolved && !resolved.isExternalLibraryImport) {
    return projectOrExternal(resolved.resolvedFileName);
  }
  if (!resolved && (spec.startsWith('.') || spec.startsWith('/'))) {
    return projectOrExternal(path.resolve(path.dirname(ctx.file), spec));
  }
  return { kind: 'external', target: '' };
}

function projectOrExternal(abs) {
  if (!isWithinRoot(abs) || abs.split(path.sep).includes('node_modules')) {
    return { kind: 'external', target: '' };
  }
  return { kind: 'project', target: toRepoPath(abs) };
}

// --- functions ---

function collectFunctions(ctx) {
  const { ts } = ctx;
  const out = [];
  const seen = new Map();
  const record = (key, node, body, recursion) => {
    const count = (seen.get(key) || 0) + 1;
    seen.set(key, count);
    const m = measure(ctx, body, recursion);
    const start = lineOf(ctx, node.getStart(ctx.source));
    out.push({
      key: count > 1 ? `${key}#${count}` : key,
      line: start,
      lines: lineOf(ctx, node.getEnd()) - start + 1,
      nesting: m.maxDepth,
      cognitive: m.cognitive,
    });
  };
  for (const statement of ctx.source.statements) {
    topLevel(ctx, statement, record);
  }
  return out;
}

function topLevel(ctx, statement, record) {
  const { ts } = ctx;
  if (ts.isFunctionDeclaration(statement) && statement.body) {
    const name = statement.name ? statement.name.text : 'default';
    record(name, statement, statement.body, { name, receiver: '' });
  } else if (ts.isClassDeclaration(statement)) {
    classMembers(ctx, statement.name ? statement.name.text : 'default', statement, record);
  } else if (ts.isVariableStatement(statement)) {
    for (const decl of statement.declarationList.declarations) {
      variable(ctx, decl, record);
    }
  } else if (ts.isExportAssignment(statement) && isFunctionLike(ts, statement.expression)) {
    record('default', statement, statement.expression.body, { name: '', receiver: '' });
  } else if (ts.isExpressionStatement(statement) && ts.isCallExpression(statement.expression)) {
    callbacks(ctx, statement.expression, record);
  }
}

function classMembers(ctx, className, cls, record) {
  const { ts } = ctx;
  for (const member of cls.members) {
    const name = memberName(ts, member);
    if ((ts.isMethodDeclaration(member) || ts.isConstructorDeclaration(member) || ts.isGetAccessor(member) || ts.isSetAccessor(member)) && member.body) {
      record(`${className}.${name}`, member, member.body, { name, receiver: 'this' });
    } else if (ts.isPropertyDeclaration(member) && member.initializer && isFunctionLike(ts, member.initializer)) {
      record(`${className}.${name}`, member, member.initializer.body, { name, receiver: 'this' });
    }
  }
}

function memberName(ts, member) {
  if (ts.isConstructorDeclaration(member)) return 'constructor';
  const base = member.name && (ts.isIdentifier(member.name) || ts.isStringLiteral(member.name) || ts.isPrivateIdentifier(member.name)) ? member.name.text : '?';
  if (ts.isGetAccessor(member)) return `get ${base}`;
  if (ts.isSetAccessor(member)) return `set ${base}`;
  return base;
}

function variable(ctx, decl, record) {
  const { ts } = ctx;
  if (!ts.isIdentifier(decl.name) || !decl.initializer) return;
  const name = decl.name.text;
  if (isFunctionLike(ts, decl.initializer)) {
    record(name, decl, decl.initializer.body, { name, receiver: '' });
  } else if (ts.isObjectLiteralExpression(decl.initializer)) {
    for (const prop of decl.initializer.properties) {
      const propName = prop.name && ts.isIdentifier(prop.name) ? prop.name.text : '?';
      if (ts.isMethodDeclaration(prop) && prop.body) {
        record(`${name}.${propName}`, prop, prop.body, { name: propName, receiver: 'this' });
      } else if (ts.isPropertyAssignment(prop) && isFunctionLike(ts, prop.initializer)) {
        record(`${name}.${propName}`, prop, prop.initializer.body, { name: propName, receiver: '' });
      }
    }
  }
}

// callbacks measures the function arguments of a top-level call, such as an
// Express route handler or a test body, keyed by the call and its first
// string argument: app.get("/users"), test("rejects bad input").
function callbacks(ctx, call, record) {
  const { ts } = ctx;
  const label = call.arguments.length > 0 && ts.isStringLiteralLike(call.arguments[0]) ? JSON.stringify(call.arguments[0].text) : '';
  const callee = call.expression.getText(ctx.source).replace(/\s+/g, '');
  for (const arg of call.arguments) {
    if (isFunctionLike(ts, arg)) {
      record(`${callee}(${label})`, arg, arg.body, { name: '', receiver: '' });
    }
  }
}

function isFunctionLike(ts, node) {
  return node && (ts.isArrowFunction(node) || ts.isFunctionExpression(node));
}

// measure follows the SonarSource cognitive complexity definition, as the Go
// analyzer does: structures cost 1 plus their nesting level, else and else-if
// cost 1, each run of a logical operator costs 1, labelled jumps and direct
// recursion cost 1, and nested functions add a nesting level.
function measure(ctx, body, recursion) {
  const { ts } = ctx;
  const m = { cognitive: 0, maxDepth: 0 };
  const deepen = (level) => { if (level > m.maxDepth) m.maxDepth = level; };
  const structure = (nesting) => { m.cognitive += 1 + nesting; deepen(nesting + 1); };
  const walk = (node, nesting) => {
    if (!node) return;
    if (ts.isIfStatement(node)) return ifStatement(node, nesting, false);
    if (ts.isConditionalExpression(node)) {
      structure(nesting);
      walk(node.condition, nesting);
      walk(node.whenTrue, nesting + 1);
      return walk(node.whenFalse, nesting + 1);
    }
    if (ts.isSwitchStatement(node) || isLoop(ts, node)) {
      structure(nesting);
      return ts.forEachChild(node, (child) => walk(child, child === node.statement || ts.isCaseBlock(child) ? nesting + 1 : nesting));
    }
    if (ts.isCatchClause(node)) {
      structure(nesting);
      return walk(node.block, nesting + 1);
    }
    if (isNestedFunction(ts, node)) {
      deepen(nesting + 1);
      return ts.forEachChild(node, (child) => walk(child, nesting + 1));
    }
    if (ts.isBinaryExpression(node) && isLogical(ts, node.operatorToken.kind)) return logical(node, nesting);
    if ((ts.isBreakStatement(node) || ts.isContinueStatement(node)) && node.label) m.cognitive++;
    if (ts.isCallExpression(node) && isRecursive(ts, node, recursion)) m.cognitive++;
    ts.forEachChild(node, (child) => walk(child, nesting));
  };
  const ifStatement = (node, nesting, elseIf) => {
    if (elseIf) m.cognitive++; else structure(nesting);
    deepen(nesting + 1);
    walk(node.expression, nesting);
    walk(node.thenStatement, nesting + 1);
    if (node.elseStatement && ts.isIfStatement(node.elseStatement)) return ifStatement(node.elseStatement, nesting, true);
    if (node.elseStatement) {
      m.cognitive++;
      walk(node.elseStatement, nesting + 1);
    }
  };
  const logical = (node, nesting) => {
    const ops = [];
    const operands = [];
    flattenLogical(ts, node, ops, operands);
    ops.forEach((op, i) => { if (i === 0 || op !== ops[i - 1]) m.cognitive++; });
    operands.forEach((operand) => walk(operand, nesting));
  };
  walk(body, 0);
  return m;
}

// isNestedFunction matches functions with a body; ts.isFunctionLike would
// also match function types in annotations, which contain no code.
function isNestedFunction(ts, node) {
  return ts.isArrowFunction(node) || ts.isFunctionExpression(node) || ts.isFunctionDeclaration(node) ||
    ts.isMethodDeclaration(node) || ts.isConstructorDeclaration(node) || ts.isGetAccessor(node) || ts.isSetAccessor(node);
}

function isLoop(ts, node) {
  return ts.isForStatement(node) || ts.isForInStatement(node) || ts.isForOfStatement(node) || ts.isWhileStatement(node) || ts.isDoStatement(node);
}

function isLogical(ts, kind) {
  return kind === ts.SyntaxKind.AmpersandAmpersandToken || kind === ts.SyntaxKind.BarBarToken || kind === ts.SyntaxKind.QuestionQuestionToken;
}

function flattenLogical(ts, node, ops, operands) {
  if (ts.isParenthesizedExpression(node) && ts.isBinaryExpression(node.expression) && isLogical(ts, node.expression.operatorToken.kind)) {
    return flattenLogical(ts, node.expression, ops, operands);
  }
  if (ts.isBinaryExpression(node) && isLogical(ts, node.operatorToken.kind)) {
    flattenLogical(ts, node.left, ops, operands);
    ops.push(node.operatorToken.kind);
    return flattenLogical(ts, node.right, ops, operands);
  }
  operands.push(node);
}

function isRecursive(ts, call, recursion) {
  if (!recursion.name) return false;
  const callee = call.expression;
  if (ts.isIdentifier(callee)) return recursion.receiver === '' && callee.text === recursion.name;
  return ts.isPropertyAccessExpression(callee) && callee.expression.kind === ts.SyntaxKind.ThisKeyword &&
    recursion.receiver === 'this' && callee.name.text === recursion.name;
}

// --- comments ---

function collectComments(ctx) {
  const text = ctx.source.getFullText();
  return commentRanges(ctx)
    .map((range) => ({ line: lineOf(ctx, range.pos), text: text.slice(range.pos, range.end) }))
    .sort((a, b) => a.line - b.line);
}

// commentRanges finds the comments around every node, as the compiler
// attaches them.
function commentRanges(ctx) {
  const { ts, source } = ctx;
  const text = source.getFullText();
  const seen = new Set();
  const out = [];
  const take = (ranges) => {
    for (const range of ranges || []) {
      if (seen.has(range.pos)) continue;
      seen.add(range.pos);
      out.push(range);
    }
  };
  const visit = (node) => {
    take(ts.getLeadingCommentRanges(text, node.getFullStart()));
    take(ts.getTrailingCommentRanges(text, node.getEnd()));
    ts.forEachChild(node, visit);
  };
  visit(source);
  take(ts.getLeadingCommentRanges(text, source.endOfFileToken.getFullStart()));
  return out;
}

// --- runtime code ---

// runtimeCode reports the lines that hold runtime code and a digest of it.
// The compiler decides what is erased: the digest covers the tokens of the
// comment-free output of ts.transpileModule with the project's options, and
// a line is runtime when the output's source map points into it (see
// emittedCode, which hashed the output already). The output spells out
// every semicolon, so its tokens keep the line breaks that end a statement
// while its layout, which follows the source's, stays out. Syntax the
// output can drop while it still changes what runs is added from the syntax
// tree (see keptNodes), and so are the comments the toolchain reads. A
// hashbang counts in the digest but marks no line: no test can run it, and
// Node's coverage gives its line zero hits. Without the compiler's pragmas
// the facts are unknown (null).
function runtimeCode(ctx, content, emitted) {
  const { ts, source } = ctx;
  if (!(source.pragmas instanceof Map)) return null;
  const { digest, lines } = emitted;
  multiLineLiterals(ctx, lines);
  addField(digest, 'kept syntax');
  const walker = tokenWalker(ts, source);
  for (const node of keptNodes(ctx)) {
    walker.walk(node, (kind, start, end) => {
      addToken(ts, source, kind, start, end, digest);
      addLines(ctx, start, end, lines);
    });
  }
  for (const range of directiveComments(ctx)) {
    addField(digest, 'directive');
    addField(digest, content.slice(range.pos, range.end));
    addLines(ctx, range.pos, range.end, lines);
  }
  if (hashbangFillsLine(content)) lines.delete(1);
  const count = gitLineCount(content);
  return { lines: [...lines].filter((line) => line <= count).sort((a, b) => a - b), digest: digest.digest('hex') };
}

// emittedCode transpiles the file and returns a digest of the output's
// tokens, still open, and the source lines the source map points at; null
// when the file cannot be transpiled. The emit and its syntax tree are as
// large as the file's own; nothing keeps them once it returns, and the
// emit's text is let go as soon as its tree is built.
function emittedCode(ts, file, options, content) {
  let emit = transpile(ts, file, options, content);
  if (!emit) return null;
  const lines = mappedLines(JSON.parse(emit.sourceMapText).mappings);
  const output = ts.createSourceFile('output.jsx', emit.outputText, ts.ScriptTarget.Latest, false, ts.ScriptKind.JSX);
  emit = null;
  if (output.parseDiagnostics && output.parseDiagnostics.length > 0) return null;
  const digest = crypto.createHash('sha256');
  tokenWalker(ts, output).walk(output, (kind, start, end) => addToken(ts, output, kind, start, end, digest));
  return { digest, lines };
}

function transpile(ts, file, options, content) {
  try {
    return ts.transpileModule(content, { compilerOptions: emitOptions(options), fileName: file, reportDiagnostics: false });
  } catch {
    return null;
  }
}

function emitOptions(options) {
  return Object.assign({}, options, {
    removeComments: true, sourceMap: true, inlineSourceMap: false, inlineSources: false,
    sourceRoot: undefined, mapRoot: undefined, declaration: false, declarationMap: false,
    emitDeclarationOnly: false, noEmit: false,
  });
}

// addField hashes text prefixed with its length, so no two sequences of
// fields share an encoding.
function addField(hash, text) {
  const data = Buffer.from(text, 'utf8');
  const size = Buffer.alloc(8);
  size.writeBigUInt64BE(BigInt(data.length));
  hash.update(size);
  hash.update(data);
}

function addToken(ts, source, kind, start, end, hash) {
  addField(hash, ts.SyntaxKind[kind]);
  addField(hash, source.text.slice(start, end));
}

// tokenWalker walks the tokens of a syntax tree in order, calling
// visit(kind, start, end) for each, without node.getChildren: that builds a
// node for every token and keeps it with the tree, which on a large file
// takes more memory than the tree itself. Child nodes come from
// forEachChild; the tokens between them (punctuation and keywords) are
// rescanned from the text, as getChildren does. A childless node of a token
// kind is one token; any other childless node (`debugger;`) is rescanned
// whole. JSDoc is comment, not syntax. Two things of TypeScript are erased
// here: an inline `type` specifier of an import or export, and the commas
// between specifiers, so adding `type T` to a list leaves its tokens as
// they were.
function tokenWalker(ts, source) {
  const { SyntaxKind } = ts;
  const scanner = ts.createScanner(ts.ScriptTarget.Latest, true, source.languageVariant, source.text);
  const reset = (pos) => (scanner.resetTokenState ? scanner.resetTokenState(pos) : scanner.setTextPos(pos));
  const tokenStart = () => (scanner.getTokenStart ? scanner.getTokenStart() : scanner.getTokenPos());
  const isList = (node) => node.kind === SyntaxKind.NamedImports || node.kind === SyntaxKind.NamedExports;
  // An empty gap holds no token. Scanning it anyway would scan the token
  // after it, the leftmost of the child, so each level of a left-nested
  // chain would scan its leftmost token again: depth times its length.
  const scanGap = (parent, pos, end, visit) => {
    if (pos >= end) return;
    reset(pos);
    for (;;) {
      const kind = scanner.scan();
      const start = tokenStart();
      const tokenEnd = scanner.getTextPos();
      if (kind === SyntaxKind.EndOfFileToken || start >= end || tokenEnd > end) return;
      if (!(kind === SyntaxKind.CommaToken && isList(parent))) visit(kind, start, tokenEnd);
    }
  };
  const erased = (node) => (node.kind >= SyntaxKind.FirstJSDocNode && node.kind <= SyntaxKind.LastJSDocNode) ||
    ((ts.isImportSpecifier(node) || ts.isExportSpecifier(node)) && node.isTypeOnly);
  const walk = (node, visit) => {
    if (erased(node)) return;
    const children = [];
    ts.forEachChild(node, (child) => { children.push(child); });
    if (children.length === 0 && node.kind <= SyntaxKind.LastToken) {
      if (node.kind !== SyntaxKind.EndOfFileToken) visit(node.kind, node.getStart(source), node.end);
      return;
    }
    let pos = node.pos;
    for (const child of children) {
      scanGap(node, pos, child.pos, visit);
      walk(child, visit);
      pos = Math.max(pos, child.end);
    }
    scanGap(node, pos, node.end, visit);
  };
  return { walk };
}

function addLines(ctx, start, end, lines) {
  for (let line = lineOf(ctx, start); line <= lineOf(ctx, end); line++) lines.add(line);
}

// gitLineCount numbers lines as git does: only a line feed ends one.
function gitLineCount(content) {
  const breaks = content.split('\n').length - 1;
  return content === '' || content.endsWith('\n') ? breaks : breaks + 1;
}

const BASE64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';

// mappedLines decodes a source map's mappings (base64 VLQ) into the
// one-based source lines they point at.
function mappedLines(mappings) {
  const lines = new Set();
  let sourceLine = 0;
  for (const segment of mappings.split(/[;,]/)) {
    const fields = decodeVLQ(segment);
    if (fields.length >= 4) {
      sourceLine += fields[2];
      lines.add(sourceLine + 1);
    }
  }
  return lines;
}

function decodeVLQ(segment) {
  const out = [];
  let value = 0;
  let shift = 0;
  for (const char of segment) {
    const digit = BASE64.indexOf(char);
    if (digit < 0) throw new Error(`the compiler's source map holds ${JSON.stringify(char)}, which is not base64`);
    value += (digit & 31) * 2 ** shift;
    shift += 5;
    if (digit & 32) continue;
    out.push(value % 2 === 1 ? -(value - 1) / 2 : value / 2);
    value = 0;
    shift = 0;
  }
  return out;
}

// multiLineLiterals marks every line of a string, template or JSX text that
// spans lines once the output maps any line of it: the source map points
// only at its ends.
function multiLineLiterals(ctx, lines) {
  const { ts, source } = ctx;
  const visit = (node) => {
    if (isLiteralToken(ts, node)) {
      const first = lineOf(ctx, node.getStart(source));
      const last = lineOf(ctx, node.getEnd());
      let mapped = false;
      for (let line = first; line <= last; line++) mapped = mapped || lines.has(line);
      if (mapped) addLines(ctx, node.getStart(source), node.getEnd(), lines);
      return;
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
}

function isLiteralToken(ts, node) {
  return ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node) || ts.isTemplateHead(node) ||
    ts.isTemplateMiddle(node) || ts.isTemplateTail(node) || ts.isJsxText(node);
}

// keptNodes finds syntax that is runtime whatever this compilation emits:
// value imports and exports, which the output drops when they look unused;
// enums, const and declared ones included, whose values other files inline;
// class fields that are not declare or abstract, which other options emit
// (parameter properties always emit); and decorators, with the annotations
// of what they decorate, which emitDecoratorMetadata turns into code. The
// annotations of undecorated fields stay erased.
function keptNodes(ctx) {
  const { ts, source } = ctx;
  const kept = [];
  const visit = (node) => {
    if (isKeptWhole(ts, node)) kept.push(node);
    if (isRuntimeField(ts, node)) kept.push(...(node.modifiers || []), node.name, ...(node.initializer ? [node.initializer] : []));
    if (hasDecorators(ts, node)) kept.push(...decoratedAnnotations(ts, node));
    ts.forEachChild(node, visit);
  };
  visit(source);
  return kept;
}

function isKeptWhole(ts, node) {
  if (ts.isImportDeclaration(node)) return !(node.importClause && node.importClause.isTypeOnly);
  if (ts.isImportEqualsDeclaration(node) || ts.isExportDeclaration(node)) return !node.isTypeOnly;
  return ts.isExportAssignment(node) || ts.isEnumDeclaration(node);
}

function isRuntimeField(ts, node) {
  return ts.isPropertyDeclaration(node) && !hasModifier(node, ts.SyntaxKind.DeclareKeyword) && !hasModifier(node, ts.SyntaxKind.AbstractKeyword);
}

function hasModifier(node, kind) {
  return Boolean(node.modifiers && node.modifiers.some((m) => m.kind === kind));
}

// hasDecorators reads decorators where TypeScript 5 and later keep them, in
// the modifiers, and where older versions did.
function hasDecorators(ts, node) {
  return hasModifier(node, ts.SyntaxKind.Decorator) || Boolean(node.decorators && node.decorators.length);
}

// decoratedAnnotations are a decorated node's decorators and the
// annotations emitDecoratorMetadata records for it: its type, its
// parameters' types, and for a class its constructor's parameter types.
function decoratedAnnotations(ts, node) {
  const owner = ts.isParameter(node) ? node.parent : node;
  const signatures = [owner];
  if (ts.isClassLike(owner)) signatures.push(...owner.members.filter((m) => ts.isConstructorDeclaration(m)));
  const parts = [...(node.modifiers || []), ...(node.decorators || [])].filter((m) => ts.isDecorator(m));
  for (const signature of signatures) {
    if (signature.type) parts.push(signature.type);
    for (const parameter of signature.parameters || []) {
      if (parameter.type) parts.push(parameter.type);
    }
  }
  return parts;
}

// directiveComments are the comments the toolchain reads: the compiler's
// pragmas (triple-slash directives, @jsx and its siblings), the source map
// comments node reads at run time, and a hashbang, which the emit keeps
// and which picks the program that runs the file (see runtimeCode for its
// line).
function directiveComments(ctx) {
  const ranges = [];
  const shebang = hashbangRange(ctx.source.text);
  if (shebang) ranges.push(shebang);
  for (const value of ctx.source.pragmas.values()) {
    for (const pragma of [].concat(value)) ranges.push(pragma.range);
  }
  const text = ctx.source.getFullText();
  for (const range of commentRanges(ctx)) {
    if (/^\/[/*][#@]\s*source(Mapping)?URL=/.test(text.slice(range.pos, range.end))) ranges.push(range);
  }
  return ranges;
}

// hashbangRange is the range of a hashbang line, the compiler's own reading:
// "#!" at the very start, up to the line break.
function hashbangRange(text) {
  if (!text.startsWith('#!')) return null;
  const match = /[\r\n\u2028\u2029]/.exec(text);
  return { pos: 0, end: match ? match.index : text.length };
}

// hashbangFillsLine reports whether a hashbang is all of line 1 as git
// numbers it: it ends at a line feed or at the end of the text. A line
// break git does not count leaves more code on git's line 1.
function hashbangFillsLine(text) {
  const range = hashbangRange(text);
  return range !== null && (range.end === text.length || text[range.end] === '\n' || text.startsWith('\r\n', range.end));
}

// --- constructs ---

function collectConstructs(ctx, imports) {
  const { ts, source } = ctx;
  const out = [];
  const add = (kind, node, detail) => out.push({ kind, line: lineOf(ctx, node.getStart(source)), detail });
  const importsChildProcess = imports.some((imp) => imp.path === 'child_process' || imp.path === 'node:child_process');
  const visit = (node) => {
    if (ts.isCallExpression(node)) callConstruct(ts, node, importsChildProcess, add);
    if (ts.isNewExpression(node) && ts.isIdentifier(node.expression) && node.expression.text === 'Function') add('dynamic-code', node, 'new Function');
    if (ts.isJsxAttribute(node) && node.name.getText(source) === 'dangerouslySetInnerHTML') add('html-injection', node, 'dangerouslySetInnerHTML');
    if (ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.EqualsToken && ts.isPropertyAccessExpression(node.left)) {
      assignmentConstruct(ts, node, add);
    }
    if (ts.isPropertyAssignment(node) && propertyName(ts, node) === 'rejectUnauthorized' && node.initializer.kind === ts.SyntaxKind.FalseKeyword) {
      add('tls-verify-off', node, 'rejectUnauthorized: false');
    }
    if (node.kind === ts.SyntaxKind.AnyKeyword) add('explicit-any', node, 'any');
    ts.forEachChild(node, visit);
  };
  visit(source);
  return out;
}

function callConstruct(ts, call, importsChildProcess, add) {
  const name = calleeName(ts, call.expression);
  if (name === 'eval') add('dynamic-code', call, 'eval');
  if (importsChildProcess && (name === 'exec' || name === 'execSync')) add('shell-command', call, name);
  if (name === 'insertAdjacentHTML' || name === 'document.write') add('html-injection', call, name);
  const options = call.arguments.find((arg) => ts.isObjectLiteralExpression(arg));
  if (importsChildProcess && options && options.properties.some((p) => ts.isPropertyAssignment(p) && propertyName(ts, p) === 'shell' && p.initializer.kind === ts.SyntaxKind.TrueKeyword)) {
    add('shell-command', call, 'shell: true');
  }
}

function assignmentConstruct(ts, node, add) {
  const prop = node.left.name.text;
  if (prop === 'innerHTML' || prop === 'outerHTML') add('html-injection', node, prop);
  if (node.left.getText().replace(/\s+/g, '') === 'process.env.NODE_TLS_REJECT_UNAUTHORIZED') add('tls-verify-off', node, 'NODE_TLS_REJECT_UNAUTHORIZED');
}

function calleeName(ts, expr) {
  if (ts.isIdentifier(expr)) return expr.text;
  if (ts.isPropertyAccessExpression(expr)) {
    return ts.isIdentifier(expr.expression) && expr.expression.text === 'document' ? `document.${expr.name.text}` : expr.name.text;
  }
  return '';
}

function propertyName(ts, prop) {
  return prop.name && (ts.isIdentifier(prop.name) || ts.isStringLiteral(prop.name)) ? prop.name.text : '';
}

main();
