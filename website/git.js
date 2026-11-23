// limeGit answers the git commands Lime runs (cmd/lime/git_js.go) from a
// repository in memory: the demo's commits, an index, and the work tree in
// limeFS. It prints what git prints for the arguments Lime gives, and no
// more.
(() => {
  let repo

  const load = (root, demo) => {
    const baseCount = demo.blobs.length
    const cache = demo.blobs.slice()
    const deltas = demo.deltas || []
    const getBlob = (i) => {
      if (cache[i] !== undefined) return cache[i]
      const entry = deltas[i - baseCount]
      const base = getBlob(entry[0]).split("\n")
      const out = []
      let cur = 0
      for (let k = 1; k < entry.length; k++) {
        const op = entry[k]
        if (typeof op === "number") {
          if (op > 0) {
            for (let j = 0; j < op; j++) out.push(base[cur++])
          } else {
            cur -= op
          }
        } else {
          out.push(op)
        }
      }
      return (cache[i] = out.join("\n"))
    }
    const materialize = (raw) => new Map(Object.entries(raw).map(([p, i]) => [p, getBlob(i)]))
    const lazyTree = (raw) => {
      let m = null
      return () => (m || (m = materialize(raw)))
    }
    const headTree = materialize(demo.commits[0].tree)
    repo = {
      root, branch: "main",
      commits: demo.commits.map((c, idx) => ({ ...c, getTree: idx === 0 ? () => headTree : lazyTree(c.tree) })),
      getBase: lazyTree(demo.parent),
    }
    repo.index = new Map(headTree)
    return headTree
  }

  const head = () => repo.commits[0]
  const treeOfCommit = (c) => c.tree instanceof Map ? c.tree : c.getTree()
  const short = (hash) => hash.slice(0, 7)
  const subject = (c) => c.body.split("\n")[0]
  const work = () => limeFS.files(repo.root)
  const has = (args, flag) => args.includes(flag)
  const paths = (input) => input.split("\0").filter(Boolean)
  const bytes = (text) => new TextEncoder().encode(text).length

  // oid stands for an object's id: the same for the same text.
  const oid = (text) => {
    let h = 0x811c9dc5, id = ""
    for (let r = 0; r < 5; r++) {
      for (let i = 0; i < text.length; i++) h = Math.imul(h ^ text.charCodeAt(i), 16777619)
      h = Math.imul(h ^ r, 16777619)
      id += (h >>> 0).toString(16).padStart(8, "0")
    }
    return id
  }

  const commitOf = (rev) => {
    const c = rev === "HEAD" ? head() : repo.commits.find((c) => c.hash.startsWith(rev))
    if (!c) throw new Error(`fatal: bad revision '${rev}'`)
    return c
  }
  const parentTree = (c) => {
    const next = repo.commits[repo.commits.indexOf(c) + 1]
    return next ? treeOfCommit(next) : repo.getBase()
  }
  const treeOf = (rev) => {
    if (rev === "") return repo.index
    return rev.endsWith("^") ? parentTree(commitOf(rev.slice(0, -1))) : treeOfCommit(commitOf(rev))
  }

  const format = (fmt, c) => fmt.replace(/%(x[0-9a-fA-F]{2}|H|h|s|an|ae|at|B|n)/g, (_, k) =>
    k[0] === "x" ? String.fromCharCode(parseInt(k.slice(1), 16))
      : { H: c.hash, h: short(c.hash), s: subject(c), an: c.name, ae: c.email, at: String(c.time), B: c.body, n: "\n" }[k])
  const option = (args, name) => args.find((a) => a.startsWith(name))?.slice(name.length)

  const status = () => {
    const h = treeOfCommit(head()), index = repo.index, w = work()
    const all = [...new Set([...h.keys(), ...index.keys(), ...w.keys()])].sort()
    let out = ""
    for (const p of all) {
      const ht = h.get(p), it = index.get(p), wt = w.get(p)
      if (it === undefined && wt !== undefined) {
        if (ht !== undefined) out += `1 D. N... 100644 000000 000000 ${oid(ht)} ${oid("")} ${p}\0`
        out += `? ${p}\0`
        continue
      }
      const x = ht === undefined ? "A" : it === undefined ? "D" : ht !== it ? "M" : "."
      const y = it === undefined ? "." : wt === undefined ? "D" : wt !== it ? "M" : "."
      if (x !== "." || y !== ".") out += `1 ${x}${y} N... 100644 100644 100644 ${oid(ht ?? "")} ${oid(it ?? "")} ${p}\0`
    }
    return out
  }

  const commit = (message, all, amend) => {
    const w = work()
    if (all) {
      for (const p of [...repo.index.keys()]) {
        if (w.has(p)) repo.index.set(p, w.get(p))
        else repo.index.delete(p)
      }
    }
    const h = treeOfCommit(head())
    if (!amend && h.size === repo.index.size && [...h].every(([p, t]) => repo.index.get(p) === t)) {
      throw new Error("nothing to commit, working tree clean")
    }
    const hash = [...crypto.getRandomValues(new Uint8Array(20))].map((b) => b.toString(16).padStart(2, "0")).join("")
    const c = { hash, body: message.trim() + "\n", name: "You", email: "you@example.com", time: Math.floor(Date.now() / 1000), tree: new Map(repo.index) }
    if (amend) repo.commits.shift()
    repo.commits.unshift(c)
    return `[${repo.branch} ${short(hash)}] ${subject(c)}\n`
  }

  const split = (t) => t === "" ? { lines: [], eol: true }
    : t.endsWith("\n") ? { lines: t.slice(0, -1).split("\n"), eol: true } : { lines: t.split("\n"), eol: false }

  const diffLines = (a, b) => {
    let pre = 0, post = 0
    while (pre < a.length && pre < b.length && a[pre] === b[pre]) pre++
    while (post < a.length - pre && post < b.length - pre && a[a.length - 1 - post] === b[b.length - 1 - post]) post++
    const ops = []
    for (let i = 0; i < pre; i++) ops.push([" ", i, i])
    const A = a.slice(pre, a.length - post), B = b.slice(pre, b.length - post)
    const n = A.length, m = B.length, off = n + m + 1
    const v = new Int32Array(2 * off + 1), trace = []
    search: for (let d = 0; d <= n + m; d++) {
      trace.push(v.slice())
      for (let k = -d; k <= d; k += 2) {
        let x = k === -d || (k !== d && v[off + k - 1] < v[off + k + 1]) ? v[off + k + 1] : v[off + k - 1] + 1
        let y = x - k
        while (x < n && y < m && A[x] === B[y]) x++, y++
        v[off + k] = x
        if (x >= n && y >= m) break search
      }
    }
    const mid = []
    let x = n, y = m
    for (let d = trace.length - 1; d >= 0; d--) {
      const t = trace[d], k = x - y
      const pk = k === -d || (k !== d && t[off + k - 1] < t[off + k + 1]) ? k + 1 : k - 1
      const px = t[off + pk], py = px - pk
      while (x > px && y > py) mid.push([" ", --x + pre, --y + pre])
      if (d > 0) mid.push(x === px ? ["+", x + pre, --y + pre] : ["-", --x + pre, y + pre])
      x = px, y = py
    }
    ops.push(...mid.reverse())
    for (let i = 0; i < post; i++) ops.push([" ", a.length - post + i, b.length - post + i])
    return ops
  }

  const hunks = (ta, tb) => {
    const A = split(ta), B = split(tb)
    // A last line without a newline differs from the same line with one.
    const keys = (s) => s.lines.map((l, i) => i === s.lines.length - 1 && !s.eol ? l + "\0" : l)
    const ops = diffLines(keys(A), keys(B))
    const ctx = 3
    const noEOL = (op, i, j) => (op !== "+" && !A.eol && i === A.lines.length - 1) || (op !== "-" && !B.eol && j === B.lines.length - 1)
    let out = ""
    for (let i = 0; i < ops.length; i++) {
      if (ops[i][0] === " ") continue
      let last = i
      for (let k = i + 1; k < ops.length && k - last - 1 <= 2 * ctx; k++) {
        if (ops[k][0] !== " ") last = k
      }
      const lo = Math.max(0, i - ctx), hi = Math.min(ops.length, last + 1 + ctx)
      const run = ops.slice(lo, hi)
      const oldN = run.filter((o) => o[0] !== "+").length, newN = run.filter((o) => o[0] !== "-").length
      const oldAt = run[0][1] + (oldN ? 1 : 0), newAt = run[0][2] + (newN ? 1 : 0)
      out += `@@ -${oldAt},${oldN} +${newAt},${newN} @@\n`
      for (const [op, a, b] of run) {
        out += op + (op === "+" ? B.lines[b] : A.lines[a]) + "\n"
        if (noEOL(op, a, b)) out += "\\ No newline at end of file\n"
      }
      i = hi - 1
    }
    return out
  }

  const patch = (from, to, exclude = []) => {
    let out = ""
    for (const p of [...new Set([...from.keys(), ...to.keys()])].sort()) {
      const a = from.get(p), b = to.get(p)
      if (a === b || exclude.includes(":(top,exclude,literal)" + p)) continue
      out += `diff --git a/${p} b/${p}\n`
      if (a === undefined) out += "new file mode 100644\n"
      if (b === undefined) out += "deleted file mode 100644\n"
      out += `index ${short(oid(a ?? ""))}..${short(oid(b ?? ""))}${a !== undefined && b !== undefined ? " 100644" : ""}\n`
      out += `--- ${a === undefined ? "/dev/null" : "a/" + p}\n+++ ${b === undefined ? "/dev/null" : "b/" + p}\n`
      out += hunks(a ?? "", b ?? "")
    }
    return out
  }

  const nameStatus = (from, to) => {
    let out = ""
    for (const p of [...new Set([...from.keys(), ...to.keys()])].sort()) {
      const a = from.get(p), b = to.get(p)
      if (a !== b) out += `${a === undefined ? "A" : b === undefined ? "D" : "M"}\0${p}\0`
    }
    return out
  }

  const tracked = () => {
    const w = work(), t = new Map()
    for (const p of repo.index.keys()) if (w.has(p)) t.set(p, w.get(p))
    return t
  }

  const catFile = (dir, input, sizes) => {
    const here = dir === repo.root ? "" : dir.slice(repo.root.length + 1) + "/"
    let out = ""
    for (const spec of input.split(sizes ? "\0" : "\n").filter(Boolean)) {
      const at = spec.indexOf(":")
      let path = spec.slice(at + 1), text
      if (path.startsWith("./")) path = here + path.slice(2)
      try {
        text = treeOf(spec.slice(0, at)).get(path)
      } catch {}
      if (sizes) {
        out += (text === undefined ? `${spec} missing` : bytes(text)) + "\0"
        continue
      }
      out += text === undefined ? `${spec} missing\n` : `${oid(text)} blob ${bytes(text)}\n${text}\n`
    }
    return out
  }

  // apply applies the hunks of a patch to the index or work tree, or takes
  // them back out with reverse; --recount: the counts are not read.
  const apply = (input, reverse, cached) => {
    const files = []
    let cur
    for (const l of input.split("\n")) {
      if (l.startsWith("diff --git ")) {
        cur = { oldPath: undefined, newPath: undefined, hunks: [] }
        files.push(cur)
        continue
      }
      if (!cur && (l.startsWith("--- ") || l.startsWith("+++ "))) {
        cur = { oldPath: undefined, newPath: undefined, hunks: [] }
        files.push(cur)
      }
      if (!cur) continue
      const m = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(l)
      if (!cur.hunks.length && l.startsWith("--- ")) cur.oldPath = l === "--- /dev/null" ? null : l.startsWith("--- a/") ? l.slice(6) : l.slice(4)
      else if (!cur.hunks.length && l.startsWith("+++ ")) cur.newPath = l === "+++ /dev/null" ? null : l.startsWith("+++ b/") ? l.slice(6) : l.slice(4)
      else if (m) cur.hunks.push({ old: +m[1], new: +m[2], lines: [] })
      else if (cur.hunks.length && /^[ +\-\\]/.test(l)) cur.hunks.at(-1).lines.push(l)
    }
    if (!files.length) throw new Error("error: unrecognized input")
    const source = cached ? repo.index : work()
    const [onlyFrom, onlyTo] = reverse ? ["+", "-"] : ["-", "+"]
    const updates = []
    for (const f of files) {
      const path = f.newPath ?? f.oldPath
      if (!path || path.startsWith("/") || path.split("/").includes("..")) throw new Error(`error: invalid path '${path ?? ""}'`)
      const creating = reverse ? f.newPath === null : f.oldPath === null
      const deleting = reverse ? f.oldPath === null : f.newPath === null
      if (creating) {
        if (source.has(path)) throw new Error(`error: ${path}: already exists in ${cached ? "index" : "working directory"}`)
      } else if (!source.has(path)) {
        throw new Error(cached ? `error: ${path}: does not exist in index` : `error: ${path}: No such file or directory`)
      }
      const t = split(creating ? "" : source.get(path))
      for (const h of f.hunks.reverse()) {
        const from = [], to = []
        let toNoEOL = false, prev
        for (const l of h.lines) {
          if (l[0] === "\\") {
            if (prev !== onlyFrom) toNoEOL = true
            continue
          }
          prev = l[0]
          if (l[0] !== onlyTo) from.push(l.slice(1))
          if (l[0] !== onlyFrom) to.push(l.slice(1))
        }
        const start = reverse ? h.new : h.old
        const at = from.length ? start - 1 : start
        const got = t.lines.slice(at, at + from.length)
        if (at < 0 || at > t.lines.length || got.length !== from.length || got.some((l, i) => l !== from[i])) {
          throw new Error(`error: patch failed: ${path}:${start}`)
        }
        t.lines.splice(at, from.length, ...to)
        if (at + to.length === t.lines.length) t.eol = !toNoEOL
      }
      if (deleting && t.lines.length) throw new Error(`error: patch failed: ${path}:1`)
      updates.push({ path, deleting, text: t.lines.length ? t.lines.join("\n") + (t.eol ? "\n" : "") : "" })
    }
    for (const u of updates) {
      if (cached) {
        if (u.deleting) repo.index.delete(u.path)
        else repo.index.set(u.path, u.text)
      } else {
        if (u.deleting) limeFS.remove(repo.root + "/" + u.path)
        else limeFS.put(repo.root + "/" + u.path, u.text)
      }
    }
  }

  const run = (dir, input, args) => {
    if (dir !== repo.root && !dir.startsWith(repo.root + "/")) throw new Error("fatal: not a git repository (or any of the parent directories): .git")
    while (args[0] === "--literal-pathspecs") args = args.slice(1)
    const [cmd, ...rest] = args
    switch (cmd) {
      case "rev-parse":
        if (has(rest, "--show-prefix")) return `${repo.root}\n${dir === repo.root ? "" : dir.slice(repo.root.length + 1) + "/"}\n`
        if (has(rest, "--show-toplevel")) return `${repo.root}\n${repo.root}/.git\n${repo.root}/.git\n`
        return (has(rest, "--short") ? short(head().hash) : head().hash) + "\n"
      case "symbolic-ref":
        return repo.branch + "\n"
      case "status":
        return status()
      case "add":
      case "reset": {
        const w = work(), h = treeOfCommit(head())
        const from = cmd === "add" ? w : h
        for (const p of paths(input)) {
          if (from.has(p)) repo.index.set(p, from.get(p))
          else repo.index.delete(p)
        }
        return ""
      }
      case "commit":
        return commit(input, has(rest, "-a"), has(rest, "--amend"))
      case "log": {
        const n = +(option(rest, "-n") ?? Infinity)
        const path = rest[rest.indexOf("--") + 1]
        const cs = rest.includes("--") ? repo.commits.filter((c) => treeOfCommit(c).get(path) !== parentTree(c).get(path)) : repo.commits
        const names = has(rest, "--name-only") ? `\0\n${path}\0` : "\n"
        return cs.slice(0, n).map((c) => format(option(rest, "--format=") ?? "%H", c) + names).join("")
      }
      case "show":
        return format(option(rest, "--format=") ?? "%B", commitOf(rest.at(-1))) + "\n"
      case "diff-tree": {
        const c = commitOf(rest.at(-1))
        return has(rest, "-p") ? patch(parentTree(c), treeOfCommit(c)) : nameStatus(parentTree(c), treeOfCommit(c))
      }
      case "diff":
        return has(rest, "--cached") ? patch(treeOfCommit(head()), repo.index) : patch(repo.index, tracked(), rest)
      case "cat-file":
        return catFile(dir, input, has(rest, "--batch-check=%(objectsize)"))
      case "apply":
        apply(input, has(rest, "--reverse"), has(rest, "--cached"))
        return ""
      case "for-each-ref":
        return rest.at(-1) === "refs/heads" ? `${repo.branch}\t${repo.root}\n` : ""
      case "worktree":
        return `worktree ${repo.root}\nHEAD ${head().hash}\nbranch refs/heads/${repo.branch}\n\n`
      case "switch":
        if (rest.at(-1) === repo.branch) return ""
        throw new Error(`fatal: invalid reference: ${rest.at(-1)}`)
      case "remote":
      case "stash":
        return ""
    }
    throw new Error(`git: '${cmd}' is not a git command`)
  }

  globalThis.limeGit = (dir, input, args) => {
    if (!repo) return { error: "fatal: not a git repository (or any of the parent directories): .git" }
    try {
      return { out: run(dir, input, args) }
    } catch (e) {
      return { error: e.message }
    }
  }
  limeGit.load = load
})()
