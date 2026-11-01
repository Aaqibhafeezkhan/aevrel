package observe

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// writeDigestCheckpoint writes one shard and a manifest that carries its real digest.
func writeDigestCheckpoint(t *testing.T, dir string, step int64, data []byte) string {
	t.Helper()
	d := filepath.Join(dir, dirName(step))
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	shard := filepath.Join(d, shardName(0))
	if err := os.WriteFile(shard, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	raw, _ := json.Marshal(map[string]any{
		"step": step,
		"shards": []map[string]any{
			{"rank": 0, "bytes": len(data), "sha256": hex.EncodeToString(sum[:])},
		},
	})
	if err := os.WriteFile(filepath.Join(d, ManifestName), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return shard
}

func TestVerifyReadsTheContentBack(t *testing.T) {
	dir := t.TempDir()
	data := bytes.Repeat([]byte{0xAB}, 1024)
	writeDigestCheckpoint(t, dir, 4, data)

	v, err := Verify(dir, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Checked || v.Detail != "" {
		t.Fatalf("verification = %+v, want checked and matching", v)
	}
	if v.Bytes != int64(len(data)) {
		t.Fatalf("bytes = %d, want %d", v.Bytes, len(data))
	}
}

func TestVerifyNamesTheShardThatChanged(t *testing.T) {
	dir := t.TempDir()
	data := bytes.Repeat([]byte{0xAB}, 1024)
	shard := writeDigestCheckpoint(t, dir, 4, data)

	broken := append([]byte(nil), data...)
	broken[10] = 0x00
	if err := os.WriteFile(shard, broken, 0o644); err != nil {
		t.Fatal(err)
	}

	v, err := Verify(dir, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Checked || v.Detail == "" {
		t.Fatalf("verification = %+v, want a mismatch naming the shard", v)
	}
}

func TestVerifySkipsAManifestWithoutADigest(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, dirName(4))
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, shardName(0)), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"step": 4, "shards": []map[string]any{{"rank": 0, "bytes": 100}}})
	if err := os.WriteFile(filepath.Join(d, ManifestName), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	v, err := Verify(dir, 4)
	if err != nil {
		t.Fatal(err)
	}
	if v.Checked {
		t.Fatal("a manifest without a digest cannot be called verified")
	}
}

// A verified copy is still a checkpoint. Forgetting that made the controller believe it had none and
// ask for a save on every round — the bug this test exists for.
func TestAVerifiedCopyCountsAsComplete(t *testing.T) {
	dir := t.TempDir()
	writeCheckpoint(t, dir, 4, 100)

	req := request(t, dir)
	req.Verified = map[int64]bool{4: true}
	s, _, err := Scan(req)
	if err != nil {
		t.Fatal(err)
	}
	if s.CheckpointBytes != 100 {
		t.Fatalf("checkpoint bytes = %d, want 100", s.CheckpointBytes)
	}
	// A verified copy is still the newest complete checkpoint: it is a copy, and the contract keeps
	// copies. (The moment it landed is a fact about the copy, not a field of the snapshot.)
	if c := copyAt(&s, 4); c == nil || c.State != policy.CopyVerified {
		t.Fatalf("step 4 = %+v, want it kept as a verified copy", c)
	}
}

// The observer's side of the same fact: a copy the verifier found wrong is broken, and one it read
// is verified.
func TestScanUsesTheVerifiersResults(t *testing.T) {
	dir := t.TempDir()
	writeCheckpoint(t, dir, 4, 100)
	writeCheckpoint(t, dir, 8, 100)

	req := request(t, dir)
	req.Verified = map[int64]bool{4: true}
	req.Mismatched = map[int64]bool{8: true}
	s, _, err := Scan(req)
	if err != nil {
		t.Fatal(err)
	}
	if c := copyAt(&s, 4); c == nil || c.State != policy.CopyVerified {
		t.Fatalf("step 4 = %+v, want verified", c)
	}
	if c := copyAt(&s, 8); c == nil || c.State != policy.CopyBroken {
		t.Fatalf("step 8 = %+v, want broken", c)
	}
}
