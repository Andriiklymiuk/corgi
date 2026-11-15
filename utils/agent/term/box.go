package term

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

const (
	dirServer = 'S'
	dirClient = 'C'
)

type Box struct {
	aead    cipher.AEAD
	send    byte
	recv    byte
	sendCtr uint64
	recvCtr uint64
}

func NewBox(shared []byte, ticket string, server bool) (*Box, error) {
	if len(shared) < 16 || ticket == "" {
		return nil, errors.New("terminal keys need the pairing key and a ticket")
	}
	key, err := hkdf.Key(sha256.New, shared, []byte(ticket), "corgi-term-v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	b := &Box{aead: aead, send: dirClient, recv: dirServer}
	if server {
		b.send, b.recv = dirServer, dirClient
	}
	return b, nil
}

func nonce(dir byte, n uint64) []byte {
	out := make([]byte, 12)
	out[0] = dir
	binary.BigEndian.PutUint64(out[4:], n)
	return out
}

func (b *Box) Seal(plain []byte) []byte {
	ct := b.aead.Seal(nil, nonce(b.send, b.sendCtr), plain, nil)
	b.sendCtr++
	return ct
}

var ErrTampered = errors.New("a terminal frame did not check out")

func (b *Box) Open(ct []byte) ([]byte, error) {
	plain, err := b.aead.Open(nil, nonce(b.recv, b.recvCtr), ct, nil)
	if err != nil {
		return nil, ErrTampered
	}
	b.recvCtr++
	return plain, nil
}

const (
	KindData   byte = 0
	KindResize byte = 1
	KindExit   byte = 2
	KindPing   byte = 3
)

func Data(p []byte) []byte { return append([]byte{KindData}, p...) }

func Resize(cols, rows uint16) []byte {
	out := []byte{KindResize, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(out[1:], cols)
	binary.BigEndian.PutUint16(out[3:], rows)
	return out
}

func Exit(code int) []byte {
	out := []byte{KindExit, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(out[1:], uint32(int32(code)))
	return out
}

func ParseResize(m []byte) (cols, rows uint16, ok bool) {
	if len(m) != 5 || m[0] != KindResize {
		return 0, 0, false
	}
	return binary.BigEndian.Uint16(m[1:]), binary.BigEndian.Uint16(m[3:]), true
}

func ParseExit(m []byte) (int, bool) {
	if len(m) != 5 || m[0] != KindExit {
		return 0, false
	}
	return int(int32(binary.BigEndian.Uint32(m[1:]))), true
}
