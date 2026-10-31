package player

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"sync"
)

// aacStream decodes an MP4 (m4a) download for package audio: the AAC that
// source scripts often serve, which is neither MP3 nor FLAC.
//
// The MP4 is read through the download rather than seeked: its index may
// sit at the front, as a song prepared for streaming has it, and then the
// music plays as its bytes come in, or at the end, as most encoders write
// it, and then the song is waited for. Either way the samples after the
// index are read where it says they are.
type aacStream struct {
	dl   *download
	idx  *mp4Index
	dec  aacDecoder
	rate int
	ch   int // the song's own channels: 1 is played as stereo

	at     int // the sample of the index decoded next
	r      *downloadReader
	pend   []float32
	skip   int64 // samples to drop, after a seek
	pos    int64 // the frame Read returns next
	frames int64 // the song's length in frames
	spec   *spectrum
	drop   func()
	closeM sync.Once
}

// mp4Index is the AudioSpecificConfig of the MP4's audio, and the tables
// that say where and when its samples are: what the stream decodes by.
type mp4Index struct {
	asc      []byte
	rate     int
	channels int
	// sizes are the length of each sample in bytes, or uniform when it is
	// the same for all of them.
	sizes   []uint32
	uniform uint32
	// chunks are where each run of samples starts in the file, and
	// samples the number of samples in each run.
	chunks  []uint64
	samples []uint32
	// offs is where each sample starts, worked out once from the two
	// above.
	offs []uint64
	// stts is how long each run of samples lasts, in timescale units.
	stts      []sttsEntry
	timescale uint32
	// total is how many samples the track has.
	total int64
}

// sttsEntry is a run of samples of the same length in time.
type sttsEntry struct {
	count uint32
	delta uint32
}

// openAACStream reads the MP4's index and prepares its decoder.
func openAACStream(dl *download, durationMs int64) (*aacStream, error) {
	idx, err := readMP4Index(dl)
	if err != nil {
		return nil, err
	}
	dec, err := newAACDecoder(idx.asc)
	if err != nil {
		return nil, err
	}
	s := &aacStream{dl: dl, idx: idx, dec: dec, drop: dl.Close}
	s.rate = dec.SampleRate()
	s.ch = dec.Channels()
	if s.rate == 0 {
		s.rate = idx.rate
	}
	if s.ch == 0 {
		s.ch = idx.channels
	}
	frames := idx.frames(s.rate)
	if frames <= 0 {
		frames = durationMs * int64(s.rate) / 1000
	}
	s.frames = frames
	s.spec = newSpectrum(s.rate, frames)
	return s, nil
}

func (s *aacStream) SampleRate() int { return s.rate }
func (s *aacStream) Channels() int   { return 2 }

// Len returns the song's length in frames: what its index says it is, or
// what its duration does when the index is not read.
func (s *aacStream) Len() int64 {
	if n := s.idx.frames(s.rate); n > 0 {
		return n
	}
	return s.frames
}

func (s *aacStream) source() (*download, *spectrum) { return s.dl, s.spec }

func (s *aacStream) Close() error {
	s.closeM.Do(func() {
		s.dec.Close()
		s.drop()
	})
	return nil
}

// Read decodes samples until it has some to give.
func (s *aacStream) Read(dst []float32) (int, error) {
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

// next decodes the sample at s.at into s.pend, in stereo.
func (s *aacStream) next() error {
	if s.at >= int(s.idx.total) {
		return io.EOF
	}
	off := s.idx.offsetOf(s.at)
	if s.r == nil || s.r.pos != int64(off) {
		s.r = &downloadReader{d: s.dl, pos: int64(off)}
	}
	size := s.idx.sizeOf(s.at)
	frame := make([]byte, size)
	if _, err := io.ReadFull(s.r, frame); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return io.EOF
		}
		return err
	}
	s.at++
	pcm, err := s.dec.Decode(frame)
	if err != nil {
		// A frame the decoder will not take is skipped, as Music does:
		// the rest of the song plays.
		return nil
	}
	ch := max(s.ch, 1)
	nframes := len(pcm) / ch
	from := int(min(s.skip, int64(nframes)))
	s.skip -= int64(from)
	if from == nframes {
		return nil
	}
	if cap(s.pend) < (nframes-from)*2 {
		s.pend = make([]float32, 0, (nframes-from)*2)
	}
	out := s.pend[:0]
	scale := float32(1.0 / 32768)
	for i := from; i < nframes; i++ {
		l := pcm[i*ch]
		r := l
		if ch > 1 {
			r = pcm[i*ch+1]
		}
		out = append(out, float32(l)*scale, float32(r)*scale)
	}
	s.pend = out
	return nil
}

// SetPosition goes to frame: to the sample holding it, then drops what of
// it comes before. AAC frames borrow nothing from those before, so the
// samples are independent and a seek lands on the one asked for.
func (s *aacStream) SetPosition(frame int64) error {
	if frame < 0 {
		frame = 0
	}
	sample, offset := s.idx.sampleAt(frame, s.rate)
	s.at, s.r, s.pend = sample, nil, s.pend[:0]
	s.pos, s.skip = frame, offset
	return nil
}

// sizeOf returns the length of sample i.
func (idx *mp4Index) sizeOf(i int) int {
	if idx.uniform > 0 {
		return int(idx.uniform)
	}
	if i < 0 || i >= len(idx.sizes) {
		return 0
	}
	return int(idx.sizes[i])
}

// buildOffs works out where each sample starts: each run of chunks from
// its offset, each sample of it after the one before.
func (idx *mp4Index) buildOffs() {
	if len(idx.chunks) == 0 || len(idx.samples) == 0 {
		return
	}
	idx.offs = make([]uint64, 0, idx.total)
	i := 0
	for c, start := range idx.chunks {
		per := idx.samples[c]
		off := start
		for range per {
			idx.offs = append(idx.offs, off)
			off += uint64(idx.sizeOf(i))
			i++
		}
	}
}

// offsetOf returns where sample i starts in the file.
func (idx *mp4Index) offsetOf(i int) uint64 {
	if i < 0 || i >= len(idx.offs) {
		return 0
	}
	return idx.offs[i]
}

// frames is how many frames the song is, at rate.
func (idx *mp4Index) frames(rate int) int64 {
	if idx.timescale == 0 || rate == 0 {
		return idx.total
	}
	var d int64
	for _, e := range idx.stts {
		d += int64(e.count) * int64(e.delta)
	}
	if d == 0 {
		return idx.total
	}
	return d * int64(rate) / int64(idx.timescale)
}

// sampleAt returns the sample holding frame, at rate, and the samples of
// it to drop: what of it comes before the frame.
func (idx *mp4Index) sampleAt(frame int64, rate int) (int, int64) {
	if idx.timescale == 0 || rate == 0 || len(idx.stts) == 0 {
		return int(frame), 0
	}
	t := frame * int64(idx.timescale) / int64(rate)
	var at, start int64
	for _, e := range idx.stts {
		span := int64(e.count) * int64(e.delta)
		if t < start+span {
			n := (t - start) / int64(e.delta)
			sample := int(at + n)
			// Where the sample starts, in the frames the player counts.
			begin := (start + n*int64(e.delta)) * int64(rate) / int64(idx.timescale)
			return sample, frame - begin
		}
		at += int64(e.count)
		start += span
	}
	return int(idx.total), 0
}

// readMP4Index reads an MP4's audio track index: its AudioSpecificConfig
// and where its samples are. It reads through the download, waiting for
// the bytes it needs, so an index at the front starts the song at once.
func readMP4Index(dl *download) (*mp4Index, error) {
	if dl.waitFor(8) < 8 {
		return nil, ErrUnsupportedFormat
	}
	// The index may be at the end, as most encoders write it: the boxes
	// are read as they come in.
	moov, err := findBox(dl, 0, "moov")
	if err != nil {
		return nil, ErrUnsupportedFormat
	}
	idx := &mp4Index{}
	if err := readMoov(dl, moov, idx); err != nil {
		return nil, err
	}
	if len(idx.asc) == 0 || len(idx.chunks) == 0 || idx.total == 0 {
		return nil, ErrUnsupportedFormat
	}
	return idx, nil
}

// box is where a box starts, and where its payload and it end.
type box struct {
	off, body, end int64
	typ            string
}

// findBox reads the boxes in [from, to) and returns the first of type typ,
// waiting for the bytes it needs.
func findBox(dl *download, from int64, typ string) (box, error) {
	bs, err := boxesIn(dl, from, 0, []string{typ})
	if err != nil {
		return box{}, err
	}
	if len(bs) == 0 {
		return box{}, ErrUnsupportedFormat
	}
	return bs[0], nil
}

// boxesIn returns the boxes in [from, to) of the types wanted, waiting
// for the bytes: to 0 reads to the end of the file, known once the
// download has it all or a box says its size.
func boxesIn(dl *download, from, to int64, want []string) ([]box, error) {
	var out []box
	off := from
	for {
		have := dl.waitFor(off + 8)
		if have < off+8 {
			return out, nil
		}
		b := dl.peek(off, 16)
		size := int64(binary.BigEndian.Uint32(b[0:4]))
		typ := string(b[4:8])
		body := off + 8
		switch size {
		case 1:
			if len(b) < 16 {
				have = dl.waitFor(off + 16)
				if have < off+16 {
					return out, nil
				}
				b = dl.peek(off, 16)
			}
			size = int64(binary.BigEndian.Uint64(b[8:16]))
			body = off + 16
		case 0:
			// The box runs to the end of the file.
			size = dl.waitFor(math.MaxInt64) - off
		}
		if size < body-off {
			return out, errors.New("mp4: a box shorter than its header")
		}
		end := off + size
		if to > 0 && end > to {
			end = to
		}
		for _, w := range want {
			if typ == w {
				out = append(out, box{off: off, body: body, end: end, typ: typ})
				break
			}
		}
		if size == 0 {
			return out, nil
		}
		off = end
		if to > 0 && off >= to {
			return out, nil
		}
	}
}

// readMoov reads the index of the moov box of an MP4.
func readMoov(dl *download, moov box, idx *mp4Index) error {
	traks, err := boxesIn(dl, moov.body, moov.end, []string{"trak"})
	if err != nil {
		return err
	}
	for _, trak := range traks {
		mdias, err := boxesIn(dl, trak.body, trak.end, []string{"mdia"})
		if err != nil || len(mdias) == 0 {
			continue
		}
		mdia := mdias[0]
		if !isSound(dl, mdia) {
			continue
		}
		if err := readTrack(dl, mdia, idx); err != nil {
			return err
		}
		return nil
	}
	return ErrUnsupportedFormat
}

// isSound reports whether the track of mdia is its audio.
func isSound(dl *download, mdia box) bool {
	hdlrs, err := boxesIn(dl, mdia.body, mdia.end, []string{"hdlr"})
	if err != nil || len(hdlrs) == 0 {
		// A track with no handler is taken as audio: the songs the app is
		// given have no other track.
		return true
	}
	b := dl.peek(hdlrs[0].body, 12)
	return len(b) >= 12 && string(b[8:12]) == "soun"
}

// readTrack reads a track's durations, sample sizes and offsets.
func readTrack(dl *download, mdia box, idx *mp4Index) error {
	if mdhs, err := boxesIn(dl, mdia.body, mdia.end, []string{"mdhd"}); err == nil && len(mdhs) > 0 {
		idx.timescale = readTimescale(dl, mdhs[0])
	}
	minfs, err := boxesIn(dl, mdia.body, mdia.end, []string{"minf"})
	if err != nil || len(minfs) == 0 {
		return ErrUnsupportedFormat
	}
	stbls, err := boxesIn(dl, minfs[0].body, minfs[0].end, []string{"stbl"})
	if err != nil || len(stbls) == 0 {
		return ErrUnsupportedFormat
	}
	stbl := stbls[0]
	bs, err := boxesIn(dl, stbl.body, stbl.end, []string{"stsd", "stsz", "stsc", "stco", "co64", "stts"})
	if err != nil {
		return err
	}
	var stsc *box
	for i := range bs {
		b := bs[i]
		switch b.typ {
		case "stsd":
			if err := readStsd(dl, b, idx); err != nil {
				return err
			}
		case "stsz":
			readStsz(dl, b, idx)
		case "stsc":
			stsc = &bs[i]
		case "stco":
			idx.chunks = readChunks(dl, b, false)
		case "co64":
			idx.chunks = readChunks(dl, b, true)
		case "stts":
			idx.stts = readStts(dl, b)
		}
	}
	if stsc != nil {
		idx.samples = readStsc(dl, *stsc, len(idx.chunks))
	}
	idx.buildOffs()
	return nil
}

// readTimescale returns the media timescale of an mdhd box.
func readTimescale(dl *download, b box) uint32 {
	head := dl.peek(b.body, 20)
	if len(head) < 4 {
		return 0
	}
	if head[0] == 1 {
		if len(head) < 20 {
			return 0
		}
		return binary.BigEndian.Uint32(head[16:20])
	}
	if len(head) < 16 {
		return 0
	}
	return binary.BigEndian.Uint32(head[12:16])
}

// readStsd reads the audio sample entry's AudioSpecificConfig.
func readStsd(dl *download, b box, idx *mp4Index) error {
	entries, err := boxesIn(dl, b.body+8, b.end, []string{"mp4a", "enca", "alac", "ec-3", "ac-3"})
	if err != nil || len(entries) == 0 {
		return ErrUnsupportedFormat
	}
	e := entries[0]
	if e.typ != "mp4a" && e.typ != "enca" {
		return ErrUnsupportedFormat
	}
	// The sample entry's fields before its child boxes: 28 bytes of them,
	// and 16 more in a version 1 entry.
	head := dl.peek(e.body, 28)
	if len(head) < 28 {
		return ErrUnsupportedFormat
	}
	idx.channels = int(binary.BigEndian.Uint16(head[16:18]))
	idx.rate = int(binary.BigEndian.Uint32(head[24:28]) >> 16)
	child := e.body + 28
	if v := binary.BigEndian.Uint16(head[8:10]); v == 1 {
		child += 16
	} else if v == 2 {
		child += 36
	}
	es, err := boxesIn(dl, child, e.end, []string{"esds"})
	if err != nil || len(es) == 0 {
		return ErrUnsupportedFormat
	}
	// The descriptor body, after the full-box header.
	body := es[0].end - es[0].body - 4
	if body <= 0 || body > 1<<16 {
		return ErrUnsupportedFormat
	}
	idx.asc = decodeSpecific(dl.peek(es[0].body+4, int(body)))
	if len(idx.asc) == 0 {
		return ErrUnsupportedFormat
	}
	return nil
}

// decodeSpecific finds the DecoderSpecificInfo in a list of MPEG-4
// descriptors: the AudioSpecificConfig.
func decodeSpecific(b []byte) []byte {
	for len(b) >= 2 {
		tag, size, n := b[0], uint32(0), 0
		for i := 1; i < len(b) && i <= 4; i++ {
			size = size<<7 | uint32(b[i]&0x7f)
			n = i
			if b[i]&0x80 == 0 {
				break
			}
		}
		if n == 0 {
			return nil
		}
		start := 1 + n
		if start > len(b) || int(size) > len(b)-start {
			return nil
		}
		body := b[start : start+int(size)]
		switch tag {
		case 0x05:
			return body
		case 0x03:
			// The ES descriptor's own fields, then its descriptors.
			if len(body) < 3 {
				return nil
			}
			at := 3
			flags := body[2]
			if flags&0x80 != 0 {
				at += 2
			}
			if flags&0x40 != 0 && at < len(body) {
				at += 1 + int(body[at])
			}
			if flags&0x20 != 0 {
				at += 2
			}
			if at < len(body) {
				if asc := decodeSpecific(body[at:]); asc != nil {
					return asc
				}
			}
		case 0x04:
			// Its fixed fields, then its descriptors.
			if len(body) < 13 {
				return nil
			}
			if asc := decodeSpecific(body[13:]); asc != nil {
				return asc
			}
		}
		b = b[start+int(size):]
	}
	return nil
}

// readStsz reads the length of each sample.
func readStsz(dl *download, b box, idx *mp4Index) {
	head := dl.peek(b.body, 12)
	if len(head) < 12 {
		return
	}
	uniform := binary.BigEndian.Uint32(head[4:8])
	n := int(binary.BigEndian.Uint32(head[8:12]))
	idx.total = int64(n)
	if uniform > 0 {
		idx.uniform = uniform
		return
	}
	if n <= 0 || n > 1<<24 {
		idx.total = 0
		return
	}
	tbl := dl.peek(b.body+12, n*4)
	if len(tbl) < n*4 {
		return
	}
	idx.sizes = make([]uint32, n)
	for i := range n {
		idx.sizes[i] = binary.BigEndian.Uint32(tbl[i*4:])
	}
}

// readStsc reads how many samples each run of chunks holds: one entry per
// chunk, as stsc gives them in runs of chunks.
func readStsc(dl *download, b box, chunks int) []uint32 {
	head := dl.peek(b.body, 8)
	if len(head) < 8 {
		return nil
	}
	n := int(binary.BigEndian.Uint32(head[4:8]))
	if n <= 0 || n > 1<<16 {
		return nil
	}
	tbl := dl.peek(b.body+8, n*12)
	if len(tbl) < n*12 {
		return nil
	}
	type entry struct{ first, per uint32 }
	entries := make([]entry, n)
	for i := range n {
		entries[i] = entry{
			first: binary.BigEndian.Uint32(tbl[i*12:]),
			per:   binary.BigEndian.Uint32(tbl[i*12+4:]),
		}
	}
	// Every chunk from one entry's first to the next's holds that entry's
	// samples.
	out := make([]uint32, chunks)
	for i, e := range entries {
		from := int(e.first) - 1
		to := chunks
		if i+1 < len(entries) {
			to = int(entries[i+1].first) - 1
		}
		for c := max(from, 0); c < to && c < chunks; c++ {
			out[c] = e.per
		}
	}
	return out
}

// readChunks reads where each run of samples starts in the file.
func readChunks(dl *download, b box, wide bool) []uint64 {
	head := dl.peek(b.body, 8)
	if len(head) < 8 {
		return nil
	}
	n := int(binary.BigEndian.Uint32(head[4:8]))
	if n <= 0 || n > 1<<20 {
		return nil
	}
	width := 4
	if wide {
		width = 8
	}
	tbl := dl.peek(b.body+8, n*width)
	if len(tbl) < n*width {
		return nil
	}
	out := make([]uint64, n)
	for i := range n {
		if wide {
			out[i] = binary.BigEndian.Uint64(tbl[i*8:])
		} else {
			out[i] = uint64(binary.BigEndian.Uint32(tbl[i*4:]))
		}
	}
	return out
}

// readStts reads how long each sample lasts, in runs.
func readStts(dl *download, b box) []sttsEntry {
	head := dl.peek(b.body, 8)
	if len(head) < 8 {
		return nil
	}
	n := int(binary.BigEndian.Uint32(head[4:8]))
	if n <= 0 || n > 1<<20 {
		return nil
	}
	tbl := dl.peek(b.body+8, n*8)
	if len(tbl) < n*8 {
		return nil
	}
	out := make([]sttsEntry, n)
	for i := range n {
		out[i] = sttsEntry{
			count: binary.BigEndian.Uint32(tbl[i*8:]),
			delta: binary.BigEndian.Uint32(tbl[i*8+4:]),
		}
	}
	return out
}
