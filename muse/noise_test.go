package muse

// Đối chiếu từng byte với bản TypeScript gốc (desktop/src/shared/gateway/noise.ts).
// Vector sinh bằng chính mã TS chạy trên Node — sai một byte là bắt tay với gateway sẽ hỏng.

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestNoiseVectorsMatchTypeScript(t *testing.T) {
	ss := newSymmetricState()
	want := "4e6f6973655f58585f32353531395f41455347434d5f53484132353600000000"
	if got := hex.EncodeToString(ss.h); got != want {
		t.Fatalf("h0 = %s, muốn %s", got, want)
	}
	if got := hex.EncodeToString(ss.ck); got != want {
		t.Fatalf("ck0 = %s, muốn %s", got, want)
	}

	ss.mixHash(nil) // prologue rỗng
	if got, want := hex.EncodeToString(ss.h), "5df72b67b965add1168f0a6c756df21c204f7e64fc682be6a3ab4b682c8db64b"; got != want {
		t.Fatalf("h sau prologue = %s, muốn %s", got, want)
	}

	ikm := make([]byte, 32)
	for i := range ikm {
		ikm[i] = byte(i + 1)
	}
	out := hkdf(ss.ck, ikm, 2)
	if got, want := hex.EncodeToString(out[0]), "a4add2f53296ef3518e0477a8283bb8158b6d027664d5ed4d55428cf518ed963"; got != want {
		t.Fatalf("hkdf k1 = %s, muốn %s", got, want)
	}
	if got, want := hex.EncodeToString(out[1]), "9917e9a3982f0ce0197c198bcfd6cf9ab61d68750bf0317648b552e54039bf5a"; got != want {
		t.Fatalf("hkdf k2 = %s, muốn %s", got, want)
	}

	if err := ss.mixKey(ikm); err != nil {
		t.Fatal(err)
	}
	if got, want := hex.EncodeToString(ss.ck), "a4add2f53296ef3518e0477a8283bb8158b6d027664d5ed4d55428cf518ed963"; got != want {
		t.Fatalf("ck sau mixKey = %s, muốn %s", got, want)
	}

	ct, err := ss.encryptAndHash([]byte("xin chao"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := hex.EncodeToString(ct), "830c18aa250b54b1269aee656b5b1a1c3de6a6d5ac13891b"; got != want {
		t.Fatalf("encryptAndHash = %s, muốn %s", got, want)
	}
	if got, want := hex.EncodeToString(ss.h), "5c9f66133a962373e1a5c71c57676043d23ac77a7cb6f6d0e9308834a2097a7d"; got != want {
		t.Fatalf("h sau encryptAndHash = %s, muốn %s", got, want)
	}
}

// Bộ đếm nonce phải là uint64 big-endian ở offset 4 của 12 byte.
func TestNonceLayout(t *testing.T) {
	c := &cipherState{}
	if err := c.init(make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	c.n = 1
	if got, want := hex.EncodeToString(c.nonce()), "000000000000000000000001"; got != want {
		t.Fatalf("nonce(1) = %s, muốn %s", got, want)
	}
	c.n = 0x0102030405060708
	if got, want := hex.EncodeToString(c.nonce()), "000000000102030405060708"; got != want {
		t.Fatalf("nonce lớn = %s, muốn %s", got, want)
	}
}

// Bắt tay trọn vẹn initiator ↔ responder: chứng minh máy trạng thái tự nhất quán
// và hai chiều khoá truyền tin ghép đúng (send của bên này giải được bằng recv của bên kia).
func TestHandshakeRoundTrip(t *testing.T) {
	toResponder := make(chan []byte, 2)
	toInitiator := make(chan []byte, 2)
	done := make(chan *Transport, 1)
	errc := make(chan error, 1)

	go func() { // phía responder, cổng từ responderHandshake trong noise.ts
		ss := newSymmetricState()
		ss.mixHash(nil)
		s, _ := generateKeyPair()
		e, _ := generateKeyPair()
		m1 := <-toResponder
		re := m1[0:32]
		ss.mixHash(re)
		if _, err := ss.decryptAndHash(m1[32:]); err != nil {
			errc <- err
			return
		}
		ss.mixHash(e.pub)
		sec, _ := dh(e, re)
		if err := ss.mixKey(sec); err != nil {
			errc <- err
			return
		}
		encS, err := ss.encryptAndHash(s.pub)
		if err != nil {
			errc <- err
			return
		}
		sec2, _ := dh(s, re)
		if err := ss.mixKey(sec2); err != nil {
			errc <- err
			return
		}
		encP, err := ss.encryptAndHash([]byte("ok"))
		if err != nil {
			errc <- err
			return
		}
		toInitiator <- Concat(e.pub, encS, encP)
		m3 := <-toResponder
		rs, err := ss.decryptAndHash(m3[0:48])
		if err != nil {
			errc <- err
			return
		}
		sec3, _ := dh(e, rs)
		if err := ss.mixKey(sec3); err != nil {
			errc <- err
			return
		}
		if _, err := ss.decryptAndHash(m3[48:]); err != nil {
			errc <- err
			return
		}
		c1, c2, err := ss.split()
		if err != nil {
			errc <- err
			return
		}
		done <- &Transport{send: c2, recv: c1} // responder đảo chiều so với initiator
	}()

	tx, err := initiatorHandshake(handshakeIO{
		payload1: FieldBytes(1, make([]byte, 32)),
		exchange: func(m1 []byte) ([]byte, error) {
			toResponder <- m1
			select {
			case m2 := <-toInitiator:
				return m2, nil
			case e := <-errc:
				return nil, e
			}
		},
		deliver: func(m3 []byte) error { toResponder <- m3; return nil },
	})
	if err != nil {
		t.Fatalf("initiator: %v", err)
	}
	var rtx *Transport
	select {
	case rtx = <-done:
	case e := <-errc:
		t.Fatalf("responder: %v", e)
	}

	// initiator gửi → responder đọc được
	msg := []byte("khung du lieu thu nghiem")
	ct, err := tx.send.encrypt(nil, msg)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := rtx.recv.decrypt(nil, ct)
	if err != nil {
		t.Fatalf("responder không giải mã được: %v", err)
	}
	if !bytes.Equal(pt, msg) {
		t.Fatalf("giải mã ra %q", pt)
	}
	// và chiều ngược lại
	ct2, _ := rtx.send.encrypt(nil, []byte("tra loi"))
	pt2, err := tx.recv.decrypt(nil, ct2)
	if err != nil {
		t.Fatalf("initiator không giải mã được: %v", err)
	}
	if string(pt2) != "tra loi" {
		t.Fatalf("giải mã ra %q", pt2)
	}
}

func TestIsChallengePayload(t *testing.T) {
	c := Concat(FieldBytes(1, []byte("x")), FieldVarint(2, 1), FieldBytes(3, make([]byte, 32)))
	if !isChallengePayload(c) {
		t.Fatal("phải nhận ra gói challenge")
	}
	if isChallengePayload(FieldBytes(1, []byte("x"))) {
		t.Fatal("không được coi payload thường là challenge")
	}
	_ = mustHex
}
