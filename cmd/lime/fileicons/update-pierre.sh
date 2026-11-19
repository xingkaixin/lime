#!/bin/sh
# Rebuilds the Pierre file icons from https://github.com/pierrecomputer/vscode-icons.
# Needs git and node.
set -eu
cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
git clone -q --depth 1 https://github.com/pierrecomputer/vscode-icons.git "$work/pierre"
node --input-type=module - "$work/pierre/scripts/themes" <<'JS' > "$work/pierre.json"
import path from "node:path";
import { pathToFileURL } from "node:url";
const dir = process.argv[2];
const load = async (f) => (await import(pathToFileURL(path.join(dir, f)).href)).default;
const all = [...(await load("minimal.mjs")), ...(await load("default.mjs")), ...(await load("complete.mjs"))];
process.stdout.write(JSON.stringify(all));
JS
cp "$work/pierre/LICENSE.md" fileicons/LICENSE-pierre-icons.md
go run fileicons/gen_pierre.go "$work/pierre.json" "$work/pierre/svgs" fileicons/pierre
