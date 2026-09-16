package main

import "testing"

func TestSignAndVerify(t *testing.T) {
	alice := NewWallet()
	tx := &Transaction{From: alice.Address(), To: "receiver", Amount: 10}
	tx.Sign(alice.PrivateKey)
	if !tx.Verify() {
		t.Fatal("合法签名的交易应验签通过")
	}
}

func TestVerifyRejectsTampering(t *testing.T) {
	alice := NewWallet()
	tx := &Transaction{From: alice.Address(), To: "receiver", Amount: 10}
	tx.Sign(alice.PrivateKey)
	tx.Amount = 999 // 改金额
	if tx.Verify() {
		t.Fatal("篡改金额后应验签失败")
	}
}

func TestVerifyRejectsWrongSigner(t *testing.T) {
	alice := NewWallet()
	mallory := NewWallet()
	tx := &Transaction{From: alice.Address(), To: "receiver", Amount: 10}
	tx.Sign(mallory.PrivateKey) // 用别人的私钥签，但 From 填的是 alice
	if tx.Verify() {
		t.Fatal("错误签名者应验签失败")
	}
}
