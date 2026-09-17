package app

import (
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
)

type pinyinBoundary struct {
	val int
	b   byte
}

var gbkPinyinBoundaries = []pinyinBoundary{
	{0xB0A1, 'a'}, {0xB0C5, 'b'}, {0xB2C1, 'c'}, {0xB4EE, 'd'},
	{0xB6EA, 'e'}, {0xB7A2, 'f'}, {0xB8C1, 'g'}, {0xB9FE, 'h'},
	{0xBBF7, 'j'}, {0xBFA6, 'k'}, {0xC0AC, 'l'}, {0xC2E8, 'm'},
	{0xC4FF, 'n'}, {0xC5B6, 'o'}, {0xC5BE, 'p'}, {0xC6DA, 'q'},
	{0xC8BB, 'r'}, {0xC8F6, 's'}, {0xCBFA, 't'}, {0xCDDA, 'w'},
	{0xCEF4, 'x'}, {0xD1B9, 'y'}, {0xD4D1, 'z'}, {0xD7FA, 0},
}

// pinyinInitial returns the first lowercase pinyin letter for a rune.
func pinyinInitial(r rune) byte {
	if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
		return byte(r)
	}
	if r >= 'A' && r <= 'Z' {
		return byte(r + 32)
	}
	if r < 0x4e00 || r > 0x9fa5 {
		return 0
	}
	encoder := simplifiedchinese.GBK.NewEncoder()
	gbk, err := encoder.Bytes([]byte(string(r)))
	if err != nil || len(gbk) != 2 {
		return 0
	}
	val := (int(gbk[0]) << 8) | int(gbk[1])
	if val < 0xB0A1 || val >= 0xD7FA {
		return 0
	}
	for i := 0; i < len(gbkPinyinBoundaries)-1; i++ {
		if val >= gbkPinyinBoundaries[i].val && val < gbkPinyinBoundaries[i+1].val {
			return gbkPinyinBoundaries[i].b
		}
	}
	return 0
}

// dramaPinyinInitials extracts initials for a title (e.g. "霸道总裁" -> "bdzc").
func dramaPinyinInitials(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if b := pinyinInitial(r); b != 0 {
			sb.WriteByte(b)
		}
	}
	return sb.String()
}

// enrichDramaPinyin fills in the Initials for a Drama based on its Title/Name/Category.
func enrichDramaPinyin(drama *Drama) {
	if drama == nil {
		return
	}
	if drama.Initials == "" {
		drama.Initials = dramaPinyinInitials(drama.DisplayTitle())
	}
}
