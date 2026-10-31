package player

import "testing"

// A stream the pure-Go decoder does not do is decoded by FAAD2 instead,
// so that a song it cannot take still plays: an HE-AAC AudioSpecificConfig,
// whose SBR go-aac refuses, falls back rather than failing the song.
func TestAACDecoderFallsBack(t *testing.T) {
	// HE-AAC v1 (object type 5), 44.1 kHz, stereo. go-aac refuses the
	// config outright, which is what the fallback is for.
	dec, err := newAACDecoder([]byte{0x2b, 0x92, 0x08, 0x00})
	if err != nil {
		t.Fatalf("no decoder for an HE-AAC stream: %v", err)
	}
	defer dec.Close()
	if _, ok := dec.(*wasmAAC); !ok {
		t.Errorf("HE-AAC took %T, want FAAD2", dec)
	}
	// And AAC-LC, which the sources serve, is the pure-Go one.
	lc, err := newAACDecoder([]byte{0x12, 0x10})
	if err != nil {
		t.Fatalf("no decoder for an AAC-LC stream: %v", err)
	}
	defer lc.Close()
	if _, ok := lc.(*pureAAC); !ok {
		t.Errorf("AAC-LC took %T, want the pure-Go decoder", lc)
	}
}
