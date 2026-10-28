// An in-memory file system for Lime in browsers: it stands in for Node's
// fs, which Go's syscall package calls there. limeFS fills it.
(() => {
  const C = { O_RDONLY: 0, O_WRONLY: 1, O_RDWR: 2, O_CREAT: 64, O_EXCL: 128, O_TRUNC: 512, O_APPEND: 1024, O_DIRECTORY: 65536 }
  const nodes = new Map()
  const fds = new Map()
  let nextIno = 1, nextFd = 100
  const fail = (code) => Object.assign(new Error(code), { code })
  const now = () => Date.now()

  const norm = (p) => {
    const out = []
    for (const part of String(p).split("/")) {
      if (part === "" || part === ".") continue
      if (part === "..") out.pop()
      else out.push(part)
    }
    return "/" + out.join("/")
  }
  const dirOf = (p) => p === "/" ? "/" : p.slice(0, p.lastIndexOf("/")) || "/"
  const make = (dir, data) => ({ dir, data: data || new Uint8Array(0), ino: nextIno++, mtime: now() })
  nodes.set("/", make(true))
  const mkdirs = (p) => {
    p = norm(p)
    if (nodes.has(p)) return
    mkdirs(dirOf(p))
    nodes.set(p, make(true))
  }
  const put = (p, data) => {
    p = norm(p)
    mkdirs(dirOf(p))
    nodes.set(p, make(false, typeof data === "string" ? new TextEncoder().encode(data) : data))
  }
  const children = (p) => {
    const prefix = p === "/" ? "/" : p + "/"
    const names = []
    for (const k of nodes.keys()) {
      if (k !== p && k.startsWith(prefix) && !k.slice(prefix.length).includes("/")) names.push(k.slice(prefix.length))
    }
    return names.sort()
  }
  const stat = (n) => ({
    dev: 1, ino: n.ino, mode: n.dir ? 0o40755 : 0o100644, nlink: 1, uid: 0, gid: 0, rdev: 0,
    size: n.dir ? 0 : n.data.length, blksize: 4096, blocks: Math.ceil((n.dir ? 0 : n.data.length) / 512),
    atimeMs: n.mtime, mtimeMs: n.mtime, ctimeMs: n.mtime,
    isDirectory: () => n.dir,
  })
  const resize = (n, size) => {
    if (size === n.data.length) return
    const data = new Uint8Array(size)
    data.set(n.data.subarray(0, Math.min(size, n.data.length)))
    n.data = data
  }
  const writeAt = (n, buf, at) => {
    if (at + buf.length > n.data.length) resize(n, at + buf.length)
    n.data.set(buf, at)
    n.mtime = now()
  }

  let line = ""
  const log = (buf) => {
    line += new TextDecoder().decode(buf)
    const nl = line.lastIndexOf("\n")
    if (nl >= 0) {
      console.log(line.slice(0, nl))
      line = line.slice(nl + 1)
    }
  }
  const write = (fd, buf, offset, length, position) => {
    buf = buf.subarray(offset, offset + length)
    if (fd === 1 || fd === 2) {
      log(buf)
      return buf.length
    }
    const f = fds.get(fd)
    if (!f) throw fail("EBADF")
    if (f.node.dir) throw fail("EISDIR")
    const at = position ?? (f.flags & C.O_APPEND ? f.node.data.length : f.pos)
    writeAt(f.node, buf, at)
    if (position == null) f.pos = at + buf.length
    return buf.length
  }
  const ok = (cb, fn) => {
    let v
    try {
      v = fn()
    } catch (e) {
      if (!e.code) console.error(e)
      return cb(e.code ? e : fail("EIO"))
    }
    cb(null, v)
  }
  const node = (p) => {
    const n = nodes.get(norm(p))
    if (!n) throw fail("ENOENT")
    return n
  }
  const file = (fd) => {
    const f = fds.get(fd)
    if (!f) throw fail("EBADF")
    return f
  }
  const noop = (...args) => args[args.length - 1](null)

  globalThis.fs = {
    constants: C,
    writeSync: (fd, buf) => write(fd, buf, 0, buf.length, null),
    write: (fd, buf, offset, length, position, cb) => ok(cb, () => write(fd, buf, offset, length, position)),
    open: (p, flags, mode, cb) => ok(cb, () => {
      p = norm(p)
      let n = nodes.get(p)
      if (n && flags & C.O_CREAT && flags & C.O_EXCL) throw fail("EEXIST")
      if (!n) {
        if (!(flags & C.O_CREAT)) throw fail("ENOENT")
        const parent = nodes.get(dirOf(p))
        if (!parent) throw fail("ENOENT")
        if (!parent.dir) throw fail("ENOTDIR")
        n = make(false)
        nodes.set(p, n)
      }
      if (n.dir && flags & (C.O_WRONLY | C.O_RDWR)) throw fail("EISDIR")
      if (!n.dir && flags & C.O_DIRECTORY) throw fail("ENOTDIR")
      if (flags & C.O_TRUNC) resize(n, 0)
      fds.set(nextFd, { node: n, pos: 0, flags })
      return nextFd++
    }),
    close: (fd, cb) => ok(cb, () => { file(fd); fds.delete(fd) }),
    read: (fd, buf, offset, length, position, cb) => ok(cb, () => {
      const f = file(fd)
      if (f.node.dir) throw fail("EISDIR")
      const at = position ?? f.pos
      const n = Math.max(0, Math.min(length, f.node.data.length - at))
      buf.set(f.node.data.subarray(at, at + n), offset)
      if (position == null) f.pos = at + n
      return n
    }),
    fstat: (fd, cb) => ok(cb, () => stat(file(fd).node)),
    stat: (p, cb) => ok(cb, () => stat(node(p))),
    lstat: (p, cb) => ok(cb, () => stat(node(p))),
    readdir: (p, cb) => ok(cb, () => {
      if (!node(p).dir) throw fail("ENOTDIR")
      return children(norm(p))
    }),
    mkdir: (p, perm, cb) => ok(cb, () => {
      p = norm(p)
      if (nodes.has(p)) throw fail("EEXIST")
      if (!nodes.get(dirOf(p))?.dir) throw fail("ENOENT")
      nodes.set(p, make(true))
    }),
    rmdir: (p, cb) => ok(cb, () => {
      if (!node(p).dir) throw fail("ENOTDIR")
      if (children(norm(p)).length) throw fail("ENOTEMPTY")
      nodes.delete(norm(p))
    }),
    unlink: (p, cb) => ok(cb, () => {
      if (node(p).dir) throw fail("EISDIR")
      nodes.delete(norm(p))
    }),
    rename: (from, to, cb) => ok(cb, () => {
      from = norm(from), to = norm(to)
      node(from)
      if (!nodes.get(dirOf(to))?.dir) throw fail("ENOENT")
      for (const [k, n] of [...nodes]) {
        if (k === from || k.startsWith(from + "/")) {
          nodes.delete(k)
          nodes.set(to + k.slice(from.length), n)
        }
      }
    }),
    truncate: (p, len, cb) => ok(cb, () => resize(node(p), len)),
    ftruncate: (fd, len, cb) => ok(cb, () => resize(file(fd).node, len)),
    readlink: (p, cb) => ok(cb, () => { node(p); throw fail("EINVAL") }),
    link: (a, b, cb) => cb(fail("ENOSYS")),
    symlink: (a, b, cb) => cb(fail("ENOSYS")),
    chmod: noop, fchmod: noop, chown: noop, fchown: noop, lchown: noop, utimes: noop, futimes: noop, fsync: noop,
  }

  // files returns the texts of the files under root, by path from it,
  // as git's work tree has them: without .git.
  const files = (root) => {
    const prefix = norm(root) + "/", texts = new Map(), decoder = new TextDecoder()
    for (const [k, n] of nodes) {
      if (!n.dir && k.startsWith(prefix) && !k.startsWith(prefix + ".git/")) texts.set(k.slice(prefix.length), decoder.decode(n.data))
    }
    return texts
  }

  globalThis.limeFS = { put, mkdirs, files }
})()
