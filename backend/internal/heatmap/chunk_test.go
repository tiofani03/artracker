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
