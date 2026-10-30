package player

import (
	"context"
	"errors"
	"io"
	"math"

	"github.com/llehouerou/go-faad2"
)

// adtsStream decodes a bare AAC download for package audio: the ADTS a
// source script sometimes serves, which is the same AAC frames as an MP4's
// but one after another, each behind a header that says how long it is.
//
// ADTS has no index, so a seek is by proportion: a song's frames are much
// the same size, and the frame asked for is near the byte the song's
// length puts it at, found there by its sync word.
type adtsStream struct {
	dl   *download
	dec  *faad2.Decoder
	ctx  context.Context
	rate int
	ch   int

	r      *downloadReader
	start  int64 // where the first frame starts
	frames int64 // the song's length in frames, from its duration
	pos    int64 // the frame Read returns next
	pend   []float32
	skip   int64 // frames to drop, after a seek
	spec   *spectrum
	drop   func()
	closeM chan struct{}
}

func openADTSStream(dl *download, durationMs int64) (*adtsStream, error) {
	start := id3Size(dl)
	h, ok := adtsHeader(dl, start)
	if !ok {
		return nil, ErrUnsupportedFormat
	}
	ctx := context.Background()
	dec, err := faad2.NewDecoder(ctx)
	if err != nil {
		return nil, err
	}
	// The AudioSpecificConfig an ADTS header spells out: 5 bits of object
	// type, 4 of sampling rate, 4 of channels, then the frame's own flags,
	// all clear.
	asc := []byte{
		byte((h.profile+1)<<3) | byte(h.rate>>1),
		byte(h.rate&1)<<7 | byte(h.channels&0xF)<<3,
		0,
	}
	if err := dec.Init(ctx, asc); err != nil {
		dec.Close(ctx)
		return nil, err
	}
	s := &adtsStream{
		dl: dl, dec: dec, ctx: ctx, start: start,
		rate: int(dec.SampleRate()), ch: int(dec.Channels()),
		closeM: make(chan struct{}),
	}
	if s.rate == 0 {
		dec.Close(ctx)
		return nil, ErrUnsupportedFormat
	}
	// The header says how many channels there are; what the decoder says
	// of a bare stream may not, and it decodes the frames all the same.
	if s.ch == 0 {
		s.ch = h.channels
	}
	s.frames = durationMs * int64(s.rate) / 1000
	s.spec = newSpectrum(s.rate, s.frames)
	s.drop = dl.Close
	return s, nil
}

func (s *adtsStream) SampleRate() int { return s.rate }
func (s *adtsStream) Channels() int   { return 2 }
func (s *adtsStream) Len() int64      { return s.frames }

func (s *adtsStream) source() (*download, *spectrum) { return s.dl, s.spec }

func (s *adtsStream) Close() error {
	select {
	case <-s.closeM:
	default:
		close(s.closeM)
		s.dec.Close(s.ctx)
		s.drop()
	}
	return nil
}

func (s *adtsStream) Read(dst []float32) (int, error) {
	for len(s.pend) == 0 {
		if err := s.next(); err != nil {
			return 0, err
		}
	}
	n := copy(dst[:len(dst)/2*2], s.pend)
	s.pend = s.pend[n:]
	s.spec.feed(s.pos, dst[:n])
	s.pos += int64(n / 2)
	return n, nil
}

// adtsInfo is what a frame's header says of the stream and of the frame.
type adtsInfo struct {
	profile  int // object type - 1, as ADTS counts it
	rate     int // the sampling rate index
	channels int
	size     int // the frame's length, header and all
}

// adtsHeader reads the ADTS header at off, and whether there is one.
func adtsHeader(dl *download, off int64) (adtsInfo, bool) {
	if dl.waitFor(off+7) < off+7 {
		return adtsInfo{}, false
	}
	b := dl.peek(off, 7)
	if b[0] != 0xFF || b[1]&0xF6 != 0xF0 {
		return adtsInfo{}, false
	}
	h := adtsInfo{
		profile:  int(b[2]>>6) & 3,
		rate:     int(b[2]>>2) & 0xF,
		channels: int(b[2]&1)<<2 | int(b[3]>>6),
		size:     int(b[3]&3)<<11 | int(b[4])<<3 | int(b[5]>>5),
	}
	if h.rate >= 13 || h.size < 7 {
		return adtsInfo{}, false
	}
	return h, true
}

// next decodes the next frame into s.pend, in stereo.
func (s *adtsStream) next() error {
	if s.r == nil {
		s.r = &downloadReader{d: s.dl, pos: s.start}
	}
	// Skip what of the stream does not start a frame, as a bad byte does.
	var h adtsInfo
	for tries := 0; tries < 64<<10; tries++ {
		if dl, ok := adtsHeaderAt(s); ok {
			h = dl
			break
		}
		s.r.pos++
		if s.r.pos >= s.dl.waitFor(math.MaxInt64) && s.dl.done {
			return io.EOF
		}
	}
	if h.size == 0 {
		return io.EOF
	}
	frame := make([]byte, h.size)
	if _, err := io.ReadFull(s.r, frame); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return io.EOF
		}
		return err
	}
	// The header, or the header and its checksum, is not the decoder's.
	at := 7
	if frame[1]&1 == 0 {
		at = 9
	}
	if at > len(frame) {
		return io.EOF
	}
	pcm, err := s.dec.Decode(s.ctx, frame[at:])
	if err != nil {
		// A frame the decoder will not take is skipped: the rest of the
		// song plays.
		return nil
	}
	s.take(pcm)
	return nil
}

// adtsHeaderAt reads the header at the reader's place.
func adtsHeaderAt(s *adtsStream) (adtsInfo, bool) {
	return adtsHeader(s.dl, s.r.pos)
}

// take puts a decoded frame's samples in s.pend, in stereo, bar those a
// seek drops.
func (s *adtsStream) take(pcm []int16) {
	ch := max(s.ch, 1)
	n := len(pcm) / ch
	from := int(min(s.skip, int64(n)))
	s.skip -= int64(from)
	if from == n {
		s.pend = s.pend[:0]
		return
	}
	if cap(s.pend) < (n-from)*2 {
		s.pend = make([]float32, 0, (n-from)*2)
	}
	out := s.pend[:0]
	scale := float32(1.0 / 32768)
	for i := from; i < n; i++ {
		l := pcm[i*ch]
		r := l
		if ch > 1 {
			r = pcm[i*ch+1]
		}
		out = append(out, float32(l)*scale, float32(r)*scale)
	}
	s.pend = out
}

// SetPosition goes to frame, by proportion: the byte the frame is near,
// then the frame that starts a little after it. The frames are of much the
// same length, so the one found is close to the one asked for, and the
// rest is dropped as they are decoded.
func (s *adtsStream) SetPosition(frame int64) error {
	frame = max(frame, 0)
	size := s.dl.size
	if size <= 0 {
		size = s.dl.waitFor(math.MaxInt64)
	}
	off := s.start
	if s.frames > 0 {
		off = s.start + int64(float64(size-s.start)*math.Min(float64(frame)/float64(s.frames), 1))
	}
	s.r = &downloadReader{d: s.dl, pos: off}
	// Where there in the song it is: what the byte says of it, less what
	// is dropped after it.
	at := int64(0)
	if s.frames > 0 && size > s.start {
		at = int64(float64(frame) * float64(off-s.start) / float64(size-s.start))
	}
	s.pos, s.pend = frame, s.pend[:0]
	s.skip = frame - at
	if s.skip < 0 {
		s.skip = 0
	}
	return nil
}
