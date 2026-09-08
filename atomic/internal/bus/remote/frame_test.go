package remote

import (
	"bytes"
	"crypto/rand"
	"strconv"
	"testing"
	"time"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return key
}

func TestSealOpen_RoundTrip_C2S(t *testing.T) {
	key := testKey(t)
	keyID := []byte("k1")
	plaintext := []byte(`{"op":"send"}`)

	frame, nonce, err := SealC2S(key, keyID, plaintext, time.Now())
	if err != nil {
		t.Fatalf("SealC2S: %v", err)
	}

	got, header, err := OpenC2S(key, frame)
	if err != nil {
		t.Fatalf("OpenC2S: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("plaintext mismatch: got %q want %q", got, plaintext)
	}
	if header.Nonce != nonce {
		t.Fatalf("header nonce mismatch: got %x want %x", header.Nonce, nonce)
	}
}

func TestSealOpen_RoundTrip_S2C(t *testing.T) {
	key := testKey(t)
	keyID := []byte("k1")
	_, requestNonce, err := SealC2S(key, keyID, []byte("req"), time.Now())
	if err != nil {
		t.Fatalf("SealC2S: %v", err)
	}

	plaintext := []byte(`{"envelope":"hi"}`)
	frame, err := SealS2C(key, keyID, requestNonce, 0, plaintext, time.Now())
	if err != nil {
		t.Fatalf("SealS2C: %v", err)
	}

	got, _, err := OpenS2C(key, requestNonce, frame, 0)
	if err != nil {
		t.Fatalf("OpenS2C: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("plaintext mismatch: got %q want %q", got, plaintext)
	}
}

func TestOpenC2S_AnyAlteredByte_FailsToOpen(t *testing.T) {
	key := testKey(t)
	frame, _, err := SealC2S(key, []byte("k1"), []byte("payload"), time.Now())
	if err != nil {
		t.Fatalf("SealC2S: %v", err)
	}

	for i := range frame {
		t.Run("byte "+strconv.Itoa(i), func(t *testing.T) {
			tampered := make([]byte, len(frame))
			copy(tampered, frame)
			tampered[i] ^= 0xFF

			if _, _, err := OpenC2S(key, tampered); err != ErrOpen {
				t.Fatalf("altering byte %d: expected ErrOpen, got %v", i, err)
			}
		})
	}
}

func TestOpenS2C_AnyAlteredByte_FailsToOpen(t *testing.T) {
	key := testKey(t)
	keyID := []byte("k1")
	_, requestNonce, err := SealC2S(key, keyID, []byte("req"), time.Now())
	if err != nil {
		t.Fatalf("SealC2S: %v", err)
	}
	frame, err := SealS2C(key, keyID, requestNonce, 0, []byte("envelope"), time.Now())
	if err != nil {
		t.Fatalf("SealS2C: %v", err)
	}

	for i := range frame {
		t.Run("byte "+strconv.Itoa(i), func(t *testing.T) {
			tampered := make([]byte, len(frame))
			copy(tampered, frame)
			tampered[i] ^= 0xFF

			if _, _, err := OpenS2C(key, requestNonce, tampered, 0); err != ErrOpen {
				t.Fatalf("altering byte %d: expected ErrOpen, got %v", i, err)
			}
		})
	}
}

func TestOpenC2S_WrongKey_Fails(t *testing.T) {
	key := testKey(t)
	wrongKey := testKey(t)
	frame, _, err := SealC2S(key, []byte("k1"), []byte("payload"), time.Now())
	if err != nil {
		t.Fatalf("SealC2S: %v", err)
	}

	if _, _, err := OpenC2S(wrongKey, frame); err != ErrOpen {
		t.Fatalf("expected ErrOpen with wrong key, got %v", err)
	}
}

func TestOpenS2C_CrossStreamFails(t *testing.T) {
	key := testKey(t)
	keyID := []byte("k1")

	_, nonceA, err := SealC2S(key, keyID, []byte("req-a"), time.Now())
	if err != nil {
		t.Fatalf("SealC2S A: %v", err)
	}
	_, nonceB, err := SealC2S(key, keyID, []byte("req-b"), time.Now())
	if err != nil {
		t.Fatalf("SealC2S B: %v", err)
	}

	frame, err := SealS2C(key, keyID, nonceA, 0, []byte("for stream A"), time.Now())
	if err != nil {
		t.Fatalf("SealS2C: %v", err)
	}

	if _, _, err := OpenS2C(key, nonceB, frame, 0); err != ErrOpen {
		t.Fatalf("expected ErrOpen opening stream A's frame against stream B, got %v", err)
	}
}

func TestOpenS2C_WholeStreamReplayFails(t *testing.T) {
	key := testKey(t)
	keyID := []byte("k1")

	_, oldNonce, err := SealC2S(key, keyID, []byte("req-old"), time.Now())
	if err != nil {
		t.Fatalf("SealC2S old: %v", err)
	}

	var captured [][]byte
	for seq := uint64(0); seq < 3; seq++ {
		frame, err := SealS2C(key, keyID, oldNonce, seq, []byte("envelope"), time.Now())
		if err != nil {
			t.Fatalf("SealS2C seq %d: %v", seq, err)
		}
		captured = append(captured, frame)
	}

	_, newNonce, err := SealC2S(key, keyID, []byte("req-new"), time.Now())
	if err != nil {
		t.Fatalf("SealC2S new: %v", err)
	}

	for seq, frame := range captured {
		if _, _, err := OpenS2C(key, newNonce, frame, uint64(seq)); err != ErrOpen {
			t.Fatalf("frame %d from captured stream opened against a reconnecting client's new nonce", seq)
		}
	}
}

func TestOpenS2C_SequenceHandling(t *testing.T) {
	key := testKey(t)
	keyID := []byte("k1")
	_, requestNonce, err := SealC2S(key, keyID, []byte("req"), time.Now())
	if err != nil {
		t.Fatalf("SealC2S: %v", err)
	}

	seal := func(seq uint64) []byte {
		frame, err := SealS2C(key, keyID, requestNonce, seq, []byte("envelope"), time.Now())
		if err != nil {
			t.Fatalf("SealS2C seq %d: %v", seq, err)
		}
		return frame
	}

	cases := []struct {
		name string
		seal uint64
		want uint64
	}{
		{"first counter nonzero", 1, 0},
		{"repeat (behind)", 0, 1},
		{"gap or reorder (ahead)", 2, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame := seal(tc.seal)
			if _, _, err := OpenS2C(key, requestNonce, frame, tc.want); err != ErrOpen {
				t.Fatalf("expected ErrOpen for seal %d against want %d, got %v", tc.seal, tc.want, err)
			}
		})
	}

	t.Run("in-order sequence is accepted", func(t *testing.T) {
		for seq := uint64(0); seq < 3; seq++ {
			frame := seal(seq)
			if _, _, err := OpenS2C(key, requestNonce, frame, seq); err != nil {
				t.Fatalf("expected seq %d to open, got %v", seq, err)
			}
		}
	})
}

func TestS2CSubkey_IsDeterministic(t *testing.T) {
	key := testKey(t)
	var nonce [nonceSize]byte
	nonce[nonceSize-1] = 1

	subkey1, err := s2cSubkey(key, nonce)
	if err != nil {
		t.Fatalf("s2cSubkey: %v", err)
	}
	subkey2, err := s2cSubkey(key, nonce)
	if err != nil {
		t.Fatalf("s2cSubkey repeat: %v", err)
	}
	if !bytes.Equal(subkey1, subkey2) {
		t.Fatal("s2cSubkey must be deterministic: same key and nonce produced different subkeys")
	}
}

// Every nonce byte must reach the subkey.
func TestS2CSubkey_EveryNonceByteAffectsSubkey(t *testing.T) {
	key := testKey(t)
	var base [nonceSize]byte

	baseline, err := s2cSubkey(key, base)
	if err != nil {
		t.Fatalf("s2cSubkey baseline: %v", err)
	}

	for i := 0; i < nonceSize; i++ {
		t.Run("byte "+strconv.Itoa(i), func(t *testing.T) {
			flipped := base
			flipped[i] ^= 0xFF

			subkey, err := s2cSubkey(key, flipped)
			if err != nil {
				t.Fatalf("s2cSubkey: %v", err)
			}
			if bytes.Equal(subkey, baseline) {
				t.Fatalf("flipping nonce byte %d did not change the subkey", i)
			}
		})
	}
}

func TestOpenC2S_IgnoresTimestamp(t *testing.T) {
	key := testKey(t)
	staleFrame, _, err := SealC2S(key, []byte("k1"), []byte("stale"), time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("SealC2S stale: %v", err)
	}

	if _, _, err := OpenC2S(key, staleFrame); err != nil {
		t.Fatalf("Open must not itself reject on timestamp, got %v", err)
	}
}

func TestOpenFailures_ReturnSameOpaqueError(t *testing.T) {
	key := testKey(t)
	wrongKey := testKey(t)
	keyID := []byte("k1")
	frame, requestNonce, err := SealC2S(key, keyID, []byte("payload"), time.Now())
	if err != nil {
		t.Fatalf("SealC2S: %v", err)
	}

	tamperedBody := make([]byte, len(frame))
	copy(tamperedBody, frame)
	tamperedBody[len(tamperedBody)-1] ^= 0xFF

	_, otherNonce, err := SealC2S(key, keyID, []byte("other"), time.Now())
	if err != nil {
		t.Fatalf("SealC2S other: %v", err)
	}
	s2cFrame, err := SealS2C(key, keyID, requestNonce, 0, []byte("envelope"), time.Now())
	if err != nil {
		t.Fatalf("SealS2C: %v", err)
	}

	cases := []struct {
		name string
		open func() error
	}{
		{"c2s tampered body", func() error {
			_, _, err := OpenC2S(key, tamperedBody)
			return err
		}},
		{"c2s wrong key", func() error {
			_, _, err := OpenC2S(wrongKey, frame)
			return err
		}},
		{"c2s truncated frame", func() error {
			_, _, err := OpenC2S(key, frame[:len(frame)-1])
			return err
		}},
		{"s2c cross-stream", func() error {
			_, _, err := OpenS2C(key, otherNonce, s2cFrame, 0)
			return err
		}},
		{"s2c wrong key", func() error {
			_, _, err := OpenS2C(wrongKey, requestNonce, s2cFrame, 0)
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.open(); err != ErrOpen {
				t.Fatalf("expected the single ErrOpen value, got %v", err)
			}
		})
	}
}

func TestSealC2S_KeyIDTooLong_Rejected(t *testing.T) {
	key := testKey(t)
	keyID := make([]byte, 256)

	if _, _, err := SealC2S(key, keyID, []byte("payload"), time.Now()); err != ErrKeyIDTooLong {
		t.Fatalf("expected ErrKeyIDTooLong, got %v", err)
	}
}

func TestSealS2C_KeyIDTooLong_Rejected(t *testing.T) {
	key := testKey(t)
	keyID := make([]byte, 256)
	var requestNonce [nonceSize]byte

	if _, err := SealS2C(key, keyID, requestNonce, 0, []byte("payload"), time.Now()); err != ErrKeyIDTooLong {
		t.Fatalf("expected ErrKeyIDTooLong, got %v", err)
	}
}

func TestSealOpen_RoundTrip_MaxKeyIDLength(t *testing.T) {
	key := testKey(t)
	keyID := make([]byte, 255)
	plaintext := []byte("payload")

	frame, _, err := SealC2S(key, keyID, plaintext, time.Now())
	if err != nil {
		t.Fatalf("SealC2S: %v", err)
	}

	got, _, err := OpenC2S(key, frame)
	if err != nil {
		t.Fatalf("OpenC2S: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("plaintext mismatch: got %q want %q", got, plaintext)
	}
}

func TestParseHeader_KeyIDDoesNotAliasFrame(t *testing.T) {
	key := testKey(t)
	frame, _, err := SealC2S(key, []byte("k1"), []byte("payload"), time.Now())
	if err != nil {
		t.Fatalf("SealC2S: %v", err)
	}

	header, _, err := ParseHeader(frame)
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}

	want := append([]byte(nil), header.KeyID...)
	for i := range frame {
		frame[i] = 0
	}
	if !bytes.Equal(header.KeyID, want) {
		t.Fatalf("KeyID aliases the frame buffer: got %q after zeroing frame, want %q", header.KeyID, want)
	}
}
