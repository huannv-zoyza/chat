package muse

// Noise_XX_25519_AESGCM_SHA256 — vai trò initiator, đủ để bắt tay với gateway của Muse.
// Cổng từ desktop/src/shared/gateway/noise.ts. Chỉ dùng thư viện chuẩn: crypto/ecdh có sẵn X25519 từ Go 1.20.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

const protocolName = "Noise_XX_25519_AESGCM_SHA256"

// HandshakeError nói rõ bắt tay hỏng ở bước nào để tầng trên phân loại lỗi.
type HandshakeError struct {
	Step string // "message2" | "challenge" | "crypto"
	Msg  string
}

func (e *HandshakeError) Error() string { return fmt.Sprintf("noise %s: %s", e.Step, e.Msg) }

func sha256sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

func hmac256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}

// hkdf sinh n khoá liên tiếp theo đúng cách Noise dùng HMAC.
func hkdf(chainingKey, ikm []byte, n int) [][]byte {
	temp := hmac256(chainingKey, ikm)
	out := make([][]byte, 0, n)
	var prev []byte
	for i := 1; i <= n; i++ {
		prev = hmac256(temp, append(append([]byte{}, prev...), byte(i)))
		out = append(out, prev)
	}
	return out
}

// cipherState giữ khoá đối xứng và bộ đếm nonce của một chiều.
type cipherState struct {
	k    []byte
	n    uint64
	aead cipher.AEAD
}

func (c *cipherState) init(k []byte) error {
	c.k, c.n, c.aead = k, 0, nil
	if k == nil {
		return nil
	}
	blk, err := aes.NewCipher(k)
	if err != nil {
		return err
	}
	c.aead, err = cipher.NewGCM(blk)
	return err
}

func (c *cipherState) hasKey() bool { return c.k != nil }

// nonce: 12 byte, 4 byte đầu là 0, 8 byte sau là bộ đếm big-endian (đúng chuẩn Noise AESGCM).
func (c *cipherState) nonce() []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint64(b[4:], c.n)
	return b
}

func (c *cipherState) encrypt(ad, plaintext []byte) ([]byte, error) {
	if !c.hasKey() {
		return plaintext, nil
	}
	ct := c.aead.Seal(nil, c.nonce(), plaintext, ad)
	c.n++
	return ct, nil
}

func (c *cipherState) decrypt(ad, ciphertext []byte) ([]byte, error) {
	if !c.hasKey() {
		return ciphertext, nil
	}
	pt, err := c.aead.Open(nil, c.nonce(), ciphertext, ad)
	if err != nil {
		return nil, err
	}
	c.n++
	return pt, nil
}

type symmetricState struct {
	h  []byte
	ck []byte
	cs cipherState
}

func newSymmetricState() *symmetricState {
	name := make([]byte, 32) // tên giao thức ngắn hơn 32 byte thì đệm số 0, không băm
	copy(name, protocolName)
	return &symmetricState{h: append([]byte{}, name...), ck: append([]byte{}, name...)}
}

func (s *symmetricState) mixHash(data []byte) { s.h = sha256sum(Concat(s.h, data)) }

func (s *symmetricState) mixKey(ikm []byte) error {
	out := hkdf(s.ck, ikm, 2)
	s.ck = out[0]
	return s.cs.init(out[1][:32])
}

func (s *symmetricState) encryptAndHash(pt []byte) ([]byte, error) {
	ct, err := s.cs.encrypt(s.h, pt)
	if err != nil {
		return nil, err
	}
	s.mixHash(ct)
	return ct, nil
}

func (s *symmetricState) decryptAndHash(ct []byte) ([]byte, error) {
	pt, err := s.cs.decrypt(s.h, ct)
	if err != nil {
		return nil, err
	}
	s.mixHash(ct)
	return pt, nil
}

// split sinh hai bộ khoá truyền tin: bên initiator thì khoá đầu để gửi, khoá sau để nhận.
func (s *symmetricState) split() (*cipherState, *cipherState, error) {
	out := hkdf(s.ck, nil, 2)
	send, recv := &cipherState{}, &cipherState{}
	if err := send.init(out[0][:32]); err != nil {
		return nil, nil, err
	}
	if err := recv.init(out[1][:32]); err != nil {
		return nil, nil, err
	}
	return send, recv, nil
}

type keyPair struct {
	priv *ecdh.PrivateKey
	pub  []byte
}

func generateKeyPair() (*keyPair, error) {
	p, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &keyPair{priv: p, pub: p.PublicKey().Bytes()}, nil
}

func dh(k *keyPair, publicRaw []byte) ([]byte, error) {
	pub, err := ecdh.X25519().NewPublicKey(publicRaw)
	if err != nil {
		return nil, err
	}
	return k.priv.ECDH(pub)
}

// Transport là hai chiều mã hoá sau khi bắt tay xong.
type Transport struct {
	send *cipherState
	recv *cipherState
}

// handshakeIO tách phần mạng ra khỏi phần mật mã, để test được mà không cần socket thật.
type handshakeIO struct {
	exchange    func(msg1 []byte) ([]byte, error) // gửi message 1, chờ message 2
	deliver     func(msg3 []byte) error           // gửi message 3
	payload1    []byte
	isChallenge func(payload2 []byte) bool
}

// initiatorHandshake chạy vai initiator của Noise XX: -> e | <- e, ee, s, es | -> s, se
func initiatorHandshake(io handshakeIO) (*Transport, error) {
	ss := newSymmetricState()
	ss.mixHash(nil) // prologue rỗng
	s, err := generateKeyPair()
	if err != nil {
		return nil, err
	}
	e, err := generateKeyPair()
	if err != nil {
		return nil, err
	}
	ss.mixHash(e.pub)
	enc1, err := ss.encryptAndHash(io.payload1)
	if err != nil {
		return nil, err
	}
	m2, err := io.exchange(Concat(e.pub, enc1))
	if err != nil {
		return nil, err
	}
	if len(m2) < 80 {
		return nil, &HandshakeError{Step: "message2", Msg: "message 2 quá ngắn"}
	}
	re := m2[0:32]
	ss.mixHash(re)
	sec, err := dh(e, re)
	if err != nil {
		return nil, &HandshakeError{Step: "crypto", Msg: err.Error()}
	}
	if err := ss.mixKey(sec); err != nil {
		return nil, &HandshakeError{Step: "crypto", Msg: err.Error()}
	}
	rs, err := ss.decryptAndHash(m2[32:80])
	if err != nil {
		return nil, &HandshakeError{Step: "crypto", Msg: "giải mã message 2 hỏng: " + err.Error()}
	}
	sec2, err := dh(e, rs)
	if err != nil {
		return nil, &HandshakeError{Step: "crypto", Msg: err.Error()}
	}
	if err := ss.mixKey(sec2); err != nil {
		return nil, &HandshakeError{Step: "crypto", Msg: err.Error()}
	}
	payload2, err := ss.decryptAndHash(m2[80:])
	if err != nil {
		return nil, &HandshakeError{Step: "crypto", Msg: "giải mã payload 2 hỏng: " + err.Error()}
	}
	if io.isChallenge != nil && io.isChallenge(payload2) {
		return nil, &HandshakeError{Step: "challenge", Msg: "gateway đòi xác minh thêm"}
	}
	out3, err := ss.encryptAndHash(s.pub)
	if err != nil {
		return nil, err
	}
	sec3, err := dh(s, re)
	if err != nil {
		return nil, &HandshakeError{Step: "crypto", Msg: err.Error()}
	}
	if err := ss.mixKey(sec3); err != nil {
		return nil, &HandshakeError{Step: "crypto", Msg: err.Error()}
	}
	tail, err := ss.encryptAndHash(nil)
	if err != nil {
		return nil, err
	}
	if err := io.deliver(Concat(out3, tail)); err != nil {
		return nil, err
	}
	send, recv, err := ss.split()
	if err != nil {
		return nil, err
	}
	return &Transport{send: send, recv: recv}, nil
}

// isChallengePayload nhận diện gói "challenge" mà gateway gửi khi đòi xác minh thêm.
func isChallengePayload(p2 []byte) bool {
	f, err := DecodeFields(p2)
	if err != nil {
		return false
	}
	kind := f.Num(2, 0)
	c := f.Bytes(3)
	return f.Has(1) && (kind == 1 || kind == 2) && len(c) == 32
}

var errClosed = errors.New("muse: kết nối đã đóng")
