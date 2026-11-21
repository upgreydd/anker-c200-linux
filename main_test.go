package main

import "testing"

func TestFrameRoundTrip(t *testing.T) {
	b := []byte{0, 0, 0x41, 0, 0, 0x5a, 0, 0} // captured from a real C200: fov 65, mic 90
	if fov, mic := parseFrame(b); fov != 65 || mic != 90 {
		t.Fatalf("parse real frame: %d %d", fov, mic)
	}
	packFov(b, 95)
	packMic(b, 0) // omnidirectional in AnkerWork
	if fov, mic := parseFrame(b); fov != 95 || mic != 0 || b[0] != 0 || b[1] != 1 {
		t.Fatalf("round trip: %d %d % x", fov, mic, b)
	}
}

func TestYUYV(t *testing.T) {
	img := yuyvToImage([]byte{10, 20, 30, 40, 50, 60, 70, 80}, 4, 1)
	if img.Y[0] != 10 || img.Y[1] != 30 || img.Y[3] != 70 || img.Cb[1] != 60 || img.Cr[0] != 40 {
		t.Fatalf("bad conversion %v %v %v", img.Y, img.Cb, img.Cr)
	}
}
