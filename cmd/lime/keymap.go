package main

import (
	"maps"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// keyChange is the keys of a command as the user changed them, written
// as Sublime Text writes keys: "super+shift+d", or "super+k super+u" for
// a chord.
type keyChange struct {
	Command string   `json:"command"`
	Keys    []string `json:"keys"`
}

// defaultKeys are the keys of each command as Sublime Text has them: the
// window's, then the editor's. fixedKeys are the editor's keys of no
// command, as the arrows, which a command cannot take, nor can the keys
// of the system's menus. Both are set by init, after the commands.
var (
	defaultKeys map[string][]appKey
	fixedKeys   []editor.Binding
	reserved    map[appKey]bool
)

func initKeys() {
	defaultKeys = make(map[string][]appKey)
	for _, k := range appKeys {
		defaultKeys[k.id] = append(defaultKeys[k.id], k)
	}
	byEditor := make(map[editor.Command]string)
	for _, c := range commands {
		if c.ed != editor.NoCommand {
			byEditor[c.ed] = c.id
		}
	}
	reserved = make(map[appKey]bool)
	for _, b := range editorKeys {
		id, ok := byEditor[b.Command]
		switch {
		case ok:
			defaultKeys[id] = append(defaultKeys[id], appKey{b.Mods, b.Key, false, id})
		case b.Command != editor.NoCommand:
			fixedKeys = append(fixedKeys, b)
			reserved[appKey{mods: b.Mods, key: b.Key}] = true
		}
	}
	for _, k := range systemKeys() {
		reserved[k] = true
	}
}

// systemKeys are the keys the system's menus and the window manager take
// before the app does.
func systemKeys() []appKey {
	cmd, alt, ctrl := ui.Super, ui.Alt, ui.Ctrl
	if !mac {
		return []appKey{{mods: alt, key: ui.KeyF4}}
	}
	return []appKey{
		{mods: cmd, key: ui.KeyQ}, {mods: cmd, key: ui.KeyH}, {mods: cmd | alt, key: ui.KeyH},
		{mods: cmd, key: ui.KeyM}, {mods: cmd, key: ui.KeyBackquote}, {mods: ctrl | cmd, key: ui.KeyF},
	}
}

// keymap holds the keys of the commands: Sublime Text's, as the user
// changed them. Of the commands sharing a key, the one changed last runs.
type keymap struct {
	keys    map[string][]appKey // each command's, the first shown in menus
	changed map[string]bool
	users   map[appKey][]string // the commands of each key, the one it runs first
	window  []appKey            // the keys the window runs
	editor  []editor.Binding
}

func newKeymap(changes []keyChange) *keymap {
	km := &keymap{keys: maps.Clone(defaultKeys), changed: make(map[string]bool), users: make(map[appKey][]string)}
	var order []string
	for _, ch := range slices.Backward(changes) {
		if commandByID[ch.Command] == nil || km.changed[ch.Command] {
			continue
		}
		keys := []appKey{}
		for _, s := range ch.Keys {
			if k, ok := parseKey(s); ok && !slices.Contains(keys, k) {
				k.id = ch.Command
				keys = append(keys, k)
			}
		}
		km.keys[ch.Command], km.changed[ch.Command] = keys, true
		order = append(order, ch.Command)
	}
	for _, c := range commands {
		if !km.changed[c.id] {
			order = append(order, c.id)
		}
	}
	for _, id := range order {
		ed := commandByID[id].ed
		for _, k := range km.keys[id] {
			s := k.seq()
			km.users[s] = append(km.users[s], id)
			switch {
			case len(km.users[s]) > 1:
			case ed != editor.NoCommand && !k.chord:
				km.editor = append(km.editor, editor.Binding{Mods: k.mods, Key: k.key, Command: ed})
			default:
				km.window = append(km.window, k)
			}
		}
	}
	km.editor = sublimeOnly(append(km.editor, fixedKeys...))
	return km
}

// runs reports whether k runs its command, rather than another command
// with the same key.
func (km *keymap) runs(k appKey) bool {
	u := km.users[k.seq()]
	return len(u) > 0 && u[0] == k.id
}

// first returns the first key that runs the command named id.
func (km *keymap) first(id string) (appKey, bool) {
	for _, k := range km.keys[id] {
		if km.runs(k) {
			return k, true
		}
	}
	return appKey{}, false
}

func (km *keymap) text(id string) string {
	if k, ok := km.first(id); ok {
		return k.String()
	}
	return ""
}

// others returns the commands other than id with the key k.
func (km *keymap) others(k appKey, id string) []string {
	return slices.DeleteFunc(slices.Clone(km.users[k.seq()]), func(o string) bool { return o == id })
}

func (km *keymap) conflicted(k appKey) bool { return len(km.users[k.seq()]) > 1 }

// without returns the keys of the command named id but k.
func (km *keymap) without(id string, k appKey) []appKey {
	keys := []appKey{}
	for _, o := range km.keys[id] {
		if o.seq() != k.seq() {
			keys = append(keys, o)
		}
	}
	return keys
}

// setKeys gives the command named id keys, and keeps them: nil gives it
// Sublime Text's. The command changed goes last in settings.json, so
// that it runs a key it shares.
func (l *lime) setKeys(id string, keys []appKey) {
	l.set.Keys = slices.DeleteFunc(l.set.Keys, func(ch keyChange) bool { return ch.Command == id })
	if keys != nil && !slices.Equal(keys, defaultKeys[id]) {
		specs := make([]string, len(keys))
		for i, k := range keys {
			specs[i] = k.spec()
		}
		l.set.Keys = append(l.set.Keys, keyChange{Command: id, Keys: specs})
	}
	l.keysChanged()
}

// resetKeys gives every command Sublime Text's keys.
func (l *lime) resetKeys() {
	l.set.Keys = nil
	l.keysChanged()
}

func (l *lime) keysChanged() {
	l.keys = newKeymap(l.set.Keys)
	l.saveSettings()
	l.refreshMenu()
}

// pauseMenu takes the keys away from the menu while a key is recorded,
// as the menu would take them first.
func (l *lime) pauseMenu(paused bool) {
	if l.menuPaused != paused {
		l.menuPaused = paused
		l.refreshMenu()
	}
}

// refreshMenu makes the menu anew, with its keys as they are.
func (l *lime) refreshMenu() {
	if l.menu == nil {
		return
	}
	l.menu, l.menuState = l.newMenu(), ""
	mygo.App.SetMenu(l.menu)
	if l.last != nil {
		l.last.syncMenu()
	}
}

// menuKey is the accelerator of the menu item running the command named
// id: its first key that is not a chord, when it runs it.
func (l *lime) menuKey(id string) string {
	if l.menuPaused {
		return ""
	}
	for _, k := range l.keys.keys[id] {
		if !k.chord && l.keys.runs(k) {
			return accelerator(k.mods, k.key)
		}
	}
	return ""
}

// seq is k without its command, the key alone.
func (k appKey) seq() appKey {
	k.id = ""
	return k
}

// spec writes k as settings.json keeps it.
func (k appKey) spec() string {
	s := strings.ToLower(accelerator(k.mods, k.key))
	if k.chord {
		s = strings.ToLower(accelerator(ui.Cmd, ui.KeyK)) + " " + s
	}
	return s
}

var keysByName = func() map[string]ui.Key {
	m := make(map[string]ui.Key)
	add := func(k ui.Key) { m[strings.ToLower(keyName(k, false))] = k }
	for k := ui.KeyA; k <= ui.KeyZ; k++ {
		add(k)
	}
	for k := ui.Key0; k <= ui.Key9; k++ {
		add(k)
	}
	for k := ui.KeyF1; k <= ui.KeyF12; k++ {
		add(k)
	}
	for k := range keyNames {
		add(k)
	}
	return m
}()

// parseKey reads a key as spec writes it.
func parseKey(s string) (appKey, bool) {
	strokes := strings.Fields(s)
	var k appKey
	switch len(strokes) {
	case 1:
	case 2:
		mods, key, ok := parseStroke(strokes[0])
		if !ok || mods != ui.Cmd || key != ui.KeyK {
			return appKey{}, false
		}
		k.chord, strokes = true, strokes[1:]
	default:
		return appKey{}, false
	}
	mods, key, ok := parseStroke(strokes[0])
	k.mods, k.key = mods, key
	return k, ok
}

func parseStroke(s string) (ui.Modifiers, ui.Key, bool) {
	// No key is named "+": Shift+= is written for it.
	names := strings.Split(s, "+")
	var mods ui.Modifiers
	for _, n := range names[:len(names)-1] {
		switch n {
		case "super":
			mods |= ui.Super
		case "ctrl":
			mods |= ui.Ctrl
		case "alt":
			mods |= ui.Alt
		case "shift":
			mods |= ui.Shift
		default:
			return 0, 0, false
		}
	}
	key, ok := keysByName[names[len(names)-1]]
	return mods, key, ok
}
