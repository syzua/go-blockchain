package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Block 一个区块。阶段1 的 Data 字段，现在升级成真正的交易列表 Transactions。
type Block struct {
	Index        int            `json:"index"`
	Timestamp    int64          `json:"timestamp"`
	Transactions []*Transaction `json:"transactions"` // 这个区块打包的交易（阶段2 核心变化）
	PrevHash     string         `json:"prev_hash"`
	Hash         string         `json:"hash"`
	Nonce        int            `json:"nonce"`
}

// calcHash 把区块关键字段拼起来算 SHA-256 指纹。
// 交易也参与哈希：任何一笔交易的任何字段被改，区块哈希就变。
func (b *Block) calcHash() string {
	record := strconv.Itoa(b.Index) + strconv.FormatInt(b.Timestamp, 10)
	for _, tx := range b.Transactions {
		record += tx.String()
	}
	record += b.PrevHash + strconv.Itoa(b.Nonce)
	sum := sha256.Sum256([]byte(record))
	return hex.EncodeToString(sum[:])
}

// mine 挖矿：穷举 Nonce，直到哈希以 difficulty 个 0 开头。
func (b *Block) mine(difficulty int) {
	target := ""
	for i := 0; i < difficulty; i++ {
		target += "0"
	}
	for {
		b.Hash = b.calcHash()
		if b.Hash[:difficulty] == target {
			return
		}
		b.Nonce++
	}
}

// Blockchain 整条链。
type Blockchain struct {
	blocks     []*Block
	difficulty int
	store      *Store         // BadgerDB 持久化（链落在 KV 数据库里，不再是 JSON 文件）
	balances   map[string]int // 地址 → 余额。由整条链重放得出，不落盘（阶段6 新增）
}

// genesisBlock 全网统一的创世块：不挖矿、时间戳固定 0，内容确定所以哈希唯一。
// 所有节点必须先从这里出发，否则会各自形成一条互不相认的链。
func genesisBlock() *Block {
	g := &Block{Index: 0, Timestamp: 0, PrevHash: ""}
	g.Hash = g.calcHash()
	return g
}

// NewBlockchain 建链：先打开数据库目录，能恢复就恢复，恢复不了（第一次）才新建创世块。
func NewBlockchain(difficulty int, dir string) (*Blockchain, error) {
	store, err := OpenStore(dir)
	if err != nil {
		return nil, err
	}
	bc := &Blockchain{difficulty: difficulty, store: store, balances: make(map[string]int)}

	blocks, err := store.LoadBlocks()
	if err == nil {
		// 数据库里已有链，校验 + 重放账本后直接恢复
		bc.blocks = blocks
		if !bc.IsValid() {
			store.Close()
			return nil, errors.New("链校验失败，可能被篡改")
		}
		if err := bc.rebuildBalances(); err != nil {
			store.Close()
			return nil, err // 账本重放失败（链里藏着余额不足的交易）
		}
		return bc, nil
	}
	if !errors.Is(err, ErrEmptyStore) {
		store.Close()
		return nil, err // 不是「空库」，是真出错了
	}

	// 第一次运行：数据库是空的，从创世块开始
	genesis := genesisBlock()
	bc.blocks = []*Block{genesis}
	if err := bc.save(); err != nil {
		store.Close()
		return nil, err // 创世块都存不下来，别带着一条存不住的链继续跑
	}
	return bc, nil
}

// save 把当前整条链写进 BadgerDB（每个区块按哈希存一条 + 更新链尾指针）。
// 每次链变化后调用，保证重启不丢。
func (bc *Blockchain) save() error {
	return bc.store.SaveBlocks(bc.blocks)
}

// Close 关闭底层数据库（节点退出时调用，让 Badger 正常刷盘释放锁）。
func (bc *Blockchain) Close() error {
	if bc.store != nil {
		return bc.store.Close()
	}
	return nil
}

// AddBlock 打包一批交易、挖矿、挂到链尾，返回挖出的新区块和落盘结果。
// 打包前会在「试算账本」上校验每一笔：要么是铸币，要么付款方余额足够。
func (bc *Blockchain) AddBlock(txs []*Transaction) (*Block, error) {
	snapshot := bc.cloneBalances()
	for _, tx := range txs {
		if err := bc.applyTx(snapshot, tx); err != nil {
			return nil, err
		}
	}
	prev := bc.blocks[len(bc.blocks)-1]
	b := &Block{
		Index:        prev.Index + 1,
		Timestamp:    time.Now().Unix(),
		Transactions: txs,
		PrevHash:     prev.Hash,
	}
	b.mine(bc.difficulty)
	bc.blocks = append(bc.blocks, b)
	bc.balances = snapshot // 全部通过，一次性提交账本
	return b, bc.save()    // 返回块 + 落盘结果（nil 表示成功）
}

// AddExternalBlock 接收别的节点广播来的区块，校验后挂到自己链尾。
// 多个节点靠它保持同一份账本。第二个返回值是落盘错误。
func (bc *Blockchain) AddExternalBlock(b *Block) (bool, error) {
	last := bc.blocks[len(bc.blocks)-1]
	if b.Index != last.Index+1 || b.PrevHash != last.Hash {
		return false, nil // 不是紧接在当前链尾之后，或前驱对不上
	}
	if b.calcHash() != b.Hash {
		return false, nil // 重新算哈希对不上（区块被篡改 / 挖矿无效）
	}
	// 在试算账本上逐笔校验：验签 + 余额，防止坏人广播「凭空转账」的块
	snapshot := bc.cloneBalances()
	for _, tx := range b.Transactions {
		if err := bc.applyTx(snapshot, tx); err != nil {
			return false, nil
		}
	}
	bc.blocks = append(bc.blocks, b)
	bc.balances = snapshot
	return true, bc.save() // 收到外来块，立刻落盘
}

// cloneBalances 复制一份余额账本，供「先试算再提交」用，避免半路出错污染真实账本。
func (bc *Blockchain) cloneBalances() map[string]int {
	m := make(map[string]int, len(bc.balances))
	for k, v := range bc.balances {
		m[k] = v
	}
	return m
}

// applyTx 在给定账本快照上试算一笔交易：验签、查余额、改余额。
// 成功返回 nil；失败返回错误且快照保持原样。铸币交易跳过验签和扣款。
func (bc *Blockchain) applyTx(snapshot map[string]int, tx *Transaction) error {
	if tx.Amount <= 0 {
		return errors.New("金额必须为正")
	}
	if !tx.IsCoinbase() {
		if !tx.Verify() {
			return errors.New("交易签名无效")
		}
		if snapshot[tx.From] < tx.Amount {
			return fmt.Errorf("付款方 %s 余额不足", tx.From)
		}
		snapshot[tx.From] -= tx.Amount
	}
	snapshot[tx.To] += tx.Amount
	return nil
}

// Balances 返回当前余额账本的一份拷贝（外部只读，改不影响内部数据）。
func (bc *Blockchain) Balances() map[string]int {
	return bc.cloneBalances()
}

// Len 当前链的区块数。
func (bc *Blockchain) Len() int {
	return len(bc.blocks)
}

// replayBalances 对给定链从创世重放，算出余额账本；含非法交易（无效签名/余额不足）则报错。
func replayBalances(blocks []*Block) (map[string]int, error) {
	m := make(map[string]int)
	for _, b := range blocks {
		for _, tx := range b.Transactions {
			if !tx.IsCoinbase() {
				if !tx.Verify() {
					return nil, fmt.Errorf("区块 %d 含无效签名交易", b.Index)
				}
				if m[tx.From] < tx.Amount {
					return nil, fmt.Errorf("区块 %d 含余额不足交易", b.Index)
				}
				m[tx.From] -= tx.Amount
			}
			m[tx.To] += tx.Amount
		}
	}
	return m, nil
}

// rebuildBalances 用当前链重放并刷新余额账本（恢复磁盘链或换链后调用）。
func (bc *Blockchain) rebuildBalances() error {
	m, err := replayBalances(bc.blocks)
	if err != nil {
		return err
	}
	bc.balances = m
	return nil
}

// validChain 校验一整条链的结构：创世一致、哈希逐块咬合、非铸币交易全部验签通过。
func validChain(blocks []*Block) bool {
	if len(blocks) == 0 || blocks[0].Hash != genesisBlock().Hash {
		return false // 空链，或创世块不是全网统一那个
	}
	for i := 1; i < len(blocks); i++ {
		cur, prev := blocks[i], blocks[i-1]
		if cur.PrevHash != prev.Hash {
			return false // 防线1：前后块哈希断了链
		}
		if cur.calcHash() != cur.Hash {
			return false // 防线2：块内容被改，重算哈希对不上
		}
		for _, tx := range cur.Transactions {
			if tx.IsCoinbase() {
				continue // 铸币交易没有签名，跳过验签
			}
			if !tx.Verify() {
				return false // 防线3（新增）：有交易签名无效
			}
		}
	}
	return true
}

// IsValid 校验当前链。
func (bc *Blockchain) IsValid() bool {
	return validChain(bc.blocks)
}

// ReplaceChain 共识核心：用一条「更长且合法」的链替换当前链（reorg）。
// 候选链必须严格更长（认最长链）、创世一致、哈希链咬合、交易与余额都能重放，才会替换。
func (bc *Blockchain) ReplaceChain(newBlocks []*Block) (bool, error) {
	if len(newBlocks) <= len(bc.blocks) {
		return false, nil // 不比当前长，不换
	}
	if !validChain(newBlocks) {
		return false, errors.New("候选链结构非法，拒绝替换")
	}
	bal, err := replayBalances(newBlocks)
	if err != nil {
		return false, err // 候选链里藏着非法交易，拒绝替换
	}
	bc.blocks = append([]*Block(nil), newBlocks...) // 拷贝一份，避免和来源共享切片
	bc.balances = bal
	return true, bc.save()
}
