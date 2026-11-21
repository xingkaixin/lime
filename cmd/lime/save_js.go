package main

import (
	"slices"
	"sync"
	"syscall/js"

	"github.com/egoist/mygo"
)

func init() {
	askSaveConflict = showWebModalDialog
}

func showWebModalDialog(a *app, o mygo.MessageOptions) (mygo.MessageResult, error) {
	if a != nil && a.messageDialog != nil {
		return a.messageDialog(o)
	}
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return mygo.MessageResult{Button: o.CancelButton}, nil
	}
	body := doc.Get("body")
	if !body.Truthy() {
		return mygo.MessageResult{Button: o.CancelButton}, nil
	}

	dialog := doc.Call("createElement", "dialog")
	if dialog.Get("showModal").Type() != js.TypeFunction {
		return mygo.MessageResult{Button: o.CancelButton}, nil
	}

	dark := webDarkTheme(a, doc)
	styleWebDialog(dialog, dark)

	if o.Message != "" {
		msg := doc.Call("createElement", "p")
		msg.Set("textContent", o.Message)
		msg.Get("style").Set("cssText", "margin:0;font-size:14px;font-weight:600;word-break:break-word")
		dialog.Call("appendChild", msg)
	}

	if o.Detail != "" {
		detail := doc.Call("createElement", "p")
		detail.Set("textContent", o.Detail)
		detailColor := "#59636e"
		if dark {
			detailColor = "#9ca3af"
		}
		detail.Get("style").Set("cssText", "margin:8px 0 0;font-size:13px;white-space:pre-wrap;word-break:break-word;color:"+detailColor)
		dialog.Call("appendChild", detail)
	}

	buttons := o.Buttons
	if len(buttons) == 0 {
		buttons = []string{"OK"}
	}

	done := make(chan int, 1)
	var once sync.Once
	choose := func(idx int) {
		once.Do(func() { done <- idx })
	}

	type listener struct {
		target js.Value
		event  string
		fn     js.Func
	}
	listeners := make([]listener, 0, len(buttons)+2)
	addListener := func(target js.Value, event string, fn js.Func) {
		listeners = append(listeners, listener{target: target, event: event, fn: fn})
		target.Call("addEventListener", event, fn)
	}

	defer func() {
		// Detach listeners before closing so the async close event cannot invoke released callbacks.
		for _, l := range listeners {
			l.target.Call("removeEventListener", l.event, l.fn)
		}
		if dialog.Get("open").Bool() {
			dialog.Call("close")
		}
		dialog.Call("remove")
		for _, l := range listeners {
			l.fn.Release()
		}
	}()

	actions := doc.Call("createElement", "div")
	actions.Get("style").Set("cssText", "display:flex;justify-content:flex-end;flex-wrap:wrap;gap:8px;margin-top:18px")

	var defaultBtn js.Value
	for i, label := range buttons {
		btn := doc.Call("createElement", "button")
		btn.Set("type", "button")
		btn.Set("textContent", label)

		primary := i == o.DefaultButton
		destructive := slices.Contains(o.DestructiveButtons, i)
		styleWebDialogButton(btn, dark, primary, destructive)
		if primary {
			btn.Set("autofocus", true)
			defaultBtn = btn
		}

		idx := i
		addListener(btn, "click", js.FuncOf(func(js.Value, []js.Value) any {
			choose(idx)
			return nil
		}))
		actions.Call("appendChild", btn)
	}
	dialog.Call("appendChild", actions)

	addListener(dialog, "cancel", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			args[0].Call("preventDefault")
		}
		choose(o.CancelButton)
		return nil
	}))
	addListener(dialog, "close", js.FuncOf(func(js.Value, []js.Value) any {
		choose(o.CancelButton)
		return nil
	}))

	body.Call("appendChild", dialog)
	dialog.Call("showModal")
	if defaultBtn.Truthy() {
		defaultBtn.Call("focus")
	}

	return mygo.MessageResult{Button: <-done}, nil
}

func webDarkTheme(a *app, doc js.Value) bool {
	if a != nil && a.win != nil {
		return mygo.Theme.IsDark()
	}
	if root := doc.Get("documentElement"); root.Truthy() {
		if attr := root.Call("getAttribute", "data-mygo-theme"); attr.Type() == js.TypeString {
			switch attr.String() {
			case "dark":
				return true
			case "light":
				return false
			}
		}
	}
	if mm := js.Global().Get("matchMedia"); mm.Type() == js.TypeFunction {
		return js.Global().Call("matchMedia", "(prefers-color-scheme: dark)").Get("matches").Bool()
	}
	return false
}

func styleWebDialog(dialog js.Value, dark bool) {
	theme := "color-scheme:light;background:#ffffff;color:#1f2328;border:1px solid rgba(0,0,0,.14);box-shadow:0 12px 32px rgba(0,0,0,.2)"
	if dark {
		theme = "color-scheme:dark;background:#24292f;color:#f0f3f6;border:1px solid rgba(255,255,255,.16);box-shadow:0 12px 32px rgba(0,0,0,.48)"
	}
	dialog.Get("style").Set("cssText",
		"box-sizing:border-box;width:min(420px,calc(100vw - 32px));max-width:calc(100vw - 32px);padding:20px;border-radius:10px;"+
			`font:13px/1.45 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;`+theme)
}

func styleWebDialogButton(btn js.Value, dark, primary, destructive bool) {
	colors := "background:#f6f8fa;color:#1f2328;border:1px solid rgba(0,0,0,.15)"
	switch {
	case primary && destructive:
		colors = "background:#cf222e;color:#ffffff;border:1px solid transparent"
		if dark {
			colors = "background:#da3633;color:#ffffff;border:1px solid transparent"
		}
	case primary:
		colors = "background:#0969da;color:#ffffff;border:1px solid transparent"
		if dark {
			colors = "background:#1f6feb;color:#ffffff;border:1px solid transparent"
		}
	case destructive:
		colors = "background:#f6f8fa;color:#cf222e;border:1px solid rgba(0,0,0,.15)"
		if dark {
			colors = "background:rgba(255,255,255,.08);color:#ff7b72;border:1px solid rgba(255,255,255,.16)"
		}
	case dark:
		colors = "background:rgba(255,255,255,.08);color:#f0f3f6;border:1px solid rgba(255,255,255,.16)"
	}
	btn.Get("style").Set("cssText", "padding:6px 14px;border-radius:6px;font:inherit;font-weight:500;cursor:pointer;"+colors)
}
