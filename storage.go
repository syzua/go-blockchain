package main

import (
	"encoding/json"
	"errors"

	badger "github.com/dgraph-io/badger/v4"
)

// 存储层的 key 设计（KV 数据库里就这么「放」一条链）：
//
//	"last"          → 链尾指针，存最新区块的哈希（像链的「表头」）
//	"block:<hash>"  → 某个区块的 JSON（每个区块按哈希单独存一条）
//
// 这样落盘的东西就和内存里的关系型切片不一样了——是真正的键值对。
var (
	keyLast     = []byte("last")
	prefixBlock = []byte("block:")
)

// ErrEmptyStore 数据库里还没有链（第一次运行）。
var ErrEmptyStore = errors.New("数据库里还没有链")

// Store 用 BadgerDB 做链的持久化。BadgerDB 是 Go 写的嵌入式 KV 数据库
// （类似 LevelDB 但纯 Go、不用 Cgo），数据存在一个目录里的多个文件中。
type Store struct {
	db *badger.DB
}

// OpenStore 打开（不存在则创建）一个 BadgerDB 数据目录。
func OpenStore(dir string) (*Store, error) {
	opts := badger.DefaultOptions(dir)
	opts.Logger = nil // 关掉 Badger 自带的日志，换我们自己用 log 打
	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close 关闭数据库（优雅退出时调用，让 Badger 把内存里的 WAL 刷盘）。
func (s *Store) Close() error {
	return s.db.Close()
}

// SaveBlocks 把整条链写进 BadgerDB：每个区块按哈希写一条 + 更新链尾指针。
// 用一次写事务（Update）保证「要么全写、要么全不写」，不会写一半。
func (s *Store) SaveBlocks(blocks []*Block) error {
	if len(blocks) == 0 {
		return errors.New("不能保存空链")
	}
	return s.db.Update(func(txn *badger.Txn) error {
		for _, b := range blocks {
			data, err := json.Marshal(b)
			if err != nil {
				return err
			}
			if err := txn.Set(blockKey(b.Hash), data); err != nil {
				return err
			}
		}
		// 最后把头指针指向最新的块，重启时从这里往回找
		return txn.Set(keyLast, []byte(blocks[len(blocks)-1].Hash))
	})
}

// LoadBlocks 从链尾指针出发，按 PrevHash 一路回溯到创世块，还原整条链。
// 回溯得到的是「从尾到头」，最后反转为「从创世到链尾」再返回。
func (s *Store) LoadBlocks() ([]*Block, error) {
	var reversed []*Block
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(keyLast)
		if errors.Is(err, badger.ErrKeyNotFound) {
			return ErrEmptyStore // 第一次运行，还没链
		}
		if err != nil {
			return err
		}
		tip, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}
		cur := string(tip)
		for cur != "" {
			item, err := txn.Get(blockKey(cur))
			if err != nil {
				return err // 头指针指向的块找不到 → 数据损坏
			}
			data, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			var b Block
			if err := json.Unmarshal(data, &b); err != nil {
				return err
			}
			reversed = append(reversed, &b)
			cur = b.PrevHash // 上一块哈希；创世块的 PrevHash 为空，循环结束
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	blocks := make([]*Block, len(reversed))
	for i := range reversed {
		blocks[i] = reversed[len(reversed)-1-i]
	}
	return blocks, nil
}

// blockKey 拼出某个区块的存储键："block:" + 区块哈希。
func blockKey(hash string) []byte {
	return append(append([]byte(nil), prefixBlock...), hash...)
}