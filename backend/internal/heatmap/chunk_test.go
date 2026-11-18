package heatmap

import (
	"bytes"
	"compress/gzip"
	"testing"
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
	  {"ts":1000,"screen":"Home","w":200,"h":400,"vw":400,"vh":800,"mime":"image/jpeg","masked":2,"data":"AAAA"},
	  {"ts":1100,"screen":"Bad","w":0,"h":400,"vw":400,"vh":800,"mime":"image/jpeg","data":"AAAA"},
	  {"ts":1200,"screen":"NoData","w":200,"h":400,"vw":400,"vh":800,"mime":"image/jpeg","data":""},
	  {"ts":1300,"w":200,"h":400,"vw":400,"vh":800,"mime":"image/jpeg","data":"AAAA"}],
	 "touches":[{"ts":1010,"x":5,"y":6,"kind":"down"},{"ts":1020,"x":5,"y":6,"kind":"tap"},{"ts":1030,"y":6,"kind":"up"}]}`
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
				{TS: 1000, Screen: "Home", VW: 400, VH: 800, Masked: 2, ChunkSeq: 7, Index: 0},
				{TS: 1300, Screen: "", VW: 400, VH: 800, ChunkSeq: 7, Index: 3},
			}
			if len(c.Frames) != len(want) || c.Frames[0] != want[0] || c.Frames[1] != want[1] {
				t.Fatalf("frames = %+v", c.Frames)
			}
			if len(c.Touches) != 1 || c.Touches[0] != (Touch{TS: 1010, X: 5, Y: 6, Kind: "down"}) {
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
	  {"ts":1,"screen":5,"w":1,"h":1,"vw":1,"vh":1,"mime":"image/jpeg","data":"A"},
	  null,
	  {"ts":2,"screen":"Good","w":1,"h":1,"vw":4,"vh":8,"mime":"image/jpeg","data":"A"}],
	 "touches":[{"ts":1,"x":"5","y":6,"kind":"down"},7,{"ts":2,"x":5,"y":6,"kind":"up"}]}`
	c, ok, err := DecodeChunk(0, []byte(body))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(c.Frames) != 1 || c.Frames[0].Screen != "Good" || c.Frames[0].Index != 3 {
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
