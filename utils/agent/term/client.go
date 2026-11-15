package term

import (
	"io"
	"sync"
)

func Attach(wire Wire, box *Box, stdin io.Reader, stdout io.Writer, sizes <-chan [2]uint16) (int, error) {
	var mu sync.Mutex
	send := func(p []byte) error {
		mu.Lock()
		defer mu.Unlock()
		return wire.WriteMessage(opBinary, box.Seal(p))
	}
	go func() {
		for sz := range sizes {
			if send(Resize(sz[0], sz[1])) != nil {
				return
			}
		}
	}()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdin.Read(buf)
			if n > 0 && send(Data(buf[:n])) != nil {
				return
			}
			if err != nil {
				return
			}
		}
	}()
	for {
		_, ct, err := wire.ReadMessage()
		if err != nil {
			return -1, err
		}
		m, err := box.Open(ct)
		if err != nil {
			return -1, err
		}
		if len(m) == 0 {
			continue
		}
		switch m[0] {
		case KindData:
			if _, err := stdout.Write(m[1:]); err != nil {
				return -1, err
			}
		case KindExit:
			code, _ := ParseExit(m)
			return code, nil
		}
	}
}
