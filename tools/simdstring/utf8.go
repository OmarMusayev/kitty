// License: GPLv3 Copyright: 2024, Kovid Goyal, <kovid at kovidgoyal.net>

package simdstring

import (
"unsafe"
)

// UTF-8 decode DFA taken from: https://bjoern.hoehrmann.de/utf-8/decoder/dfa/
type utf8State uint32

var utf8DFAData = [...]uint8{
0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9,
7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
8, 8, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
0xa, 0x3, 0x3, 0x3, 0x3, 0x3, 0x3, 0x3, 0x3, 0x3, 0x3, 0x3, 0x3, 0x4, 0x3, 0x3,
0xb, 0x6, 0x6, 0x6, 0x5, 0x8, 0x8, 0x8, 0x8, 0x8, 0x8, 0x8, 0x8, 0x8, 0x8, 0x8,
0x0, 0x1, 0x2, 0x3, 0x5, 0x8, 0x7, 0x1, 0x1, 0x1, 0x4, 0x6, 0x1, 0x1, 0x1, 0x1,
1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 1, 1, 1, 1, 1, 0, 1, 0, 1, 1, 1, 1, 1, 1,
1, 2, 1, 1, 1, 1, 1, 2, 1, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 1,
1, 2, 1, 1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 3, 1, 3, 1, 1, 1, 1, 1, 1,
1, 3, 1, 1, 1, 1, 1, 3, 1, 3, 1, 1, 1, 1, 1, 1, 1, 3, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
}

const (
utf8Accept utf8State = 0
utf8Reject utf8State = 1
)

func decodeUtf8(state *utf8State, codep *utf8State, b byte) utf8State {
typ := utf8State(utf8DFAData[b])
bv := utf8State(b)
if *state != utf8Accept {
*codep = (bv & 0x3f) | (*codep << 6)
} else {
*codep = (0xff >> typ) & bv
}
idx := 256 + *state*16 + typ
*state = utf8State(utf8DFAData[idx])
return *state
}

// UTF8Decoder holds the state for incremental UTF-8 decoding.
type UTF8Decoder struct {
Output      []uint32
StateCur    uint32
StatePrev   uint32
StateCodep  uint32
NumConsumed uint
}

// Reset clears the decoder state for reuse.
func (d *UTF8Decoder) Reset() {
d.StateCur = 0
d.StatePrev = 0
d.StateCodep = 0
}

// numbered_bytes_128 and numbered_bytes_256 are constant vectors used as
// indexed position arrays. They are referenced from assembly code.
var utf8_numbered_bytes_128 = [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
var utf8_numbered_bytes_256 = [32]byte{
0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31,
}

func utf8ScalarToAccept(d *UTF8Decoder, src []byte) (n int, foundEsc bool) {
cur := utf8State(d.StateCur)
codep := utf8State(d.StateCodep)
for n < len(src) && cur != utf8Accept {
ch := src[n]
n++
if ch == 0x1b {
d.Output = append(d.Output, 0xfffd)
cur = 0
codep = 0
d.StatePrev = 0
d.StateCur = 0
d.StateCodep = 0
return n, true
}
switch decodeUtf8(&cur, &codep, ch) {
case utf8Accept:
d.Output = append(d.Output, uint32(codep))
case utf8Reject:
prevWasAccept := d.StatePrev == uint32(utf8Accept)
d.Output = append(d.Output, 0xfffd)
cur = 0
codep = 0
if !prevWasAccept && n > 0 {
n--
d.StatePrev = uint32(cur)
d.StateCur = uint32(cur)
d.StateCodep = uint32(codep)
continue
}
}
d.StatePrev = d.StateCur
d.StateCur = uint32(cur)
d.StateCodep = uint32(codep)
}
d.StateCur = uint32(cur)
d.StateCodep = uint32(codep)
return n, false
}

func utf8ScalarChunk(d *UTF8Decoder, src []byte) bool {
cur := utf8State(d.StateCur)
codep := utf8State(d.StateCodep)
for i := 0; i < len(src); i++ {
ch := src[i]
if ch == 0x1b {
if cur != utf8Accept {
d.Output = append(d.Output, 0xfffd)
cur = 0
codep = 0
}
d.StateCur = uint32(cur)
d.StateCodep = uint32(codep)
return true
}
switch decodeUtf8(&cur, &codep, ch) {
case utf8Accept:
d.Output = append(d.Output, uint32(codep))
case utf8Reject:
prevWasAccept := d.StatePrev == uint32(utf8Accept)
d.Output = append(d.Output, 0xfffd)
cur = 0
codep = 0
if !prevWasAccept && i > 0 {
i--
d.StatePrev = uint32(cur)
d.StateCur = uint32(cur)
d.StateCodep = uint32(codep)
continue
}
}
d.StatePrev = d.StateCur
d.StateCur = uint32(cur)
d.StateCodep = uint32(codep)
}
d.StateCur = uint32(cur)
d.StateCodep = uint32(codep)
return false
}

// Utf8DecodeToEscScalar is the scalar (non-SIMD) implementation of Utf8DecodeToEsc.
func Utf8DecodeToEscScalar(d *UTF8Decoder, src []byte) bool {
d.Output = d.Output[:0]
d.NumConsumed = 0
cur := utf8State(d.StateCur)
codep := utf8State(d.StateCodep)
for d.NumConsumed < uint(len(src)) {
ch := src[d.NumConsumed]
d.NumConsumed++
if ch == 0x1b {
if cur != utf8Accept {
d.Output = append(d.Output, 0xfffd)
cur = 0
codep = 0
}
d.StateCur = uint32(cur)
d.StateCodep = uint32(codep)
return true
}
switch decodeUtf8(&cur, &codep, ch) {
case utf8Accept:
d.Output = append(d.Output, uint32(codep))
case utf8Reject:
prevWasAccept := d.StatePrev == uint32(utf8Accept)
d.Output = append(d.Output, 0xfffd)
cur = 0
codep = 0
if !prevWasAccept && d.NumConsumed > 0 {
d.NumConsumed--
d.StatePrev = uint32(cur)
d.StateCur = uint32(cur)
d.StateCodep = uint32(codep)
continue
}
}
d.StatePrev = d.StateCur
d.StateCur = uint32(cur)
d.StateCodep = uint32(codep)
}
d.StateCur = uint32(cur)
d.StateCodep = uint32(codep)
return false
}

func utf8DecodeToEscSIMD(d *UTF8Decoder, src []byte, vecSize int,
asmFn func(srcData *byte, srcLen int, outData *uint32) (consumed, produced int, foundEsc, foundInvalid bool),
) bool {
d.Output = d.Output[:0]
d.NumConsumed = 0

if d.StateCur != uint32(utf8Accept) {
n, foundEsc := utf8ScalarToAccept(d, src)
d.NumConsumed = uint(n)
if foundEsc {
return true
}
src = src[n:]
}

needed := len(src)
if needed > 0 && cap(d.Output)-len(d.Output) < needed {
newOut := make([]uint32, len(d.Output), len(d.Output)+needed+64)
copy(newOut, d.Output)
d.Output = newOut
}

for {
if len(src) == 0 {
break
}
if len(src) < vecSize {
found := utf8ScalarChunk(d, src)
d.NumConsumed += uint(len(src))
return found
}

startLen := len(d.Output)
fullSlice := d.Output[:cap(d.Output)]
var outPtr *uint32
if len(fullSlice) > startLen {
outPtr = &fullSlice[startLen]
} else {
newOut := make([]uint32, startLen, startLen+vecSize+64)
copy(newOut, d.Output)
d.Output = newOut
fullSlice = d.Output[:cap(d.Output)]
outPtr = &fullSlice[startLen]
}

consumed, produced, foundEsc, foundInvalid := asmFn(
unsafe.SliceData(src), len(src), outPtr)

d.Output = d.Output[:startLen+produced]

if foundInvalid {
d.NumConsumed += uint(consumed)
src = src[consumed:]
chunkSz := min(len(src), vecSize)
if utf8ScalarChunk(d, src[:chunkSz]) {
d.NumConsumed += uint(chunkSz)
return true
}
d.NumConsumed += uint(chunkSz)
src = src[chunkSz:]
} else if foundEsc {
d.NumConsumed += uint(consumed)
return true
} else {
d.NumConsumed += uint(consumed)
if consumed == 0 {
break
}
src = src[consumed:]
}
}

return false
}

// Utf8DecodeToEsc decodes UTF-8 bytes from src into Unicode codepoints stored in
// d.Output (as uint32 values). Decoding stops at the first ESC byte (0x1b),
// which is consumed. Returns true if an ESC was found.
var Utf8DecodeToEsc func(d *UTF8Decoder, src []byte) bool = Utf8DecodeToEscScalar

func init() {
if Have256bit {
Utf8DecodeToEsc = func(d *UTF8Decoder, src []byte) bool {
return utf8DecodeToEscSIMD(d, src, 32, utf8_decode_to_esc_asm_256)
}
} else if Have128bit {
Utf8DecodeToEsc = func(d *UTF8Decoder, src []byte) bool {
return utf8DecodeToEscSIMD(d, src, 16, utf8_decode_to_esc_asm_128)
}
}
}
