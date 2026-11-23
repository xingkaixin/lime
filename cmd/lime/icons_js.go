//go:build js

package main

import (
	"sync"
	"syscall/js"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var (
	iconFontMu     sync.Mutex
	iconFontLoaded = map[string]bool{}
)

// ensureIconFont fetches icon fonts lazily from the website instead of
// embedding ~685 KB of TrueType fonts inside lime.wasm.
func ensureIconFont(font string) {
	if font == "" {
		return
	}
	iconFontMu.Lock()
	if iconFontLoaded[font] {
		iconFontMu.Unlock()
		return
	}
	iconFontLoaded[font] = true
	iconFontMu.Unlock()

	fetch := js.Global().Get("fetch")
	if fetch.Type() != js.TypeFunction {
		return
	}
	url := "fonts/" + font + ".ttf"
	if fn := js.Global().Get("limeFontURL"); fn.Type() == js.TypeFunction {
		if u := fn.Invoke(font); u.Type() == js.TypeString && u.String() != "" {
			url = u.String()
		}
	}
	promise := fetch.Invoke(url)
	var onResp, onBuf js.Func
	onBuf = js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer onBuf.Release()
		if len(args) == 0 {
			return nil
		}
		arr := js.Global().Get("Uint8Array").New(args[0])
		data := make([]byte, arr.Get("byteLength").Int())
		js.CopyBytesToGo(data, arr)
		mygo.RunOnMain(func() {
			if err := ui.RegisterFont(data, fileIconFamily(font)); err == nil && onIconFontLoaded != nil {
				onIconFontLoaded()
			}
		})
		return nil
	})
	onResp = js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer onResp.Release()
		if len(args) == 0 || !args[0].Get("ok").Bool() {
			onBuf.Release()
			return nil
		}
		args[0].Call("arrayBuffer").Call("then", onBuf)
		return nil
	})
	promise.Call("then", onResp)
}
