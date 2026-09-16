package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"strconv"
)

// MiningReward 挖出一个区块，矿工得到的奖励——也是全系统「货币发行」的唯一来源。
const MiningReward = 50

// Transaction 一笔转账交易。
type Transaction struct {
	From      string `json:"from"`      // 付款方地址（其实存的就是付款方公钥的 hex）
	To        string `json:"to"`        // 收款方地址
	Amount    int    `json:"amount"`    // 转账金额
	Signature string `json:"signature"` // 数字签名：对「From+To+Amount」的签名结果
}

// payload 拼出交易「实质内容」的字符串。签名和验签都针对这一段内容。
func (t *Transaction) payload() string {
	return t.From + t.To + strconv.Itoa(t.Amount)
}

// Sign 付款方用自己的【私钥】对交易内容签名，结果存进 t.Signature。
func (t *Transaction) Sign(priv ed25519.PrivateKey) {
	sig := ed25519.Sign(priv, []byte(t.payload()))
	t.Signature = hex.EncodeToString(sig)
}

// Verify 验签：从 From 字段里解出付款方【公钥】，验证 Signature 是不是它签的。
// 任何人（不需要私钥）都能调它来检验这笔交易的真假。
func (t *Transaction) Verify() bool {
	pub, err := hex.DecodeString(t.From)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	sig, err := hex.DecodeString(t.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(pub, []byte(t.payload()), sig)
}

// IsCoinbase 判断是不是「铸币」交易：From 为空就是区块奖励，不由任何人签名、不扣任何人余额。
func (t *Transaction) IsCoinbase() bool {
	return t.From == ""
}

// String 给区块算哈希时用：把交易的完整内容拼成一段可哈希的字串。
func (t *Transaction) String() string {
	return t.payload() + t.Signature
}
