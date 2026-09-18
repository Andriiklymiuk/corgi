package sessions

type Board struct {
	Size     int      `json:"size"`
	Slots    []string `json:"slots"`
	Pinned   []bool   `json:"pinned"`
	Overflow []string `json:"overflow,omitempty"`
}

const DefaultSize = 6

func NewBoard(size int) Board {
	if size <= 0 {
		size = DefaultSize
	}
	return Board{Size: size, Slots: make([]string, size), Pinned: make([]bool, size)}
}

func (b *Board) Resize(size int) {
	if size <= 0 || size == b.Size {
		return
	}
	slots := make([]string, size)
	pinned := make([]bool, size)
	var spilled []string
	for i, id := range b.Slots {
		if i < size {
			slots[i], pinned[i] = id, b.Pinned[i]
		} else if id != "" {
			spilled = append(spilled, id)
		}
	}
	b.Size, b.Slots, b.Pinned = size, slots, pinned
	b.Overflow = append(spilled, b.Overflow...)
	b.fill()
}

func (b *Board) IndexOf(id string) int {
	for i, s := range b.Slots {
		if s == id && id != "" {
			return i
		}
	}
	return -1
}

func (b *Board) Has(id string) bool {
	if b.IndexOf(id) >= 0 {
		return true
	}
	for _, o := range b.Overflow {
		if o == id {
			return true
		}
	}
	return false
}

func (b *Board) Place(id string) {
	if id == "" || b.Has(id) {
		return
	}
	for i, s := range b.Slots {
		if s == "" && !b.Pinned[i] {
			b.Slots[i] = id
			return
		}
	}
	b.Overflow = append(b.Overflow, id)
}

func (b *Board) Remove(id string) (kept bool) {
	if i := b.IndexOf(id); i >= 0 {
		if b.Pinned[i] {
			return true
		}
		b.Slots[i] = ""
		b.fill()
		return false
	}
	b.Overflow = without(b.Overflow, id)
	return false
}

func (b *Board) Rename(old, id string) {
	if i := b.IndexOf(old); i >= 0 {
		b.Slots[i] = id
		return
	}
	for i, o := range b.Overflow {
		if o == old {
			b.Overflow[i] = id
		}
	}
}

func (b *Board) Pin(index int, on bool) bool {
	if index < 0 || index >= b.Size {
		return false
	}
	if on && b.Slots[index] == "" {
		return false
	}
	b.Pinned[index] = on
	return true
}

func (b *Board) IsPinned(id string) bool {
	i := b.IndexOf(id)
	return i >= 0 && b.Pinned[i]
}

func (b *Board) Free(index int) {
	if index < 0 || index >= b.Size {
		return
	}
	b.Slots[index] = ""
	b.Pinned[index] = false
	b.fill()
}

func (b *Board) PagerIndex() int {
	if len(b.Overflow) == 0 {
		return -1
	}
	for i := b.Size - 1; i >= 0; i-- {
		if !b.Pinned[i] && b.Slots[i] != "" {
			return i
		}
	}
	return -1
}

func (b *Board) Hidden() int {
	if b.PagerIndex() < 0 {
		return len(b.Overflow)
	}
	return len(b.Overflow) + 1
}

func (b *Board) Page(direction int) bool {
	if len(b.Overflow) == 0 || direction == 0 {
		return false
	}
	var indexes []int
	for i, s := range b.Slots {
		if !b.Pinned[i] && s != "" {
			indexes = append(indexes, i)
		}
	}
	visible := len(indexes) - 1
	if visible < 1 {
		return false
	}
	ring := make([]string, 0, len(indexes)+len(b.Overflow))
	for _, i := range indexes {
		ring = append(ring, b.Slots[i])
	}
	ring = append(ring, b.Overflow...)
	shift := visible % len(ring)
	if direction < 0 {
		shift = len(ring) - shift
	}
	ring = append(ring[shift:], ring[:shift]...)
	for n, i := range indexes {
		b.Slots[i] = ring[n]
	}
	b.Overflow = append([]string(nil), ring[len(indexes):]...)
	return true
}

func (b *Board) fill() {
	for i, s := range b.Slots {
		if len(b.Overflow) == 0 {
			return
		}
		if s == "" && !b.Pinned[i] {
			b.Slots[i] = b.Overflow[0]
			b.Overflow = b.Overflow[1:]
		}
	}
}

func without(list []string, id string) []string {
	out := list[:0]
	for _, s := range list {
		if s != id {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
