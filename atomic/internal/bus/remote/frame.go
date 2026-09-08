// Package remote implements the sealed frame format that carries atomic
// bus traffic between a client and a gateway over an untrusted network. See
// docs/design/atomic-bus-network.md, "The frame".
package remote

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"
)

const frameVersion = 1

// nonceSize is the AES-256-GCM nonce length: 96 bits of randomness per
// frame on c2s, a big-endian sequence counter zero-padded on the left on
// s2c, so the wire nonce doubles as the frame's position in the stream.
const nonceSize = 12

// ErrOpen is the single failure value for every way a frame can fail to
// open. A caller receiving it cannot tell which check fired — that
// indistinguishability is what keeps admission failures from becoming an
// oracle for an attacker on the network path.
var ErrOpen = errors.New("remote: frame did not open")

// ErrKeyIDTooLong guards the wire format's single-byte length prefix: past
// 255 the length would truncate silently and seal a frame that can never
// open. That failure mode belongs to the caller at seal time, not to the
// network, so it stays distinct from ErrOpen rather than sharing it.
var ErrKeyIDTooLong = errors.New("remote: key id longer than 255 bytes")

// Header is the frame's fixed binary preamble. Its marshaled bytes are the
// AEAD's additional data verbatim: Open slices them straight out of the
// wire frame instead of reconstructing and re-marshaling a Header, so
// there is no path where the bytes that were verified differ from the
// bytes an attacker could have altered.
type Header struct {
	Ver       byte
	KeyID     []byte
	Timestamp int64
	Nonce     [nonceSize]byte
}

func marshalHeader(h Header) []byte {
	buf := make([]byte, 0, 2+len(h.KeyID)+8+nonceSize)
	buf = append(buf, h.Ver, byte(len(h.KeyID)))
	buf = append(buf, h.KeyID...)
	buf = binary.BigEndian.AppendUint64(buf, uint64(h.Timestamp))
	buf = append(buf, h.Nonce[:]...)
	return buf
}

// ParseHeader reads a Header off the front of frame and returns the exact
// byte span it occupied, for use as AAD. Exported for the gateway, which
// must read key_id before it can choose which key to open with.
func ParseHeader(frame []byte) (Header, []byte, error) {
	if len(frame) < 2 {
		return Header{}, nil, ErrOpen
	}
	ver, idLen := frame[0], int(frame[1])
	headerLen := 2 + idLen + 8 + nonceSize
	if ver != frameVersion || len(frame) < headerLen {
		return Header{}, nil, ErrOpen
	}

	h := Header{
		Ver:       ver,
		KeyID:     append([]byte(nil), frame[2:2+idLen]...),
		Timestamp: int64(binary.BigEndian.Uint64(frame[2+idLen : 2+idLen+8])),
	}
	copy(h.Nonce[:], frame[2+idLen+8:headerLen])
	return h, frame[:headerLen], nil
}

func c2sSubkey(key []byte) ([]byte, error) {
	return hkdf.Key(sha256.New, key, nil, "c2s", 32)
}

// per stream rather than per key: a per-key subkey would reuse a (key,
// nonce) pair across streams, and AES-GCM nonce reuse yields forgery. See
// docs/design/atomic-bus-network.md, "Key material".
func s2cSubkey(key []byte, requestNonce [nonceSize]byte) ([]byte, error) {
	info := "s2c" + string(requestNonce[:])
	return hkdf.Key(sha256.New, key, nil, info, 32)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// SealC2S seals plaintext under the c2s subkey with a fresh random nonce.
// It returns the wire frame and the nonce, which the caller keeps to
// derive this stream's s2c subkey for opening the response.
func SealC2S(key, keyID, plaintext []byte, now time.Time) ([]byte, [nonceSize]byte, error) {
	var nonce [nonceSize]byte
	if len(keyID) > 255 {
		return nil, nonce, ErrKeyIDTooLong
	}
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, nonce, err
	}

	subkey, err := c2sSubkey(key)
	if err != nil {
		return nil, nonce, err
	}
	gcm, err := newGCM(subkey)
	if err != nil {
		return nil, nonce, err
	}

	header := Header{Ver: frameVersion, KeyID: keyID, Timestamp: now.Unix(), Nonce: nonce}
	aad := marshalHeader(header)
	sealed := gcm.Seal(nil, nonce[:], plaintext, aad)
	return append(aad, sealed...), nonce, nil
}

// OpenC2S is responsible only for opening the frame; the caller owns the
// timestamp window.
func OpenC2S(key, frame []byte) ([]byte, Header, error) {
	header, aad, err := ParseHeader(frame)
	if err != nil {
		return nil, Header{}, ErrOpen
	}

	subkey, err := c2sSubkey(key)
	if err != nil {
		return nil, Header{}, ErrOpen
	}
	gcm, err := newGCM(subkey)
	if err != nil {
		return nil, Header{}, ErrOpen
	}

	plaintext, err := gcm.Open(nil, header.Nonce[:], frame[len(aad):], aad)
	if err != nil {
		return nil, Header{}, ErrOpen
	}
	return plaintext, header, nil
}

// SealS2C seals plaintext for one stream, identified by requestNonce, with
// seq as the wire nonce. Sequencing is the caller's: seq must be exactly
// one past the last sequence sealed for this stream.
func SealS2C(key, keyID []byte, requestNonce [nonceSize]byte, seq uint64, plaintext []byte, now time.Time) ([]byte, error) {
	if len(keyID) > 255 {
		return nil, ErrKeyIDTooLong
	}
	subkey, err := s2cSubkey(key, requestNonce)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(subkey)
	if err != nil {
		return nil, err
	}

	var nonce [nonceSize]byte
	binary.BigEndian.PutUint64(nonce[nonceSize-8:], seq)

	header := Header{Ver: frameVersion, KeyID: keyID, Timestamp: now.Unix(), Nonce: nonce}
	aad := marshalHeader(header)
	sealed := gcm.Seal(nil, nonce[:], plaintext, aad)
	return append(aad, sealed...), nil
}

// OpenS2C requires the frame's sequence counter to equal wantSeq exactly.
func OpenS2C(key []byte, requestNonce [nonceSize]byte, frame []byte, wantSeq uint64) ([]byte, Header, error) {
	header, aad, err := ParseHeader(frame)
	if err != nil {
		return nil, Header{}, ErrOpen
	}
	if binary.BigEndian.Uint64(header.Nonce[nonceSize-8:]) != wantSeq {
		return nil, Header{}, ErrOpen
	}
	if binary.BigEndian.Uint32(header.Nonce[:4]) != 0 {
		return nil, Header{}, ErrOpen
	}

	subkey, err := s2cSubkey(key, requestNonce)
	if err != nil {
		return nil, Header{}, ErrOpen
	}
	gcm, err := newGCM(subkey)
	if err != nil {
		return nil, Header{}, ErrOpen
	}

	plaintext, err := gcm.Open(nil, header.Nonce[:], frame[len(aad):], aad)
	if err != nil {
		return nil, Header{}, ErrOpen
	}
	return plaintext, header, nil
}
