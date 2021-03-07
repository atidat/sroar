package roar

import (
	"fmt"
	"strings"

	"github.com/dgraph-io/ristretto/z/simd"
)

// node stores uint64 keys and the corresponding container offset in the buffer.
// 0th index (keyOffset) is unused.
// 1st index (valOffset) is used for storing the number of keys.
type node []uint64

func (n node) uint64(start int) uint64 { return n[start] }

// func (n node) uint32(start int) uint32 { return *(*uint32)(unsafe.Pointer(&n[start])) }

func keyOffset(i int) int          { return 2 + 2*i }
func valOffset(i int) int          { return 3 + 2*i }
func (n node) numKeys() int        { return int(n.uint64(1)) }
func (n node) maxKeys() int        { return len(n)/2 - 1 }
func (n node) key(i int) uint64    { return n.uint64(keyOffset(i)) }
func (n node) val(i int) uint64    { return n.uint64(valOffset(i)) }
func (n node) data(i int) []uint64 { return n[keyOffset(i):keyOffset(i+1)] }

func (n node) setAt(start int, k uint64) {
	n[start] = k
}

func (n node) setNumKeys(num int) {
	// 1st index is used for storing the number of keys.
	n[1] = uint64(num)
}

func (n node) maxKey() uint64 {
	idx := n.numKeys()
	// numKeys == index of the max key, because 0th index is being used for meta information.
	if idx == 0 {
		return 0
	}
	return n.key(idx)
}

func (n node) moveRight(lo int) {
	hi := n.numKeys()
	assert(hi != n.maxKeys())
	// copy works despite of overlap in src and dst.
	// See https://golang.org/pkg/builtin/#copy
	copy(n[keyOffset(lo+1):keyOffset(hi+1)], n[keyOffset(lo):keyOffset(hi)])
}

// isFull checks that the node is already full.
func (n node) isFull() bool {
	return n.numKeys() == n.maxKeys()
}

// Search returns the index of a smallest key >= k in a node.
func (n node) search(k uint64) int {
	N := n.numKeys()
	if N < 4 {
		for i := 0; i < N; i++ {
			if ki := n.key(i); ki >= k {
				return i
			}
		}
		return N
	}
	fmt.Printf("Size N: %d n: %d Want: %d\n", N, len(n), 2*N)
	return int(simd.Search(n[2:2*N], k))
}

func zeroOut(data []uint64) {
	for i := 0; i < len(data); i++ {
		data[i] = 0
	}
}

// compacts the node i.e., remove all the kvs with value < lo. It returns the remaining number of
// keys.
func (n node) compact(lo uint64) int {
	N := n.numKeys()
	mk := n.maxKey()
	var left, right int
	for right = 0; right < N; right++ {
		if n.val(right) < lo && n.key(right) < mk {
			// Skip over this key. Don't copy it.
			continue
		}
		// Valid data. Copy it from right to left. Advance left.
		if left != right {
			copy(n.data(left), n.data(right))
		}
		left++
	}
	// zero out rest of the kv pairs.
	zeroOut(n[keyOffset(left):keyOffset(right)])
	n.setNumKeys(left)

	// If the only key we have is the max key, and its value is less than lo, then we can indicate
	// to the caller by returning a zero that it's OK to drop the node.
	if left == 1 && n.key(0) == mk && n.val(0) < lo {
		return 0
	}
	return left
}

// getValue returns the value corresponding to the key if found.
func (n node) getValue(k uint64) (uint64, bool) {
	idx := n.search(k)
	fmt.Printf("getValue.idx: %d. numKeys: %d\n", idx, n.numKeys())
	// key is not found
	if idx == n.numKeys() {
		return 0, false
	}
	if ki := n.key(idx); ki == k {
		fmt.Printf("ki: %d k: %d\n", ki, k)
		return n.val(idx), true
	} else {
		fmt.Printf("ki: %d k: %d\n", ki, k)
	}
	return 0, false
}

// set returns true if it added a new key.
func (n node) set(k, v uint64) (numAdded int) {
	fmt.Printf("set k: %d v: %d\n", k, v)
	idx := n.search(k)
	fmt.Printf("0 idx: %d for key: %d\n", idx, k)
	ki := n.key(idx)
	fmt.Printf("idx: %d for key: %d. ki: %d\n", idx, k, ki)
	if n.numKeys() == n.maxKeys() {
		// This happens during split of non-root node, when we are updating the child pointer of
		// right node. Hence, the key should already exist.
		assert(ki == k)
	}
	if ki > k {
		// Found the first entry which is greater than k. So, we need to fit k
		// just before it. For that, we should move the rest of the data in the
		// node to the right to make space for k.
		n.moveRight(idx)
	}
	// If the k does not exist already, increment the number of keys.
	if ki != k {
		n.setNumKeys(n.numKeys() + 1)
		numAdded = 1
	}
	if ki == 0 || ki >= k {
		fmt.Printf("len n: %d\n", len(n))
		n.setAt(keyOffset(idx), k)
		n.setAt(valOffset(idx), v)
		return
	}
	panic("shouldn't reach here")
}

func (n node) iterate(fn func(node, int)) {
	for i := 0; i < n.maxKeys(); i++ {
		if k := n.key(i); k > 0 {
			fn(n, i)
		} else {
			break
		}
	}
}

func (n node) print(parentID uint64) {
	var keys []string
	n.iterate(func(n node, i int) {
		keys = append(keys, fmt.Sprintf("%d", n.key(i)))
	})
	if len(keys) > 8 {
		copy(keys[4:], keys[len(keys)-4:])
		keys[3] = "..."
		keys = keys[:8]
	}
	fmt.Printf("num keys: %d keys: %s\n", n.numKeys(), strings.Join(keys, " "))
}