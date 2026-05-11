package imageutil

// MaxImageSize is the maximum allowed raw image size (4 MB).
const MaxImageSize = 4 * 1024 * 1024

// KnownImageMagic maps magic byte prefixes to MIME types.
var KnownImageMagic = []struct {
	Magic []byte
	Mime  string
}{
	{[]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, "image/png"},
	{[]byte{0xff, 0xd8, 0xff}, "image/jpeg"},
	{[]byte{'G', 'I', 'F', '8', '7', 'a'}, "image/gif"},
	{[]byte{'G', 'I', 'F', '8', '9', 'a'}, "image/gif"},
	{[]byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P'}, "image/webp"},
	{[]byte{'B', 'M'}, "image/bmp"},
	{[]byte{0x49, 0x49, 0x2a, 0x00}, "image/tiff"}, // II*
	{[]byte{0x4d, 0x4d, 0x00, 0x2a}, "image/tiff"}, // MM*
}

// DetectMIME returns the MIME type based on magic bytes,
// or an empty string if the format is not recognized.
func DetectMIME(data []byte) string {
	for _, m := range KnownImageMagic {
		if len(data) >= len(m.Magic) {
			match := true
			for i, b := range m.Magic {
				if b != 0 && data[i] != b {
					match = false
					break
				}
			}
			if match {
				return m.Mime
			}
		}
	}
	return ""
}
