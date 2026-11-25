package muse

// Bộ mã hoá/giải mã protobuf tối giản cho khung dữ liệu của gateway Muse.
// Cổng từ desktop/src/shared/gateway/proto.ts — giữ nguyên số hiệu trường.

import (
	"errors"
	"fmt"
)

var ErrProto = errors.New("muse: khung dữ liệu sai định dạng")

func protoErr(f string, a ...any) error { return fmt.Errorf("%w: %s", ErrProto, fmt.Sprintf(f, a...)) }

// Varint mã hoá số nguyên không dấu theo kiểu protobuf.
func Varint(v uint64) []byte {
	out := make([]byte, 0, 10)
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			out = append(out, b|0x80)
		} else {
			return append(out, b)
		}
	}
}

func FieldVarint(f int, v uint64) []byte { return append(Varint(uint64(f)<<3), Varint(v)...) }

func FieldBytes(f int, b []byte) []byte {
	out := Varint(uint64(f)<<3 | 2)
	out = append(out, Varint(uint64(len(b)))...)
	return append(out, b...)
}

func FieldString(f int, s string) []byte { return FieldBytes(f, []byte(s)) }

func Concat(parts ...[]byte) []byte {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func readVarint(buf []byte, pos int) (uint64, int, error) {
	var shift uint
	var val uint64
	for i := 0; i < 10; i++ {
		if pos >= len(buf) {
			return 0, 0, protoErr("varint bị cắt")
		}
		b := buf[pos]
		pos++
		val |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return val, pos, nil
		}
		shift += 7
	}
	return 0, 0, protoErr("varint quá dài")
}

// Value là một giá trị của trường: số (wire type 0) hoặc chuỗi byte (wire type 1/2/5).
type Value struct {
	Num   uint64
	Bytes []byte
	IsNum bool
}

// Fields là một mức message đã giải mã: số hiệu trường → danh sách giá trị.
type Fields map[int][]Value

// DecodeFields giải mã một mức message. Wire type lạ hoặc tràn độ dài đều là lỗi giao thức.
func DecodeFields(buf []byte) (Fields, error) {
	out := Fields{}
	pos := 0
	for pos < len(buf) {
		key, p, err := readVarint(buf, pos)
		if err != nil {
			return nil, err
		}
		pos = p
		f := int(key >> 3)
		wt := int(key & 7)
		var v Value
		switch wt {
		case 0:
			x, p2, err := readVarint(buf, pos)
			if err != nil {
				return nil, err
			}
			pos = p2
			v = Value{Num: x, IsNum: true}
		case 2:
			ln, p2, err := readVarint(buf, pos)
			if err != nil {
				return nil, err
			}
			pos = p2
			n := int(ln)
			if n < 0 || pos+n > len(buf) {
				return nil, protoErr("tràn độ dài")
			}
			v = Value{Bytes: buf[pos : pos+n]}
			pos += n
		case 5:
			if pos+4 > len(buf) {
				return nil, protoErr("tràn fixed32")
			}
			v = Value{Bytes: buf[pos : pos+4]}
			pos += 4
		case 1:
			if pos+8 > len(buf) {
				return nil, protoErr("tràn fixed64")
			}
			v = Value{Bytes: buf[pos : pos+8]}
			pos += 8
		default:
			return nil, protoErr("wire type %d", wt)
		}
		out[f] = append(out[f], v)
	}
	return out, nil
}

func (f Fields) Num(k int, d uint64) uint64 {
	if v, ok := f[k]; ok && len(v) > 0 && v[0].IsNum {
		return v[0].Num
	}
	return d
}

func (f Fields) Bytes(k int) []byte {
	if v, ok := f[k]; ok && len(v) > 0 && !v[0].IsNum {
		return v[0].Bytes
	}
	return nil
}

func (f Fields) Str(k int) string { return string(f.Bytes(k)) }

// Has cho biết trường có xuất hiện hay không (dùng để nhận diện gói challenge).
func (f Fields) Has(k int) bool { return len(f[k]) > 0 }
