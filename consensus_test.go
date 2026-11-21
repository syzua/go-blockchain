package main

import (
	"testing"
)

// mineBelowDifficulty 造一个「哈希自洽、但不满足难度」的块。
// 用来模拟不花算力、只改字段就想骗过校验的伪造块。
func mineBelowDifficulty(b *Block, difficulty int) {
	b.mine(0)
	for b.hasValidPoW(difficulty) {
		b.Nonce++
		b.Hash = b.calcHash()
	}
}

// buildChain 造一条含 n 个区块的链（创世块 + miner 挖 n-1 块），返回链和挖矿钱包。
func buildChain(t *testing.T, n int) (*Blockchain, *Wallet) {
	t.Helper()
	bc, err := NewBlockchain(1, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bc.Close() }) // 测试结束后关库，让 TempDir 能正常清理（Windows 上 Badger 会占文件句柄）
	miner := NewWallet()
	for i := 1; i < n; i++ {
		if _, err := bc.AddBlock([]*Transaction{{To: miner.Address(), Amount: MiningReward}}); err != nil {
			t.Fatal(err)
		}
	}
	return bc, miner
}

// 更长的合法链应被采纳（reorg）。
func TestReplaceChainAdoptsLongerChain(t *testing.T) {
	long, longMiner := buildChain(t, 3)
	short, _ := buildChain(t, 2)

	replaced, err := short.ReplaceChain(long.blocks)
	if err != nil {
		t.Fatal(err)
	}
	if !replaced {
		t.Fatal("更长的链应被采纳")
	}
	if short.Len() != 3 {
		t.Fatalf("替换后链长应为 3，实际 %d", short.Len())
	}
	if short.Balances()[longMiner.Address()] != MiningReward*2 {
		t.Fatalf("长链矿工余额应为 %d，实际 %d", MiningReward*2, short.Balances()[longMiner.Address()])
	}
}

// 短的或一样长的链不应替换当前链（认最长链）。
func TestReplaceChainRejectsShorter(t *testing.T) {
	long, _ := buildChain(t, 3)
	short, _ := buildChain(t, 2)

	replaced, err := long.ReplaceChain(short.blocks)
	if err != nil {
		t.Fatal(err)
	}
	if replaced {
		t.Fatal("短的链不应替换长的链")
	}
	if long.Len() != 3 {
		t.Fatalf("长链不应变，实际 %d", long.Len())
	}
}

// 更长的但被篡改的链应被拒绝，不能污染当前链。
func TestReplaceChainRejectsInvalid(t *testing.T) {
	long, _ := buildChain(t, 3)
	short, _ := buildChain(t, 2)

	bad := append([]*Block(nil), long.blocks...)
	last := *bad[len(bad)-1] // 深拷贝末块，避免改到 long 本身
	last.Timestamp = 12345
	bad[len(bad)-1] = &last

	if _, err := short.ReplaceChain(bad); err == nil {
		t.Fatal("被篡改的候选链应被拒绝")
	}
	if short.Len() != 2 {
		t.Fatalf("拒绝后短链不应变，实际 %d", short.Len())
	}
}

// 外来块的哈希必须真的达到难度门槛：哈希自洽但没挖够的块要拒绝。
func TestAddExternalBlockRejectsInvalidPoW(t *testing.T) {
	bc, err := NewBlockchain(1, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()

	b := &Block{Index: 1, Timestamp: 1, PrevHash: bc.blocks[0].Hash}
	mineBelowDifficulty(b, 1) // 自洽，但哈希不以 0 开头
	if ok, _ := bc.AddExternalBlock(b); ok {
		t.Fatal("未达到难度门槛的区块应被拒绝")
	}
	if bc.Len() != 1 {
		t.Fatalf("拒绝后链长应仍为 1，实际 %d", bc.Len())
	}
}

// 铸币金额必须等于固定奖励，否则等于让任何人凭空印钱。
func TestAddBlockRejectsWrongCoinbaseAmount(t *testing.T) {
	bc, err := NewBlockchain(1, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()

	miner := NewWallet()
	if _, err := bc.AddBlock([]*Transaction{{To: miner.Address(), Amount: MiningReward * 20}}); err == nil {
		t.Fatal("超额铸币应被拒绝")
	}
	if _, err := bc.AddBlock([]*Transaction{{To: miner.Address(), Amount: MiningReward}}); err != nil {
		t.Fatalf("正常奖励应被接受：%v", err)
	}
}

// 外来块里的超额铸币同样要拒绝，防止绕过本节点直接广播假块。
func TestAddExternalBlockRejectsOversizedCoinbase(t *testing.T) {
	bc, err := NewBlockchain(1, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()

	miner := NewWallet()
	b := &Block{
		Index:        1,
		Timestamp:    1,
		PrevHash:     bc.blocks[0].Hash,
		Transactions: []*Transaction{{To: miner.Address(), Amount: MiningReward * 1000}},
	}
	b.mine(1) // PoW 合法，只有金额非法
	if ok, _ := bc.AddExternalBlock(b); ok {
		t.Fatal("含超额铸币的区块应被拒绝")
	}
}

// 候选链里只要有一块没达到难度门槛，整条链都不能替换当前链。
func TestReplaceChainRejectsLowPoWChain(t *testing.T) {
	bc, err := NewBlockchain(1, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()

	g := genesisBlock()
	b1 := &Block{Index: 1, Timestamp: 1, PrevHash: g.Hash}
	mineBelowDifficulty(b1, 1)
	b2 := &Block{Index: 2, Timestamp: 2, PrevHash: b1.Hash}
	mineBelowDifficulty(b2, 1)

	if _, err := bc.ReplaceChain([]*Block{g, b1, b2}); err == nil {
		t.Fatal("含低难度块的候选链应被拒绝")
	}
	if bc.Len() != 1 {
		t.Fatalf("拒绝后链长应仍为 1，实际 %d", bc.Len())
	}
}

// reorg 后余额按新链重放：孤立块里的奖励作废。
func TestReplaceChainRebuildsBalances(t *testing.T) {
	long, longMiner := buildChain(t, 3)
	short, shortMiner := buildChain(t, 2)

	if _, err := short.ReplaceChain(long.blocks); err != nil {
		t.Fatal(err)
	}
	bal := short.Balances()
	if bal[longMiner.Address()] != MiningReward*2 {
		t.Fatalf("长链矿工的两个奖励应保留，实际 %d", bal[longMiner.Address()])
	}
	if bal[shortMiner.Address()] != 0 {
		t.Fatalf("短链矿工的孤立块奖励应作废，实际 %d", bal[shortMiner.Address()])
	}
}