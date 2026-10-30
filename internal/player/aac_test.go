package player

import (
	"math"
	"os"
	"testing"
)

// decodedAAC decodes a whole song from its start.
func decodedAAC(t *testing.T, dl *download, durationMs int64) []float32 {
	t.Helper()
	s, err := openStream(dl, durationMs)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var out []float32
	buf := make([]float32, 4096)
	for {
		n, err := s.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil || n == 0 {
			return out
		}
	}
}

// sineRMS returns what a run of stereo samples' sine is, as its loudness.
func sineRMS(b []float32) float64 {
	if len(b) == 0 {
		return 0
	}
	var sum float64
	for _, v := range b {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(len(b)))
}

// An m4a's AAC is decoded: the song plays, as loud as it was encoded, for
// as long as it is.
func TestM4ADecodes(t *testing.T) {
	b, err := os.ReadFile("testdata/sine.m4a")
	if err != nil {
		t.Skipf("sine.m4a: %v", err)
	}
	dl := downloaded(b)
	got := decodedAAC(t, dl, 3000)
	if len(got) == 0 {
		t.Fatal("nothing decoded")
	}
	// 3 seconds of stereo at 44.1 kHz, give or take the encoder's own.
	frames := len(got) / 2
	if frames < 44100*3*9/10 || frames > 44100*3*11/10 {
		t.Errorf("%d frames, want about 132300", frames)
	}
	// ffmpeg's sine is a fifth of full scale, which is about 0.071 in
	// RMS.
	if rms := sineRMS(got); rms < 0.05 || rms > 0.10 {
		t.Errorf("rms %.3f, want about 0.071", rms)
	}
}

// A seek into an m4a lands on the music there, not on noise: the samples
// after the one seeked to are the very ones played in order.
func TestM4ASeek(t *testing.T) {
	b, err := os.ReadFile("testdata/sine.m4a")
	if err != nil {
		t.Skipf("sine.m4a: %v", err)
	}
	all := decodedAAC(t, downloaded(b), 3000)
	if len(all) < 44100*4 {
		t.Fatalf("%d samples", len(all))
	}
	for _, frame := range []int64{44100, 88200, 120000} {
		dl := downloaded(b)
		s, err := openStream(dl, 3000)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetPosition(frame); err != nil {
			t.Fatal(err)
		}
		buf := make([]float32, 4096)
		n, _ := s.Read(buf)
		s.Close()
		if n == 0 {
			t.Fatalf("nothing at %d", frame)
		}
		// What plays is the sine, from around there: its loudness is the
		// song's, not a burst of noise.
		if rms := sineRMS(buf[:n]); rms < 0.04 || rms > 0.12 {
			t.Errorf("at %d: rms %.3f", frame, rms)
		}
		// And it is what the song has there, within a frame or two of it.
		want := all[int(frame)*2 : int(frame)*2+n]
		if d := maxDiff(buf[:n], want); d > 0.2 {
			t.Errorf("at %d: off by %.3f", frame, d)
		}
	}
}

// A bare AAC stream, ADTS, is decoded too.
func TestADTSDecodes(t *testing.T) {
	b, err := os.ReadFile("testdata/sine.aac")
	if err != nil {
		t.Skipf("sine.aac: %v", err)
	}
	got := decodedAAC(t, downloaded(b), 3000)
	if len(got) == 0 {
		t.Fatal("nothing decoded")
	}
	if frames := len(got) / 2; frames < 44100*3*9/10 || frames > 44100*3*11/10 {
		t.Errorf("%d frames, want about 132300", frames)
	}
	if rms := sineRMS(got); rms < 0.05 || rms > 0.10 {
		t.Errorf("rms %.3f, want about 0.071", rms)
	}
}
