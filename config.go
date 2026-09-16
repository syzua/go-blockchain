package main

import (
	"flag"
	"strings"
)

// Config 一个节点的运行配置。
type Config struct {
	Demo       bool     // 是否直接跑内置的三节点演示
	Port       string   // 单节点监听端口（含冒号，如 ":8001"）
	Peers      []string // 邻居节点地址列表（单节点模式）
	Difficulty int      // 挖矿难度：哈希前导 0 的个数
	DataPath   string   // 链落盘目录（BadgerDB 数据目录，单节点模式）
}

// parseConfig 从命令行参数读配置，没传的用默认值。
//
//	用法：
//	  go run .                                                       // 三节点演示（默认）
//	  go run . -demo=false -port=:8001 -peers=localhost:8002,localhost:8003
func parseConfig() Config {
	demo := flag.Bool("demo", true, "跑内置三节点演示；设 false 则跑单个可配置节点")
	port := flag.String("port", ":8001", "单节点监听端口（含冒号）")
	peers := flag.String("peers", "", "邻居节点地址，逗号分隔，如 localhost:8002,localhost:8003")
	difficulty := flag.Int("difficulty", 4, "挖矿难度（哈希前导 0 个数）")
	data := flag.String("data", "chain.db", "链落盘目录（BadgerDB 数据目录）")
	flag.Parse()

	var peerList []string
	if *peers != "" {
		for _, p := range strings.Split(*peers, ",") {
			if p = strings.TrimSpace(p); p != "" {
				peerList = append(peerList, p)
			}
		}
	}
	return Config{
		Demo:       *demo,
		Port:       *port,
		Peers:      peerList,
		Difficulty: *difficulty,
		DataPath:   *data,
	}
}
