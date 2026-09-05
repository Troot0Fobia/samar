package cinema

// ITU-T G.711 decode. Dahua DHAV audio ships as A-law (0x83 block byte[2]==0x0e)
// or µ-law (0x0a) at 8 kHz mono; the cinema audio path expands it to signed
// 16-bit little-endian PCM in Go instead of spending an ffmpeg process on it.
//
// The per-code math is the Sun/BSD g711.c reference (`alaw2linear` /
// `ulaw2linear`), precomputed into 256-entry tables at init.

var (
	alawDecodeTable  [256]int16
	mulawDecodeTable [256]int16
)

func init() {
	for i := range 256 {
		alawDecodeTable[i] = alawToLinear(byte(i))
		mulawDecodeTable[i] = mulawToLinear(byte(i))
	}
}

func alawToLinear(a byte) int16 {
	a ^= 0x55
	t := int(a&0x0f) << 4
	seg := int(a&0x70) >> 4
	switch seg {
	case 0:
		t += 8
	case 1:
		t += 0x108
	default:
		t += 0x108
		t <<= seg - 1
	}
	if a&0x80 != 0 {
		return int16(t)
	}
	return int16(-t)
}

func mulawToLinear(u byte) int16 {
	u = ^u
	t := (int(u&0x0f) << 3) + 0x84
	t <<= int(u&0x70) >> 4
	if u&0x80 != 0 {
		return int16(0x84 - t)
	}
	return int16(t - 0x84)
}

// DecodeG711 expands an A-law ("alaw") or µ-law ("mulaw") payload to signed
// 16-bit little-endian mono PCM (output is exactly 2× the input length).
// Returns nil for any other format.
func DecodeG711(format string, payload []byte) []byte {
	var tbl *[256]int16
	switch format {
	case "alaw":
		tbl = &alawDecodeTable
	case "mulaw":
		tbl = &mulawDecodeTable
	default:
		return nil
	}
	out := make([]byte, len(payload)*2)
	for i, b := range payload {
		s := uint16(tbl[b])
		out[2*i] = byte(s)
		out[2*i+1] = byte(s >> 8)
	}
	return out
}
