package main

import (
	"testing"
)

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