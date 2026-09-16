package main

import (
	"testing"
)

// 挖矿应给矿工铸币奖励，并能在账本里查到。
func TestMiningReward(t *testing.T) {
	bc, err := NewBlockchain(1, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()
	miner := NewWallet()
	if _, err := bc.AddBlock([]*Transaction{{To: miner.Address(), Amount: MiningReward}}); err != nil {
		t.Fatal(err)
	}
	if got := bc.Balances()[miner.Address()]; got != MiningReward {
		t.Fatalf("矿工余额应为 %d，实际 %d", MiningReward, got)
	}
}

// 付款方没钱时，AddBlock 必须报错拒绝，不能凭空造钱。
func TestAddBlockRejectsOverspending(t *testing.T) {
	bc, err := NewBlockchain(1, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()
	alice := NewWallet()
	bob := NewWallet()
	tx := &Transaction{From: alice.Address(), To: bob.Address(), Amount: 999}
	tx.Sign(alice.PrivateKey)
	if _, err := bc.AddBlock([]*Transaction{tx}); err == nil {
		t.Fatal("余额不足的交易应被拒绝")
	}
}

// 完整转账链路：铸币给 alice → alice 转 30 给 bob → 两边余额都对。
func TestTransferUpdatesBalances(t *testing.T) {
	bc, err := NewBlockchain(1, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()
	alice := NewWallet()
	bob := NewWallet()

	if _, err := bc.AddBlock([]*Transaction{{To: alice.Address(), Amount: 100}}); err != nil {
		t.Fatal(err)
	}
	tx := &Transaction{From: alice.Address(), To: bob.Address(), Amount: 30}
	tx.Sign(alice.PrivateKey)
	if _, err := bc.AddBlock([]*Transaction{tx}); err != nil {
		t.Fatal(err)
	}

	bal := bc.Balances()
	if bal[alice.Address()] != 70 {
		t.Fatalf("alice 余额应为 70，实际 %d", bal[alice.Address()])
	}
	if bal[bob.Address()] != 30 {
		t.Fatalf("bob 余额应为 30，实际 %d", bal[bob.Address()])
	}
}

// 同一区块里重复花同一笔钱（双花）应被拒绝。
func TestAddBlockRejectsDoubleSpend(t *testing.T) {
	bc, err := NewBlockchain(1, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()
	alice := NewWallet()
	bob := NewWallet()
	if _, err := bc.AddBlock([]*Transaction{{To: alice.Address(), Amount: 100}}); err != nil {
		t.Fatal(err)
	}
	tx1 := &Transaction{From: alice.Address(), To: bob.Address(), Amount: 100}
	tx1.Sign(alice.PrivateKey)
	tx2 := &Transaction{From: alice.Address(), To: bob.Address(), Amount: 100}
	tx2.Sign(alice.PrivateKey)
	if _, err := bc.AddBlock([]*Transaction{tx1, tx2}); err == nil {
		t.Fatal("同块内双花应被拒绝")
	}
}

// 重启后账本从落盘链重放恢复，与关前一致。
func TestBalancesRestoredFromDisk(t *testing.T) {
	dir := t.TempDir()
	bc, err := NewBlockchain(1, dir)
	if err != nil {
		t.Fatal(err)
	}
	alice := NewWallet()
	bob := NewWallet()
	if _, err := bc.AddBlock([]*Transaction{{To: alice.Address(), Amount: 100}}); err != nil {
		t.Fatal(err)
	}
	tx := &Transaction{From: alice.Address(), To: bob.Address(), Amount: 40}
	tx.Sign(alice.PrivateKey)
	if _, err := bc.AddBlock([]*Transaction{tx}); err != nil {
		t.Fatal(err)
	}

	// 模拟重启：先关掉（释放 Badger 的目录锁 + 刷盘），再用同一目录重新打开
	if err := bc.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewBlockchain(1, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	bal := reloaded.Balances()
	if bal[alice.Address()] != 60 || bal[bob.Address()] != 40 {
		t.Fatalf("恢复后账本不对：alice=%d bob=%d", bal[alice.Address()], bal[bob.Address()])
	}
}
