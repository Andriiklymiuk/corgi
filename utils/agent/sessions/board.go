package sessions

// Board is the fixed array of keys. Slot assignment lives here, not in the
// plugin: a plugin restarts whenever the Stream Deck app does, and a board
// that reshuffled on every restart would be a board nobody could learn.
//
// Rules, in priority order:
//  1. A pinned slot never moves.
//  2. A new session takes the lowest free index. Nothing is ever re-sorted.
//  3. A session that ends frees its index, unless pinned, in which case the
//     slot keeps the id and the session reads "gone" until unpinned.
//  4. A free slot with sessions in overflow takes the oldest overflow entry.
//  5. With overflow, the highest unpinned slot becomes the pager. Its own
//     session is hidden behind the "+N", and paging rotates the unpinned
//     slots through the overflow.
type Board struct {
	Size     int      `json:"size"`
	Slots    []string `json:"slots"`
	Pinned   []bool   `json:"pinned"`
	Overflow []string `json:"overflow,omitempty"`
}

// DefaultSize is a Stream Deck Mini.
const DefaultSize = 6

// NewBoard returns an empty board of size keys.
func NewBoard(size int) Board {
	if size <= 0 {
		size = DefaultSize
	}
	return Board{Size: size, Slots: make([]string, size), Pinned: make([]bool, size)}
}

// Resize keeps every assignment that still fits and moves the rest to the
// front of the overflow, so shrinking the board loses no session.
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

// IndexOf is the slot holding id, or -1.
func (b *Board) IndexOf(id string) int {
	for i, s := range b.Slots {
		if s == id && id != "" {
			return i
		}
	}
	return -1
}

// Has reports whether id is on the board or in its overflow.
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

// Place seats a new session: lowest free slot, else the end of the overflow.
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

// Remove frees a session's seat. A pinned slot keeps the id (the caller marks
// the session gone) and reports true, so the session record must survive.
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

// Rename swaps one id for another in place, for a rescan placeholder that
// just learned its real session id.
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

// Pin reserves a slot for whatever it holds. Unpinning a slot whose session
// is gone frees it; the caller drops the session. Returns false for an index
// off the board or an empty slot (there is nothing to keep).
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

// Pinned reports whether id sits in a pinned slot.
func (b *Board) IsPinned(id string) bool {
	i := b.IndexOf(id)
	return i >= 0 && b.Pinned[i]
}

// Free empties a slot outright, pinned or not, and refills it from overflow.
func (b *Board) Free(index int) {
	if index < 0 || index >= b.Size {
		return
	}
	b.Slots[index] = ""
	b.Pinned[index] = false
	b.fill()
}

// PagerIndex is the slot showing "+N", or -1 when nothing overflows or every
// slot is pinned.
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

// Hidden counts the sessions a pager stands for: the overflow plus the one
// behind the pager key itself.
func (b *Board) Hidden() int {
	if b.PagerIndex() < 0 {
		return len(b.Overflow)
	}
	return len(b.Overflow) + 1
}

// Page rotates the unpinned slots through the overflow: +1 shows the next
// page, -1 the previous. The pager key's own hidden session leads the next
// page, so nothing is skipped.
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

// fill promotes overflow into free unpinned slots, oldest first.
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
