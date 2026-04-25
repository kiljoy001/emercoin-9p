package emercoin9p

import (
	"crypto/sha256"
	"errors"
)

const BitcoinHashSize = sha256.Size

var (
	ErrEmptyMerkleTree  = errors.New("merkle tree requires at least one leaf")
	ErrInvalidLeafHash  = errors.New("merkle leaf hash must be 32 bytes")
	ErrInvalidLeafIndex = errors.New("merkle proof index out of range")
)

// BitcoinMerkleTree stores a Bitcoin-style Merkle tree whose leaves are
// already-hashed 32-byte values. Internal nodes are double-SHA256 of the
// concatenated children, and odd nodes are duplicated on each level.
type BitcoinMerkleTree struct {
	levels [][][]byte
}

func NewBitcoinMerkleTree(leafHashes [][]byte) (*BitcoinMerkleTree, error) {
	if len(leafHashes) == 0 {
		return nil, ErrEmptyMerkleTree
	}

	current := make([][]byte, len(leafHashes))
	for i, leaf := range leafHashes {
		if len(leaf) != BitcoinHashSize {
			return nil, ErrInvalidLeafHash
		}
		current[i] = cloneBytes(leaf)
	}

	levels := [][][]byte{current}
	for len(current) > 1 {
		next := make([][]byte, 0, (len(current)+1)/2)
		for i := 0; i < len(current); i += 2 {
			left := current[i]
			right := left
			if i+1 < len(current) {
				right = current[i+1]
			}
			next = append(next, hashMerklePair(left, right))
		}
		levels = append(levels, next)
		current = next
	}

	return &BitcoinMerkleTree{levels: levels}, nil
}

func (t *BitcoinMerkleTree) Root() []byte {
	if t == nil || len(t.levels) == 0 {
		return nil
	}
	top := t.levels[len(t.levels)-1]
	if len(top) == 0 {
		return nil
	}
	return cloneBytes(top[0])
}

func (t *BitcoinMerkleTree) Proof(index int) ([][]byte, error) {
	if t == nil || len(t.levels) == 0 || index < 0 || index >= len(t.levels[0]) {
		return nil, ErrInvalidLeafIndex
	}

	proof := make([][]byte, 0, len(t.levels)-1)
	position := index

	for level := 0; level < len(t.levels)-1; level++ {
		nodes := t.levels[level]
		sibling := position ^ 1
		if sibling >= len(nodes) {
			sibling = position
		}
		proof = append(proof, cloneBytes(nodes[sibling]))
		position /= 2
	}

	return proof, nil
}

func VerifyBitcoinMerkleProof(leafHash []byte, index int, proof [][]byte, root []byte) bool {
	if len(leafHash) != BitcoinHashSize || len(root) != BitcoinHashSize || index < 0 {
		return false
	}

	current := cloneBytes(leafHash)
	position := index

	for _, sibling := range proof {
		if len(sibling) != BitcoinHashSize {
			return false
		}
		if position%2 == 0 {
			current = hashMerklePair(current, sibling)
		} else {
			current = hashMerklePair(sibling, current)
		}
		position /= 2
	}

	return equalBytes(current, root)
}

func DoubleSHA256(data []byte) []byte {
	first := sha256.Sum256(data)
	second := sha256.Sum256(first[:])
	return second[:]
}

func hashMerklePair(left, right []byte) []byte {
	buf := make([]byte, 0, len(left)+len(right))
	buf = append(buf, left...)
	buf = append(buf, right...)
	return DoubleSHA256(buf)
}

func cloneBytes(src []byte) []byte {
	if src == nil {
		return nil
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
