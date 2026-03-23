package storage

import (
	"fmt"
	"time"

	badger "github.com/dgraph-io/badger/v4"
)

// DB: BadgerDB 래퍼.
type DB struct {
	bdb *badger.DB
}

// Options: DB 열기 옵션.
type Options struct {
	Path     string
	InMemory bool
}

// Open: DB 열기.
func Open(opts Options) (*DB, error) {
	bo := badger.DefaultOptions(opts.Path).
		WithLogger(nil)
	if opts.InMemory {
		bo = bo.WithInMemory(true)
	}
	bdb, err := badger.Open(bo)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	return &DB{bdb: bdb}, nil
}

// Close: DB 닫기.
func (db *DB) Close() error { return db.bdb.Close() }

// Get: 키 조회.
func (db *DB) Get(key []byte) ([]byte, error) {
	var val []byte
	err := db.bdb.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		val, err = item.ValueCopy(nil)
		return err
	})
	if err == badger.ErrKeyNotFound {
		return nil, nil
	}
	return val, err
}

// Set: 키-값 저장.
func (db *DB) Set(key, val []byte) error {
	return db.bdb.Update(func(txn *badger.Txn) error {
		return txn.Set(key, val)
	})
}

// SetWithTTL: TTL이 있는 키-값 저장.
func (db *DB) SetWithTTL(key, val []byte, ttl time.Duration) error {
	return db.bdb.Update(func(txn *badger.Txn) error {
		e := badger.NewEntry(key, val).WithTTL(ttl)
		return txn.SetEntry(e)
	})
}

// AtomicWrite: 단일 트랜잭션으로 여러 쓰기 작업 수행.
func (db *DB) AtomicWrite(fn func(txn *badger.Txn) error) error {
	return db.bdb.Update(fn)
}

// Delete: 키 삭제.
func (db *DB) Delete(key []byte) error {
	return db.bdb.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
}

// SetBatch: 여러 키-값 원자적 저장.
func (db *DB) SetBatch(pairs map[string][]byte) error {
	return db.bdb.Update(func(txn *badger.Txn) error {
		for k, v := range pairs {
			if err := txn.Set([]byte(k), v); err != nil {
				return err
			}
		}
		return nil
	})
}

// HasPrefix: 접두사로 키 존재 여부 확인.
func (db *DB) HasPrefix(prefix []byte) bool {
	found := false
	_ = db.bdb.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		it.Seek(prefix)
		if it.ValidForPrefix(prefix) {
			found = true
		}
		return nil
	})
	return found
}

// ScanPrefix: 접두사로 모든 키-값 스캔.
func (db *DB) ScanPrefix(prefix []byte) ([][]byte, error) {
	var vals [][]byte
	err := db.bdb.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			v, err := it.Item().ValueCopy(nil)
			if err != nil {
				return err
			}
			vals = append(vals, v)
		}
		return nil
	})
	return vals, err
}
