//go:build ignore

package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/image/draw"
)

const out = "build/site"

// demoFiles are the files the demo opens, as HEAD has them: enough to
// show Lime, few enough to load fast.
var demoFiles = []string{
	".gitignore", "AGENTS.md", "go.mod", "mise.toml", "mygo.json", "locales/en.json",
	"cmd/lime/main.go", "cmd/lime/commands.go", "cmd/lime/keys.go", "cmd/lime/find.go",
	"cmd/lime/settings.go", "cmd/lime/theme.go",
}

const demoCommits = 12

// demo is demo.<hash>.json: HEAD blobs in full, older blob versions as line
// deltas decoded lazily in git.js, and commits newest first.
type demo struct {
	Blobs   []string       `json:"blobs"`
	Deltas  [][]any        `json:"deltas,omitempty"`
	Commits []demoCommit   `json:"commits"`
	Parent  map[string]int `json:"parent"`
}

type demoCommit struct {
	Hash  string         `json:"hash"`
	Body  string         `json:"body"`
	Name  string         `json:"name"`
	Email string         `json:"email"`
	Time  int64          `json:"time"`
	Tree  map[string]int `json:"tree"`
}

type rawDemo struct {
	blobs   []string
	blobIdx map[string]int
	commits []demoCommit
	parent  map[string]int
}

func main() {
	if err := os.RemoveAll(out); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(out, "fonts"), 0o755); err != nil {
		log.Fatal(err)
	}

	tmpWasm := filepath.Join(out, "lime.wasm")
	wasm := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w -X github.com/egoist/mygo.production=1 -X github.com/egoist/mygo.packageName=Lime -X github.com/egoist/mygo.packageIdentifier=me.xingkaixin.lime", "-tags=mygo_noinspector", "-o", tmpWasm, "./cmd/lime")
	wasm.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")
	wasm.Stdout, wasm.Stderr = os.Stdout, os.Stderr
	if err := wasm.Run(); err != nil {
		log.Fatal(err)
	}
	wasmBytes, err := os.ReadFile(tmpWasm)
	if err != nil {
		log.Fatal(err)
	}
	wasmName := "lime." + shortHash(wasmBytes) + ".wasm"
	if err := os.Rename(tmpWasm, filepath.Join(out, wasmName)); err != nil {
		log.Fatal(err)
	}

	iconBytes := buildIconPNG("resources/icon.png", 128)
	iconName := "icon." + shortHash(iconBytes) + ".png"
	if err := os.WriteFile(filepath.Join(out, iconName), iconBytes, 0o644); err != nil {
		log.Fatal(err)
	}

	copyFile("cmd/lime/fileicons/LICENSE-pierre-icons.md", "LICENSE-pierre-icons.md")

	fontMap := map[string]string{}
	fontEntries, err := os.ReadDir("cmd/lime/fileicons")
	if err != nil {
		log.Fatal(err)
	}
	for _, e := range fontEntries {
		if filepath.Ext(e.Name()) != ".ttf" {
			continue
		}
		b, err := os.ReadFile(filepath.Join("cmd/lime/fileicons", e.Name()))
		if err != nil {
			log.Fatal(err)
		}
		base := strings.TrimSuffix(e.Name(), ".ttf")
		hashed := "fonts/" + base + "." + shortHash(b) + ".ttf"
		if err := os.WriteFile(filepath.Join(out, hashed), b, 0o644); err != nil {
			log.Fatal(err)
		}
		fontMap[base] = hashed
	}
	fontMapJSON, err := json.Marshal(fontMap)
	if err != nil {
		log.Fatal(err)
	}

	demoBytes := buildDemoJSON()
	demoName := "demo." + shortHash(demoBytes) + ".json"
	if err := os.WriteFile(filepath.Join(out, demoName), demoBytes, 0o644); err != nil {
		log.Fatal(err)
	}

	wasmExecPath := filepath.Join(strings.TrimSpace(run(nil, "go", "env", "GOROOT")), "lib", "wasm", "wasm_exec.js")
	inlineJS := strings.Join([]string{
		mustRead("website/fs.js"),
		mustRead("website/git.js"),
		mustRead(wasmExecPath),
	}, ";\n")

	appHTML := mustRead("website/app.html")
	appHTML = strings.NewReplacer(
		"/*__INLINE_SCRIPTS__*/", inlineJS,
		"__WASM_URL__", wasmName,
		"__WASM_SIZE__", strconv.Itoa(len(wasmBytes)),
		"__DEMO_URL__", demoName,
		"__FONT_URLS__", string(fontMapJSON),
	).Replace(appHTML)
	if err := os.WriteFile(filepath.Join(out, "app.html"), []byte(appHTML), 0o644); err != nil {
		log.Fatal(err)
	}

	indexHTML := mustRead("website/index.html")
	indexHTML = strings.NewReplacer(
		"__ICON_URL__", iconName,
		"__WASM_URL__", wasmName,
		"__DEMO_URL__", demoName,
	).Replace(indexHTML)
	if err := os.WriteFile(filepath.Join(out, "index.html"), []byte(indexHTML), 0o644); err != nil {
		log.Fatal(err)
	}
}

func shortHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:4])
}

func mustRead(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	return string(b)
}

// buildIconPNG downsamples the 1024x1024 app icon to size x size (2x Retina
// for the 64px boot spinner) and optimizes it with pngquant/oxipng if available.
func buildIconPNG(srcPath string, size int) []byte {
	f, err := os.Open(srcPath)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		log.Fatal(err)
	}
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, dst); err != nil {
		log.Fatal(err)
	}
	best := buf.Bytes()
	if pq, err := exec.LookPath("pngquant"); err == nil {
		cmd := exec.Command(pq, "--quality=80-95", "--speed=1", "--strip", "-")
		cmd.Stdin = bytes.NewReader(best)
		if qout, err := cmd.Output(); err == nil && len(qout) > 0 && len(qout) < len(best) {
			best = qout
		}
	}
	if oxi, err := exec.LookPath("oxipng"); err == nil {
		tmp := filepath.Join(os.TempDir(), "lime-icon-opt.png")
		if os.WriteFile(tmp, best, 0o600) == nil {
			defer os.Remove(tmp)
			if exec.Command(oxi, "-o", "4", "--strip", "safe", "-q", tmp).Run() == nil {
				if opt, err := os.ReadFile(tmp); err == nil && len(opt) > 0 && len(opt) < len(best) {
					best = opt
				}
			}
		}
	}
	return best
}

func buildDemoJSON() []byte {
	rd := rawDemo{blobIdx: map[string]int{}}
	args := append([]string{"git", "log", "-n" + strconv.Itoa(demoCommits), "--format=%H%x1f%an%x1f%ae%x1f%at%x1f%B%x1e", "--"}, demoFiles...)
	for _, rec := range strings.Split(run(nil, args...), "\x1e") {
		f := strings.Split(strings.TrimLeft(rec, "\n"), "\x1f")
		if len(f) != 5 {
			continue
		}
		at, _ := strconv.ParseInt(f[3], 10, 64)
		rd.commits = append(rd.commits, demoCommit{Hash: f[0], Name: f[1], Email: f[2], Time: at, Body: strings.TrimSpace(f[4]) + "\n"})
	}
	if len(rd.commits) == 0 {
		log.Fatal("no commits of the demo's files")
	}
	for i := range rd.commits {
		rd.commits[i].Tree = rd.tree(rd.commits[i].Hash)
	}
	rd.parent = rd.tree(rd.commits[len(rd.commits)-1].Hash + "^")

	// Encode HEAD blobs in full and older versions of each file as reverse
	// line deltas from the next-newer version.
	d := demo{Commits: make([]demoCommit, len(rd.commits)), Parent: map[string]int{}}
	rawToOut := map[int]int{}
	// First assign full blob indices to HEAD's tree.
	headIdx := map[string]int{}
	for _, p := range demoFiles {
		if old, ok := rd.commits[0].Tree[p]; ok {
			if outIdx, exists := rawToOut[old]; exists {
				headIdx[p] = outIdx
			} else {
				outIdx = len(d.Blobs)
				d.Blobs = append(d.Blobs, rd.blobs[old])
				rawToOut[old] = outIdx
				headIdx[p] = outIdx
			}
		}
	}

	// Track the most recent encoded index per file path as we walk backwards.
	prevForPath := map[string]int{}
	for p, idx := range headIdx {
		prevForPath[p] = idx
	}

	type deltaSpec struct {
		rawIdx  int
		baseOut int
	}
	var pendingDeltas []deltaSpec
	rawToDeltaSlot := map[int]int{}

	encodeTree := func(in map[string]int) map[string]int {
		res := make(map[string]int, len(in))
		for _, p := range demoFiles {
			old, ok := in[p]
			if !ok {
				continue
			}
			if outIdx, exists := rawToOut[old]; exists {
				res[p] = outIdx
				prevForPath[p] = outIdx
				continue
			}
			baseOut, hasBase := prevForPath[p]
			if !hasBase {
				outIdx := len(d.Blobs)
				d.Blobs = append(d.Blobs, rd.blobs[old])
				rawToOut[old] = outIdx
				res[p] = outIdx
				prevForPath[p] = outIdx
				continue
			}
			if slot, exists := rawToDeltaSlot[old]; exists {
				res[p] = -(slot + 1) // temporary negative marker, resolved once len(d.Blobs) is final
				prevForPath[p] = res[p]
				continue
			}
			slot := len(pendingDeltas)
			pendingDeltas = append(pendingDeltas, deltaSpec{rawIdx: old, baseOut: baseOut})
			rawToDeltaSlot[old] = slot
			res[p] = -(slot + 1)
			prevForPath[p] = res[p]
		}
		return res
	}

	for i, c := range rd.commits {
		d.Commits[i] = c
		d.Commits[i].Tree = encodeTree(c.Tree)
	}
	d.Parent = encodeTree(rd.parent)

	baseCount := len(d.Blobs)
	resolveIdx := func(v int) int {
		if v < 0 {
			return baseCount + (-v - 1)
		}
		return v
	}
	for i := range d.Commits {
		for p, v := range d.Commits[i].Tree {
			d.Commits[i].Tree[p] = resolveIdx(v)
		}
	}
	for p, v := range d.Parent {
		d.Parent[p] = resolveIdx(v)
	}

	// Materialize deltas and verify exact round-trip reconstruction of every blob.
	decoded := make([]string, baseCount+len(pendingDeltas))
	copy(decoded, d.Blobs)
	d.Deltas = make([][]any, len(pendingDeltas))
	for slot, sp := range pendingDeltas {
		baseIdx := resolveIdx(sp.baseOut)
		baseText := decoded[baseIdx]
		targetText := rd.blobs[sp.rawIdx]
		ops := lineDelta(baseText, targetText)
		entry := make([]any, 0, len(ops)+1)
		entry = append(entry, baseIdx)
		entry = append(entry, ops...)
		d.Deltas[slot] = entry

		got := applyLineDelta(baseText, ops)
		if got != targetText {
			log.Fatalf("delta round-trip mismatch for slot %d: got len %d, want %d", slot, len(got), len(targetText))
		}
		decoded[baseCount+slot] = got
	}

	b, err := json.Marshal(d)
	if err != nil {
		log.Fatal(err)
	}
	return b
}

// lineDelta computes a compact sequence of copy (>0), skip (<0), and insert
// (string) ops that transform baseText into targetText, using Myers line diff.
func lineDelta(baseText, targetText string) []any {
	a := strings.Split(baseText, "\n")
	b := strings.Split(targetText, "\n")
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	post := 0
	for post < len(a)-pre && post < len(b)-pre && a[len(a)-1-post] == b[len(b)-1-post] {
		post++
	}
	var ops []any
	emitCopy := func(n int) {
		if n <= 0 {
			return
		}
		if len(ops) > 0 {
			if prev, ok := ops[len(ops)-1].(int); ok && prev > 0 {
				ops[len(ops)-1] = prev + n
				return
			}
		}
		ops = append(ops, n)
	}
	emitSkip := func(n int) {
		if n <= 0 {
			return
		}
		if len(ops) > 0 {
			if prev, ok := ops[len(ops)-1].(int); ok && prev < 0 {
				ops[len(ops)-1] = prev - n
				return
			}
		}
		ops = append(ops, -n)
	}

	emitCopy(pre)
	A := a[pre : len(a)-post]
	B := b[pre : len(b)-post]
	n, m := len(A), len(B)
	if n > 0 || m > 0 {
		off := n + m + 1
		v := make([]int, 2*off+1)
		var trace [][]int
		found := false
		for d := 0; d <= n+m && !found; d++ {
			snap := make([]int, len(v))
			copy(snap, v)
			trace = append(trace, snap)
			for k := -d; k <= d; k += 2 {
				var x int
				if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
					x = v[off+k+1]
				} else {
					x = v[off+k-1] + 1
				}
				y := x - k
				for x < n && y < m && A[x] == B[y] {
					x++
					y++
				}
				v[off+k] = x
				if x >= n && y >= m {
					found = true
					break
				}
			}
		}
		type step struct {
			op   byte
			line string
		}
		var rev []step
		x, y := n, m
		for d := len(trace) - 1; d >= 0; d-- {
			t := trace[d]
			k := x - y
			var pk int
			if k == -d || (k != d && t[off+k-1] < t[off+k+1]) {
				pk = k + 1
			} else {
				pk = k - 1
			}
			px := t[off+pk]
			py := px - pk
			for x > px && y > py {
				x--
				y--
				rev = append(rev, step{op: '='})
			}
			if d > 0 {
				if x == px {
					y--
					rev = append(rev, step{op: '+', line: B[y]})
				} else {
					x--
					rev = append(rev, step{op: '-'})
				}
			}
			x, y = px, py
		}
		for i := len(rev) - 1; i >= 0; i-- {
			switch rev[i].op {
			case '=':
				emitCopy(1)
			case '-':
				emitSkip(1)
			case '+':
				ops = append(ops, rev[i].line)
			}
		}
	}
	emitCopy(post)
	return ops
}

func applyLineDelta(baseText string, ops []any) string {
	base := strings.Split(baseText, "\n")
	var out []string
	cur := 0
	for _, op := range ops {
		switch v := op.(type) {
		case int:
			if v > 0 {
				out = append(out, base[cur:cur+v]...)
				cur += v
			} else {
				cur -= v
			}
		case string:
			out = append(out, v)
		}
	}
	return strings.Join(out, "\n")
}

func (d *rawDemo) tree(rev string) map[string]int {
	var in strings.Builder
	for _, p := range demoFiles {
		in.WriteString(rev + ":" + p + "\n")
	}
	r := bufio.NewReader(strings.NewReader(run(strings.NewReader(in.String()), "git", "cat-file", "--batch")))
	tree := map[string]int{}
	for _, p := range demoFiles {
		head, err := r.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}
		f := strings.Fields(head)
		if len(f) != 3 {
			continue
		}
		size, _ := strconv.Atoi(f[2])
		text := make([]byte, size+1) // and the newline after it
		if _, err := io.ReadFull(r, text); err != nil {
			log.Fatal(err)
		}
		i, ok := d.blobIdx[f[0]]
		if !ok {
			i = len(d.blobs)
			d.blobIdx[f[0]] = i
			d.blobs = append(d.blobs, string(text[:size]))
		}
		tree[p] = i
	}
	return tree
}

func run(stdin io.Reader, args ...string) string {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin, cmd.Stderr = stdin, os.Stderr
	b, err := cmd.Output()
	if err != nil {
		log.Fatal(err)
	}
	return string(b)
}

func copyFile(from, to string) {
	b, err := os.ReadFile(from)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, to), b, 0o644); err != nil {
		log.Fatal(err)
	}
}
