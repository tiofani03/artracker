package heatmap

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
	"unicode/utf8"
)

func gz(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestDecodeChunk(t *testing.T) {
	const body = `{"format":"artracker-screen-v1","platform":"android","seq":3,
	 "frames":[
	  {"ts":1700000001000,"screen":"Home","w":200,"h":400,"vw":400,"vh":800,"mime":"image/jpeg","masked":2,"data":"AAAA"},
	  {"ts":1700000001100,"screen":"Bad","w":0,"h":400,"vw":400,"vh":800,"mime":"image/jpeg","data":"AAAA"},
	  {"ts":1700000001200,"screen":"NoData","w":200,"h":400,"vw":400,"vh":800,"mime":"image/jpeg","data":""},
	  {"ts":1700000001300,"w":200,"h":400,"vw":400,"vh":800,"mime":"image/jpeg","data":"AAAA"}],
	 "touches":[{"ts":1700000001010,"x":5,"y":6,"kind":"down"},{"ts":1700000001020,"x":5,"y":6,"kind":"tap"},{"ts":1700000001030,"y":6,"kind":"up"}]}`
	for name, raw := range map[string][]byte{"gzip": gz(t, body), "plain": []byte(body)} {
		t.Run(name, func(t *testing.T) {
			c, ok, err := DecodeChunk(7, raw)
			if err != nil || !ok {
				t.Fatalf("ok=%v err=%v", ok, err)
			}
			if c.Seq != 7 || c.Platform != "android" {
				t.Fatalf("seq/platform = %d/%q", c.Seq, c.Platform)
			}
			want := []Frame{
				{TS: 1700000001000, Screen: "Home", VW: 400, VH: 800, Masked: 2, ChunkSeq: 7, Index: 0},
				{TS: 1700000001300, Screen: "", VW: 400, VH: 800, ChunkSeq: 7, Index: 3},
			}
			if len(c.Frames) != len(want) || c.Frames[0] != want[0] || c.Frames[1] != want[1] {
				t.Fatalf("frames = %+v", c.Frames)
			}
			if len(c.Touches) != 1 || c.Touches[0] != (Touch{TS: 1700000001010, X: 5, Y: 6, Kind: "down"}) {
				t.Fatalf("touches = %+v", c.Touches)
			}
		})
	}

	if _, ok, err := DecodeChunk(0, gz(t, `{"format":"rrweb","events":[]}`)); ok || err != nil {
		t.Fatalf("rrweb: ok=%v err=%v", ok, err)
	}
	if _, _, err := DecodeChunk(0, []byte("not json at all")); err == nil {
		t.Fatal("garbage: want error")
	}
	if _, _, err := DecodeChunk(0, []byte{0x1f, 0x8b, 0x00}); err == nil {
		t.Fatal("bad gzip: want error")
	}
}

func TestDecodeChunkNonObjectIsSkipped(t *testing.T) {
	for _, body := range []string{`[{"type":2,"timestamp":1}]`, `"text"`, `null`, `42`} {
		if _, ok, err := DecodeChunk(0, gz(t, body)); ok || err != nil {
			t.Fatalf("%s: ok=%v err=%v", body, ok, err)
		}
	}
}

func TestDecodeChunkDropsWrongTypedItems(t *testing.T) {
	const body = `{"format":"artracker-screen-v1","platform":"ios",
	 "frames":[
	  {"ts":"x","screen":"Bad","w":1,"h":1,"vw":1,"vh":1,"mime":"image/jpeg","data":"A"},
	  {"ts":1700000000001,"screen":5,"w":1,"h":1,"vw":1,"vh":1,"mime":"image/jpeg","data":"A"},
	  null,
	  {"ts":1700000000002,"screen":"Good","w":1,"h":1,"vw":4,"vh":8,"mime":"image/jpeg","data":"A"}],
	 "touches":[{"ts":1700000000001,"x":"5","y":6,"kind":"down"},7,{"ts":1700000000002,"x":5,"y":6,"kind":"up"}]}`
	c, ok, err := DecodeChunk(0, []byte(body))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	// the non-string screen (index 1) is kept as "" (unrecognized), only the bad ts (index 0) is dropped
	if len(c.Frames) != 2 || c.Frames[0].Screen != "" || c.Frames[0].Index != 1 || c.Frames[1].Screen != "Good" || c.Frames[1].Index != 3 {
		t.Fatalf("frames = %+v", c.Frames)
	}
	if len(c.Touches) != 1 || c.Touches[0].Kind != "up" {
		t.Fatalf("touches = %+v", c.Touches)
	}
}

func TestDecodeChunkSizeLimit(t *testing.T) {
	old := maxDecompressed
	maxDecompressed = 16
	defer func() { maxDecompressed = old }()
	if _, _, err := DecodeChunk(0, gz(t, `{"format":"artracker-screen-v1","frames":[]}`)); err == nil {
		t.Fatal("oversized chunk: want error")
	}
}

func TestDecodeChunkSanitizes(t *testing.T) {
	body := `{"format":"artracker-screen-v1","platform":"an\u0000droid","frames":[
	 {"ts":1700000000000,"screen":"a\u0000b","w":1,"h":1,"vw":4,"vh":8,"mime":"image/jpeg","data":"AA"},
	 {"ts":1e300,"screen":"Far","w":1,"h":1,"vw":4,"vh":8,"mime":"image/jpeg","data":"AA"},
	 {"ts":5,"screen":"Early","w":1,"h":1,"vw":4,"vh":8,"mime":"image/jpeg","data":"AA"}],
	 "touches":[{"ts":1e300,"x":1,"y":1,"kind":"down"},{"ts":1700000000000,"x":1,"y":1,"kind":"up"}]}`
	c, ok, err := DecodeChunk(0, []byte(body))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if c.Platform != "android" || len(c.Frames) != 1 || c.Frames[0].Screen != "ab" {
		t.Fatalf("platform=%q frames=%+v", c.Platform, c.Frames)
	}
	if len(c.Touches) != 1 {
		t.Fatalf("touches = %+v", c.Touches)
	}
}

func TestDecodeChunkBooleanMaskedKeepsFrame(t *testing.T) {
	frame := func(masked string) string {
		return `{"ts":1700000001000,"screen":"Home","w":200,"h":400,"vw":400,"vh":800,"mime":"image/jpeg","masked":` + masked + `,"data":"AAAA"}`
	}
	raw := `{"format":"artracker-screen-v1","platform":"ios","frames":[` +
		frame("false") + "," + frame("true") + "," + frame("3") + `],"touches":[]}`
	c, ok, err := DecodeChunk(1, []byte(raw))
	if err != nil || !ok || len(c.Frames) != 3 {
		t.Fatalf("got %+v ok=%v err=%v", c, ok, err)
	}
	for i, want := range []int{0, 0, 3} {
		if c.Frames[i].Masked != want {
			t.Fatalf("frame %d Masked = %d, want %d", i, c.Frames[i].Masked, want)
		}
	}
}

func TestDecodeChunkFrameEdgeCases(t *testing.T) {
	fr := func(screen string, vw, vh string) string {
		return `{"ts":1700000001000,"screen":` + screen + `,"w":200,"h":400,"vw":` + vw + `,"vh":` + vh + `,"mime":"image/jpeg","data":"AAAA"}`
	}
	long := strings.Repeat("é", 200) // 400 bytes, 2 bytes per rune
	body := `{"format":"artracker-screen-v1","frames":[` +
		fr(`"TinyVw"`, "0.5", "800") + `,` + // dropped: vw < 1
		fr(`"TinyVh"`, "400", "0.001") + `,` + // dropped: vh < 1
		fr(`42`, "400", "800") + `,` + // non-string screen kept as ""
		fr(`"`+long+`"`, "400", "800") + `]}`
	c, ok, err := DecodeChunk(0, []byte(body))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(c.Frames) != 2 {
		t.Fatalf("frames = %+v", c.Frames)
	}
	if c.Frames[0].Screen != "" {
		t.Errorf("non-string screen = %q, want empty", c.Frames[0].Screen)
	}
	if s := c.Frames[1].Screen; len(s) != 256 || !utf8.ValidString(s) {
		t.Errorf("long screen len=%d valid=%v, want 256 valid", len(s), utf8.ValidString(s))
	}
}
