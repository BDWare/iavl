package iavl

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/rand"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	db "github.com/tendermint/tm-db"
)

func TestFlushVersion(t *testing.T) {
	memDB := db.NewMemDB()
	opts := PruningOptions(5, 1)

	tree, err := NewMutableTreeWithOpts(memDB, db.NewMemDB(), 0, opts)
	require.NoError(t, err)
	require.NotNil(t, tree)

	// set key/value pairs and commit up to KeepEvery
	rootHashes := make([][]byte, 0)
	for i := int64(0); i < opts.KeepEvery; i++ {
		tree.Set([]byte(fmt.Sprintf("key-%d", i)), []byte(fmt.Sprintf("value-%d", i)))

		rh, v, err := tree.SaveVersion() // nolint: govet
		require.NoError(t, err)
		require.Equal(t, i+1, v)

		rootHashes = append(rootHashes, rh)
	}

	// verify the latest version
	require.Equal(t, int64(5), tree.Version())

	// verify we only have the 1st and KeepEvery version flushed to disk
	for i, rh := range rootHashes {
		version := int64(i + 1)

		ok, err := tree.ndb.HasSnapshot(rh) // nolint: govet
		require.NoError(t, err)

		if version == 1 || version%opts.KeepEvery == 0 {
			require.True(t, ok)
		} else {
			require.False(t, ok)
		}
	}

	// set key/value pairs and commit 2 more times (no flush to disk should occur)
	for i := opts.KeepEvery; i < opts.KeepEvery+2; i++ {
		tree.set([]byte(fmt.Sprintf("key-%d", i)), []byte(fmt.Sprintf("value-%d", i)))

		rh, v, err := tree.SaveVersion() // nolint: govet
		require.NoError(t, err)
		require.Equal(t, i+1, v)

		rootHashes = append(rootHashes, rh)
	}

	// verify the latest version
	require.Equal(t, int64(7), tree.Version())

	// verify we do not have the latest version flushed to disk
	ok, err := tree.ndb.HasSnapshot(rootHashes[len(rootHashes)-1])
	require.NoError(t, err)
	require.False(t, ok)

	// verify flushing already flushed version is fine
	require.NoError(t, tree.FlushVersion(5))

	// verify we can flush the latest version
	require.NoError(t, tree.FlushVersion(tree.Version()))

	// verify we do have the latest version flushed to disk
	ok, err = tree.ndb.HasSnapshot(rootHashes[len(rootHashes)-1])
	require.NoError(t, err)
	require.True(t, ok)

	tree2, err := NewMutableTreeWithOpts(memDB, db.NewMemDB(), 0, opts)
	require.NoError(t, err)
	require.NotNil(t, tree2)

	// verify we can load the previously manually flushed version on a new tree
	// and fetch all keys and values
	v, err := tree2.LoadVersion(tree.Version())
	require.NoError(t, err)
	require.Equal(t, tree.Version(), v)

	for i := int64(0); i < v; i++ {
		_, value := tree2.Get([]byte(fmt.Sprintf("key-%d", i)))
		assert.Equal(t, []byte(fmt.Sprintf("value-%d", i)), value)
	}

	// also verify that we can load the automatically flushed version and fetch
	// all keys and values, and that no subsequent keys are present.
	v, err = tree2.LoadVersion(5)
	require.NoError(t, err)
	require.EqualValues(t, 5, v)

	for i := int64(0); i < v+10; i++ {
		_, value := tree2.Get([]byte(fmt.Sprintf("key-%d", i)))
		if i < v {
			assert.Equal(t, []byte(fmt.Sprintf("value-%d", i)), value)
		} else {
			assert.Nil(t, value)
		}
	}
}

func TestFlushVersion_SetGet(t *testing.T) {
	memDB := db.NewMemDB()
	opts := PruningOptions(5, 1)

	tree, err := NewMutableTreeWithOpts(memDB, db.NewMemDB(), 0, opts)
	require.NoError(t, err)
	require.NotNil(t, tree)

	tree.Set([]byte("a"), []byte{1})
	tree.Set([]byte("b"), []byte{2})
	_, version, err := tree.SaveVersion()
	require.NoError(t, err)

	err = tree.FlushVersion(version)
	require.NoError(t, err)

	tree, err = NewMutableTreeWithOpts(memDB, db.NewMemDB(), 0, opts)
	require.NoError(t, err)
	_, err = tree.LoadVersion(version)
	require.NoError(t, err)

	_, value := tree.Get([]byte("a"))
	assert.Equal(t, []byte{1}, value)
	_, value = tree.Get([]byte("b"))
	assert.Equal(t, []byte{2}, value)
}

func TestFlushVersion_Missing(t *testing.T) {
	tree, err := NewMutableTreeWithOpts(db.NewMemDB(), db.NewMemDB(), 0, PruningOptions(5, 1))
	require.NoError(t, err)
	require.NotNil(t, tree)

	tree.Set([]byte("a"), []byte{1})
	tree.Set([]byte("b"), []byte{2})
	_, _, err = tree.SaveVersion()
	require.NoError(t, err)

	err = tree.FlushVersion(2)
	require.Error(t, err)
}

func TestFlushVersion_Empty(t *testing.T) {
	memDB := db.NewMemDB()
	opts := PruningOptions(5, 1)
	tree, err := NewMutableTreeWithOpts(memDB, db.NewMemDB(), 0, opts)
	require.NoError(t, err)
	require.NotNil(t, tree)

	// save a couple of versions
	_, version, err := tree.SaveVersion()
	require.NoError(t, err)
	assert.EqualValues(t, 1, version)

	_, version, err = tree.SaveVersion()
	require.NoError(t, err)
	assert.EqualValues(t, 2, version)

	// flush the latest version
	err = tree.FlushVersion(2)
	require.NoError(t, err)

	// try to load the tree in a new memDB
	tree, err = NewMutableTreeWithOpts(memDB, db.NewMemDB(), 0, opts)
	require.NoError(t, err)
	require.NotNil(t, tree)

	version, err = tree.LoadVersion(2)
	require.NoError(t, err)
	assert.EqualValues(t, 2, version)

	// loading the first version should fail
	tree, err = NewMutableTreeWithOpts(memDB, db.NewMemDB(), 0, opts)
	require.NoError(t, err)
	require.NotNil(t, tree)

	_, err = tree.LoadVersion(1)
	require.Error(t, err)
}

func TestDelete(t *testing.T) {
	memDB := db.NewMemDB()
	tree, err := NewMutableTree(memDB, 0)
	require.NoError(t, err)

	tree.set([]byte("k1"), []byte("Fred"))
	hash, version, err := tree.SaveVersion()
	require.NoError(t, err)
	_, _, err = tree.SaveVersion()
	require.NoError(t, err)

	require.NoError(t, tree.DeleteVersion(version))

	k1Value, _, _ := tree.GetVersionedWithProof([]byte("k1"), version)
	require.Nil(t, k1Value)

	key := tree.ndb.rootKey(version)
	err = memDB.Set(key, hash)
	require.NoError(t, err)
	tree.versions[version] = true

	k1Value, _, err = tree.GetVersionedWithProof([]byte("k1"), version)
	require.Nil(t, err)
	require.Equal(t, 0, bytes.Compare([]byte("Fred"), k1Value))
}

// This is a test for issue #261, where deleting the previous version while creating new versions
// which update keys caused panics. If this does not panic, then the test passes. This test case
// evolved from a debug script.
// https://github.com/tendermint/iavl/issues/261
func TestDeleteVersion_issue261(t *testing.T) {
	tree, err := NewMutableTreeWithOpts(db.NewMemDB(), db.NewMemDB(), 0, &Options{
		// These settings correspond to PruneEverything in the SDK.
		KeepEvery:  1,
		KeepRecent: 0,
	})
	require.NoError(t, err)

	for version := int64(1); version < 10; version++ {
		// For each version, reset the PRNG so we generate the same key sequence in each iteration.
		r := rand.New(rand.NewSource(49872768940))
		for i := 0; i < int(4+2*version); i++ {
			key := make([]byte, 16)
			value := make([]byte, 16)
			r.Read(key)
			r.Read(value)
			// If the Set hits an existing key, generate a new key until we find an unused one.
			for {
				if fmt.Sprintf("%x", key) == "7964949ef8e454db964cee3fc608b69a" {
					fmt.Printf("Set v%v %x = %x\n", version, key, value)
				}
				if !tree.Set(key, value) {
					break
				}
				r.Read(key)
			}
		}

		_, _, err = tree.SaveVersion()
		require.NoError(t, err)
		t.Logf("Saved version %v", version)

		k, err := hex.DecodeString("7964949ef8e454db964cee3fc608b69a")
		require.NoError(t, err)
		_, value := tree.Get(k)
		fmt.Printf("Get v%v %x = %x\n", version, k, value)

		fmt.Printf("Version %v\n", version)
		fmt.Println(tree.String())

		// Delete the previous version
		if version > 1 {
			err = tree.DeleteVersion(version - 1)
			require.NoError(t, err)
			t.Logf("Deleted version %v", version-1)
		}
	}
}

// This is a test for issue #261, where deleting the previous version while creating new versions
// which update keys caused panics. If this does not panic, then the test passes. This test case
// evolved from a debug script.
// https://github.com/tendermint/iavl/issues/261
func TestDeleteVersion_issue261_minimal(t *testing.T) {
	tree, err := NewMutableTreeWithOpts(db.NewMemDB(), db.NewMemDB(), 0, &Options{
		// These settings correspond to PruneEverything in the SDK.
		KeepEvery:  1,
		KeepRecent: 0,
	})
	require.NoError(t, err)

	tree.Set([]byte("a"), []byte{1})
	tree.Set([]byte("b"), []byte{1})
	tree.Set([]byte("c"), []byte{1})
	_, _, err = tree.SaveVersion()
	require.NoError(t, err)

	tree.Set([]byte("aa"), []byte{2})
	tree.Set([]byte("b"), []byte{2})
	tree.Set([]byte("cc"), []byte{2})
	_, _, err = tree.SaveVersion()
	require.NoError(t, err)
	err = tree.DeleteVersion(1)
	require.NoError(t, err)

	tree.Set([]byte("a"), []byte{3})
	tree.Set([]byte("bb"), []byte{3})
	tree.Set([]byte("c"), []byte{3})
	_, _, err = tree.SaveVersion()
	require.NoError(t, err)
	err = tree.DeleteVersion(2)
	require.NoError(t, err)

	tree.Set([]byte("a"), []byte{4})
	tree.Set([]byte("b"), []byte{4})
	tree.Set([]byte("c"), []byte{4})
	_, _, err = tree.SaveVersion()
	require.NoError(t, err)
	err = tree.DeleteVersion(3)
	require.NoError(t, err)
}

func TestTraverse(t *testing.T) {
	memDB := db.NewMemDB()
	tree, err := NewMutableTree(memDB, 0)
	require.NoError(t, err)

	for i := 0; i < 6; i++ {
		tree.set([]byte(fmt.Sprintf("k%d", i)), []byte(fmt.Sprintf("v%d", i)))
	}

	require.Equal(t, 11, tree.nodeSize(), "Size of tree unexpected")
}

func TestEmptyRecents(t *testing.T) {
	memDB := db.NewMemDB()
	opts := Options{
		KeepRecent: 100,
		KeepEvery:  10000,
	}

	tree, err := NewMutableTreeWithOpts(memDB, db.NewMemDB(), 0, &opts)
	require.NoError(t, err)
	hash, version, err := tree.SaveVersion()

	require.Nil(t, err)
	require.Equal(t, int64(1), version)
	require.Nil(t, hash)
	require.True(t, tree.VersionExists(int64(1)))

	_, err = tree.GetImmutable(int64(1))
	require.NoError(t, err)
}

func BenchmarkMutableTree_Set(b *testing.B) {
	db := db.NewDB("test", db.MemDBBackend, "")
	t, err := NewMutableTree(db, 100000)
	require.NoError(b, err)
	for i := 0; i < 1000000; i++ {
		t.Set(randBytes(10), []byte{})
	}
	b.ReportAllocs()
	runtime.GC()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		t.Set(randBytes(10), []byte{})
	}
}
