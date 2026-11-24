package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"unicode/utf16"
)

type dsRecord struct {
	name  string
	code  string
	kind  string
	value []byte
}

func dsLong(name, code string, v uint32) dsRecord {
	return dsRecord{name, code, "long", binary.BigEndian.AppendUint32(nil, v)}
}

func dsBlob(name, code string, b []byte) dsRecord {
	return dsRecord{name, code, "blob", b}
}

func dsIconLocation(name string, x, y int) dsRecord {
	b := binary.BigEndian.AppendUint32(nil, uint32(x))
	b = binary.BigEndian.AppendUint32(b, uint32(y))
	b = append(b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0, 0)
	return dsBlob(name, "Iloc", b)
}

func (r dsRecord) encode(b []byte) []byte {
	name := utf16.Encode([]rune(r.name))
	b = binary.BigEndian.AppendUint32(b, uint32(len(name)))
	for _, u := range name {
		b = binary.BigEndian.AppendUint16(b, u)
	}
	b = append(b, r.code...)
	b = append(b, r.kind...)
	if r.kind == "blob" {
		b = binary.BigEndian.AppendUint32(b, uint32(len(r.value)))
	}
	return append(b, r.value...)
}

// dsStore encodes a single-leaf .DS_Store file (buddy allocator + DSDB B-tree).
func dsStore(records []dsRecord) ([]byte, error) {
	records = slices.Clone(records)
	slices.SortStableFunc(records, func(a, b dsRecord) int {
		if c := strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name)); c != 0 {
			return c
		}
		return strings.Compare(a.code, b.code)
	})
	node := binary.BigEndian.AppendUint32(nil, 0)
	node = binary.BigEndian.AppendUint32(node, uint32(len(records)))
	for _, r := range records {
		node = r.encode(node)
	}
	const pageSize = 0x1000
	if len(node) > pageSize {
		return nil, fmt.Errorf(".DS_Store records need %d bytes, more than one page", len(node))
	}

	const (
		dsdbAddr = 0x20
		rootAddr = 0x800
		nodeAddr = 0x2000
	)
	file := make([]byte, 4+0x4000)
	file[3] = 1
	mem := file[4:]
	be := binary.BigEndian

	copy(mem, "Bud1")
	be.PutUint32(mem[4:], rootAddr)
	be.PutUint32(mem[8:], 0x800)
	be.PutUint32(mem[12:], rootAddr)
	copy(mem[16:], []byte{0, 0, 0x10, 0x0c, 0, 0, 0, 0x87, 0, 0, 0x20, 0x0b, 0, 0, 0, 0})

	be.PutUint32(mem[dsdbAddr:], 2)
	be.PutUint32(mem[dsdbAddr+4:], 0)
	be.PutUint32(mem[dsdbAddr+8:], uint32(len(records)))
	be.PutUint32(mem[dsdbAddr+12:], 1)
	be.PutUint32(mem[dsdbAddr+16:], pageSize)

	root := mem[rootAddr:]
	be.PutUint32(root, 3)
	for i, addr := range []uint32{rootAddr | 11, dsdbAddr | 5, nodeAddr | 13} {
		be.PutUint32(root[8+4*i:], addr)
	}
	p := 8 + 256*4
	be.PutUint32(root[p:], 1)
	root[p+4] = 4
	copy(root[p+5:], "DSDB")
	be.PutUint32(root[p+9:], 1)
	p += 13
	for width := range 32 {
		if width < 6 || width == 11 || width == 13 || width == 31 {
			p += 4
			continue
		}
		be.PutUint32(root[p:], 1)
		be.PutUint32(root[p+4:], 1<<width)
		p += 8
	}

	copy(mem[nodeAddr:], node)
	return file, nil
}

// binaryPlist encodes a flat dictionary of bool, int, float64, string, and []byte values.
func binaryPlist(dict map[string]any) []byte {
	keys := make([]string, 0, len(dict))
	for k := range dict {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	type scalar struct {
		kind  string
		value any
	}
	objs := []any{dict}
	refs := map[scalar]int{}
	ref := func(v any) int {
		k := scalar{fmt.Sprintf("%T", v), v}
		if b, ok := v.([]byte); ok {
			k.value = string(b)
		}
		if i, ok := refs[k]; ok {
			return i
		}
		refs[k] = len(objs)
		objs = append(objs, v)
		return len(objs) - 1
	}
	keyRefs := make([]int, len(keys))
	for i, k := range keys {
		keyRefs[i] = ref(k)
	}
	valRefs := make([]int, len(keys))
	for i, k := range keys {
		valRefs[i] = ref(dict[k])
	}

	sizeOf := func(n int) int {
		switch v := uint64(n); {
		case v < 1<<8:
			return 1
		case v < 1<<16:
			return 2
		case v < 1<<32:
			return 4
		}
		return 8
	}
	putUint := func(b []byte, v uint64, size int) []byte {
		for i := size - 1; i >= 0; i-- {
			b = append(b, byte(v>>(8*i)))
		}
		return b
	}
	refSize := sizeOf(len(objs))
	appendInt := func(b []byte, v int) []byte {
		switch {
		case v < 0:
			return putUint(append(b, 0x13), uint64(v), 8)
		case v < 1<<8:
			return append(b, 0x10, byte(v))
		case v < 1<<16:
			return putUint(append(b, 0x11), uint64(v), 2)
		case int64(v) < 1<<32:
			return putUint(append(b, 0x12), uint64(v), 4)
		}
		return putUint(append(b, 0x13), uint64(v), 8)
	}
	appendSize := func(b []byte, marker byte, n int) []byte {
		if n < 15 {
			return append(b, marker|byte(n))
		}
		return appendInt(append(b, marker|0xf), n)
	}

	var buf bytes.Buffer
	buf.WriteString("bplist00")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = buf.Len()
		var b []byte
		switch v := o.(type) {
		case map[string]any:
			b = appendSize(b, 0xd0, len(keys))
			for _, r := range keyRefs {
				b = putUint(b, uint64(r), refSize)
			}
			for _, r := range valRefs {
				b = putUint(b, uint64(r), refSize)
			}
		case bool:
			b = []byte{0x08}
			if v {
				b[0] = 0x09
			}
		case int:
			b = appendInt(b, v)
		case float64:
			b = putUint([]byte{0x23}, math.Float64bits(v), 8)
		case []byte:
			b = append(appendSize(b, 0x40, len(v)), v...)
		case string:
			if isASCII(v) {
				b = append(appendSize(b, 0x50, len(v)), v...)
			} else {
				u := utf16.Encode([]rune(v))
				b = appendSize(b, 0x60, len(u))
				for _, c := range u {
					b = binary.BigEndian.AppendUint16(b, c)
				}
			}
		default:
			panic(fmt.Sprintf("binaryPlist: unsupported %T", o))
		}
		buf.Write(b)
	}
	tableOffset := buf.Len()
	offsetSize := sizeOf(tableOffset)
	var t []byte
	for _, off := range offsets {
		t = putUint(t, uint64(off), offsetSize)
	}
	t = append(t, 0, 0, 0, 0, 0, 0, byte(offsetSize), byte(refSize))
	t = putUint(t, uint64(len(objs)), 8)
	t = putUint(t, 0, 8)
	t = putUint(t, uint64(tableOffset), 8)
	buf.Write(t)
	return buf.Bytes()
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

type dmgLayout struct {
	AppName         string
	WindowX         int
	WindowY         int
	WindowWidth     int
	WindowHeight    int
	IconSize        int
	AppX, AppY      int
	AppsX, AppsY    int
	BackgroundAlias []byte
}

func dmgRecords(l dmgLayout) []dsRecord {
	bwsp := map[string]any{
		"ShowStatusBar":         false,
		"WindowBounds":          fmt.Sprintf("{{%d, %d}, {%d, %d}}", l.WindowX, l.WindowY, l.WindowWidth, l.WindowHeight),
		"ContainerShowSidebar":  false,
		"PreviewPaneVisibility": false,
		"SidebarWidth":          180,
		"ShowTabView":           false,
		"ShowToolbar":           false,
		"ShowPathbar":           false,
		"ShowSidebar":           false,
	}
	icvp := map[string]any{
		"viewOptionsVersion":   1,
		"backgroundType":       0,
		"backgroundColorRed":   1.0,
		"backgroundColorGreen": 1.0,
		"backgroundColorBlue":  1.0,
		"gridOffsetX":          0.0,
		"gridOffsetY":          0.0,
		"gridSpacing":          100.0,
		"arrangeBy":            "none",
		"showIconPreview":      false,
		"showItemInfo":         false,
		"labelOnBottom":        true,
		"textSize":             16.0,
		"iconSize":             float64(l.IconSize),
		"scrollPositionX":      0.0,
		"scrollPositionY":      0.0,
	}
	if len(l.BackgroundAlias) > 0 {
		icvp["backgroundType"] = 2
		icvp["backgroundImageAlias"] = l.BackgroundAlias
	}
	return []dsRecord{
		dsLong(".", "vSrn", 1),
		dsBlob(".", "bwsp", binaryPlist(bwsp)),
		dsBlob(".", "icvp", binaryPlist(icvp)),
		{".", "icvl", "type", []byte("icnv")},
		dsIconLocation(l.AppName, l.AppX, l.AppY),
		dsIconLocation("Applications", l.AppsX, l.AppsY),
	}
}

func writeDSStore(path string, l dmgLayout) error {
	data, err := dsStore(dmgRecords(l))
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
