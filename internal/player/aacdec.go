package player

import (
	"context"

	"github.com/llehouerou/go-faad2"
	"github.com/tphakala/go-aac/pcm"
)

// AAC is decoded by go-aac, ported from FFmpeg's fixed-point decoder and
// written in Go, wherever the stream is what it does: AAC-LC in one or two
// channels, which is what the sources serve. It decodes a song in about
// half the time FAAD2 does, and holds no WebAssembly runtime, which costs
// some twenty megabytes of the app's memory for as long as it lives.
//
// A stream beyond it — HE-AAC, whose SBR the pure-Go decoder does not do,
// or one of more than two channels — goes to FAAD2 all the same, which
// decodes it. That is why FAAD2 is still here: it is the fallback now,
// rather than the only decoder, and its runtime is started only by the
// songs that need it.

// aacDecoder decodes one AAC access unit — an MP4's sample, or an ADTS
// frame's payload — to interleaved 16-bit samples.
type aacDecoder interface {
	Decode(au []byte) ([]int16, error)
	SampleRate() int
	Channels() int
	Close()
}

// newAACDecoder returns the decoder for the AudioSpecificConfig asc: the
// pure-Go one, or FAAD2 for a stream that is not AAC-LC in one or two
// channels.
func newAACDecoder(asc []byte) (aacDecoder, error) {
	if d, err := pcm.NewRawDecoder(asc); err == nil && decodableByPureGo(d) {
		return &pureAAC{dec: d, rate: d.SampleRate(), ch: d.Channels(), first: true}, nil
	}
	return newWasmAAC(asc)
}

// decodableByPureGo reports whether the decoder d may take a song's
// stream. AAC-LC in two channels it does; a stream that says nothing of
// its channels until its first frame it may, and it is given that frame to
// find out.
func decodableByPureGo(d *pcm.FrameDecoder) bool {
	switch d.Channels() {
	case 0, 1, 2:
		return true
	}
	return false
}

// pureAAC decodes with go-aac. Its output buffer is reused from frame to
// frame, so a song decodes without allocating, and its rate and channels
// are known from asc, or from the first frame.
type pureAAC struct {
	dec  *pcm.FrameDecoder
	out  []byte  // the bytes of the frame decoded last
	nat  []int16 // its samples, as the decoder gave them
	rate int     // 0 until the stream says
	ch   int     // 0 until it says
	// first is whether no frame has been decoded yet. The decoder's first
	// frame is the one an AAC stream keeps for its overlap, and FAAD2
	// gives nothing for it either: what plays starts at the frame after,
	// where the second's samples are whole.
	first bool
}

func (d *pureAAC) Decode(au []byte) ([]int16, error) {
	var n int
	var err error
	d.out, n, err = d.dec.DecodeFrame(d.out[:0], au)
	if err != nil {
		return nil, err
	}
	if d.rate == 0 {
		d.rate, d.ch = d.dec.SampleRate(), d.dec.Channels()
	}
	if d.first {
		d.first = false
		return nil, nil
	}
	total := n * max(d.ch, 1)
	if cap(d.nat) < total {
		d.nat = make([]int16, total)
	}
	d.nat = d.nat[:total]
	for i := range total {
		d.nat[i] = int16(uint16(d.out[2*i]) | uint16(d.out[2*i+1])<<8)
	}
	return d.nat, nil
}

func (d *pureAAC) SampleRate() int { return d.rate }
func (d *pureAAC) Channels() int   { return d.ch }
func (d *pureAAC) Close()          {}

// newWasmAAC returns FAAD2's decoder for asc.
func newWasmAAC(asc []byte) (aacDecoder, error) {
	ctx := context.Background()
	dec, err := faad2.NewDecoder(ctx)
	if err != nil {
		return nil, err
	}
	if err := dec.Init(ctx, asc); err != nil {
		dec.Close(ctx)
		return nil, err
	}
	return &wasmAAC{dec: dec, ctx: ctx}, nil
}

// wasmAAC decodes with FAAD2, an AAC library compiled to WebAssembly and
// run by wazero: it takes the streams the pure-Go decoder does not, and
// gives nothing for a stream's first frame, as pureAAC does.
type wasmAAC struct {
	dec *faad2.Decoder
	ctx context.Context
}

func (d *wasmAAC) Decode(au []byte) ([]int16, error) { return d.dec.Decode(d.ctx, au) }
func (d *wasmAAC) SampleRate() int                   { return int(d.dec.SampleRate()) }
func (d *wasmAAC) Channels() int                     { return int(d.dec.Channels()) }
func (d *wasmAAC) Close()                            { d.dec.Close(d.ctx) }
