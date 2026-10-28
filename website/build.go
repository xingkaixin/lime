//go:build ignore

// Builds the website into build/site: the landing page, and the demo it
// runs, Lime compiled to WebAssembly with a few of the repository's files
// and their last commits.
//
//	go run website/build.go
package main

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const out = "build/site"

// demoFiles are the files the demo opens, as HEAD has them: enough to
// show Lime, few enough to load fast.
var demoFiles = []string{
	".gitignore", "AGENTS.md", "go.mod", "mise.toml", "mygo.json", "locales/en.json",
	"cmd/lime/main.go", "cmd/lime/commands.go", "cmd/lime/keys.go", "cmd/lime/find.go",
	"cmd/lime/settings.go", "cmd/lime/theme.go",
}

// demoCommits is how many of the commits that changed demoFiles the
// demo's history has.
const demoCommits = 12

// demo is demo.json: the commits, newest first, each with demoFiles as it
// left them, by index in Blobs; Parent has them before the oldest commit.
type demo struct {
	WasmSize int            `json:"wasmSize"`
	Blobs    []string       `json:"blobs"`
	Commits  []demoCommit   `json:"commits"`
	Parent   map[string]int `json:"parent"`
	blob     map[string]int // index in Blobs by object id
}

type demoCommit struct {
	Hash  string         `json:"hash"`
	Body  string         `json:"body"`
	Name  string         `json:"name"`
	Email string         `json:"email"`
	Time  int64          `json:"time"`
	Tree  map[string]int `json:"tree"`
}

func main() {
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatal(err)
	}
	wasm := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w -X github.com/egoist/mygo.production=1 -X github.com/egoist/mygo.packageName=Lime -X github.com/egoist/mygo.packageIdentifier=me.xingkaixin.lime", "-tags=mygo_noinspector", "-o", filepath.Join(out, "lime.wasm"), "./cmd/lime")
	wasm.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")
	wasm.Stdout, wasm.Stderr = os.Stdout, os.Stderr
	if err := wasm.Run(); err != nil {
		log.Fatal(err)
	}
	copyFile(filepath.Join(strings.TrimSpace(run(nil, "go", "env", "GOROOT")), "lib", "wasm", "wasm_exec.js"), "wasm_exec.js")
	copyFile("resources/icon.png", "icon.png")
	site, err := os.ReadDir("website")
	if err != nil {
		log.Fatal(err)
	}
	for _, e := range site {
		if filepath.Ext(e.Name()) != ".go" {
			copyFile(filepath.Join("website", e.Name()), e.Name())
		}
	}

	d := demo{blob: map[string]int{}}
	fi, err := os.Stat(filepath.Join(out, "lime.wasm"))
	if err != nil {
		log.Fatal(err)
	}
	d.WasmSize = int(fi.Size())
	args := append([]string{"git", "log", "-n" + strconv.Itoa(demoCommits), "--format=%H%x1f%an%x1f%ae%x1f%at%x1f%B%x1e", "--"}, demoFiles...)
	for _, rec := range strings.Split(run(nil, args...), "\x1e") {
		f := strings.Split(strings.TrimLeft(rec, "\n"), "\x1f")
		if len(f) != 5 {
			continue
		}
		at, _ := strconv.ParseInt(f[3], 10, 64)
		d.Commits = append(d.Commits, demoCommit{Hash: f[0], Name: f[1], Email: f[2], Time: at, Body: strings.TrimSpace(f[4]) + "\n"})
	}
	if len(d.Commits) == 0 {
		log.Fatal("no commits of the demo's files")
	}
	for i := range d.Commits {
		d.Commits[i].Tree = d.tree(d.Commits[i].Hash)
	}
	d.Parent = d.tree(d.Commits[len(d.Commits)-1].Hash + "^")
	b, err := json.Marshal(d)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "demo.json"), b, 0o644); err != nil {
		log.Fatal(err)
	}
}

// tree reads demoFiles as rev has them, with one git, keeping each text
// once. A file rev lacks is left out.
func (d *demo) tree(rev string) map[string]int {
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
		i, ok := d.blob[f[0]]
		if !ok {
			i = len(d.Blobs)
			d.blob[f[0]] = i
			d.Blobs = append(d.Blobs, string(text[:size]))
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
