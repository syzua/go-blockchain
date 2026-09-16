package main

import (
	"strings"
	"testing"
)

// 创世块必须全网一致：不同链、不同文件，创世哈希也应一样。
func TestGenesisDeterministic(t *testing.T) {
	a, err := NewBlockchain(2, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := NewBlockchain(2, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if a.blocks[0].Hash != b.blocks[0].Hash {
		t.Fatalf("创世块哈希不一致: %s vs %s", a.blocks[0].Hash, b.blocks[0].Hash)
	}
}

// 挖矿出的新区块：索引递增、咬住前一块、哈希满足难度前缀、内容一致。
func TestAddBlock(t *testing.T) {
	bc, err := NewBlockchain(2, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()
	block, err := bc.AddBlock(nil)
	if err != nil {
		t.Fatal(err)
	}
	if block.Index != 1 {
		t.Fatalf("期望 index=1，得到 %d", block.Index)
	}
	if block.PrevHash != bc.blocks[0].Hash {
		t.Fatal("新区块的 prev_hash 没咬住创世块")
	}
	if !strings.HasPrefix(block.Hash, "00") {
		t.Fatalf("难度 2，哈希应以 00 开头，得到 %s", block.Hash)
	}
	if block.calcHash() != block.Hash {
		t.Fatal("重新计算哈希和存储的哈希不一致")
	}
}

// 篡改区块内容后，整条链应被判定为非法。
func TestIsValidDetectsTampering(t *testing.T) {
	bc, err := NewBlockchain(1, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()
	if _, err := bc.AddBlock(nil); err != nil {
		t.Fatal(err)
	}
	if !bc.IsValid() {
		t.Fatal("正常链应校验通过")
	}
	bc.blocks[1].Timestamp = 999 // 改内容但不重新挖矿 → 哈希对不上
	if bc.IsValid() {
		t.Fatal("被篡改的链应校验失败")
	}
}

// 接收外块：合法的接收，被篡改的 / 索引不连续的拒绝。
func TestAddExternalBlock(t *testing.T) {
	bc, err := NewBlockchain(1, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()
	// 合法下一块
	block := &Block{Index: 1, Timestamp: 123, PrevHash: bc.blocks[0].Hash}
	block.mine(1)
	if ok, err := bc.AddExternalBlock(block); err != nil || !ok {
		t.Fatalf("合法区块应被接收：ok=%v err=%v", ok, err)
	}
	// 篡改内容
	tampered := &Block{Index: 2, Timestamp: 456, PrevHash: block.Hash}
	tampered.mine(1)
	tampered.Timestamp = 999
	if ok, _ := bc.AddExternalBlock(tampered); ok {
		t.Fatal("被篡改的区块应被拒绝")
	}
	// 索引不连续
	gap := &Block{Index: 5, Timestamp: 456, PrevHash: block.Hash}
	gap.mine(1)
	if ok, _ := bc.AddExternalBlock(gap); ok {
		t.Fatal("索引不连续的区块应被拒绝")
	}
}
