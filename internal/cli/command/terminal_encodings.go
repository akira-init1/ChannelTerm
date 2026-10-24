package command

import (
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// terminalEncodings describe client text, independent of Serial/SSH configuration.
// UTF-8 preserves the established raw console path; ASCII is checked explicitly.
var terminalEncodings = []struct {
	name    string
	codec   encoding.Encoding
	aliases []string
}{
	{"utf-8", nil, []string{"utf8"}}, {"ascii", nil, []string{"us-ascii"}},
	{"gbk", simplifiedchinese.GBK, []string{"cp936", "windows-936"}},
	{"gb18030", simplifiedchinese.GB18030, nil},
	{"big5", traditionalchinese.Big5, []string{"cp950"}},
	{"shift_jis", japanese.ShiftJIS, []string{"shift-jis", "sjis", "cp932"}},
	{"euc-jp", japanese.EUCJP, []string{"eucjp"}},
	{"euc-kr", korean.EUCKR, []string{"euckr", "cp949"}},
	{"windows-874", charmap.Windows874, []string{"cp874"}},
	{"windows-1250", charmap.Windows1250, []string{"cp1250"}},
	{"windows-1251", charmap.Windows1251, []string{"cp1251"}},
	{"windows-1252", charmap.Windows1252, []string{"cp1252"}},
	{"windows-1253", charmap.Windows1253, []string{"cp1253"}},
	{"windows-1254", charmap.Windows1254, []string{"cp1254"}},
	{"windows-1255", charmap.Windows1255, []string{"cp1255"}},
	{"windows-1256", charmap.Windows1256, []string{"cp1256"}},
	{"windows-1257", charmap.Windows1257, []string{"cp1257"}},
	{"windows-1258", charmap.Windows1258, []string{"cp1258"}},
	{"iso-8859-1", charmap.ISO8859_1, []string{"latin1"}},
	{"iso-8859-2", charmap.ISO8859_2, []string{"latin2"}},
	{"iso-8859-3", charmap.ISO8859_3, nil}, {"iso-8859-4", charmap.ISO8859_4, nil},
	{"iso-8859-5", charmap.ISO8859_5, nil}, {"iso-8859-6", charmap.ISO8859_6, nil},
	{"iso-8859-7", charmap.ISO8859_7, nil}, {"iso-8859-8", charmap.ISO8859_8, nil},
	{"iso-8859-9", charmap.ISO8859_9, nil}, {"iso-8859-10", charmap.ISO8859_10, nil},
	{"iso-8859-13", charmap.ISO8859_13, nil}, {"iso-8859-14", charmap.ISO8859_14, nil},
	{"iso-8859-15", charmap.ISO8859_15, nil}, {"iso-8859-16", charmap.ISO8859_16, nil},
	{"koi8-r", charmap.KOI8R, []string{"koi8r"}}, {"koi8-u", charmap.KOI8U, []string{"koi8u"}},
	{"cp437", charmap.CodePage437, []string{"ibm437"}},
	{"cp850", charmap.CodePage850, []string{"ibm850"}},
	{"cp866", charmap.CodePage866, []string{"ibm866"}},
}

func terminalEncodingNames() string {
	names := make([]string, 0, len(terminalEncodings))
	for _, item := range terminalEncodings {
		names = append(names, item.name)
	}
	return strings.Join(names, ", ")
}

func resolveTerminalEncoding(name string) (string, encoding.Encoding, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, item := range terminalEncodings {
		if name == item.name {
			return item.name, item.codec, true
		}
		for _, alias := range item.aliases {
			if name == alias {
				return item.name, item.codec, true
			}
		}
	}
	return "", nil, false
}
