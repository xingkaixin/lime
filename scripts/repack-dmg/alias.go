package main

import (
	"encoding/binary"
	"time"
	"unicode/utf16"
)

const (
	// HFS/Mac epoch is 1904-01-01 00:00:00 UTC (2082844800 seconds before Unix epoch).
	hfsEpochOffset = 2082844800

	aliasHeaderSize = 150
	aliasVersion2   = 2
	aliasKindFile   = 0
	aliasDiskOther  = 5

	tagCarbonFolderName  = 0
	tagCNIDPath          = 1
	tagCarbonPath        = 2
	tagUnicodeFilename   = 14
	tagUnicodeVolumeName = 15
	tagPOSIXPath         = 18
	tagVolumeMountPoint  = 19
	tagEnd               = 0xffff
)

type aliasTarget struct {
	VolumeName     string
	VolumeCreated  time.Time
	FolderName     string
	FolderCNID     uint32
	FileName       string
	FileCNID       uint32
	FileCreated    time.Time
	POSIXRelPath   string // e.g. "/.background/background.tiff"
	MountPointPath string // e.g. "/Volumes/Lime"
}

// encodeCarbonAlias encodes a classic macOS Carbon Alias v2 record, which Finder
// requires for icvp.backgroundImageAlias in .DS_Store files.
func encodeCarbonAlias(t aliasTarget) []byte {
	be := binary.BigEndian
	buf := make([]byte, aliasHeaderSize)

	be.PutUint16(buf[6:8], aliasVersion2)
	be.PutUint16(buf[8:10], aliasKindFile)
	putPascalString(buf[10:38], t.VolumeName, 27)
	be.PutUint32(buf[38:42], hfsTimestamp(t.VolumeCreated))
	copy(buf[42:44], "H+")
	be.PutUint16(buf[44:46], aliasDiskOther)
	be.PutUint32(buf[46:50], t.FolderCNID)
	putPascalString(buf[50:114], t.FileName, 63)
	be.PutUint32(buf[114:118], t.FileCNID)
	be.PutUint32(buf[118:122], hfsTimestamp(t.FileCreated))
	be.PutUint16(buf[130:132], 0xffff)
	be.PutUint16(buf[132:134], 0xffff)
	be.PutUint32(buf[134:138], 0x00000d02)

	appendTag := func(tag uint16, payload []byte) {
		buf = be.AppendUint16(buf, tag)
		buf = be.AppendUint16(buf, uint16(len(payload)))
		buf = append(buf, payload...)
		if len(payload)%2 == 1 {
			buf = append(buf, 0)
		}
	}

	if t.FolderName != "" {
		appendTag(tagCarbonFolderName, []byte(t.FolderName))
	}
	if t.FolderCNID != 0 {
		appendTag(tagCNIDPath, be.AppendUint32(nil, t.FolderCNID))
	}
	if t.VolumeName != "" && t.FolderName != "" && t.FileName != "" {
		appendTag(tagCarbonPath, []byte(t.VolumeName+":"+t.FolderName+":"+t.FileName))
	}
	appendTag(tagUnicodeFilename, encodeUTF16BEWithCount(t.FileName))
	appendTag(tagUnicodeVolumeName, encodeUTF16BEWithCount(t.VolumeName))
	if t.POSIXRelPath != "" {
		appendTag(tagPOSIXPath, []byte(t.POSIXRelPath))
	}
	if t.MountPointPath != "" {
		appendTag(tagVolumeMountPoint, []byte(t.MountPointPath))
	}
	buf = be.AppendUint16(buf, tagEnd)
	buf = be.AppendUint16(buf, 0)

	be.PutUint16(buf[4:6], uint16(len(buf)))
	return buf
}

func putPascalString(dst []byte, s string, maxLen int) {
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	dst[0] = byte(len(s))
	copy(dst[1:], s)
}

func encodeUTF16BEWithCount(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := binary.BigEndian.AppendUint16(nil, uint16(len(u)))
	for _, c := range u {
		out = binary.BigEndian.AppendUint16(out, c)
	}
	return out
}

func hfsTimestamp(t time.Time) uint32 {
	if t.IsZero() {
		return 0
	}
	sec := t.Unix() + hfsEpochOffset
	if sec < 0 {
		return 0
	}
	return uint32(sec)
}
