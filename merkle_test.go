package emercoin9p

import (
	"crypto/sha256"
	"testing"
)

func TestBitcoinMerkleRootOddLeafCount(t *testing.T) {
	leaves := [][]byte{
		hashLeaf("a"),
		hashLeaf("b"),
		hashLeaf("c"),
	}

	tree, err := NewBitcoinMerkleTree(leaves)
	if err != nil {
		t.Fatalf("NewBitcoinMerkleTree returned error: %v", err)
	}

	left := manualDoubleSHA256(appendPair(leaves[0], leaves[1]))
	right := manualDoubleSHA256(appendPair(leaves[2], leaves[2]))
	want := manualDoubleSHA256(appendPair(left, right))

	if got := tree.Root(); !equalBytes(got, want) {
		t.Fatalf("unexpected Merkle root\n got: %x\nwant: %x", got, want)
	}
}

func TestBitcoinMerkleProofVerifies(t *testing.T) {
	leaves := [][]byte{
		hashLeaf("leaf-0"),
		hashLeaf("leaf-1"),
		hashLeaf("leaf-2"),
		hashLeaf("leaf-3"),
		hashLeaf("leaf-4"),
	}

	tree, err := NewBitcoinMerkleTree(leaves)
	if err != nil {
		t.Fatalf("NewBitcoinMerkleTree returned error: %v", err)
	}

	index := 4
	proof, err := tree.Proof(index)
	if err != nil {
		t.Fatalf("Proof returned error: %v", err)
	}

	if !VerifyBitcoinMerkleProof(leaves[index], index, proof, tree.Root()) {
		t.Fatal("VerifyBitcoinMerkleProof returned false")
	}
}

func TestBitcoinMerkleTreeRejectsInvalidLeafSize(t *testing.T) {
	_, err := NewBitcoinMerkleTree([][]byte{{1, 2, 3}})
	if err != ErrInvalidLeafHash {
		t.Fatalf("expected ErrInvalidLeafHash, got %v", err)
	}
}

func hashLeaf(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

func manualDoubleSHA256(data []byte) []byte {
	first := sha256.Sum256(data)
	second := sha256.Sum256(first[:])
	return second[:]
}

func appendPair(left, right []byte) []byte {
	buf := make([]byte, 0, len(left)+len(right))
	buf = append(buf, left...)
	buf = append(buf, right...)
	return buf
}
