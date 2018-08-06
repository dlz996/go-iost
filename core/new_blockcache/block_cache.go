package blockcache

import (
	"bytes"
	"errors"
	"sync"
	"fmt"
	"github.com/iost-official/Go-IOS-Protocol/core/block"
	"github.com/iost-official/Go-IOS-Protocol/db"
	"github.com/iost-official/Go-IOS-Protocol/log"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	blockCachedLength = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "block_cached_length",
			Help: "Length of cached block chain",
		},
	)
)

func init() {
	prometheus.MustRegister(blockCachedLength)
}

func IF(condition bool, trueRes, falseRes interface{}) interface{} {
	if condition {
		return trueRes
	}
	return falseRes
}

type CacheStatus int

const (
	Extend CacheStatus = iota
	Fork
	NotFound
	ErrorBlock
	Duplicate
)

const (
	DelSingleBlockTime uint64 = 10
)

type BCNType int

const (
	Linked BCNType = iota
	Single
)

// BlockCacheTree 缓存链分叉的树结构
type BlockCacheNode struct {
	Block                 *block.Block
	Parent                *BlockCacheNode
	Children              map[*BlockCacheNode]bool
	Type                  BCNType
	Number                uint64
	Witness               string
	ConfirmUntil          uint64
	LastWitnessListNumber uint64
	PendingWitnessList    []string
	Extension             []byte
}

func (bcn *BlockCacheNode) addChild(child *BlockCacheNode) {
	if child == nil {
		return
	}
	_, ok := bcn.Children[child]
	if ok {
		return
	}
	child.Parent = bcn
	bcn.Children[child] = true
	return
}

func (bcn *BlockCacheNode) delChild(child *BlockCacheNode) {
	if child == nil {
		return
	}
	delete(bcn.Children, child)
	child.Parent = nil
}

func NewBCN(parent *BlockCacheNode, block *block.Block, nodeType BCNType) *BlockCacheNode {
	bcn := BlockCacheNode{
		Block:    block,
		Children: make(map[*BlockCacheNode]bool),
		Parent:   parent,
		//initialize others
	}
	if parent==nil{
		bcn.Type=nodeType
	}else{
		bcn.Type=parent.Type
	}
	if parent != nil {
		parent.addChild(&bcn)
	}
	return &bcn
}

type BlockCache struct {
	LinkedTree *BlockCacheNode
	SingleTree *BlockCacheNode
	Head       *BlockCacheNode
	hash2node  *sync.Map
	Leaf       map[*BlockCacheNode]uint64
}

var (
	ErrNotFound = errors.New("not found")
	ErrBlock    = errors.New("error block")
	ErrTooOld   = errors.New("block too old")
	ErrDup      = errors.New("block duplicate")
)

func (bc *BlockCache) hmget(hash []byte) (*BlockCacheNode, bool) {
	rtnI, ok := bc.hash2node.Load(string(hash))
	if !ok {
		return nil, false
	}
	return rtnI.(*BlockCacheNode), true
}

func (bc *BlockCache) hmset(hash []byte, bcn *BlockCacheNode) {
	bc.hash2node.Store(string(hash), bcn)
}

func (bc *BlockCache) hmdel(hash []byte) {
	bc.hash2node.Delete(string(hash))
}

var StateDB *db.MVCCDB

func NewBlockCache(MVCCDB *db.MVCCDB) *BlockCache {
	StateDB = MVCCDB
	bc := BlockCache{
		LinkedTree: NewBCN(nil, nil, Linked),
		SingleTree: NewBCN(nil, nil, Single),
		hash2node:  new(sync.Map),
		Leaf:	make(map[*BlockCacheNode]uint64),
	}
	bc.Head=bc.LinkedTree
	blkchain := block.BChain
	lib := blkchain.Top()
	bc.LinkedTree.Block = lib
	if lib != nil {
		bc.hmset(lib.HeadHash(), bc.LinkedTree)
	}
	bc.Leaf[bc.LinkedTree]=bc.LinkedTree.Number
	return &bc
}

//call this when you run the block verify after Add() to ensure add single bcn to linkedTree
func (bc *BlockCache) Link(bcn *BlockCacheNode) {
	if bcn == nil {
		return
	}
	bcn.Type = Linked
	delete(bc.Leaf, bcn.Parent)
	bc.Leaf[bcn] = bcn.Number
	bc.updateLongest()
}

func (bc *BlockCache) updateLongest() {
	cur := bc.Head.Number
	newHead := bc.Head
	for key, val := range bc.Leaf {
		if val > cur {
			cur = val
			newHead = key
		}
	}
	bc.Head = newHead
}
func (bc *BlockCache) Add(blk *block.Block) (*BlockCacheNode, error) {
	var code CacheStatus
	var newNode *BlockCacheNode
	_, ok := bc.hmget(blk.HeadHash())
	if ok {
		return nil,ErrDup
	}
	parent, ok := bc.hmget(blk.Head.ParentHash)
	bcnType:=IF(ok,Linked,Single).(BCNType)
	fa:=IF(ok,parent,bc.SingleTree).(*BlockCacheNode)
	newNode = NewBCN(fa, blk, bcnType)
	if ok {
		code=IF(len(parent.Children) > 1, Fork, Extend).(CacheStatus)
	}else{
		code=NotFound
	}
	bc.hmset(blk.HeadHash(), newNode)
	switch code {
	case Extend:
		fallthrough
	case Fork:
		// Added to cached tree or added to single tree
		if newNode.Type == Linked {
			bc.addSingle(newNode)
			bc.Link(newNode)
		} else {
			bc.mergeSingle(newNode)
			return newNode, ErrNotFound
		}
	case NotFound:
		// Added as a child of single root
		bc.mergeSingle(newNode)
		return newNode, ErrNotFound
	}
	return newNode, nil
}

func (bc *BlockCache) delNode(bcn *BlockCacheNode) {
	fa := bcn.Parent
	bcn.Parent = nil
	bc.hmdel(bcn.Block.HeadHash())
	delete(bc.Leaf, bcn)
	if fa == nil {
		return
	}
	bc.Leaf[fa] = fa.Number
	fa.delChild(bcn)
	return
}
func (bc *BlockCache) Del(bcn *BlockCacheNode) {
	if bcn == nil {
		return
	}
	len := len(bcn.Children)
	for ch, _ := range bcn.Children {
		bc.Del(ch)
	}
	bc.delNode(bcn)
	if len == 0 {
		bc.updateLongest()
	}
}

func (bc *BlockCache) addSingle(newNode *BlockCacheNode) {
	bc.mergeSingle(newNode)
	//modify Type from child to end
}

func (bc *BlockCache) mergeSingle(newNode *BlockCacheNode) {
	block := newNode.Block
	var child *BlockCacheNode
	for bcn, _ := range bc.SingleTree.Children {
		if bytes.Equal(bcn.Block.Head.ParentHash, block.HeadHash()) {
			child = bcn
			break
		}
	}

	if child == nil {
		return
	}
	child.Parent.delChild(child)
	newNode.addChild(child)
}

func (bc *BlockCache) delSingle() {
	height := bc.LinkedTree.Number
	if height%DelSingleBlockTime != 0 {
		return
	}
	for bcn, _ := range bc.SingleTree.Children {
		if bcn.Number <= height {
			bc.Del(bcn)
		}
	}
	return
}

func (bc *BlockCache) flush(cur *BlockCacheNode, retain *BlockCacheNode) error {
	if cur != bc.LinkedTree {
		bc.flush(cur.Parent, cur)
	}
	for child, _ := range cur.Children {
		if child == retain {
			continue
		}
		bc.Del(child)
	}
	//confirm retain to db
	blkchain := block.BChain
	if retain.Block != nil {
		err := blkchain.Push(retain.Block)
		if err != nil {
			log.Log.E("Database error, BlockChain Push err:%v", err)
			return err
		}
		err = StateDB.Flush(string(retain.Block.HeadHash()))
		if err != nil {
			log.Log.E("MVCCDB error, State Flush err:%v", err)
			return err
		}
		//AddConfirmBlock(retain.Block)
		bc.hmdel(cur.Block.HeadHash())
		retain.Parent = nil
		bc.LinkedTree = retain

	}
	return nil
}

func (bc *BlockCache) Flush(bcn *BlockCacheNode) {
	if bcn == nil {
		return
	}
	bc.flush(bcn.Parent, bcn)
	bc.delSingle()
	return
}

func (bc *BlockCache) FindBlock(hash []byte) (*block.Block, error) {
	bcn, ok := bc.hmget(hash)
	return bcn.Block, IF(ok, nil, errors.New("block not found")).(error)
}
